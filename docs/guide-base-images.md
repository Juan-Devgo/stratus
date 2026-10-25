# Guide: building the base images

Stratus clones every VM from a prebuilt VirtualBox disk. The team builds these disks by hand, once.
There are two:

| Image | Environment variable | Used by |
|---|---|---|
| Server base image | `APP_BASE_DISK` | `create-vm.sh` (modules 001, 002, 003) and the servers of a load balancer |
| Load balancer base image | `APP_HAPROXY_BASE_DISK` | `create-lb.sh` (module 004) |

This guide is derived from the constitution, specs 001-004 and the scripts in `scripts/`. It was not
yet run against a real VirtualBox (see the pending real-host demos in each `progress.md`), so the
verification commands in every step are there to confirm each point on your machine.
Environment variables are explained in [guide-environment.md](guide-environment.md); read its
section 3 first, because the root password you set here must match the password file.

## 1. What both images must satisfy

| Requirement | Why | Source |
|---|---|---|
| Debian 13, 64-bit | `--ostype Debian_64` | `create-vm.sh` |
| BIOS firmware (not EFI) | the default of VirtualBox | `create-vm.sh` header |
| SATA (AHCI) controller, disk on port 0 | clones attach their differencing disk to `SATA` port 0 | `create-vm.sh` section 4 |
| Exactly one NIC in the template | clones define their own adapters | `create-vm.sh` header |
| Guest Additions running at boot | the bootstrap and the IP lookup use them | 001 REQ-30 |
| `openssh-server` | the backend connects by SSH | 001 REQ-24 |
| `sudo` | module 002 writes `/etc/sudoers.d/stratus-<user>` and lists sudo rights | `create-user.sh`, `list-users.sh` |
| Root password = content of `APP_GUEST_PASSWORD_FILE` | the bootstrap logs in as root through Guest Additions | 001 REQ-22 |
| No regular users (UID 1000+) | module 002 would list them and `jump_sync` would copy their keys | 002 REQ-1 |
| No keys in `/root/.ssh/authorized_keys` | every clone would inherit them | 001 REQ-30 |
| The disk is not attached to any VM when the app first uses it | `create-vm.sh` marks it `multiattach` and fails with exit 4 otherwise | `create-vm.sh` section 2.3 |

The bootstrap that runs on every new VM already regenerates `/etc/machine-id`, the SSH host keys and
the DHCP identity, sets the hostname and writes `/etc/ssh/sshd_config.d/10-stratus.conf`
(`PasswordAuthentication no`, `PermitRootLogin prohibit-password`). Do not do any of that in the image.

## 2. Build the common part (once)

Both images share steps 2.1 to 2.7. Build them in one template VM, then clone it (section 3) before
adding anything specific.

### 2.1 Create the template VM in VirtualBox

| Setting | Value |
|---|---|
| Type / version | Linux / Debian (64-bit) |
| Firmware | BIOS: leave "Enable EFI" unchecked |
| Disk | VDI, 10-20 GB, dynamically allocated. Every clone has this capacity and 001 cannot resize it. |
| Storage controller | SATA (AHCI), the default, disk on port 0 |
| Network | **One** NIC, bridged to your LAN interface (you need internet to install packages) |

### 2.2 Install Debian 13 (netinst, text mode)

1. Choose "Install" (text mode).
2. Set the **root password**. It must be exactly the content of your `APP_GUEST_PASSWORD_FILE`
   (see [guide-environment.md](guide-environment.md)).
3. Do not create a regular user. If the installer forces one, delete it in step 2.6.
4. In "Software selection" keep only **SSH server** and **standard system utilities**. Without a
   desktop environment Debian configures networking with ifupdown and not NetworkManager.
5. Install GRUB on the main disk.

### 2.3 Update

```bash
apt update && apt -y full-upgrade
```

### 2.4 Check the network stack (ifupdown)

`create-lb.sh` writes `/etc/network/interfaces.d/stratus-internal` and runs `ifup`, so ifupdown must
own the interfaces.

```bash
cat /etc/network/interfaces
```

It must contain `source /etc/network/interfaces.d/*` and a DHCP stanza for the first NIC, usually:

```
allow-hotplug enp0s3
iface enp0s3 inet dhcp
```

If the `source` line is missing, add it. Make sure no other tool manages the same interfaces:

```bash
systemctl is-enabled NetworkManager systemd-networkd 2>&1
dpkg -l | grep -E 'network-manager|netplan'
```

Expected: `disabled` or `not-found`, and no `dpkg` output. Otherwise:

```bash
apt purge -y network-manager netplan.io
systemctl disable --now systemd-networkd
```

Check that `ifup` and a DHCP client exist:

```bash
ifup --version
dpkg -l | grep -E 'dhcpcd|isc-dhcp-client'
```

If no DHCP client is installed, `apt install -y dhcpcd-base`. The bootstrap deletes
`/var/lib/dhcp/*.leases`, `/var/lib/dhcpcd/duid` and `/var/lib/dhcpcd/*.lease*`; if your client keeps
its identity elsewhere, adjust the image so clones do not share it.

### 2.5 Install the common packages

```bash
apt install -y sudo ifupdown openssh-server \
  dkms build-essential linux-headers-amd64 bzip2
```

`dkms` and the headers let the Guest Additions modules rebuild after a kernel update. Do not install
`curl`, `htop`, `git`, `nginx`, `apache2` or `ufw`: they belong to the catalog the app installs.

### 2.6 Remove any regular user

```bash
getent passwd | awk -F: '$3>=1000 && $3<60000'
userdel -r <user>        # for each line printed
```

### 2.7 Install Guest Additions

Debian 13 does not ship `virtualbox-guest-utils` in main, so use the ISO that matches your host's
VirtualBox version:

1. VirtualBox menu: Devices, "Insert Guest Additions CD image".
2. In the VM:

```bash
mount /dev/cdrom /mnt
sh /mnt/VBoxLinuxAdditions.run
umount /mnt
reboot
```

After the reboot:

```bash
systemctl is-active vboxadd-service     # active
```

### 2.8 Check SSH

```bash
grep -n '^Include' /etc/ssh/sshd_config
sshd -T | grep -Ei 'usepam|allowtcpforwarding|allowusers|allowgroups'
```

Expected: `Include /etc/ssh/sshd_config.d/*.conf` near the top, `usepam yes`,
`allowtcpforwarding yes`, and no `allowusers`/`allowgroups` lines. These are Debian's defaults. Do not
set `PasswordAuthentication` or `PermitRootLogin` before the `Include`: `10-stratus.conf` must win.
`UsePAM yes` is what lets the locked `jump` account authenticate with a key.

### 2.9 Shut down and clone

```bash
poweroff
```

In VirtualBox make a **full clone** of the template (clone every machine state off, new MAC
addresses). You will turn the clone into the server image and keep the original for the load balancer
image, or the other way around.

## 3. Server base image (`APP_BASE_DISK`)

On the clone that will become the server image:

### 3.1 DHCP on the second NIC

When a VM is assigned to a load balancer, `assign-server.sh` sets adapter 1 to the internal network and
adds adapter 2 as NAT for outbound internet. **No script configures that second interface inside
the guest**, so the image has to bring it up by itself, or `apt-get` on an assigned server (for
example installing `nginx`, 004 AC-20) fails.

Add the stanza (the NAT NIC is usually `enp0s8`; check with `ip link` on an assigned server):

```bash
cat > /etc/network/interfaces.d/second-nic <<'EOF'
allow-hotplug enp0s8
iface enp0s8 inet dhcp
EOF
```

`allow-hotplug` is harmless while the NIC does not exist. VirtualBox's internal DHCP server does not
hand out a gateway, so the default route comes from the NAT NIC. Confirm this in the real-host demo
(`ip route`, then `apt update`).

> Do **not** add this file to the load balancer image: `create-lb.sh` configures its second NIC with a
> static address and a duplicate stanza would conflict.

### 3.2 Clean and finish

Go to section 5.

## 4. Load balancer base image (`APP_HAPROXY_BASE_DISK`)

On the other clone:

### 4.1 Install HAProxy and enable it at boot

```bash
apt install -y haproxy
systemctl enable haproxy
haproxy -c -f /etc/haproxy/haproxy.cfg     # Configuration file is valid
```

The app rewrites `haproxy.cfg` completely on every change, so the stock file only has to be valid.
The generated file uses `user haproxy`/`group haproxy` and a stats socket on `127.0.0.1:9999`
(loopback only); do not expose it.

### 4.2 Things the image must not contain

`create-lb.sh` creates these, so they must not exist yet:

```bash
getent passwd jump       # nothing
ls /var/lib/jump         # No such file or directory
```

The same applies to `/etc/network/interfaces.d/stratus-internal` and
`/etc/ssh/sshd_config.d/20-stratus-jump.conf`.

### 4.3 Finish

Go to section 5.

## 5. Clean up, detach and register the disk

On each image, before the last shutdown:

```bash
getent passwd jump                                   # LB image: empty
getent passwd | awk -F: '$3>=1000 && $3<60000'       # empty
cat /root/.ssh/authorized_keys 2>/dev/null           # empty or missing
apt clean
rm -f /root/.bash_history /var/lib/dhcp/*.leases
journalctl --rotate && journalctl --vacuum-time=1s
history -c
poweroff
```

Then on the host:

1. In VirtualBox: right-click the template VM, Remove, **Remove Only** (keep the `.vdi`).
2. Keep a copy of each `.vdi`. After `create-vm.sh` marks a disk `multiattach` it becomes the
   immutable parent of every clone's differencing disk.
3. Put the absolute paths in `stratus-srv/.env`:

```bash
APP_BASE_DISK=/abs/path/debian-13-base.vdi
APP_HAPROXY_BASE_DISK=/abs/path/debian-13-haproxy-base.vdi
```

The two disks must be different files, and the root password must be the same in both
(004 REQ-2).

## 6. Quick verification

With the backend running (see [guide-environment.md](guide-environment.md)):

1. Create a VM from the UI (module "Preparar sistema"). The operation succeeds only if Guest
   Additions answered, the bootstrap ran, the VM got an IPv4 and the backend key opened an SSH
   session.
2. Inspect it:

```bash
ssh -i "$APP_SSH_KEY_PATH" root@<vm-ip> 'hostname; cat /etc/ssh/sshd_config.d/10-stratus.conf; cat /etc/machine-id'
ssh root@<vm-ip>        # password login must be refused
```

3. Create a load balancer (module "Backends HAProxy") and check over SSH that
   `systemctl is-active haproxy` is `active`, `ip -4 a` shows `10.10.N.1/24`, and `getent passwd jump`
   has a UID below 1000.
4. Assign the VM to the load balancer, then on the server run `ip route` and `apt update`.

## 7. Troubleshooting `create-vm.sh` / `create-lb.sh`

| Exit code | Message | Likely cause |
|---|---|---|
| 2 | invalid arguments or environment | `APP_GUEST_PASSWORD_FILE` unreadable, or `<APP_SSH_KEY_PATH>.pub` missing |
| 4 | "No se pudo marcar el disco base como multiattach..." | the disk is still attached to the template VM; use "Remove Only" |
| 5 | "Guest Additions ... no respondió" | Guest Additions not installed or not running (step 2.7) |
| 5 | "No se pudo ejecutar un comando como root ... verifique la contraseña" | image root password differs from `APP_GUEST_PASSWORD_FILE` |
| 5 | "La VM no obtuvo una dirección IP por DHCP" | the bridged interface does not hand out addresses (try a wired interface) |
| 5 | "La llave del backend no abrió una sesión SSH" | `openssh-server` missing or `AllowUsers` set |
| 6 | "Disco base no encontrado" / "Interfaz de puente no encontrada" | wrong `APP_BASE_DISK` / `APP_HAPROXY_BASE_DISK` or `APP_BRIDGE_ADAPTER` |
| 13 | "Red interna ya registrada en VirtualBox" | leftover `stratus-<lb>` DHCP server; `VBoxManage dhcpserver remove --network=stratus-<lb>` |

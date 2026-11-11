# Guide: environment setup

How to configure the host that runs Stratus: tools, environment variables (following
`stratus-srv/.env.example`), the backend SSH key and the root password file.

The backend runs natively on the host, because it needs `VBoxManage`. Only the UI runs in Docker.

## 1. Prerequisites on the host

| Tool | Used for |
|---|---|
| VirtualBox with `VBoxManage` in `PATH` | creating and controlling VMs |
| `ssh` and `ssh-keygen` (OpenSSH client) | every operation on a VM |
| `bash` | the scripts in `scripts/` |
| Go 1.24+ | `stratus-srv` |
| Docker and docker compose | the UI (`stratus-ui`) |
| pnpm | UI development and tests |
| `bats` | script tests (`bats -r scripts/`) |
| `golangci-lint` | backend lint |
| `arping` (iputils) with `cap_net_raw`, and `ping` | `create-dns.sh` checks that `APP_DNS_IP` is free (005 REQ-15) |

The server and load balancer base disks must exist before you can create VMs; the Apache, Nginx and
DNS base disks only before creating that kind of server. See
[guide-base-images.md](guide-base-images.md).

`arping` sends a raw ARP probe, so the backend user needs `cap_net_raw` on it (the backend does not
run as root). Debian/Ubuntu package `iputils-arping`, Arch `iputils`:

```bash
sudo setcap cap_net_raw+ep "$(command -v arping)"
getcap "$(command -v arping)"             # .../arping cap_net_raw=ep
arping -c 1 -w 1 -I "$APP_BRIDGE_ADAPTER" 192.168.1.1; echo $?    # 0 (reply) or 1 (silence), never 2
```

Without `arping`, or when it cannot send the probe, creating the DNS server fails with exit 4 and no VM
is registered. Other modules do not need it. The two messages in the history stderr are:

| stderr | Cause | Fix |
|---|---|---|
| `arping no está instalado en el host` | no `arping` in `PATH` | install the package above |
| `arping no pudo enviar la sonda ARP (código 2); revise cap_net_raw` | `arping` exits 2 (`arping: socket: Operation not permitted`): it has no `cap_net_raw` | run the `setcap` above |

Neither message is about the value of `APP_DNS_IP`: the probe fails the same way for any address.

The capability is stored on the binary file, so **a package upgrade that replaces `arping` removes it**
and the error comes back. Run the `setcap` again after upgrading. On Arch, `iputils` does not set it,
and a pacman hook can restore it after every upgrade:

```ini
# /etc/pacman.d/hooks/arping-cap-net-raw.hook
[Trigger]
Operation = Install
Operation = Upgrade
Type = Package
Target = iputils

[Action]
Description = Restaurando cap_net_raw en arping...
When = PostTransaction
Exec = /usr/bin/setcap cap_net_raw+ep /usr/bin/arping
```

## 2. Create `.env`

```bash
cd stratus-srv
cp .env.example .env        # .env is ignored by git
```

The backend reads the process environment, so load the file before starting it:

```bash
set -a; source .env; set +a; go run ./cmd/server
```

| Variable | Required | Default | Meaning |
|---|---|---|---|
| `APP_LISTEN_ADDR` | no | `127.0.0.1:8080` | HTTP address. Keep `127.0.0.1`: the UI's nginx (host network) proxies to this port. |
| `APP_DB_PATH` | no | `./stratus.db` | SQLite history, created if missing. |
| `APP_SCRIPTS_DIR` | no | `../scripts` | Root of the bash scripts. |
| `APP_BASE_DISK` | yes | none | Absolute path of the server base disk. |
| `APP_HAPROXY_BASE_DISK` | yes | none | Absolute path of the load balancer base disk. If missing or relative the backend does not start. |
| `APP_BRIDGE_ADAPTER` | yes | none | Host interface all VMs bridge to. |
| `APP_GUEST_PASSWORD_FILE` | yes | none | File (mode 600) with the root password of the base images. |
| `APP_SSH_KEY_PATH` | yes | none | The backend's private ed25519 key. |
| `APP_APACHE_BASE_DISK` | no | none | Absolute path of the Apache base disk (005). |
| `APP_NGINX_BASE_DISK` | no | none | Absolute path of the Nginx base disk (005). |
| `APP_DNS_BASE_DISK` | no | none | Absolute path of the DNS base disk (005). |
| `APP_DNS_IP` | no | none | Static IPv4 of the DNS server in CIDR, prefix 8-30 (for example `192.168.1.53/24`). |
| `APP_DNS_GATEWAY` | no | none | IPv4 gateway of the DNS server, inside the subnet of `APP_DNS_IP`. |
| `APP_DNS_DOMAIN` | no | `stratus.lan` | Zone the DNS server is authoritative for; records are `<vm>.<zone>`. |
| `APP_DNS_FORWARDERS` | no | none | 1 to 3 comma-separated IPv4 resolvers bind9 forwards other names to. |

If you have a `stratus.db` from before 2026-10-22, delete it: the schema changed and there is no
migration.

### 2.1 `APP_BRIDGE_ADAPTER`

It must be one of the names `VBoxManage list bridgedifs` prints on a `Name:` line:

```bash
VBoxManage list bridgedifs | grep '^Name:'
```

Prefer a wired interface. On Wi-Fi many access points do not give each VM its own DHCP lease.

### 2.2 `APP_BASE_DISK` and `APP_HAPROXY_BASE_DISK`

Absolute paths to the `.vdi` files built in [guide-base-images.md](guide-base-images.md). The app
never modifies or deletes them, and the two must be different files.

### 2.3 Web server variables (005)

`APP_APACHE_BASE_DISK` and `APP_NGINX_BASE_DISK` are checked when a web server is created, not at
startup. A missing file gives "Disco base de Apache no encontrado" or "Disco base de Nginx no
encontrado". The site name of every web server is `<vm>.<APP_DNS_DOMAIN>`, even without a DNS server.

### 2.4 DNS variables (005)

`APP_DNS_BASE_DISK`, `APP_DNS_IP`, `APP_DNS_GATEWAY`, `APP_DNS_DOMAIN` and `APP_DNS_FORWARDERS` are
checked when the DNS server is created and when records change. Any missing or invalid value gives
"Configuración de DNS incompleta" and no VM is registered (005 REQ-15, AC-24):

| Variable | Rule | Rejected example |
|---|---|---|
| `APP_DNS_IP` | IPv4 with a prefix from 8 to 30 | `192.168.1.53` (no prefix) |
| `APP_DNS_GATEWAY` | IPv4 inside that subnet, not its network or broadcast address | `10.0.0.1` with `192.168.1.53/24` |
| `APP_DNS_DOMAIN` | 1 to 4 labels `^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$` joined by dots | `Stratus_Lan` |
| `APP_DNS_FORWARDERS` | 1 to 3 IPv4 separated by commas | `1.1.1` |

Choose `APP_DNS_IP` **outside the DHCP pool** of the LAN router, on the network of
`APP_BRIDGE_ADAPTER`. Before creating the VM, `create-dns.sh` sends one ARP probe and one ping to that
address and stops with "La IP del servidor DNS ya está en uso" if anything answers (AC-25).

The backend connects to the DNS server over SSH at `APP_DNS_IP` and checks it with `dig`, so the host
must reach that address directly: its own IPv4 on `APP_BRIDGE_ADAPTER` must be inside the subnet of
`APP_DNS_IP`. Otherwise creation fails with "El servidor DNS <dns> no responde por SSH en <ip>" (exit 5)
and the VM is deleted.

Records hold only addresses inside the subnet of `APP_DNS_IP` (005 REQ-22). A VM whose address is
internal (`10.10.N.x`) or NAT (`10.0.2.x`) gets no record, and the servers of a load balancer are never
registered. For that reason `APP_DNS_IP` must stay outside `10.10.0.0/16`, the range of the load
balancer networks. After upgrading, one click on "Actualizar registros" removes the records stored
with internal addresses (REQ-27, AC-58).

```bash
APP_DNS_BASE_DISK=/abs/path/debian-13-dns-base.vdi
APP_DNS_IP=192.168.1.53/24
APP_DNS_GATEWAY=192.168.1.1
APP_DNS_DOMAIN=stratus.lan
APP_DNS_FORWARDERS=1.1.1.1,8.8.8.8
```

### 2.5 Resolving the zone from the host

The app does not configure the DNS server as resolver of the host (005, out of scope). To resolve
`<vm>.<APP_DNS_DOMAIN>` from the host until the next network reconnect, send only that zone to the DNS
server with systemd-resolved. It replaces the DHCP nameservers of `APP_BRIDGE_ADAPTER`, and bind9
forwards other names to `APP_DNS_FORWARDERS`, so internet names keep resolving:

```bash
set -a; source stratus-srv/.env; set +a
sudo resolvectl dns "$APP_BRIDGE_ADAPTER" "${APP_DNS_IP%/*}"
sudo resolvectl domain "$APP_BRIDGE_ADAPTER" "~$APP_DNS_DOMAIN"
resolvectl query "vm1.$APP_DNS_DOMAIN"
```

Undo it with `sudo resolvectl revert "$APP_BRIDGE_ADAPTER"`. bind9 answers only clients in the subnet of
`APP_DNS_IP` and `10.10.0.0/16` (REQ-16). A browser with secure DNS (DoH) enabled bypasses
systemd-resolved and does not resolve the zone.

## 3. The root password file (`APP_GUEST_PASSWORD_FILE`)

It holds the root password you set when installing both base images. Only `VBoxManage` reads it
(`--passwordfile`), once per VM creation, to log in as root through Guest Additions and run the
bootstrap. The backend never opens it, and the password never goes to argv, the history, the logs or
the UI.

What to put in it: **only the password, in a single line, no spaces around it**. Create it without
leaving the password in your shell history:

```bash
mkdir -p ~/.stratus && chmod 700 ~/.stratus
read -rsp 'Root password of the base images: ' pw; echo
umask 077
printf '%s\n' "$pw" > ~/.stratus/guest-password
unset pw
chmod 600 ~/.stratus/guest-password
```

Then set `APP_GUEST_PASSWORD_FILE=/home/<you>/.stratus/guest-password` (absolute path).

Notes:

- The same password must be set on both base images (004 REQ-2).
- If the password and the image differ, creation fails with "No se pudo ejecutar un comando como root
  ... verifique la contraseña de la imagen base" (exit 5) and leaves no VM behind.
- The password is shared by every clone until you change it by hand. After the bootstrap, SSH
  password login is disabled, but console login still accepts it.
- Do not commit or share this file.

## 4. The backend SSH key (`APP_SSH_KEY_PATH`)

This is the backend's own ed25519 key. The bootstrap adds its public key to `/root/.ssh/authorized_keys`
of every VM, and every later operation uses it over SSH. It is **not** the key you download from the
UI (that one is per VM or per user).

### 4.1 Automatic creation (normal case)

If the pair does not exist when the backend starts, it runs `scripts/prepare/create-backend-key.sh`,
which creates `APP_SSH_KEY_PATH` (mode 600) and `APP_SSH_KEY_PATH.pub` (mode 644) and a 700 parent
directory. It is idempotent: if both files exist it only fixes the permissions, and if only one exists
it fails with exit 5 and leaves both untouched.

### 4.2 Manual creation

Equivalent to what the script does:

```bash
mkdir -p -m 700 ~/.ssh
ssh-keygen -q -t ed25519 -N "" -C stratus-backend -f ~/.ssh/stratus_backend
chmod 600 ~/.ssh/stratus_backend && chmod 644 ~/.ssh/stratus_backend.pub
```

Then `APP_SSH_KEY_PATH=/home/<you>/.ssh/stratus_backend`.

### 4.3 Related files and cautions

- `APP_SSH_KEY_PATH.known_hosts` is created by the scripts and holds one pinned host key per VM
  (`HostKeyAlias` = VM name). `create-vm.sh` clears the entry for the name before pinning it again.
- **Do not replace this key once VMs exist.** Existing VMs only trust the old public key, so the
  backend would lose access to all of them.
- The private key must not be committed, shared or logged.

## 5. Run

Backend:

```bash
cd stratus-srv
set -a; source .env; set +a
go run ./cmd/server
```

UI in Docker, from the repository root (nginx listens on `127.0.0.1:3000` and proxies `/api` to
`127.0.0.1:8080`):

```bash
docker compose up --build
```

UI in development:

```bash
cd stratus-ui && pnpm install && pnpm dev
```

## 6. Checks before the first VM

```bash
VBoxManage --version
test -r "$APP_BASE_DISK" && test -r "$APP_HAPROXY_BASE_DISK" && echo disks ok
for d in "$APP_APACHE_BASE_DISK" "$APP_NGINX_BASE_DISK" "$APP_DNS_BASE_DISK"; do test -r "$d" && echo "$d ok"; done   # 005, optional
getcap "$(command -v arping)"                                         # cap_net_raw=ep (005 DNS only)
ip -4 -br addr show "$APP_BRIDGE_ADAPTER"                             # host IPv4 inside the subnet of APP_DNS_IP (005)
arping -c 1 -w 1 -I "$APP_BRIDGE_ADAPTER" "${APP_DNS_IP%/*}"; echo $?    # 1: free, 0: in use, 2: no cap_net_raw (005)
stat -c '%a %n' "$APP_GUEST_PASSWORD_FILE" "$APP_SSH_KEY_PATH"      # 600 for both
test -r "$APP_SSH_KEY_PATH.pub" && echo pubkey ok
VBoxManage list bridgedifs | grep -x "Name:[[:space:]]*$APP_BRIDGE_ADAPTER"
```

The first VM creation is slow (it boots, bootstraps, reboots and waits for DHCP and SSH; the script
bounds itself to about 14 minutes, below the backend's 15-minute limit) and only one creation can run
at a time. Creating the DNS server also reboots the VM onto its static IP (up to 3 more minutes), so
in the worst case the backend stops it at 15 minutes; the script then deletes the VM and its disk.

## 7. Tests

```bash
cd stratus-srv && go test ./... && gofmt -l . && golangci-lint run
cd stratus-ui && pnpm test && pnpm typecheck
bats -r scripts/
```

The script tests use stubs for `VBoxManage` and `ssh`. What they cannot cover is the before/after
demo on the real host, listed as pending in each spec's `progress.md`.

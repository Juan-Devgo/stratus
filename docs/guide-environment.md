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

The two base disks must exist before you can create VMs; see
[guide-base-images.md](guide-base-images.md).

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

If you have a `stratus.db` from before 2026-10-22, delete it: the schema changed and there is no
migration.

### 2.1 `APP_BRIDGE_ADAPTER`

It must be one of the names `VBoxManage list bridgedifs` prints on a `Name:` line:

```bash
VBoxManage list bridgedifs | grep '^Name:'
```

Use the complete name exactly as printed. On macOS it can contain spaces, for example
`APP_BRIDGE_ADAPTER='en0: Wi-Fi'`; quote that value when loading it from `.env`.

Prefer a wired interface. On Wi-Fi many access points do not give each VM its own DHCP lease.

### 2.2 `APP_BASE_DISK` and `APP_HAPROXY_BASE_DISK`

Absolute paths to the `.vdi` files built in [guide-base-images.md](guide-base-images.md). The app
never modifies or deletes them, and the two must be different files.

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

Then set `APP_GUEST_PASSWORD_FILE` to that absolute path (for example
`/Users/<you>/.stratus/guest-password` on macOS or `/home/<you>/.stratus/guest-password` on Linux).

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

UI in Docker, from the repository root (Compose publishes nginx only on `127.0.0.1:3000`; inside
the container nginx proxies `/api` to the native backend through `host.docker.internal:8080`):

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
stat -c '%a %n' "$APP_GUEST_PASSWORD_FILE" "$APP_SSH_KEY_PATH"      # 600 for both
test -r "$APP_SSH_KEY_PATH.pub" && echo pubkey ok
VBoxManage list bridgedifs | grep -x "Name:[[:space:]]*$APP_BRIDGE_ADAPTER"
```

The first VM creation is slow (it boots, bootstraps, reboots and waits for DHCP and SSH; the script
bounds itself to about 14 minutes, below the backend's 15-minute limit) and only one creation can run
at a time.

## 7. Tests

```bash
cd stratus-srv && go test ./... && gofmt -l . && golangci-lint run
cd stratus-ui && pnpm test && pnpm typecheck
bats -r scripts/
```

The script tests use stubs for `VBoxManage` and `ssh`. What they cannot cover is the before/after
demo on the real host, listed as pending in each spec's `progress.md`.

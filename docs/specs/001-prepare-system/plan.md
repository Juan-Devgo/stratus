# Plan: Prepare system

## 1. Metadata
- Spec: `docs/specs/001-prepare-system/spec.md`
- Updated: 2026-10-22 (amendment: differencing disks over a multi-attach base disk, bridged network, GA bootstrap only at creation; validator findings: constitution 2.0.0, timeout budget, graceful timeout kill, global create lock, test inventory; REQ-32 and AC-46 to AC-48 with test renames)
- Constitution version: 2.0.0

## 2. Constitution check
- P1: the backend validates input, picks the script, passes argv and maps the result to a message. Every VirtualBox, Guest Additions (GA) and SSH action lives in `scripts/prepare/*.sh`. This includes reading a VM's IP, which the scripts do. The backend only reads the IP from the `list-vms.sh` stdout to display it. The `user_key_generated` flag is app bookkeeping in SQLite, not a system operation.
- P2: there is one script per operation. Each one has `set -euo pipefail`, positional arguments, results on stdout and diagnostics on stderr. Each has an exit-code table built from 0, 2, 3, 4, 5, 6, 7 and 99, with the codes listed in section 6. Idempotence is written into each script's behavior comments.
- P3: the backend calls `exec.CommandContext(ctx, scriptPath, args...)` and never uses `bash -c`. The same allowlists are in `stratus-ui/src/features/prepare/validation.ts`, `stratus-srv/internal/service/prepare_validation.go` and each script's step "1. Validar argumentos". `create-vm.sh` also validates `base_disk` and `bridge_adapter`. The GA bootstrap is a constant `sh -c` script, and its inputs (backend public key, VM name) go in as positional arguments, never interpolated.
- P4 (2.0.0, GA until key access is configured, then SSH only): GA is used only inside `create-vm.sh`, before key access is configured. The bootstrap that configures key access adds the backend key and sets `PasswordAuthentication no` and `PermitRootLogin prohibit-password`, so password login is disabled the moment key access exists. After the bootstrap no script uses GA: every operation goes through `vm_exec`, which is SSH with the backend key to `root@<VM IPv4>:22`.
- P5: the user's private key exists only in the script's temp dir, which is deleted on exit, then in the script's stdout, then in the backend `KeyStore` map. It is wiped after one download or 10 minutes. stdout is never stored or logged. The UI shows the usage message. The backend key is read only by scripts, through `APP_SSH_KEY_PATH`. The base image root password is read only by VirtualBox through `--passwordfile "$APP_GUEST_PASSWORD_FILE"` in `create-vm.sh`. It is never in argv or script output, and the backend never opens it.
- P6: every operation returns `{message}` or `{error}`. The `operations` table keeps the command, argv, exit code, stderr (masked), result and timestamp.
- P7: `/` shows 4 module cards. `/prepare` is the only module screen. Each form shows an `Alert` with the result.
- P8: each script runs standalone with argv and env. Every script, and the sourced `lib/vm-exec.sh`, has bats tests that run it without the web app (`bats -r scripts/`). The before/after demo on the real host covers what stubs cannot: AC-26, AC-38, AC-40 and AC-44.
- P9: no new dependencies. The GA helper is a function inside `create-vm.sh`, its only user. The single Go interface is `ScriptRunner`, which tests need.

## 3. Architecture
- Host: the Go backend runs natively on `127.0.0.1:8080`, next to VirtualBox, the base disk and the bridge interface. docker-compose runs only `stratus-ui`, publishes nginx as `127.0.0.1:3000:3000` and maps `host.docker.internal` to the host gateway. nginx serves the SPA and proxies `/api/` to `host.docker.internal:8080` with a 960 s read/send timeout. This works in Docker Desktop and Linux without exposing the unauthenticated UI beyond localhost.
- VM model: each VM is registered in VirtualBox. Its SATA port 0 holds a differencing image that VirtualBox creates automatically when the multi-attach base disk `APP_BASE_DISK` is attached with `--mtype multiattach`. NIC 1 is bridged to `APP_BRIDGE_ADAPTER`, and the IPv4 comes from the LAN's DHCP server. Guest Additions publish it in the guest property `/VirtualBox/GuestInfo/Net/0/V4/IP`.
- Data flow of a mutating operation:
  1. The UI validates input with `validation.ts` and calls `api.ts` (`fetch`).
  2. The handler decodes the JSON and calls `PrepareService`.
  3. The service validates (`prepare_validation.go`) and loads the VM row (`repository`). It tries the VM lock (`VMLocks.TryLock`) and builds argv: `<vm> <operation args...>` for guest scripts, `<vm>` for start, stop and delete, or `<vm> <memory_mb> <cpus> <APP_BASE_DISK> <APP_BRIDGE_ADAPTER>` for create. Create first tries the global create lock (`createMu.TryLock`), so only one creation runs at a time, because every creation touches the shared base disk. A second concurrent create gets `ErrCreateInProgress` (409, "Ya hay una creación de VM en curso"), registers nothing and invokes no script. Operations on existing VMs still run (REQ-32, AC-46).
  4. `ExecRunner.Run` runs `<APP_SCRIPTS_DIR>/prepare/<script>.sh` with a 15-minute context. The process inherits the env (`APP_GUEST_PASSWORD_FILE`, `APP_SSH_KEY_PATH`) and runs in its own process group. On timeout it sends SIGTERM to the group, so the scripts' `EXIT` traps (for example the create cleanup) run, then SIGKILL to the group after a 30 s grace. The result is `TimedOut`, as before (AC-20).
  5. The service masks stderr (`Redact`) and inserts the `operations` row. On success it updates the VM row. It releases the lock and returns the message or a sentinel error. Exit code 7 becomes `ErrVMNoIP`.
  6. The handler maps the result to an HTTP status and `{message}` or `{error}`. The UI shows an `Alert` and reloads the VM list and the history.
- Guest scripts: right after checking that the VM is registered, each one calls `vm_ip <vm>` once. With no address it exits 7 before any remote command (REQ-29). Each `vm_exec` call resolves the IP again, because DHCP may change it between boots, then runs `ssh` as root with the backend key.
- Create (`create-vm.sh`):
  1. Checks before `createvm`: base disk, bridge interface, multi-attach type, and that the VM is not registered.
  2. `createvm` and `modifyvm` (bridged), the SATA controller and the multiattach attach, then `startvm`.
  3. Wait for GA, then one bootstrap call through GA as root.
  4. Host-side power cycle, then wait for the IPv4.
  5. `ssh-keygen -R <vm>` on the app known_hosts, then an SSH `true` check.
  6. Print "VM <vm> creada y encendida; IP <ip>".
  On any failure after `createvm`, it powers off, detaches and deletes the differencing image, and runs `unregistervm --delete`. It never closes or deletes a medium without a parent.
- Read-only list: `list-vms.sh` (no args) prints `<name> <running|poweroff> <ipv4|->` lines. The service merges them with the SQLite `vms` rows. A VM missing from the output, or any script failure, gives `state: "unknown"`, `ip: ""` and a `list_error`. List calls are not recorded in history. Failures go to the server log.
- Startup: the backend loads the env and opens SQLite, creating tables if missing. If `APP_SSH_KEY_PATH` is missing, it runs `create-backend-key.sh <APP_SSH_KEY_PATH>`; on failure it logs and keeps serving, and later SSH operations fail. Then it registers `GET /healthz` (liveness check for the deploy) and the prepare routes, and starts the `http.Server` with `WriteTimeout` 16 min (15 min script, 30 s grace, margin to write the response). The schema changed, so an old `stratus.db` must be deleted by hand. There is no migration code.
- State:
  - SQLite holds the VM names, `user_key_generated` and the history.
  - VirtualBox holds the VMs, their disks and their IPs.
  - Backend memory holds `VMLocks`, the create lock and `KeyStore`.
  - `$APP_SSH_KEY_PATH.known_hosts` holds one pinned host key per VM name.
  - UI state holds the selected VM in `Prepare.tsx` (`useState`), and each form keeps its own state.

## 4. Design patterns
- Strategy via interface in `internal/service/script_runner.go` (`ScriptRunner`): service tests use a fake that records argv and returns canned results, so no VirtualBox is needed.
- Repository in `internal/repository/`: SQL stays out of the service, and tests use a temp SQLite file.
- Try-lock map in `internal/service/vm_locks.go`: one operation per VM (REQ-15), rejected immediately, no queue.
- Single try-lock (`createMu sync.Mutex`, `TryLock`) in `PrepareService`: one creation at a time, because all creations register or attach the shared multiattach base disk. Rejected immediately, no queue (REQ-32, AC-46).
- Single source list in `internal/service/prepare_catalog.go`, served by `GET /api/prepare/catalog` (REQ-5).
- Config list in `stratus-ui/src/features/home/modules.ts`: the home cards and future module routes come from one array (REQ-19).
- Sourced helper `scripts/prepare/lib/vm-exec.sh`: the 5 guest scripts share one way to resolve the IP and to run SSH (REQ-24, REQ-29).

## 5. Code structure
Scripts (headers, comments and output in Spanish):
- modify: `scripts/prepare/lib/vm-exec.sh` - replace the `ga|ssh` transport with two functions:
  - `vm_ip <vm>` prints the IPv4, or prints the REQ-29 message and returns 7. It returns 7 when the VM is not `running`, when the property is missing, or when the value is not an IPv4.
  - `vm_exec <vm> <command...>` resolves the IP with `vm_ip`, then runs `ssh` with the options of section 6.
- modify: `scripts/prepare/create-vm.sh` - new argv. It keeps the base disk requirements in its header, has the `ga_root` helper function, a constant `BOOTSTRAP` script, the power cycle, the SSH check, and cleanup guarded by "medium has a parent". It no longer has the ISO, port forwarding, `createmedium`, the per-VM password or the `Unattended-*` cleanup. Every wait is bounded by the 14-minute budget of section 6: `ga_root` passes `--timeout <ms>` to `guestcontrol run`, and the GA wait, the IP wait and the retry counts are reduced.
- modify: `scripts/prepare/list-vms.sh` - for each `running` VM, add the IPv4 from `guestproperty get`; print `-` when the VM is off or reports no IPv4.
- keep: `scripts/prepare/start-vm.sh` - `startvm --type headless`.
- keep: `scripts/prepare/stop-vm.sh` - `acpipowerbutton`, polls `showvminfo` up to 60 s, then `poweroff`.
- modify: `scripts/prepare/delete-vm.sh` - `poweroff` if running. Read `SATA-ImageUUID-0-0`, then `storageattach ... --medium none`, then `closemedium disk <uuid> --delete`, but only if `showmediuminfo` shows a `Parent UUID:` other than `base`. A failed `closemedium` is tolerated. Then `unregistervm --delete`. No env, no password file, no `Unattended-*` cleanup.
- modify: `scripts/prepare/install-package.sh` - argv `<vm> <package>`, the `vm_ip` precheck, `apt-get install -y`, and `ufw allow 22/tcp` whenever the package is `ufw`. The openssh-server key setup branch is removed.
- modify: `scripts/prepare/remove-package.sh` - argv `<vm> <package>`, `openssh-server` is always rejected with exit 2, and the `vm_ip` precheck runs before `apt-get purge -y`.
- modify: `scripts/prepare/generate-user-key.sh` - argv `<vm>`. The `vm_ip` precheck runs before `ssh-keygen`, and the `ga` branch is removed. It still replaces the `stratus-user` lines, keeps or adds `stratus-backend`, and reasserts the sshd settings. It prints only the private key on stdout.
- modify: `scripts/prepare/set-issue.sh` - argv `<vm> <message> [field_key...]` and the `vm_ip` precheck.
- modify: `scripts/prepare/set-hostname.sh` - argv `<vm> <hostname>` and the `vm_ip` precheck.
- keep: `scripts/prepare/create-backend-key.sh` - `ssh-keygen -t ed25519 -N "" -C stratus-backend -f <key_path>` and `chmod 600`.
- modify: `scripts/prepare/test_helper.bash`:
  - keep `assert_script_structure`, `setup_stubs` (logging stubs of `VBoxManage`, `ssh`, `ssh-keygen`, `sleep`) and `stub_command`.
  - add `setup_guest_password`, which exports `APP_GUEST_PASSWORD_FILE` with a known `GUEST_PASSWORD`, mode 600.
  - add `stub_vm_ip <vm> <ipv4|->`. With an IP, `showvminfo --machinereadable` reports `VMState="running"` and `guestproperty get` prints `Value: <ip>`. With `-`, they report `VMState="poweroff"` and `No value set!`.
  - remove `setup_guest_secret`.
- modify: `scripts/prepare/<script>.bats` - one per script, with the tests of section 7. Every test name starts with its AC ID, and the stale "placeholder" wording is gone from the names and header comments of `start-vm.bats`, `stop-vm.bats` and `create-backend-key.bats`.
- create: `scripts/prepare/lib/vm-exec.bats` - sources `lib/vm-exec.sh` with `load ../test_helper` stubs. Covers `%q` quoting, ssh 255 to 4, remote failure to 5, and `vm_ip` returning 7.

Backend:
- keep: `stratus-srv/go.mod` (`modernc.org/sqlite`).
- modify: `stratus-srv/cmd/server/main.go`:
  - remove `firstSSHPort` and pass `BaseDisk` and `BridgeAdapter` to `service.Config`.
  - set `ExecRunner.KillGrace` to 30 s.
  - keep `GET /healthz` (`{"status":"ok"}`, deploy liveness check).
  - replace the stale comment "mientras el script sea un placeholder" with the real reason: the server keeps serving and only the SSH operations fail.
- keep: `stratus-srv/.golangci.yml` - the lint configuration used by `golangci-lint run` (AGENTS.md lint command).
- modify: `stratus-srv/.gitignore` - add `stratus.db` and `*.known_hosts`.
- modify: `stratus-srv/cmd/server/config.go` - require `APP_BASE_DISK`, `APP_BRIDGE_ADAPTER`, `APP_GUEST_PASSWORD_FILE` and `APP_SSH_KEY_PATH`. Remove `APP_DEBIAN_ISO`, `APP_GUEST_USER` and `APP_GUEST_SECRETS_DIR`. Never open the password file.
- modify: `stratus-srv/internal/repository/db.go` - `vms` schema without `ssh_port` and `ssh_enabled`.
- modify: `stratus-srv/internal/repository/prepare_vms.go` - `Insert` replaces `Reserve`, and `SetSSHEnabled` is removed.
- keep: `stratus-srv/internal/repository/history.go` (`HistoryRepository`).
- modify: `stratus-srv/internal/service/errors.go` - add `ErrVMNoIP` and `ErrCreateInProgress`.
- modify: `stratus-srv/internal/service/script_runner.go` - `KillGrace`. On timeout, `cmd.Cancel` sends SIGTERM to `-pgid` and arms `time.AfterFunc(KillGrace, SIGKILL to -pgid)`. `cmd.WaitDelay = KillGrace + 1 s`.
- keep: `redact.go`, `vm_locks.go`, `prepare_catalog.go`, `prepare_key_store.go`, `backend_key.go`, `prepare_system.go` in `stratus-srv/internal/service/`.
- modify: `stratus-srv/internal/service/prepare_validation.go` - remove `DiskGB` from `CreateVMInput` and its rule.
- modify: `stratus-srv/internal/service/prepare_service.go`:
  - `Config` holds `BaseDisk`, `BridgeAdapter`, `SSHKeyPath` and `KeyTTL`.
  - `guestArgs` returns `[name, extra...]`.
  - `runOperation` maps exit 7 to `ErrVMNoIP`.
  - new field `createMu sync.Mutex`.
- modify: `stratus-srv/internal/service/prepare_vms.go`:
  - `VMView.IP`, and `parseVMStates` reads 3 fields.
  - `CreateVM` takes `createMu.TryLock()` after validation (else `ErrCreateInProgress`), then uses `Insert`, the new argv, and the script's stdout as the message.
- modify: `stratus-srv/internal/service/prepare_packages.go` - removing `openssh-server` is always blocked, and installing it is a plain install.
- modify: `stratus-srv/internal/service/prepare_keys.go` - on success, set only `user_key_generated`.
- modify: `stratus-srv/internal/handler/respond.go` - `ErrVMNoIP` and `ErrCreateInProgress` map to 409.
- keep: `stratus-srv/internal/handler/prepare.go` (routes unchanged).
- modify: `_test.go` files listed in section 7. `errors_test.go` is renamed to an AC test, and the `REQ-28` comment in `prepare_vms_test.go` becomes `REQ-22`.
- modify: `stratus-srv/.env.example` - the new variables, the base disk requirements and the note "delete the old `stratus.db`".

Frontend:
- keep: `stratus-ui/components.json`, `src/lib/utils.ts`, `src/components/ui/*`, `src/index.css`, `vite.config.ts`, `tsconfig.json`, `src/App.tsx`, `src/components/AppLayout.tsx`, `src/components/OperationResult.tsx`, `src/features/home/*`, `src/routes/Home.tsx`.
- modify: `stratus-ui/src/routes/Prepare.tsx` - the "Crear VM" `CardDescription` describes the clone model: "Crea un clon de Debian 13 sobre el disco base compartido (disco diferencial, red en puente, IP por DHCP) y lo prepara para el acceso por SSH. Puede tardar varios minutos." This replaces the stale "Instalación desatendida..." text.
- modify: `stratus-ui/src/features/prepare/types.ts` - `Vm` is `{name, state, ip, user_key_generated}`.
- modify: `stratus-ui/src/features/prepare/VmActions.tsx` - the delete `AlertDialogDescription` states that only the VM's differencing disk is removed and that the shared base disk is not modified ("El disco base compartido no se modifica").
- keep: `api.ts`, `issue-preview.ts`, `useOperation.ts`, `PackageForm.tsx`, `IssueForm.tsx`, `HostnameForm.tsx`, `HistoryTable.tsx` in `stratus-ui/src/features/prepare/`.
- modify: `stratus-ui/src/features/prepare/validation.ts` - remove `disk_gb` from `CreateVmInput` and `createVmErrors`.
- modify: `stratus-ui/src/features/prepare/CreateVmForm.tsx` - fields: name, memory and CPUs.
- modify: `stratus-ui/src/features/prepare/VmSelector.tsx` - option label `<name> - <Estado> - <ip|->`.
- modify: `stratus-ui/src/features/prepare/UserKeyForm.tsx` - usage line `ssh -i id_ed25519_<vm> root@<ip>`.
- modify: `*.test.ts(x)` listed in section 7. The VM fixtures change to `{name, state, ip, user_key_generated}`.

Deploy and repository:
- keep: `stratus-ui/nginx.conf`, `stratus-ui/Dockerfile`, `docker-compose.yml`.
- modify: `.gitignore` - put `/virtualbox/` back on its own line (today the first line is `/virtualbox/# Contraseñas...`, so the directory is not ignored) and remove the `guest-secrets` comment and entry.

## 6. Interfaces and data
Environment (backend):
| Variable | Default | Use |
|---|---|---|
| `APP_LISTEN_ADDR` | `127.0.0.1:8080` | HTTP listen address |
| `APP_DB_PATH` | `./stratus.db` | SQLite file. Delete the file from before 2026-10-22 (schema change, no migration) |
| `APP_SCRIPTS_DIR` | `../scripts` | scripts root |
| `APP_BASE_DISK` | required | absolute path of the base disk; passed as argv to `create-vm.sh` |
| `APP_BRIDGE_ADAPTER` | required | host interface for every VM (one of the names that `VBoxManage list bridgedifs` lists); passed as argv to `create-vm.sh` |
| `APP_GUEST_PASSWORD_FILE` | required | host file (mode 600) with the base image root password. Inherited by `create-vm.sh` through the env; the backend only checks that it is set |
| `APP_SSH_KEY_PATH` | required | backend private key. The public key is at `<path>.pub` and the pinned host keys at `<path>.known_hosts`. Inherited by the scripts through the env |

Base disk requirements (documented in `.env.example` and in the header of `create-vm.sh`):
- Debian 13 matching the host architecture, installed with UEFI on ARM64 or VirtualBox's default
  BIOS firmware on x86-64, with a single NIC.
- Guest Additions installed and starting at boot.
- openssh-server installed.
- The root password is the content of `APP_GUEST_PASSWORD_FILE`.
- The disk is not attached to another VM when first used. `create-vm.sh` registers it as multiattach through `storageattach --mtype multiattach`.

SQLite schema:
```sql
CREATE TABLE IF NOT EXISTS vms (
  name TEXT PRIMARY KEY,
  user_key_generated INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS operations (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  module TEXT NOT NULL,          -- 'prepare'
  operation TEXT NOT NULL,       -- 'create_vm', 'start_vm', 'stop_vm', 'delete_vm', 'install_package', 'remove_package', 'generate_user_key', 'set_issue', 'set_hostname'
  vm TEXT NOT NULL,
  command TEXT NOT NULL,         -- script path
  args TEXT NOT NULL,            -- JSON array of argv
  exit_code INTEGER NOT NULL,    -- -1 on timeout
  stderr TEXT NOT NULL,          -- after Redact
  result TEXT NOT NULL,          -- 'ok' | 'error' | 'timeout'
  created_at TEXT NOT NULL       -- RFC3339 UTC
);
```

Go (package `repository`):
```go
type VM struct { Name string; UserKeyGenerated bool; CreatedAt time.Time }
var ErrDuplicateVM, ErrVMNotFound error
func Open(path string) (*sql.DB, error)
func NewVMRepository(db *sql.DB) *VMRepository
func (r *VMRepository) Insert(ctx context.Context, name string) (VM, error) // ErrDuplicateVM if the name exists
func (r *VMRepository) Get(ctx context.Context, name string) (VM, error)
func (r *VMRepository) List(ctx context.Context) ([]VM, error)          // ORDER BY name
func (r *VMRepository) Delete(ctx context.Context, name string) error
func (r *VMRepository) SetUserKeyGenerated(ctx context.Context, name string) error
type Operation struct { ID int64; Module, Operation, VM, Command string; Args []string; ExitCode int; Stderr, Result string; CreatedAt time.Time }
func NewHistoryRepository(db *sql.DB) *HistoryRepository
func (r *HistoryRepository) Insert(ctx context.Context, op Operation) error
func (r *HistoryRepository) ListRecent(ctx context.Context, module string, limit int) ([]Operation, error) // ORDER BY id DESC
```

Go (package `service`):
```go
var (
  ErrInvalidInput        = errors.New("entrada inválida")
  ErrDuplicateVM         = errors.New("Ya existe una VM con ese nombre")
  ErrVMNotFound          = errors.New("La VM no existe")
  ErrOperationInProgress = errors.New("Operación en curso en esta VM")
  ErrSSHRemovalBlocked   = errors.New("No se puede eliminar openssh-server: el backend lo usa para conectarse")
  ErrKeyUnavailable      = errors.New("La llave ya no está disponible, genere una nueva")
  ErrVMNoIP              = errors.New("La VM no tiene dirección IP; verifique que esté encendida")
  ErrCreateInProgress    = errors.New("Ya hay una creación de VM en curso")
  ErrScriptFailed        = errors.New("la operación falló")
  ErrTimeout             = errors.New("la operación superó el tiempo límite de 15 minutos")
)
const exitVMNoIP = 7 // exit code of the guest scripts when vm_ip finds no IPv4 (REQ-29)
type ValidationError struct { Message string }        // Unwrap() returns ErrInvalidInput
type OperationError struct { Operation string; ExitCode int; Detail string } // Unwrap() returns ErrScriptFailed; Detail = last non-empty stderr line, max 200 chars

type RunResult struct { ExitCode int; Stdout []byte; Stderr string; TimedOut bool }
type ScriptRunner interface { Run(ctx context.Context, script string, args ...string) (RunResult, error) }
type ExecRunner struct { ScriptsDir string; Timeout, KillGrace time.Duration } // Setpgid; on timeout SIGTERM to -pgid, then SIGKILL to -pgid after KillGrace (main: 30 s); WaitDelay = KillGrace + 1 s; result TimedOut, ExitCode -1; non-zero exit is not an error

func Redact(s string) string // replaces any "-----BEGIN ... PRIVATE KEY-----...END ... PRIVATE KEY-----" block with "[llave privada omitida]"

type VMLocks struct { /* mu sync.Mutex; busy map[string]bool */ }
func (l *VMLocks) TryLock(vm string) bool
func (l *VMLocks) Unlock(vm string)

var PackageCatalog = []string{"openssh-server", "haproxy", "nginx", "apache2", "curl", "htop", "git", "ufw"}

func ValidateVMName(s string) error          // ^[a-z0-9][a-z0-9-]{0,31}$
func ValidateCreateVM(in CreateVMInput) error // name, memory 512-8192, cpus 1-4
func ValidatePackage(s string) error          // ^[a-z0-9][a-z0-9+.-]{1,62}$
func ValidateHostname(s string) error         // ^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$
func ValidateIssue(in IssueInput) error       // <=500 runes; each rune unicode.IsPrint or '\n'; no '\\'; known field keys; message or >=1 field

type CreateVMInput struct { Name string `json:"name"`; MemoryMB int `json:"memory_mb"`; CPUs int `json:"cpus"` }
type IssueInput struct { Message string `json:"message"`; Fields []string `json:"fields"` } // keys: hostname,date,time,ipv4,os,kernel,arch,tty,users
type VMView struct { Name string `json:"name"`; State string `json:"state"`; IP string `json:"ip"`; UserKeyGenerated bool `json:"user_key_generated"` } // state: running|poweroff|unknown; ip: IPv4 or "" when unknown

type Config struct { BaseDisk, BridgeAdapter, SSHKeyPath string; KeyTTL time.Duration /* 10m */ }
func NewPrepareService(cfg Config, runner ScriptRunner, vms *repository.VMRepository, history *repository.HistoryRepository, logger *log.Logger) *PrepareService
func guestArgs(vm repository.VM, extra ...string) []string // [vm.Name, extra...]
func (s *PrepareService) ListVMs(ctx context.Context) (vms []VMView, listErr string, err error) // ip only when the third field parses as IPv4 (net.ParseIP(..).To4() != nil)
func (s *PrepareService) CreateVM(ctx context.Context, in CreateVMInput) (string, error)  // validate; createMu.TryLock() or ErrCreateInProgress (no row, runner not called), unlocked on return; Insert row; argv [name memory cpus cfg.BaseDisk cfg.BridgeAdapter]; message = trimmed stdout, or "VM <name> creada y encendida" if empty; on failure deletes the row
func (s *PrepareService) StartVM(ctx context.Context, name string) (string, error)
func (s *PrepareService) StopVM(ctx context.Context, name string) (string, error)
func (s *PrepareService) DeleteVM(ctx context.Context, name string) (string, error)     // on success deletes row and KeyStore entry
func (s *PrepareService) InstallPackage(ctx context.Context, name, pkg string) (string, error) // argv [name pkg]
func (s *PrepareService) RemovePackage(ctx context.Context, name, pkg string) (string, error)  // pkg == openssh-server -> ErrSSHRemovalBlocked, runner not called
func (s *PrepareService) GenerateUserKey(ctx context.Context, name string) (string, error)     // argv [name]; ok -> KeyStore.Put(stdout), SetUserKeyGenerated
func (s *PrepareService) DownloadUserKey(name string) ([]byte, error)
func (s *PrepareService) SetIssue(ctx context.Context, name string, in IssueInput) (string, error) // argv [name message fields in canonical order]
func (s *PrepareService) SetHostname(ctx context.Context, name, hostname string) (string, error)   // argv [name hostname]
func (s *PrepareService) History(ctx context.Context) ([]repository.Operation, error) // module 'prepare', limit 50

func NewKeyStore(ttl time.Duration) *KeyStore
func (k *KeyStore) Put(vm string, key []byte) // replaces previous, time.AfterFunc(ttl) deletes
func (k *KeyStore) Take(vm string) ([]byte, error) // returns and deletes, or ErrKeyUnavailable
func (k *KeyStore) Discard(vm string)

func EnsureBackendKey(ctx context.Context, runner ScriptRunner, keyPath string) error // runs prepare/create-backend-key.sh only if keyPath is missing
```
`runOperation` never logs or stores `RunResult.Stdout`. It logs `operation vm exit_code stderr(redacted)`. When `ExitCode == exitVMNoIP` it records the row with `result = 'error'` and returns `fmt.Errorf("%s en %s: %w", op, vm, ErrVMNoIP)` instead of `OperationError`. `create-vm.sh` never uses code 7.

Go (package `main`): `type config struct { ListenAddr, DBPath, ScriptsDir, BaseDisk, BridgeAdapter, GuestPasswordFile, SSHKeyPath string }`. `loadConfig()` names the missing variables, never their values, and never opens `GuestPasswordFile`.

Go (package `handler`): `func RegisterPrepare(mux *http.ServeMux, svc *service.PrepareService)`. Error to status mapping:
- `ErrInvalidInput`: 400.
- `ErrVMNotFound`: 404.
- `ErrDuplicateVM`, `ErrOperationInProgress`, `ErrCreateInProgress`, `ErrSSHRemovalBlocked`, `ErrVMNoIP`: 409.
- `ErrKeyUnavailable`: 410.
- `ErrTimeout`: 504.
- `ErrScriptFailed`: 500 with `"La operación <op> falló (código <n>): <detail>"`. For example `create_vm` exit 6 gives `...: Disco base no encontrado: <path>`.
- Anything else: 500 `"Error interno, revise el registro del servidor"`.

HTTP API (JSON `snake_case`; success `{"message": "..."}`, failure `{"error": "..."}`):
| Method and path | Body | Success |
|---|---|---|
| `GET /healthz` | - | `{"status": "ok"}` (deploy liveness check, outside `/api`) |
| `GET /api/prepare/vms` | - | `{"vms": [{"name","state","ip","user_key_generated"}], "list_error": ""}` |
| `POST /api/prepare/vms` | `{"name","memory_mb","cpus"}` | 201 `{message}` ("VM vm1 creada y encendida; IP 192.168.1.50"). 409 "Ya hay una creación de VM en curso" while another create runs |
| `POST /api/prepare/vms/{name}/start` | - | 200 `{message}` |
| `POST /api/prepare/vms/{name}/stop` | - | 200 `{message}` |
| `DELETE /api/prepare/vms/{name}` | - | 200 `{message}` |
| `GET /api/prepare/catalog` | - | `{"packages": [...]}` |
| `POST /api/prepare/vms/{name}/install-package` | `{"package": "nginx"}` | 200 `{message}` |
| `POST /api/prepare/vms/{name}/remove-package` | `{"package": "nginx"}` | 200 `{message}` |
| `POST /api/prepare/vms/{name}/user-key` | - | 200 `{message}` |
| `GET /api/prepare/vms/{name}/user-key/download` | - | 200 `text/plain`, `Content-Disposition: attachment; filename="id_ed25519_<name>"` |
| `POST /api/prepare/vms/{name}/issue` | `IssueInput` | 200 `{message}` |
| `POST /api/prepare/vms/{name}/hostname` | `{"hostname": "web-01"}` | 200 `{message}` |
| `GET /api/prepare/history` | - | `{"operations": [{id, operation, vm, command, args, exit_code, stderr, result, created_at}]}` |

Scripts (argv; env inherited; stdout is the result; stderr is diagnostics):
| Script | Arguments | Env | stdout |
|---|---|---|---|
| `create-vm.sh` | `<vm_name> <memory_mb> <cpus> <base_disk> <bridge_adapter>` | `APP_GUEST_PASSWORD_FILE`, `APP_SSH_KEY_PATH` | `VM <vm> creada y encendida; IP <ip>` |
| `list-vms.sh` | none | - | `<name> <running\|poweroff> <ipv4\|->` per line |
| `start-vm.sh`, `stop-vm.sh`, `delete-vm.sh` | `<vm_name>` | - | message |
| `install-package.sh`, `remove-package.sh` | `<vm_name> <package>` | `APP_SSH_KEY_PATH` | message |
| `generate-user-key.sh` | `<vm_name>` | `APP_SSH_KEY_PATH` | private key only |
| `set-issue.sh` | `<vm_name> <message> [field_key...]` | `APP_SSH_KEY_PATH` | message |
| `set-hostname.sh` | `<vm_name> <hostname>` | `APP_SSH_KEY_PATH` | message |
| `create-backend-key.sh` | `<key_path>` | - | message |

`create-vm.sh` validation (step 1, no VBoxManage call on failure, exit 2):
- exactly 5 args.
- `vm_name` matches `^[a-z0-9][a-z0-9-]{0,31}$`.
- `memory_mb` is 512-8192 and `cpus` is 1-4.
- `base_disk` is an absolute path.
- `bridge_adapter` matches `^[A-Za-z0-9._:()+? -]{1,64}$`; spaces and parentheses are
  allowed because VirtualBox on macOS reports names such as `en0: Wi-Fi` and
  `en3: Adaptador Ethernet (en3)`. The value must still exactly match an interface returned by
  `VBoxManage list bridgedifs`.
- `APP_GUEST_PASSWORD_FILE` is a readable file.
- `$APP_SSH_KEY_PATH.pub` exists.

`create-vm.sh` time budget. Every wait has an upper bound, and the worst case is at most 14 min, which leaves 1 min under the backend's 15-minute limit:
| Wait | Bound | Worst case |
|---|---|---|
| GA version property (step 3) | `guestproperty wait --timeout 180000` | 180 s |
| GA readiness (step 3) | 8 tries of `ga_root --timeout 10000 /bin/true` + `sleep 5` | 120 s |
| Bootstrap (step 4) | `ga_root --timeout 60000` | 60 s |
| ACPI shutdown (step 5) | 12 polls x `sleep 5` | 60 s |
| `wait_stopped` after the forced poweroff (step 5) | 10 polls x `sleep 1` | 10 s |
| IP property (step 6) | `guestproperty wait --timeout 180000` | 180 s |
| SSH check (step 7) | 8 tries of `ssh -o ConnectTimeout=10` + `sleep 5` | 120 s |
| Sum of the waits | | 730 s |
| VBoxManage calls of steps 1-2 and 5, `ssh-keygen -R`, and cleanup (including its `wait_stopped`) | margin | 110 s |
| Total | | 840 s = 14 min |

`ga_root` takes the timeout in milliseconds as its first argument and passes it as `guestcontrol run --timeout <ms>`. The bats test `AC-44 create-vm acota sus esperas a 14 minutos` adds up the bounds that the script passes to its stubs and checks that they are within 730 s.

`create-vm.sh` steps:
1. Checks before `createvm`, none of which register a VM:
   - `base_disk` is not a readable file: `Disco base no encontrado: <path>`, exit 6.
   - `bridge_adapter` is not among the `Name:` lines of `VBoxManage list bridgedifs`: `Interfaz de puente no encontrada: <name>`, exit 6.
   - `showvminfo <vm>` succeeds: `VM ya registrada en VirtualBox`, exit 6.
2. Create the VM (exit 4 on failure; from here on the cleanup trap is active):
   - Read `VBoxManage list systemproperties`. On an ARMv8-only host use `Debian13_arm64`; otherwise use `Debian13_64`. Then run `createvm --name <vm> --ostype <selected> --register`. The VDI guest architecture must match the host architecture supported by VirtualBox.
   - `modifyvm <vm> --memory <m> --cpus <c> --ioapic on --graphicscontroller vmsvga --vram 16 --nic1 bridged --bridgeadapter1 <adapter>`. VMSVGA is explicit because VirtualBox 7.2 on an ARMv8 Mac defaults an ARM Debian VM to VBoxVGA, which fails at startup with `VERR_PGM_RAM_CONFLICT`.
   - `storagectl <vm> --name SATA --add sata --controller IntelAhci`
   - If the base reports `multiattach` but `showmediuminfo` has neither `In use by VMs:` nor `Child UUIDs:`, it is orphaned after the last child was removed. Reset it to `normal` with `modifymedium disk <base> --type normal` before creating the VM. If the base is `normal`, prime its VirtualBox 7.2 media registration with `storageattach ... --mtype normal --medium <base>` followed immediately by `storageattach ... --medium none`. Then `storageattach <vm> --storagectl SATA --port 0 --device 0 --type hdd --mtype multiattach --medium <base>` changes the registered base to multiattach and creates the VM's differencing disk. If it is an active multiattach base, only the final attachment runs. Calling `modifymedium --type multiattach` on an unattached or attached medium is rejected by VirtualBox 7.2.
   - `startvm <vm> --type headless`
3. Wait for GA (exit 5 on failure): `guestproperty wait <vm> /VirtualBox/GuestAdd/Version --timeout 180000`, then `guestproperty get` must print `Value:`. After that, `ga_root 10000 /bin/true` is retried up to 8 times with `sleep 5`.
4. Bootstrap (exit 5 on failure). One call: `ga_root 60000 /bin/sh -c "$BOOTSTRAP" sh "<backend pub key>" "<vm>"`, where `ga_root <ms> <exe> <args...>` runs `VBoxManage guestcontrol <vm> run --username root --passwordfile "$APP_GUEST_PASSWORD_FILE" --timeout <ms> --wait-stdout --wait-stderr --exe <exe> -- <argv0> <args...>`. `BOOTSTRAP` is a constant and runs with `set -e`:
   1. `install -d -m 700 /root/.ssh`, then `touch` and `chmod 600 /root/.ssh/authorized_keys`, then add `$1` if `grep -qxF` does not find it.
   2. Write `/etc/ssh/sshd_config.d/10-stratus.conf` with `PasswordAuthentication no` and `PermitRootLogin prohibit-password`.
   3. `rm -f /etc/machine-id /var/lib/dbus/machine-id`, `systemd-machine-id-setup`, `ln -sf /etc/machine-id /var/lib/dbus/machine-id`.
   4. `rm -f /var/lib/dhcp/*.leases /var/lib/dhcpcd/duid /var/lib/dhcpcd/*.lease*`.
   5. `rm -f /etc/ssh/ssh_host_*`, then `ssh-keygen -A`.
   6. `hostnamectl set-hostname "$2"`, then replace or add the `127.0.1.1 $2` line in `/etc/hosts`.
   7. `sshd -t`.
5. Power cycle:
   - `controlvm <vm> acpipowerbutton`, then poll `VMState` every 5 s up to 60 s, then `controlvm <vm> poweroff` if the VM is still running, then `wait_stopped` (up to 10 s).
   - `guestproperty delete <vm> /VirtualBox/GuestInfo/Net/0/V4/IP`.
   - `startvm <vm> --type headless` (exit 4 on failure).
6. IP (exit 5 on failure): `guestproperty wait <vm> /VirtualBox/GuestInfo/Net/0/V4/IP --timeout 180000`, then `get`. The value must match the IPv4 pattern, else `La VM no obtuvo una dirección IP por DHCP`.
7. SSH check (exit 5 on failure):
   - `ssh-keygen -R <vm> -f "$APP_SSH_KEY_PATH.known_hosts"`, tolerated if it fails.
   - `ssh <ssh options> root@<ip> true`, up to 8 tries with `sleep 5`. If none succeeds: `La llave del backend no abrió una sesión SSH en <ip>`.
8. Remove the trap and print `VM <vm> creada y encendida; IP <ip>`.

Cleanup (trap on non-zero exit after `createvm`, including exit 143 after the backend's SIGTERM on timeout; its errors only go to stderr; it keeps the original code; it must finish within the backend's 30 s grace):
1. `controlvm poweroff` if the VM is running.
2. Read `SATA-ImageUUID-0-0` from `showvminfo --machinereadable`.
3. `storageattach ... --medium none`.
4. `closemedium disk <uuid> --delete`, only when `showmediuminfo disk <uuid>` has a `Parent UUID:` other than `base`.
5. `unregistervm <vm> --delete`.

The base disk is never closed, deleted or modified after step 1.

SSH options (in `vm_exec` and the `create-vm.sh` check): `-i "$APP_SSH_KEY_PATH" -o BatchMode=yes -o ConnectTimeout=10 -o StrictHostKeyChecking=accept-new -o UserKnownHostsFile="$APP_SSH_KEY_PATH.known_hosts" -o HostKeyAlias=<vm_name> root@<ip>`. The remote command is quoted with `printf '%q '`.

`lib/vm-exec.sh`:
```
vm_ip <vm_name>                         # stdout: IPv4. returns 0; 2 bad args; 7 not running / no property / not IPv4 (prints "La VM no tiene dirección IP; verifique que esté encendida")
vm_exec <vm_name> <command> [args...]   # returns 0; 2 bad args or APP_SSH_KEY_PATH missing; 4 ssh exit 255; 5 remote command failed; 7 no IP (no ssh call)
```
Guest scripts:
1. Validate the arguments.
2. `showvminfo <vm>`: if the VM is not registered, exit 3.
3. Call `vm_ip "$vm_name" >/dev/null || exit $?`. This exits 7 before any remote command and, in `generate-user-key.sh`, before `ssh-keygen`.
4. Run the remote steps through the `guest` wrapper, which passes 2, 4 and 7 through and maps any other failure to the script's own failure code.

Script file layout:
1. Shebang `#!/usr/bin/env bash`.
2. Header: description, `Uso:`, `Argumentos:`, `Variables de entorno:`, `Códigos de salida:`.
3. `set -euo pipefail`.
4. Comment sections `# == 1. Validar argumentos ==`, `# == 2. ... ==` with numbered steps `# 2.1 ...`.
5. The body. A script that is not written yet has the placeholder `echo "Operación no implementada" >&2; exit 99`.

Exit codes shared by all scripts: 0 éxito, 2 argumentos inválidos o variables faltantes, 3 VM no encontrada, 4 fallo de comunicación (VBoxManage o SSH), 5 fallo del comando de la operación, 99 operación no implementada. Script-specific codes:
- Guest scripts (`install-package`, `remove-package`, `generate-user-key`, `set-issue`, `set-hostname`): 7 for "VM sin dirección IP".
- `create-vm.sh`: 6 for disco base no encontrado, interfaz de puente no encontrada, or VM ya registrada en VirtualBox. Its 5 covers GA sin respuesta, bootstrap fallido, sin IP tras reiniciar, and verificación SSH fallida.
- `set-hostname.sh`: 6 for fallo al actualizar /etc/hosts.

Frontend:
```ts
// types.ts
type VmState = "running" | "poweroff" | "unknown";
interface Vm { name: string; state: VmState; ip: string; user_key_generated: boolean } // ip "" when unknown
interface OperationRow { id: number; operation: string; vm: string; command: string; args: string[]; exit_code: number; stderr: string; result: "ok" | "error" | "timeout"; created_at: string }
type IssueField = "hostname" | "date" | "time" | "ipv4" | "os" | "kernel" | "arch" | "tty" | "users";
// api.ts
class ApiError extends Error { status: number }
listVms(): Promise<{ vms: Vm[]; list_error: string }>; createVm(input: CreateVmInput): Promise<string>; startVm(name); stopVm(name); deleteVm(name);
getCatalog(): Promise<string[]>; installPackage(name, pkg); removePackage(name, pkg); generateUserKey(name); downloadUserKey(name): Promise<Blob>;
setIssue(name, { message, fields }); setHostname(name, hostname); getHistory(): Promise<OperationRow[]>;
// validation.ts - each returns a Spanish error string or null
type CreateVmInput = { name: string; memory_mb: number; cpus: number };
validateVmName, validateCreateVm, createVmErrors, validatePackage, validateHostname, validateIssue
// issue-preview.ts
const ISSUE_FIELDS: readonly { key: IssueField; label: string; escape: string }[]; // REQ-26 order
function composeIssue(message: string, fields: IssueField[]): string;
// useOperation.ts
function useOperation(): { pending: boolean; result: { ok: boolean; text: string } | null; run: (fn: () => Promise<string>) => Promise<void> };
// modules.ts
const MODULES: readonly { id: string; title: string; description: string; path: string; enabled: boolean }[];
```
VM forms:
- Each VM form receives `vm: Vm | null` and `onDone(): void`. `onDone` reloads the VM list and the history.
- If `vm` is null, the submit button is disabled and the form shows the `FieldDescription` "Seleccione una VM para continuar".
- While `pending`, every button of the form is disabled and shows a `Spinner`.

`VmSelector` option label: `${name} - ${STATE_LABELS[state]} - ${ip || "-"}`.

Private-key usage text in `UserKeyForm`: keep it private, `chmod 600 id_ed25519_<vm>`, do not share or commit it, and connect with `ssh -i id_ed25519_<vm> root@<ip>`. If the selected VM has `ip === ""`, the text shows `<IP de la VM>` in place of the IP.

## 7. Test-first
- Framework and commands: Go `testing` (table-driven) with `cd stratus-srv && go test ./...`; Vitest + Testing Library with `cd stratus-ui && pnpm test`; bats with `bats -r scripts/` (`-r` also runs `scripts/prepare/lib/vm-exec.bats`).
- Location and naming: tests sit next to the code.
  - Go test names are `TestAC<n>_<Behavior>`, except the `cmd/server` config tests, which cite their AC in the table. Vitest uses `it("AC-<n> ...")` and bats uses `@test "AC-<n> ..."`.
  - UI tests mock `fetch` with `vi.spyOn(globalThis, "fetch")`.
  - Go service tests use a fake `ScriptRunner` and a temp SQLite file.
  - Bats tests use the PATH stubs from `test_helper.bash`. `stub_command` adds canned output, for example `list bridgedifs`, `showmediuminfo`, `showvminfo --machinereadable` with `SATA-ImageUUID-0-0`, and `guestproperty get`.
  - Fixtures: base disk `$BATS_TEST_TMPDIR/base.vdi` (base UUID `base-uuid`, differencing UUID `diff-uuid`), bridge `eth0`, IP `192.168.1.50`.

| Test | Level | File | Covers (US / REQ / AC) | Given - when - then |
|---|---|---|---|---|
| TestAC1_ExecRunnerPassesDiscreteArgs | unit | `stratus-srv/internal/service/script_runner_test.go` | US-1 / REQ-14 / AC-1 | Given a temp script that prints each arg on its own line, when run with `a b; c` and `x`, then stdout has exactly 2 lines `a b; c` and `x` |
| TestAC20_ExecRunnerKillsOnTimeout | unit | `stratus-srv/internal/service/script_runner_test.go` | US-1..US-5 / REQ-14 / AC-20 | Given a script that runs `sleep 30` and a 200 ms timeout, when run, then `TimedOut` is true and it returns in under 2 s |
| TestAC20_ExecRunnerSendsSIGTERMFirst | unit | `stratus-srv/internal/service/script_runner_test.go` | US-1..US-5 / REQ-14 / AC-20 | Given a script with `trap 'echo limpieza >&2; exit 143' TERM` that runs `sleep 30 & wait`, a 1 s timeout (so Homebrew Bash has installed the trap) and `KillGrace` 5 s, when run, then `TimedOut` is true, `ExitCode` is -1, stderr contains `limpieza`, and it returns in under 2 s |
| TestAC20_ExecRunnerKillsAfterGrace | unit | `stratus-srv/internal/service/script_runner_test.go` | US-1..US-5 / REQ-14 / AC-20 | Given a script with `trap '' TERM` that loops on `sleep 1`, a 200 ms timeout and `KillGrace` 300 ms, when run, then `TimedOut` is true and it returns in under 2 s |
| TestAC8_ErrorTypesUnwrapToSentinels (renamed from TestErrorTypesUnwrapToSentinels) | unit | `stratus-srv/internal/service/errors_test.go` | US-1..US-5 / REQ-16 / AC-8 | Given a `ValidationError` and an `OperationError`, when checked with `errors.Is`, then they match `ErrInvalidInput` and `ErrScriptFailed`, and `OperationError.Error()` has the operation, the code and the detail |
| TestAC37_ExecRunnerCapturesPlaceholderExit | unit | `stratus-srv/internal/service/script_runner_test.go` | US-1..US-5 / REQ-21 / AC-37 | Given a placeholder script, when run, then exit code is 99 and stderr is "Operación no implementada" |
| TestAC28_RedactMasksSecrets | unit | `stratus-srv/internal/service/redact_test.go` | US-1..US-5 / REQ-23 / AC-28 | Given stderr with an OpenSSH or RSA private key block, when redacted, then the key body is gone and the surrounding text remains |
| TestAC19_VMLocksRejectsSecondLock | unit | `stratus-srv/internal/service/vm_locks_test.go` | US-1..US-5 / REQ-15 / AC-19 | Given `vm1` locked, when TryLock(`vm1`) again, then false; TryLock(`vm2`) is true |
| TestAC2_ValidateCreateVM | unit | `stratus-srv/internal/service/prepare_validation_test.go` | US-1 / REQ-1 / AC-2 | Given `VM1`, `-vm`, a 33-char name, memory 511/8193, CPUs 0/5, when validated, then each returns `ErrInvalidInput`; `vm1`/2048/2 is valid |
| TestAC7_ValidatePackage | unit | `stratus-srv/internal/service/prepare_validation_test.go` | US-2 / REQ-4 / AC-7 | Given `nginx; rm -rf /` and empty, when validated, then `ErrInvalidInput`; `nginx` and `g++` are valid |
| TestAC15_ValidateIssueTooLong | unit | `stratus-srv/internal/service/prepare_validation_test.go` | US-4 / REQ-10 / AC-15 | Given a 501-rune message, when validated, then `ErrInvalidInput`; 500 runes is valid |
| TestAC16_ValidateIssueEmpty | unit | `stratus-srv/internal/service/prepare_validation_test.go` | US-4 / REQ-10 / AC-16 | Given no fields and an empty message, when validated, then `ErrInvalidInput` |
| TestAC25_ValidateIssueAllowlist | unit | `stratus-srv/internal/service/prepare_validation_test.go` | US-4 / REQ-10 / AC-25 | Given a message with `\`, a tab or `\x07`, when validated, then `ErrInvalidInput`; `Configuración de producción\nlínea 2` is valid |
| TestAC18_ValidateHostname | unit | `stratus-srv/internal/service/prepare_validation_test.go` | US-5 / REQ-12 / AC-18 | Given `Web_01`, `-web`, a 64-char name, when validated, then `ErrInvalidInput`; `web-01` is valid |
| TestAC29_InsertRejectsDuplicate | integration | `stratus-srv/internal/repository/prepare_vms_test.go` | US-1 / REQ-1 / AC-29 | Given `vm1` stored, when Insert(`vm1`), then `ErrDuplicateVM`; Insert(`vm2`) succeeds |
| TestAC39_DeleteClearsFlag | integration | `stratus-srv/internal/repository/prepare_vms_test.go` | US-1 / REQ-27 / AC-39 | Given `vm1` with `user_key_generated` set, when deleted and inserted again, then the flag is false |
| TestAC22_ListRecentReturns50NewestFirst | integration | `stratus-srv/internal/repository/history_test.go` | US-6 / REQ-18 / AC-22 | Given 55 operations, when ListRecent(`prepare`, 50), then 50 rows, the first is the last inserted |
| TestAC1_CreateVMInvokesScriptWithArgs | unit | `stratus-srv/internal/service/prepare_vms_test.go` | US-1 / REQ-1, REQ-14 / AC-1 | Given `Config{BaseDisk: "/vms/base.vdi", BridgeAdapter: "eth0"}` and input `vm1`/2048/2, when CreateVM and the runner stdout is `VM vm1 creada y encendida; IP 192.168.1.50\n`, then the runner gets `prepare/create-vm.sh [vm1 2048 2 /vms/base.vdi eth0]` and the message is that line |
| TestAC2_CreateVMInvalidSkipsScript | unit | `stratus-srv/internal/service/prepare_vms_test.go` | US-1 / REQ-1 / AC-2 | Given CPUs 5, when CreateVM, then `ErrInvalidInput` and the runner is not called |
| TestAC29_CreateVMDuplicateSkipsScript | unit | `stratus-srv/internal/service/prepare_vms_test.go` | US-1 / REQ-1 / AC-29 | Given `vm1` registered, when CreateVM(`vm1`), then `ErrDuplicateVM` "Ya existe una VM con ese nombre" and the runner is not called |
| TestAC41_CreateVMFailureReleasesName | unit | `stratus-srv/internal/service/prepare_vms_test.go` | US-1 / REQ-31 / AC-41, AC-44 | Given the runner exits 6 with stderr `Disco base no encontrado: /vms/base.vdi` (and, in a second case, exits 5 with a bootstrap error), when CreateVM(`vm1`), then `OperationError` whose message contains the stderr line, Get(`vm1`) is `ErrVMNotFound`, and a second CreateVM(`vm1`) reaches the runner |
| TestAC45_CreateVMNeverSeesGuestPassword | unit | `stratus-srv/internal/service/prepare_vms_test.go` | US-1 / REQ-22 / AC-45 | Given `APP_GUEST_PASSWORD_FILE` set to a temp file containing `Pr0ebaClave+/xyz`, when CreateVM runs, then argv has exactly 5 items and neither argv, the history row nor the log buffer contain the password or the file path |
| TestAC3_ListVMsMergesStates | unit | `stratus-srv/internal/service/prepare_vms_test.go` | US-1 / REQ-2 / AC-3 | Given rows `vm1`, `vm2` and list-vms stdout `vm1 running 192.168.1.50`, when ListVMs, then `vm1` running with ip `192.168.1.50` and `vm2` unknown with ip `""`; given list-vms exits 99, then both unknown, ip `""` and `list_error` is set |
| TestAC42_ListVMsParsesIP | unit | `stratus-srv/internal/service/prepare_vms_test.go` | US-1 / REQ-2 / AC-42 | Given stdout `vm1 running 192.168.1.50`, `vm2 poweroff -`, `vm3 running 999.1.1.1`, when ListVMs, then ips are `192.168.1.50`, `""`, `""` |
| TestAC47_StartVMInvokesStartScript | unit | `stratus-srv/internal/service/prepare_vms_test.go` | US-1 / REQ-3 / AC-47 | Given `vm1` registered and the runner exiting 0, when StartVM, then the runner gets `prepare/start-vm.sh [vm1]` and the message is "VM vm1 encendida"; given the runner exits 3, then `OperationError` with code 3 and a history row with result `error` |
| TestAC30_StopVMInvokesStopScript | unit | `stratus-srv/internal/service/prepare_vms_test.go` | US-1 / REQ-3 / AC-30 | Given `vm1`, when StopVM, then the runner gets `prepare/stop-vm.sh [vm1]` and a success message |
| TestAC31_DeleteVMRemovesRow | unit | `stratus-srv/internal/service/prepare_vms_test.go` | US-1 / REQ-3 / AC-31 | Given `vm1`, when DeleteVM succeeds, then the runner gets `prepare/delete-vm.sh [vm1]` and Get(`vm1`) is `ErrVMNotFound` |
| TestAC39_DeleteVMClearsFlags | unit | `stratus-srv/internal/service/prepare_vms_test.go` | US-1 / REQ-27 / AC-39 | Given `vm1` with `user_key_generated` and a pending key, when DeleteVM succeeds, then the row is gone, a new `vm1` has the flag false, and DownloadUserKey returns `ErrKeyUnavailable` |
| TestAC19_SameVMOperationRejected | unit | `stratus-srv/internal/service/prepare_vms_test.go` | US-1..US-5 / REQ-15 / AC-19 | Given a fake runner blocked on `vm1`, when StartVM(`vm1`) runs twice concurrently, then the second returns `ErrOperationInProgress` |
| TestAC46_SecondCreateRejected (renamed from TestAC19_SecondCreateRejected) | unit | `stratus-srv/internal/service/prepare_vms_test.go` | US-1 / REQ-32 / AC-46 | Given a fake runner blocked inside CreateVM(`vm1`), when CreateVM(`vm2`) runs concurrently, then it returns `ErrCreateInProgress` "Ya hay una creación de VM en curso", the runner was called once, Get(`vm2`) is `ErrVMNotFound`, and after `vm1` finishes a CreateVM(`vm2`) reaches the runner; StartVM(`vm3`) during the blocked create is not rejected |
| TestAC46_InvalidCreateDoesNotTakeLock (renamed from TestAC19_InvalidCreateDoesNotTakeLock) | unit | `stratus-srv/internal/service/prepare_vms_test.go` | US-1 / REQ-1, REQ-32 / AC-2, AC-46 | Given a create of `vm1` blocked in the runner, when CreateVM with CPUs 5 runs, then it returns `ErrInvalidInput` (validation first), not `ErrCreateInProgress` |
| TestAC8_FailedScriptRecordsHistory | unit | `stratus-srv/internal/service/prepare_vms_test.go` | US-2, US-6 / REQ-16, REQ-17 / AC-8 | Given the runner returns exit 5 and stderr `boom`, when an operation runs, then `OperationError` with code 5 and a history row with exit 5, stderr `boom`, result `error` |
| TestAC20_TimeoutRecordedInHistory | unit | `stratus-srv/internal/service/prepare_vms_test.go` | US-1..US-5 / REQ-14 / AC-20 | Given the runner returns `TimedOut`, when an operation runs, then `ErrTimeout` and a history row with result `timeout`, exit -1 |
| TestAC37_PlaceholderRecordedInHistory | integration | `stratus-srv/internal/service/prepare_vms_test.go` | US-1..US-5 / REQ-21 / AC-37 | Given `ExecRunner` on a temp dir with a placeholder `prepare/start-vm.sh`, when StartVM, then `OperationError` and the history row has exit 99 and stderr "Operación no implementada" |
| TestAC28_HistoryAndLogsHaveNoSecrets | unit | `stratus-srv/internal/service/prepare_vms_test.go` | US-1..US-5 / REQ-22, REQ-23 / AC-28 | Given a runner whose stderr has a private key block, when an operation runs, then neither the history args/stderr nor the log buffer contain it |
| TestAC43_NoIPExitMapsToErrVMNoIP | unit | `stratus-srv/internal/service/prepare_vms_test.go` | US-2..US-5 / REQ-29 / AC-43 | Given the runner exits 7 with the REQ-29 stderr, when InstallPackage, SetHostname, SetIssue and GenerateUserKey run on `vm1`, then each error satisfies `errors.Is(err, ErrVMNoIP)`, each history row has exit 7 and result `error`, and DownloadUserKey returns `ErrKeyUnavailable` |
| TestAC6_InstallRemoveInvokeScriptWithPackage | unit | `stratus-srv/internal/service/prepare_packages_test.go` | US-2 / REQ-4 / AC-6 | Given `vm1`, when InstallPackage/RemovePackage `nginx`, then argv is `[vm1 nginx]` for the matching script |
| TestAC7_InvalidPackageSkipsScript | unit | `stratus-srv/internal/service/prepare_packages_test.go` | US-2 / REQ-4 / AC-7 | Given `nginx; rm -rf /`, when InstallPackage, then `ErrInvalidInput` and the runner is not called |
| TestAC32_InstallPassesOnlyNameAndPackage | unit | `stratus-srv/internal/service/prepare_packages_test.go` | US-2 / REQ-24 / AC-32 | Given `vm1` just created, when InstallPackage `openssh-server` succeeds, then argv is exactly `[vm1 openssh-server]` and the message is "Paquete openssh-server instalado en vm1" |
| TestAC33_RemoveOpenSSHBlocked | unit | `stratus-srv/internal/service/prepare_packages_test.go` | US-2 / REQ-25 / AC-33 | Given `vm1` just created, when RemovePackage `openssh-server`, then `ErrSSHRemovalBlocked` and the runner is not called |
| TestAC34_InstallUfwPassesPackage | unit | `stratus-srv/internal/service/prepare_packages_test.go` | US-2 / REQ-25 / AC-34 | Given `vm1`, when InstallPackage `ufw`, then argv is `[vm1 ufw]` |
| TestAC6_CatalogHasEightPackages | unit | `stratus-srv/internal/service/prepare_packages_test.go` | US-2 / REQ-5 / AC-6 | Given the catalog, when read, then it equals the 8 spec packages, all passing ValidatePackage |
| TestAC9_KeyStoreReturnsKey | unit | `stratus-srv/internal/service/prepare_key_store_test.go` | US-3 / REQ-8 / AC-9 | Given Put(`vm1`, key), when Take(`vm1`), then the key |
| TestAC10_KeyStoreSecondTakeFails | unit | `stratus-srv/internal/service/prepare_key_store_test.go` | US-3 / REQ-8 / AC-10 | Given one Take done, when Take again, then `ErrKeyUnavailable` |
| TestAC11_KeyStoreExpires | unit | `stratus-srv/internal/service/prepare_key_store_test.go` | US-3 / REQ-8 / AC-11 | Given TTL 20 ms and Put, when Take after 50 ms, then `ErrKeyUnavailable` |
| TestAC9_GenerateUserKeyKeepsStdoutInMemory | unit | `stratus-srv/internal/service/prepare_keys_test.go` | US-3 / REQ-6, REQ-8 / AC-9 | Given the runner stdout is a key, when GenerateUserKey succeeds, then DownloadUserKey returns that key |
| TestAC13_PrivateKeyNotInHistoryOrLogs | unit | `stratus-srv/internal/service/prepare_keys_test.go` | US-3 / REQ-17 / AC-13 | Given the runner stdout is a key, when GenerateUserKey runs, then no history row field and no log line contain the key |
| TestAC35_GenerateUserKeySetsFlag | unit | `stratus-srv/internal/service/prepare_keys_test.go` | US-3 / REQ-6, REQ-27 / AC-35 | Given `vm1` just created, when GenerateUserKey succeeds, then the runner is called once with `prepare/generate-user-key.sh [vm1]` and `user_key_generated` is true |
| TestAC27_RegenerateReplacesKey | unit | `stratus-srv/internal/service/prepare_keys_test.go` | US-3 / REQ-6, REQ-23 / AC-27 | Given the flag set and a pending key, when GenerateUserKey succeeds with a new stdout, then argv is `[vm1]` and DownloadUserKey returns the new key |
| TestAC36_EnsureBackendKeyRunsScriptWhenMissing | unit | `stratus-srv/internal/service/backend_key_test.go` | US-3 / REQ-23 / AC-36 | Given no file at keyPath, when EnsureBackendKey, then the runner gets `prepare/create-backend-key.sh [keyPath]`; given the file exists, then the runner is not called |
| TestAC14_SetIssueCanonicalOrder | unit | `stratus-srv/internal/service/prepare_system_test.go` | US-4 / REQ-10, REQ-26 / AC-14 | Given fields `[users, hostname]` and message `Hola`, when SetIssue, then argv is `[vm1 Hola hostname users]` |
| TestAC15_SetIssueTooLongSkipsScript | unit | `stratus-srv/internal/service/prepare_system_test.go` | US-4 / REQ-10 / AC-15 | Given 501 runes, when SetIssue, then `ErrInvalidInput` and the runner is not called |
| TestAC16_SetIssueEmptySkipsScript | unit | `stratus-srv/internal/service/prepare_system_test.go` | US-4 / REQ-10 / AC-16 | Given no fields and empty message, when SetIssue, then `ErrInvalidInput` and the runner is not called |
| TestAC25_SetIssueBackslashSkipsScript | unit | `stratus-srv/internal/service/prepare_system_test.go` | US-4 / REQ-10 / AC-25 | Given a message with `\`, when SetIssue, then `ErrInvalidInput` and the runner is not called |
| TestAC17_SetHostnameInvokesScript | unit | `stratus-srv/internal/service/prepare_system_test.go` | US-5 / REQ-12 / AC-17 | Given `web-01`, when SetHostname, then argv is `[vm1 web-01]` |
| TestAC18_InvalidHostnameSkipsScript | unit | `stratus-srv/internal/service/prepare_system_test.go` | US-5 / REQ-12 / AC-18 | Given `Web_01`, when SetHostname, then `ErrInvalidInput` and the runner is not called |
| TestAC1_PostVMsReturnsCreated | integration | `stratus-srv/internal/handler/prepare_test.go` | US-1 / REQ-1, REQ-16 / AC-1 | Given a fake runner exiting 0 with stdout `VM vm1 creada y encendida; IP 192.168.1.50`, when `POST /api/prepare/vms` with `{"name":"vm1","memory_mb":2048,"cpus":2}`, then 201 and `{"message"}` with that text |
| TestAC3_GetVMsReturnsStates | integration | `stratus-srv/internal/handler/prepare_test.go` | US-1 / REQ-2 / AC-3 | Given 2 VMs, when `GET /api/prepare/vms`, then each JSON VM has exactly `name`, `state`, `ip`, `user_key_generated` |
| TestAC7_InvalidPackageReturns400 | integration | `stratus-srv/internal/handler/prepare_test.go` | US-2 / REQ-4 / AC-7 | Given `{"package":"nginx; rm -rf /"}`, when POST install-package, then 400 `{"error"}` and the runner is not called |
| TestAC6_GetCatalogReturnsPackages | integration | `stratus-srv/internal/handler/prepare_test.go` | US-2 / REQ-5 / AC-6 | Given the catalog, when `GET /api/prepare/catalog`, then `packages` has the 8 entries |
| TestAC19_BusyVMReturns409 | integration | `stratus-srv/internal/handler/prepare_test.go` | US-1..US-5 / REQ-15 / AC-19 | Given a blocked operation on `vm1`, when another POST on `vm1`, then 409 `{"error":"Operación en curso en esta VM"}` |
| TestAC46_ConcurrentCreateReturns409 (renamed from TestAC19_ConcurrentCreateReturns409) | integration | `stratus-srv/internal/handler/prepare_test.go` | US-1 / REQ-32, REQ-16 / AC-46 | Given a `POST /api/prepare/vms` for `vm1` blocked in the runner, when another `POST /api/prepare/vms` for `vm2`, then 409 `{"error":"Ya hay una creación de VM en curso"}` |
| TestAC47_PostStartReturnsMessage | integration | `stratus-srv/internal/handler/prepare_test.go` | US-1 / REQ-3, REQ-16 / AC-47 | Given `vm1` registered and the runner exiting 0, when `POST /api/prepare/vms/vm1/start`, then 200 `{"message":"VM vm1 encendida"}` and the runner got `prepare/start-vm.sh [vm1]`; given the runner exits 3, then 500 `{"error"}` containing "código 3" |
| TestAC20_TimeoutReturns504 | integration | `stratus-srv/internal/handler/prepare_test.go` | US-1..US-5 / REQ-14, REQ-16 / AC-20 | Given the runner times out, when POST start, then 504 with the timeout message |
| TestAC29_DuplicateReturns409 | integration | `stratus-srv/internal/handler/prepare_test.go` | US-1 / REQ-1 / AC-29 | Given `vm1` registered, when POST vms `vm1`, then 409 "Ya existe una VM con ese nombre" |
| TestAC41_BaseDiskMissingReturnsMessage | integration | `stratus-srv/internal/handler/prepare_test.go` | US-1 / REQ-31, REQ-16 / AC-41 | Given the runner exits 6 with stderr `Disco base no encontrado: /vms/base.vdi`, when POST vms, then 500 `{"error"}` containing "Disco base no encontrado" |
| TestAC33_RemoveOpenSSHReturns409 | integration | `stratus-srv/internal/handler/prepare_test.go` | US-2 / REQ-25 / AC-33 | Given `vm1` just created, when POST remove-package `openssh-server`, then 409 with the REQ-25 message |
| TestAC43_NoIPReturns409 | integration | `stratus-srv/internal/handler/prepare_test.go` | US-2..US-5 / REQ-29 / AC-43 | Given the runner exits 7, when POST install-package `nginx`, then 409 `{"error":"La VM no tiene dirección IP; verifique que esté encendida"}` |
| TestAC10_SecondDownloadReturns410 | integration | `stratus-srv/internal/handler/prepare_test.go` | US-3 / REQ-8 / AC-10 | Given a generated key downloaded once, when GET download again, then 410 "La llave ya no está disponible, genere una nueva" |
| TestAC37_PlaceholderReturns500Message | integration | `stratus-srv/internal/handler/prepare_test.go` | US-1..US-5 / REQ-16, REQ-21 / AC-37 | Given the runner exits 99 "Operación no implementada", when POST start, then 500 `{"error"}` containing "código 99" and no stack trace |
| TestAC17_PostHostnameReturnsMessage | integration | `stratus-srv/internal/handler/prepare_test.go` | US-5 / REQ-12 / AC-17 | Given `{"hostname":"web-01"}`, when POST hostname, then 200 `{message}` |
| TestAC18_InvalidHostnameReturns400 | integration | `stratus-srv/internal/handler/prepare_test.go` | US-5 / REQ-12 / AC-18 | Given `{"hostname":"Web_01"}`, when POST hostname, then 400 and the runner is not called |
| TestAC14_PostIssueReturnsMessage | integration | `stratus-srv/internal/handler/prepare_test.go` | US-4 / REQ-10 / AC-14 | Given a valid `IssueInput`, when POST issue, then 200 `{message}` |
| TestAC25_BackslashIssueReturns400 | integration | `stratus-srv/internal/handler/prepare_test.go` | US-4 / REQ-10 / AC-25 | Given a message with `\`, when POST issue, then 400 and the runner is not called |
| TestAC22_GetHistoryReturnsRows | integration | `stratus-srv/internal/handler/prepare_test.go` | US-6 / REQ-18 / AC-22 | Given 55 operations, when `GET /api/prepare/history`, then 50 `operations` newest first |
| TestLoadConfigUsesDefaults | unit | `stratus-srv/cmd/server/config_test.go` | US-1 / REQ-1 / AC-26 | Given the 4 required variables set and the optional ones empty, when loadConfig, then the defaults apply and `BaseDisk`, `BridgeAdapter`, `GuestPasswordFile`, `SSHKeyPath` hold the env values |
| TestLoadConfigRequiresVariables | unit | `stratus-srv/cmd/server/config_test.go` | US-1 / REQ-1, REQ-22 / AC-26, AC-45 | Given each of `APP_BASE_DISK`, `APP_BRIDGE_ADAPTER`, `APP_GUEST_PASSWORD_FILE`, `APP_SSH_KEY_PATH` empty in turn, when loadConfig, then an error naming it |
| TestLoadConfigIgnoresRemovedVariables | unit | `stratus-srv/cmd/server/config_test.go` | US-1 / REQ-1 / AC-26 | Given `APP_DEBIAN_ISO`, `APP_GUEST_USER`, `APP_GUEST_SECRETS_DIR` empty, when loadConfig, then no error |
| TestAC45_LoadConfigDoesNotOpenPasswordFile | unit | `stratus-srv/cmd/server/config_test.go` | US-1 / REQ-22 / AC-45 | Given `APP_GUEST_PASSWORD_FILE` pointing to a file with mode 000, when loadConfig, then no error and the config holds only the path |
| AC-23 shows four module cards, only Preparar sistema navigates | component | `stratus-ui/src/routes/Home.test.tsx` | US-7 / REQ-19, REQ-20 / AC-23 | Given `/`, when rendered, then 4 cards; a link named "Preparar sistema" to `/prepare`; the other 3 have "Próximamente" and no link |
| AC-2 validateCreateVm rejects invalid values | unit | `stratus-ui/src/features/prepare/validation.test.ts` | US-1 / REQ-1 / AC-2 | Same cases as TestAC2_ValidateCreateVM, then an error string; `vm1`/2048/2 gives no errors |
| AC-7 validatePackage rejects injection and empty | unit | `stratus-ui/src/features/prepare/validation.test.ts` | US-2 / REQ-4 / AC-7 | Given `nginx; rm -rf /` or empty, then an error string |
| AC-15 validateIssue rejects > 500 chars | unit | `stratus-ui/src/features/prepare/validation.test.ts` | US-4 / REQ-10 / AC-15 | Given 501 chars, then an error string |
| AC-16 validateIssue rejects nothing selected | unit | `stratus-ui/src/features/prepare/validation.test.ts` | US-4 / REQ-10 / AC-16 | Given no fields and empty message, then an error string |
| AC-25 validateIssue allowlist | unit | `stratus-ui/src/features/prepare/validation.test.ts` | US-4 / REQ-10 / AC-25 | Given `\` or a tab, then an error; `Configuración de producción\nlínea 2`, then null |
| AC-18 validateHostname rejects invalid | unit | `stratus-ui/src/features/prepare/validation.test.ts` | US-5 / REQ-12 / AC-18 | Given `Web_01`, `-web`, 64 chars, then an error; `web-01`, then null |
| AC-14 composeIssue orders message then fields with labels | unit | `stratus-ui/src/features/prepare/issue-preview.test.ts` | US-4 / REQ-26 / AC-14 | Given message `Hola` and fields `[users, hostname]`, then 3 lines: `Hola`, `Host: \n`, `Usuarios: \U` (escapes kept literal) |
| AC-3 lists each VM with its state and IP | component | `stratus-ui/src/features/prepare/VmSelector.test.tsx` | US-1 / REQ-2 / AC-3 | Given VMs `vm1` running with ip `192.168.1.50` and `vm2` unknown with ip `""`, when opened, then options show "vm1 - Encendida - 192.168.1.50" and "vm2 - Desconocido - -" |
| AC-42 shows - for a VM without IP | component | `stratus-ui/src/features/prepare/VmSelector.test.tsx` | US-1 / REQ-2 / AC-42 | Given `vm1` running with `192.168.1.50` and `vm2` poweroff with ip `""`, when opened, then "vm1 - Encendida - 192.168.1.50" and "vm2 - Apagada - -" |
| AC-3 shows SQLite VMs when listing fails | component | `stratus-ui/src/routes/Prepare.test.tsx` | US-1 / REQ-2 / AC-3 | Given `list_error` set and VMs with state `unknown`, when the screen loads, then the selector lists them and an `Alert` shows the error |
| AC-26 create card describes the base-disk clone | component | `stratus-ui/src/routes/Prepare.test.tsx` | US-1 / REQ-1, REQ-20 / AC-26 | Given the screen, when rendered, then the "Crear VM" card description mentions "disco base" and "red en puente", and has no "Instalación desatendida" |
| AC-1 submits create form and shows result | component | `stratus-ui/src/features/prepare/CreateVmForm.test.tsx` | US-1 / REQ-1, REQ-16 / AC-1 | Given valid values, when Crear VM is clicked, then fetch POST body is exactly `{name, memory_mb, cpus}`, no "Disco" field is rendered, and the success `Alert` shows the message with the IP |
| AC-2 shows validation error and does not call API | component | `stratus-ui/src/features/prepare/CreateVmForm.test.tsx` | US-1 / REQ-1 / AC-2 | Given name `-vm`, when submitted, then an error under the field and fetch not called |
| AC-29 shows duplicate message | component | `stratus-ui/src/features/prepare/CreateVmForm.test.tsx` | US-1 / REQ-1 / AC-29 | Given the API answers 409 "Ya existe una VM con ese nombre", when submitted, then that text shows |
| AC-41 shows base disk error | component | `stratus-ui/src/features/prepare/CreateVmForm.test.tsx` | US-1 / REQ-31 / AC-41 | Given the API answers 500 `{"error":"La operación create_vm falló (código 6): Disco base no encontrado: /vms/base.vdi"}`, when submitted, then a destructive `Alert` contains "Disco base no encontrado" |
| AC-46 shows create-in-progress message | component | `stratus-ui/src/features/prepare/CreateVmForm.test.tsx` | US-1 / REQ-32, REQ-16 / AC-46 | Given the API answers 409 `{"error":"Ya hay una creación de VM en curso"}`, when Crear VM is submitted with `vm2`, then a destructive `Alert` with that text |
| AC-21 shows spinner and disables buttons while pending | component | `stratus-ui/src/features/prepare/CreateVmForm.test.tsx` | US-1..US-5 / REQ-14 / AC-21 | Given fetch unresolved, when submitted, then a loading indicator shows and the submit button is disabled |
| AC-4 delete asks for confirmation first | component | `stratus-ui/src/features/prepare/VmActions.test.tsx` | US-1 / REQ-3 / AC-4 | Given `vm1` selected, when Eliminar is clicked, then a dialog shows and fetch DELETE is called only after Confirmar |
| AC-38 delete dialog states the base disk is untouched | component | `stratus-ui/src/features/prepare/VmActions.test.tsx` | US-1 / REQ-3, REQ-31 / AC-38 | Given `vm1` selected, when Eliminar is clicked, then the dialog text contains "disco diferencial" and "El disco base compartido no se modifica" |
| AC-5 disables actions without selected VM | component | `stratus-ui/src/features/prepare/VmActions.test.tsx` | US-1 / REQ-2 / AC-5 | Given `vm` null, when rendered, then Iniciar, Detener, Eliminar are disabled and "Seleccione una VM para continuar" shows |
| AC-47 start shows result | component | `stratus-ui/src/features/prepare/VmActions.test.tsx` | US-1 / REQ-3, REQ-16 / AC-47 | Given a powered-off `vm1` and the API answering 200 `{"message":"VM vm1 encendida"}`, when Iniciar is clicked, then fetch POST `/api/prepare/vms/vm1/start`, the success `Alert` shows and `onDone` is called; given the API answers 500 with "código 3", then a destructive `Alert` |
| AC-30 stop shows result | component | `stratus-ui/src/features/prepare/VmActions.test.tsx` | US-1 / REQ-3 / AC-30 | Given the API answers 200, when Detener is clicked, then the success `Alert` shows |
| AC-31 confirmed delete shows result | component | `stratus-ui/src/features/prepare/VmActions.test.tsx` | US-1 / REQ-3 / AC-31 | Given confirmation and 200, then the success `Alert` shows and `onDone` is called |
| AC-6 install from catalog sends package | component | `stratus-ui/src/features/prepare/PackageForm.test.tsx` | US-2 / REQ-4, REQ-5 / AC-6 | Given catalog entry `nginx` chosen, when Instalar is clicked, then fetch POST install-package `{"package":"nginx"}` and the result shows; Eliminar posts to remove-package |
| AC-7 rejects injected package name | component | `stratus-ui/src/features/prepare/PackageForm.test.tsx` | US-2 / REQ-4 / AC-7 | Given `nginx; rm -rf /` or empty, when submitted, then an error and fetch not called |
| AC-8 shows failure message | component | `stratus-ui/src/features/prepare/PackageForm.test.tsx` | US-2 / REQ-16 / AC-8 | Given the API answers 500 `{"error":"La operación ... falló (código 5): ..."}`, then a destructive `Alert` with that text |
| AC-33 shows openssh-server removal block | component | `stratus-ui/src/features/prepare/PackageForm.test.tsx` | US-2 / REQ-25 / AC-33 | Given the API answers 409 with the REQ-25 message, when Eliminar `openssh-server`, then that message shows |
| AC-5 package form disabled without VM | component | `stratus-ui/src/features/prepare/PackageForm.test.tsx` | US-2 / REQ-2 / AC-5 | Given `vm` null, then the buttons are disabled with the hint |
| AC-43 shows no-IP message | component | `stratus-ui/src/features/prepare/PackageForm.test.tsx` | US-2 / REQ-29 / AC-43 | Given a powered-off `vm1` (ip `""`) and the API answering 409 "La VM no tiene dirección IP; verifique que esté encendida", when Instalar `nginx`, then a destructive `Alert` with that text |
| AC-9 shows download button and usage message | component | `stratus-ui/src/features/prepare/UserKeyForm.test.tsx` | US-3 / REQ-9 / AC-9 | Given `vm1` with ip `192.168.1.50` and generation answering 200, then "Descargar llave privada" and a message with "600", "no la comparta" and `ssh -i id_ed25519_vm1 root@192.168.1.50` show, and no `-p` or `127.0.0.1` |
| AC-10 second download shows unavailable | component | `stratus-ui/src/features/prepare/UserKeyForm.test.tsx` | US-3 / REQ-8 / AC-10 | Given download answers 410, then "La llave ya no está disponible, genere una nueva" |
| AC-11 expired key shows unavailable | component | `stratus-ui/src/features/prepare/UserKeyForm.test.tsx` | US-3 / REQ-8 / AC-11 | Given the first download answers 410 (expired), then the same message |
| AC-12 regenerate asks for confirmation | component | `stratus-ui/src/features/prepare/UserKeyForm.test.tsx` | US-3 / REQ-7 / AC-12 | Given `user_key_generated` true, when Generar llave is clicked, then a dialog warns the old key stops working and fetch runs only after Confirmar |
| AC-43 shows no-IP message | component | `stratus-ui/src/features/prepare/UserKeyForm.test.tsx` | US-3 / REQ-29 / AC-43 | Given a powered-off `vm1` (ip `""`, flag false) and generation answering 409 "La VM no tiene dirección IP; verifique que esté encendida", when Generar llave, then a destructive `Alert` with that text and no "Descargar llave privada" button |
| AC-14 live preview updates | component | `stratus-ui/src/features/prepare/IssueForm.test.tsx` | US-4 / REQ-11, REQ-26 / AC-14 | Given typing `Hola` and checking Usuarios then Host, then the preview shows `Hola`, `Host: \n`, `Usuarios: \U` in that order |
| AC-15 long message shows error | component | `stratus-ui/src/features/prepare/IssueForm.test.tsx` | US-4 / REQ-10 / AC-15 | Given 501 chars, when Aplicar, then an error and fetch not called |
| AC-16 empty issue shows error | component | `stratus-ui/src/features/prepare/IssueForm.test.tsx` | US-4 / REQ-10 / AC-16 | Given nothing checked and empty message, when Aplicar, then an error and fetch not called |
| AC-25 backslash message rejected | component | `stratus-ui/src/features/prepare/IssueForm.test.tsx` | US-4 / REQ-10 / AC-25 | Given `a\b`, when Aplicar, then an error and fetch not called |
| AC-43 shows no-IP message | component | `stratus-ui/src/features/prepare/IssueForm.test.tsx` | US-4 / REQ-29 / AC-43 | Given a powered-off `vm1` (ip `""`) and the API answering 409 "La VM no tiene dirección IP; verifique que esté encendida", when Aplicar with message `Hola`, then a destructive `Alert` with that text |
| AC-17 submits hostname | component | `stratus-ui/src/features/prepare/HostnameForm.test.tsx` | US-5 / REQ-12, REQ-13 / AC-17 | Given `web-01`, when Aplicar, then fetch POST hostname `{"hostname":"web-01"}` and the result shows |
| AC-18 invalid hostname shows error | component | `stratus-ui/src/features/prepare/HostnameForm.test.tsx` | US-5 / REQ-12 / AC-18 | Given `Web_01`, when Aplicar, then an error and fetch not called |
| AC-20 shows timeout failure | component | `stratus-ui/src/features/prepare/HostnameForm.test.tsx` | US-5 / REQ-14, REQ-16 / AC-20 | Given the API answers 504 with the timeout message, then a destructive `Alert` with it |
| AC-43 shows no-IP message | component | `stratus-ui/src/features/prepare/HostnameForm.test.tsx` | US-5 / REQ-29 / AC-43 | Given a powered-off `vm1` (ip `""`) and the API answering 409 "La VM no tiene dirección IP; verifique que esté encendida", when Aplicar `web-01`, then a destructive `Alert` with that text |
| AC-22 lists rows newest first and expands stderr | component | `stratus-ui/src/features/prepare/HistoryTable.test.tsx` | US-6 / REQ-18 / AC-22 | Given 50 rows ordered newest first, when rendered, then 50 rows in that order; clicking a row shows its stderr |
| AC-8 failed row shows exit code and stderr | component | `stratus-ui/src/features/prepare/HistoryTable.test.tsx` | US-6 / REQ-17 / AC-8 | Given a row with exit 5 and stderr `boom`, when expanded, then `5` and `boom` show |
| AC-37 placeholder failure row | component | `stratus-ui/src/features/prepare/HistoryTable.test.tsx` | US-6 / REQ-21 / AC-37 | Given a row with exit 99 and "Operación no implementada", when expanded, then both show |
| AC-24 `<script>` has header, exit codes, strict mode, sections | script | `scripts/prepare/<script>.bats` (each of the 11 scripts) | US-1..US-5 / REQ-21 / AC-24 | Given the file, when read, then it has `Uso:`, `Argumentos:`, `Códigos de salida:` with `99`, `set -euo pipefail` after the header, and a `# == 1.` section with `# 1.1` steps. In `start-vm.bats`, `stop-vm.bats` and `create-backend-key.bats` the name drops "y placeholder" (`AC-24 <script>.sh tiene cabecera, códigos de salida, modo estricto y secciones`) |
| AC-14 vm_exec quotes each argument with printf %q (renamed from AC-7) | script | `scripts/prepare/lib/vm-exec.bats` | US-4 / REQ-10, REQ-24, P3 / AC-14 | Given `stub_vm_ip vm1 192.168.1.50` and a recording `ssh` stub, when `vm_exec vm1 echo 'a b; rm -rf /'`, then the last argument that `ssh` receives contains `echo a\ b\;\ rm\ -rf\ /` (each argument `%q`-quoted, so the space and the `;` are not split by the remote shell) |
| AC-8 vm_exec maps ssh 255 to 4 and remote failure to 5 | script | `scripts/prepare/lib/vm-exec.bats` | US-2..US-5 / REQ-16, REQ-24 / AC-8 | Given the `ssh` stub exits 255, when `vm_exec vm1 true`, then it returns 4; given it exits 1, then it returns 5 |
| AC-48 vm_exec returns 2 without APP_SSH_KEY_PATH or without a command (renamed from AC-8) | script | `scripts/prepare/lib/vm-exec.bats` | US-2..US-5 / REQ-24 / AC-48 | Given `APP_SSH_KEY_PATH` unset, or `vm_exec vm1` with no command, then it returns 2 and neither VBoxManage nor `ssh` is called |
| AC-43 vm_ip returns 7 for a stopped VM with a stale IP | script | `scripts/prepare/lib/vm-exec.bats` | US-2..US-5 / REQ-29 / AC-43 | Given `showvminfo` reports `VMState="poweroff"` and `guestproperty get` still prints `Value: 192.168.1.50`, when `vm_ip vm1`, then it returns 7, stdout is empty, and stderr has "La VM no tiene dirección IP; verifique que esté encendida"; given `running` and the value, then it prints `192.168.1.50` and returns 0 |
| AC-1 create-vm passes arguments to VBoxManage | script | `scripts/prepare/create-vm.bats` | US-1 / REQ-1 / AC-1 | Given stubs for a valid base disk, bridge `eth0` and IP `192.168.1.50`, when `create-vm.sh vm1 2048 2 <base> eth0`, then `calls.log` has `createvm --name vm1`, a `modifyvm vm1` with `--memory 2048 --cpus 2`, exit 0 and stdout `VM vm1 creada y encendida; IP 192.168.1.50` |
| AC-48 create-vm rejects invalid arguments (renamed from AC-2) | script | `scripts/prepare/create-vm.bats` | US-1 / REQ-1, REQ-21 / AC-2, AC-48 | Given `-vm`, `VM1`, CPUs 5, bridge `eth0;x`, a relative base path or 6 arguments, when run, then exit 2 and no VBoxManage or `ssh` call |
| AC-26 create-vm attaches the base disk multiattach on a bridged NIC | script | `scripts/prepare/create-vm.bats` | US-1 / REQ-1 / AC-26 | Given a valid run, then `calls.log` has `--nic1 bridged --bridgeadapter1 eth0`, `storageattach vm1 --storagectl SATA --port 0 --device 0 --type hdd --mtype multiattach --medium <base>`, and no `natpf1` and no `createmedium` |
| AC-26 create-vm registers a normal unattached base through storageattach | script | `scripts/prepare/create-vm.bats` | US-1 / REQ-1 / AC-26 | Given the base starts as `Type: normal`, then after `createvm` it runs, in order, `storageattach ... --mtype normal --medium <base>`, `storageattach ... --medium none`, and `storageattach ... --mtype multiattach --medium <base>`, with no `modifymedium`; given it already starts multiattach, only the last attachment runs |
| AC-38 create-vm recovers an orphaned multiattach base | script | `scripts/prepare/create-vm.bats` | US-1 / REQ-1, REQ-31 / AC-38 | Given the base reports `Type: multiattach` without `In use by VMs:` or `Child UUIDs:`, then it runs `modifymedium disk <base> --type normal` before the normal attach/detach/multiattach registration cycle; an active multiattach base is never modified |
| AC-44 create-vm cleans up when storageattach cannot register multiattach | script | `scripts/prepare/create-vm.bats` | US-1 / REQ-1, REQ-31 / AC-44 | Given `storageattach --mtype multiattach` exits 1, when run, then exit 4, stderr has "No se pudo conectar el disco base", the newly registered VM is removed, and no `modifymedium` call occurs |
| AC-41 create-vm rejects a VM already registered in VirtualBox | script | `scripts/prepare/create-vm.bats` | US-1 / REQ-31 / AC-41 | Given `showvminfo vm1` succeeds, when run, then exit 6, stderr has "VM ya registrada en VirtualBox", and no `createvm`, `unregistervm` or `closemedium` call |
| AC-40 create-vm forces poweroff when the VM ignores ACPI during the restart | script | `scripts/prepare/create-vm.bats` | US-1 / REQ-30 / AC-40 | Given the VM state stays `running` after `acpipowerbutton`, when run, then `controlvm vm1 poweroff` is logged before the second `startvm` |
| AC-44 create-vm cleans up when the backend key does not open SSH | script | `scripts/prepare/create-vm.bats` | US-1 / REQ-31 / AC-44 | Given `ssh` always exits 255, when run, then exit 5, stderr has "La llave del backend no abrió una sesión SSH" and the cleanup lines |
| AC-44 create-vm bounds its waits to 14 minutes | script | `scripts/prepare/create-vm.bats` | US-1 / REQ-14, REQ-31 / AC-20, AC-44 | Given stubs where GA readiness and SSH succeed only on their last try and ACPI is ignored, when run, then exit 0; `calls.log` has `guestproperty wait ... --timeout 180000` twice, every `guestcontrol vm1 run` line has `--timeout` (`10000` for `/bin/true`, `60000` for the bootstrap), 8 `/bin/true` tries, 8 `ssh` tries each with `ConnectTimeout=10`; and the sum of the wait timeouts, the guestcontrol timeouts, `ConnectTimeout` per ssh try and the stubbed `sleep` seconds is at most 730 |
| AC-40 create-vm bootstraps the VM as root through GA | script | `scripts/prepare/create-vm.bats` | US-1 / REQ-22, REQ-30 / AC-40 | Given a valid run, then the bootstrap `guestcontrol vm1 run` line has `--username root --passwordfile $APP_GUEST_PASSWORD_FILE`, the backend public key body, `/root/.ssh/authorized_keys`, `PasswordAuthentication no`, `PermitRootLogin prohibit-password`, `systemd-machine-id-setup`, `/var/lib/dhcpcd/duid`, `ssh_host_`, `ssh-keygen -A`, `hostnamectl set-hostname`, `127.0.1.1` and `vm1` as a separate argument |
| AC-40 create-vm restarts and checks SSH on the IP | script | `scripts/prepare/create-vm.bats` | US-1 / REQ-1, REQ-30 / AC-40 | Given a valid run, then after the bootstrap `calls.log` has, in order, `controlvm vm1 acpipowerbutton`, `guestproperty delete vm1 /VirtualBox/GuestInfo/Net/0/V4/IP`, `startvm vm1 --type headless`, `ssh-keygen -R vm1 -f $APP_SSH_KEY_PATH.known_hosts`, and an `ssh` line with `HostKeyAlias=vm1`, `-i $APP_SSH_KEY_PATH`, `root@192.168.1.50` and `true` |
| AC-41 create-vm rejects a missing base disk | script | `scripts/prepare/create-vm.bats` | US-1 / REQ-31 / AC-41 | Given the base path does not exist, when run, then exit 6, stderr has "Disco base no encontrado" and no `createvm` call |
| AC-41 create-vm rejects a missing bridge interface | script | `scripts/prepare/create-vm.bats` | US-1 / REQ-31 / AC-41 | Given `list bridgedifs` lists only `Name: wlan0`, when run with `eth0`, then exit 6, stderr has "Interfaz de puente no encontrada" and no `createvm` call |
| AC-44 create-vm cleans up after a failed bootstrap | script | `scripts/prepare/create-vm.bats` | US-1 / REQ-31 / AC-44 | Given the bootstrap `guestcontrol` exits 1, when run, then exit 5; `calls.log` has `controlvm vm1 poweroff`, `storageattach vm1 ... --medium none`, `closemedium disk diff-uuid --delete`, `unregistervm vm1 --delete`; no `closemedium` or `modifymedium` line after `createvm` names the base path or `base-uuid`; the base file still exists with the same checksum |
| AC-44 create-vm cleans up when no IP appears | script | `scripts/prepare/create-vm.bats` | US-1 / REQ-31 / AC-44 | Given `guestproperty get .../V4/IP` prints `No value set!`, when run, then exit 5, stderr has "no obtuvo una dirección IP" and the same cleanup lines |
| AC-45 create-vm never exposes the root password | script | `scripts/prepare/create-vm.bats` | US-1 / REQ-22 / AC-45 | Given `setup_guest_password`, when run (successful and failing bootstrap), then `GUEST_PASSWORD` is not in `calls.log`, stdout or stderr; given `APP_GUEST_PASSWORD_FILE` unset, then exit 2 and no VBoxManage call |
| AC-3 list-vms prints name, state and IP | script | `scripts/prepare/list-vms.bats` | US-1 / REQ-2 / AC-3 | Given the stub lists `vm1` running with `Value: 192.168.1.50` and `vm2` off, when run, then stdout is `vm1 running 192.168.1.50` and `vm2 poweroff -` |
| AC-42 list-vms prints - for a running VM without IP | script | `scripts/prepare/list-vms.bats` | US-1 / REQ-2 / AC-42 | Given `vm1` running and `guestproperty get` prints `No value set!`, when run, then stdout is `vm1 running -` |
| AC-3 list-vms does not query the IP of a stopped VM | script | `scripts/prepare/list-vms.bats` | US-1 / REQ-2 / AC-3 | Given `vm2` off, when run, then no `guestproperty get vm2` line |
| AC-42 list-vms prints - when the property is not an IPv4 or the query fails | script | `scripts/prepare/list-vms.bats` | US-1 / REQ-2 / AC-42 | Given `Value: 999.1.1.1`, or `guestproperty get` exits 1, then the third field is `-` |
| AC-3 list-vms exits 4 when VBoxManage fails (split from the exit 4/2 test) | script | `scripts/prepare/list-vms.bats` | US-1 / REQ-2, REQ-16 / AC-3 | Given `list vms` exits 1, then exit 4 (the backend then shows the SQLite VMs with `list_error`) |
| AC-48 list-vms rejects arguments (split from the exit 4/2 test) | script | `scripts/prepare/list-vms.bats` | US-1 / REQ-2 / AC-48 | Given any argument, then exit 2 and no VBoxManage call |
| AC-47 start-vm starts the VM headless (renamed from AC-38) | script | `scripts/prepare/start-vm.bats` | US-1 / REQ-3 / AC-47 | Given `vm1` registered and `VMState="poweroff"`, when `start-vm.sh vm1`, then `startvm vm1 --type headless` is logged, exit 0 and stdout "VM vm1 encendida" |
| AC-47 start-vm does nothing if the VM is already running (renamed from AC-38) | script | `scripts/prepare/start-vm.bats` | US-1 / REQ-3 / AC-47 | Given `VMState="running"`, when run, then exit 0, stdout "La VM vm1 ya estaba encendida" and no `startvm` call |
| AC-47 start-vm exits 3 when the VM does not exist (split from the AC-8 exit 3/2 test) | script | `scripts/prepare/start-vm.bats` | US-1 / REQ-3, REQ-16 / AC-47 | Given `showvminfo vm1` exits 1, when run, then exit 3 and no `startvm` call |
| AC-48 start-vm rejects invalid arguments (split from the AC-8 exit 3/2 test) | script | `scripts/prepare/start-vm.bats` | US-1 / REQ-3 / AC-48 | Given no argument, two arguments or `VM1`, when run, then exit 2 and no VBoxManage or `ssh` call |
| AC-48 stop-vm rejects invalid arguments | script | `scripts/prepare/stop-vm.bats` | US-1 / REQ-3 / AC-48 | Given no argument, two arguments or `VM1`, when run, then exit 2 and no VBoxManage or `ssh` call |
| AC-30 stop-vm forces poweroff after 60 s | script | `scripts/prepare/stop-vm.bats` | US-1 / REQ-3 / AC-30 | Given the stub always reports running, when run, then `acpipowerbutton` then `poweroff` are logged and the stubbed waits add up to 60 s |
| AC-31 delete-vm powers off, removes the differencing disk and unregisters | script | `scripts/prepare/delete-vm.bats` | US-1 / REQ-3 / AC-31 | Given `vm1` running with `SATA-ImageUUID-0-0="diff-uuid"` whose parent is `base-uuid`, when run, then in order `controlvm vm1 poweroff`, `storageattach vm1 --storagectl SATA --port 0 --device 0 --medium none`, `closemedium disk diff-uuid --delete`, `unregistervm vm1 --delete`, and exit 0 |
| AC-31 delete-vm tolerates a differencing disk already removed | script | `scripts/prepare/delete-vm.bats` | US-1 / REQ-3 / AC-31 | Given `closemedium` exits 1, when run, then `unregistervm vm1 --delete` is still logged and exit 0; given `showvminfo` fails, then exit 0 and "no existía" |
| AC-31 delete-vm does not power off a stopped VM | script | `scripts/prepare/delete-vm.bats` | US-1 / REQ-3 / AC-31 | Given `VMState="poweroff"`, when run, then no `controlvm vm1 poweroff` line and exit 0 |
| AC-31 delete-vm exits 4 when it cannot unregister | script | `scripts/prepare/delete-vm.bats` | US-1 / REQ-3 / AC-31 | Given `unregistervm` exits 1, when run, then exit 4 |
| AC-48 delete-vm rejects invalid arguments (renamed from AC-2) | script | `scripts/prepare/delete-vm.bats` | US-1 / REQ-3 / AC-48 | Given no argument, two arguments or `VM1`, when run, then exit 2 and no VBoxManage or `ssh` call |
| AC-38 delete-vm never touches the base disk | script | `scripts/prepare/delete-vm.bats` | US-1 / REQ-3, REQ-31 / AC-38 | Given the attached medium reports `Parent UUID: base`, when run, then no `closemedium` call; in every delete test no line has `base-uuid` with `closemedium` or `modifymedium` |
| AC-6 install-package installs over SSH to the IP | script | `scripts/prepare/install-package.bats` | US-2 / REQ-4, REQ-24 / AC-6 | Given `stub_vm_ip vm1 192.168.1.50`, when `install-package.sh vm1 nginx`, then an `ssh` line has `root@192.168.1.50`, `HostKeyAlias=vm1` and `apt-get install -y nginx` |
| AC-7 install-package rejects injection | script | `scripts/prepare/install-package.bats` | US-2 / REQ-4 / AC-7, AC-48 | Given `nginx; rm -rf /` or empty, when run, then exit 2 and no stub calls |
| AC-48 install-package rejects a wrong argument count or an invalid VM (renamed from AC-7) | script | `scripts/prepare/install-package.bats` | US-2 / REQ-4 / AC-48 | Given 1 or 3 arguments, or `VM1`, when run, then exit 2 and no stub calls |
| AC-34 install-package does not touch ufw for other packages (renamed from AC-6) | script | `scripts/prepare/install-package.bats` | US-2 / REQ-25 / AC-34 | Given `nginx`, when run, then no `ssh` line has `ufw` |
| AC-8 install-package exits 3 when the VM does not exist (renamed, was unprefixed) | script | `scripts/prepare/install-package.bats` | US-2 / REQ-4, REQ-16 / AC-8 | Given `showvminfo vm1` exits 1, when run, then exit 3 and no `ssh` line |
| AC-8 install-package exits 5 when apt-get fails and 4 without SSH (renamed, was unprefixed) | script | `scripts/prepare/install-package.bats` | US-2 / REQ-4, REQ-16 / AC-8 | Given `ssh` exits 100, then exit 5; given `ssh` exits 255, then exit 4 |
| AC-32 openssh-server is a plain install over SSH | script | `scripts/prepare/install-package.bats` | US-2 / REQ-24 / AC-32 | Given `vm1` with IP, when installing `openssh-server`, then exit 0, the `ssh` lines use `-i $APP_SSH_KEY_PATH` and `root@192.168.1.50`, and `calls.log` has no `guestcontrol` |
| AC-34 ufw allows 22/tcp | script | `scripts/prepare/install-package.bats` | US-2 / REQ-25 / AC-34 | Given `vm1` with IP, when installing `ufw`, then the `ssh` lines have `apt-get install -y ufw` and `ufw allow 22/tcp` |
| AC-43 install-package without IP | script | `scripts/prepare/install-package.bats` | US-2 / REQ-29 / AC-43 | Given `stub_vm_ip vm1 -`, when run, then exit 7, stderr has "La VM no tiene dirección IP; verifique que esté encendida" and no `ssh` line |
| AC-6 remove-package purges over SSH | script | `scripts/prepare/remove-package.bats` | US-2 / REQ-4 / AC-6 | Given `vm1` with IP, when removing `nginx`, then an `ssh` line to `root@192.168.1.50` has `apt-get purge -y nginx` |
| AC-7 remove-package rejects injection | script | `scripts/prepare/remove-package.bats` | US-2 / REQ-4 / AC-7, AC-48 | Given `nginx; rm -rf /` or empty, when run, then exit 2 |
| AC-48 remove-package rejects a wrong argument count or an invalid VM | script | `scripts/prepare/remove-package.bats` | US-2 / REQ-4 / AC-48 | Given 1 or 3 arguments, or `VM1 nginx`, when run, then exit 2 and no stub calls |
| AC-33 remove-package rejects openssh-server | script | `scripts/prepare/remove-package.bats` | US-2 / REQ-25 / AC-33, AC-48 | Given `openssh-server`, when run, then exit 2 and no stub calls |
| AC-43 remove-package without IP | script | `scripts/prepare/remove-package.bats` | US-2 / REQ-29 / AC-43 | Given `stub_vm_ip vm1 -`, when removing `nginx`, then exit 7 and no `ssh` line |
| AC-8 remove-package exits 3 when the VM does not exist and 5 when apt-get fails (renamed, was unprefixed) | script | `scripts/prepare/remove-package.bats` | US-2 / REQ-4, REQ-16 / AC-8 | Given `showvminfo vm1` exits 1, then exit 3; given `ssh` exits 100, then exit 5 |
| AC-9 generate-user-key prints only the private key | script | `scripts/prepare/generate-user-key.bats` | US-3 / REQ-6 / AC-9 | Given `vm1` with IP, when `generate-user-key.sh vm1`, then stdout starts with `-----BEGIN OPENSSH PRIVATE KEY-----` and has nothing else |
| AC-13 generate-user-key leaves no key on stderr or disk | script | `scripts/prepare/generate-user-key.bats` | US-3 / REQ-8 / AC-13 | Given a run, then stderr has no `PRIVATE KEY` and the temp dir no longer exists |
| AC-27 regenerate replaces user key and keeps backend key | script | `scripts/prepare/generate-user-key.bats` | US-3 / REQ-6, REQ-23 / AC-27 | Given a run, then the `ssh` lines remove lines tagged `stratus-user`, append the new `stratus-user` key and keep or add the `stratus-backend` key |
| AC-35 generate-user-key uses only SSH | script | `scripts/prepare/generate-user-key.bats` | US-3 / REQ-6, REQ-24 / AC-35 | Given `vm1` just created (IP set), when run, then every remote call is an `ssh` line to `root@192.168.1.50`, the user key is added beside the backend key, no `guestcontrol` and no `apt-get` |
| AC-43 generate-user-key without IP | script | `scripts/prepare/generate-user-key.bats` | US-3 / REQ-29 / AC-43 | Given `stub_vm_ip vm1 -`, when run, then exit 7, empty stdout, no `ssh` and no `ssh-keygen` line |
| AC-48 generate-user-key rejects invalid arguments (split from the AC-8 test) | script | `scripts/prepare/generate-user-key.bats` | US-3 / REQ-6 / AC-48 | Given no argument, `VM1` or `vm1 extra`, then exit 2, empty stdout and no VBoxManage, `ssh` or `ssh-keygen` call |
| AC-8 generate-user-key exits 3 when the VM does not exist (split from the AC-8 test) | script | `scripts/prepare/generate-user-key.bats` | US-3 / REQ-6, REQ-16 / AC-8 | Given `showvminfo vm1` exits 1, then exit 3 and empty stdout |
| AC-14 set-issue writes message then labelled fields | script | `scripts/prepare/set-issue.bats` | US-4 / REQ-10, REQ-26 / AC-14 | Given `vm1 Hola hostname users` and an IP, when run, then the content sent over `ssh` to `/etc/issue` is `Hola`, `Host: \n`, `Usuarios: \U` |
| AC-25 set-issue rejects backslash or tab | script | `scripts/prepare/set-issue.bats` | US-4 / REQ-10 / AC-25, AC-48 | Given message `a\b` or with a tab, when run, then exit 2; tildes, ñ and line breaks are accepted |
| AC-14 set-issue without message writes only the fields | script | `scripts/prepare/set-issue.bats` | US-4 / REQ-10, REQ-26 / AC-14 | Given `vm1 "" hostname`, when run, then the content sent is only `Host: \n` |
| AC-15 set-issue rejects > 500 chars, an unknown field and an empty request | script | `scripts/prepare/set-issue.bats` | US-4 / REQ-10 / AC-15, AC-16, AC-48 | Given a 501-char message, a field `foo`, or no message and no fields, when run, then exit 2 |
| AC-48 set-issue rejects a missing argument or an invalid VM | script | `scripts/prepare/set-issue.bats` | US-4 / REQ-10 / AC-48 | Given no arguments, only `vm1`, or `VM1 Hola`, when run, then exit 2 and no stub calls |
| AC-43 set-issue without IP | script | `scripts/prepare/set-issue.bats` | US-4 / REQ-29 / AC-43 | Given `stub_vm_ip vm1 -`, when run, then exit 7 and no `ssh` line |
| AC-17 set-hostname sets hostname and hosts line | script | `scripts/prepare/set-hostname.bats` | US-5 / REQ-12, REQ-13 / AC-17 | Given `vm1 web-01` and an IP, when run, then the `ssh` lines have `hostnamectl set-hostname web-01` and a `127.0.1.1` replace-or-append of `web-01` in `/etc/hosts` |
| AC-18 set-hostname rejects invalid name | script | `scripts/prepare/set-hostname.bats` | US-5 / REQ-12 / AC-18, AC-48 | Given `Web_01`, `-web` or 64 chars, when run, then exit 2 |
| AC-48 set-hostname rejects a wrong argument count (renamed from AC-18) | script | `scripts/prepare/set-hostname.bats` | US-5 / REQ-12 / AC-48 | Given 1 or 3 arguments, when run, then exit 2 and no stub calls |
| AC-43 set-hostname without IP | script | `scripts/prepare/set-hostname.bats` | US-5 / REQ-29 / AC-43 | Given `stub_vm_ip vm1 -`, when run, then exit 7 and no `ssh` line |
| AC-36 create-backend-key creates pair with mode 600 | script | `scripts/prepare/create-backend-key.bats` | US-3 / REQ-23 / AC-36 | Given a temp path, when run with real `ssh-keygen`, then the private and `.pub` files exist and the private file mode is 600 |
| AC-48 create-backend-key rejects a missing or relative key path | script | `scripts/prepare/create-backend-key.bats` | US-3 / REQ-23 / AC-48 | Given no argument, two arguments or a relative path, when run with stubs, then exit 2 and no `ssh-keygen`, VBoxManage or `ssh` call |
| Host demo: clones, independence and base disk integrity | manual (real host, P8) | - | US-1 / REQ-1, REQ-3, REQ-30, REQ-31 / AC-26, AC-38, AC-40, AC-44 | Given the real base disk and `sha256sum` of it, when `vm-a` and `vm-b` are created from the UI, then `showvminfo` shows a differencing disk and bridged NIC 1 (AC-26); their `/etc/machine-id` and `ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub` differ and the AC-40 settings hold; after deleting `vm-a` the checksum is unchanged and `vm-b` restarts and answers SSH (AC-38); with a wrong password file a creation fails and leaves no VM or differencing disk (AC-44) |

## 8. Traceability
| Requirement | Component / file | Verified by (AC) |
|---|---|---|
| REQ-1 | `create-vm.sh`, `VMRepository.Insert`, `PrepareService.CreateVM`, `loadConfig` (`APP_BASE_DISK`, `APP_BRIDGE_ADAPTER`), `CreateVmForm.tsx` | AC-1, AC-2, AC-26, AC-29, AC-40 |
| REQ-2 | `list-vms.sh` (state and IPv4), `PrepareService.ListVMs`, `VmSelector.tsx`, `Prepare.tsx` | AC-3, AC-5, AC-42 |
| REQ-3 | `start-vm.sh`, `stop-vm.sh`, `delete-vm.sh` (differencing disk only), `PrepareService.Start/Stop/DeleteVM`, `VmActions.tsx` | AC-4, AC-30, AC-31, AC-38, AC-47 |
| REQ-4 | `install-package.sh`, `remove-package.sh`, `ValidatePackage`, `PackageForm.tsx` | AC-6, AC-7 |
| REQ-5 | `prepare_catalog.go`, `GET /api/prepare/catalog`, `PackageForm.tsx` | AC-6 |
| REQ-6 | `generate-user-key.sh`, `PrepareService.GenerateUserKey` | AC-9, AC-27, AC-35 |
| REQ-7 | `UserKeyForm.tsx` (`AlertDialog`) | AC-12 |
| REQ-8 | `KeyStore`, `DownloadUserKey`, download endpoint | AC-9, AC-10, AC-11, AC-13 |
| REQ-9 | `UserKeyForm.tsx` usage `Alert` (`root@<ip>`) | AC-9 |
| REQ-10 | `ValidateIssue`, `validation.ts`, `set-issue.sh`, `IssueForm.tsx` | AC-14, AC-15, AC-16, AC-25 |
| REQ-11 | `issue-preview.ts`, `IssueForm.tsx` | AC-14 |
| REQ-12 | `ValidateHostname`, `set-hostname.sh`, `HostnameForm.tsx` | AC-17, AC-18 |
| REQ-13 | `set-hostname.sh` | AC-17 |
| REQ-14 | `ExecRunner` (15 min; SIGTERM to the process group, SIGKILL after a 30 s grace), `http.Server` `WriteTimeout`, nginx/Vite proxy timeouts, `create-vm.sh` time budget (at most 14 min, `ga_root --timeout`), `useOperation.ts` | AC-1, AC-20, AC-21 |
| REQ-15 | `VMLocks`, handler 409 | AC-19 |
| REQ-16 | `respond.go`, `errors.go` (sentinels and wrapping), `OperationResult.tsx`, `GET /healthz` (deploy liveness) | AC-1, AC-8, AC-20, AC-37, AC-41 |
| REQ-17 | `HistoryRepository.Insert`, `runOperation` | AC-8, AC-13 |
| REQ-18 | `HistoryRepository.ListRecent`, `GET /api/prepare/history`, `HistoryTable.tsx` | AC-22 |
| REQ-19 | `modules.ts`, `ModuleCard.tsx`, `Home.tsx`, `App.tsx` | AC-23 |
| REQ-20 | `src/components/ui/*`, every UI component (Spanish text) | AC-23, AC-3, AC-9 |
| REQ-21 | all `scripts/prepare/*.sh` (step 1 argument validation, exit 2 before any VBoxManage or ssh call), `lib/vm-exec.sh`, `test_helper.bash`, `lib/vm-exec.bats` | AC-24, AC-37, AC-48 |
| REQ-22 | `create-vm.sh` (`ga_root` with `--passwordfile "$APP_GUEST_PASSWORD_FILE"`), `loadConfig` (requires, never opens), `Redact`, `runOperation` | AC-28, AC-40, AC-44, AC-45 |
| REQ-23 | `create-backend-key.sh`, `EnsureBackendKey`, `vm-exec.sh` (ssh with `APP_SSH_KEY_PATH`, pinned `HostKeyAlias`), `generate-user-key.sh` | AC-27, AC-28, AC-36 |
| REQ-24 | `vm-exec.sh` (`vm_exec` to `root@<ip>:22`), `install-package.sh`, `guestArgs` | AC-32, AC-35 |
| REQ-25 | `RemovePackage` block, `remove-package.sh` (exit 2), `install-package.sh` (ufw branch) | AC-33, AC-34 |
| REQ-26 | `ISSUE_FIELDS`/`composeIssue`, canonical order in `SetIssue`, `set-issue.sh` | AC-14 |
| REQ-27 | `vms.user_key_generated`, `SetUserKeyGenerated`, `Delete` | AC-35, AC-39 |
| REQ-29 | `vm_ip` (exit 7), guest scripts' precheck, `runOperation` exit 7 to `ErrVMNoIP`, handler 409 | AC-43 |
| REQ-30 | `create-vm.sh` (`BOOTSTRAP`, power cycle, IP wait, SSH check) | AC-38, AC-40 |
| REQ-31 | `create-vm.sh` (checks before `createvm`, guarded cleanup that also runs on SIGTERM), `delete-vm.sh` guard, `CreateVM` releasing the row, `VmActions.tsx` delete dialog text | AC-38, AC-41, AC-44 |
| REQ-32 | `PrepareService.createMu` (`TryLock` after validation, before `Insert`), `ErrCreateInProgress`, handler 409, `CreateVmForm.tsx` error `Alert` | AC-46 |

## 9. Decisions and risks
- Backend native on the host, UI in docker-compose with an explicit localhost port publication and `host.docker.internal` for the proxy. Rejected `network_mode: host`: it does not publish the UI on Docker Desktop for macOS. Rejected a containerized backend: it needs `/dev/vboxdrv`, the VBoxSVC IPC socket and a matching VirtualBox version. Rejected dropping Docker entirely, because AGENTS.md deploys the UI with compose. The backend and published UI bind only to localhost, because the app has no authentication.
- Differencing disks over a multi-attach base disk. Rejected full `clonevm`: it is slow and copies the whole disk per VM. Rejected linked clones from a snapshot: they need a registered template VM. Rejected the unattended install: it was slow and not standardized.
- A normal base disk is first attached normally and detached, then registered as `multiattach` by a second `storageattach` after `createvm` and `storagectl`; the cleanup trap removes the new VM if any attachment fails. Rejected `modifymedium`: VirtualBox 7.2 refuses `MultiAttach` both before registration and while the normal disk is attached (`VBOX_E_INVALID_OBJECT_STATE`).
- `modifyvm --ioapic on`: VirtualBox needs it to start a VM with more than 1 CPU.
- The GA bootstrap is one `guestcontrol run` of a constant `sh -c` script, with the public key and VM name as positional arguments. This complies with P3, keeps one failure point and logs one auditable line. Rejected one GA call per step: more round trips and partial states. Rejected a separate `lib/` helper for GA: `create-vm.sh` is its only user (P9).
- Root password through `--passwordfile "$APP_GUEST_PASSWORD_FILE"` only. Rejected passing it in argv, where `ps` shows it and history would need redaction. Rejected having the backend read it, which would put the secret in the backend process for no need (REQ-22). `loadConfig` checks only that the variable is set.
- Host-side power cycle after the bootstrap, not an in-guest `reboot`. The IP property is deleted while the VM is off, so the script can never read the address from before the new machine-id and DUID. ACPI first, with a forced poweroff after 60 s, the same rule as `stop-vm.sh`.
- Host keys pinned per VM name: `HostKeyAlias=<vm_name>`, `StrictHostKeyChecking=accept-new`, `UserKnownHostsFile=$APP_SSH_KEY_PATH.known_hosts`. `create-vm.sh` runs `ssh-keygen -R <vm_name>` before its first SSH check. Keys stay verified on a shared LAN, and DHCP IP changes or reuse do not matter. Rejected `StrictHostKeyChecking=no` with `/dev/null`, which gives no verification. Rejected keying by IP, which breaks when another VM inherits an IP. `delete-vm.sh` leaves the entry: it is harmless, and the next creation with that name removes it.
- The IP is resolved at run time by `vm-exec.sh`, not stored in SQLite or passed by the backend. DHCP may change it between boots. Rejected passing the IP as argv: it can go stale between the list and the run, and the backend would have to query VirtualBox (P1). `vm_ip` also requires `VMState="running"`, so a stale property on a stopped VM is never used.
- Exit code 7 is reserved for "no IP" in the guest scripts and mapped once, in `runOperation`, to `ErrVMNoIP` (409). Rejected a per-operation mapping, which would be duplicated 5 times. `create-vm.sh` reports a missing IP with 5, because it is a creation failure.
- Creation errors with exit 6 (base disk, bridge) reach the UI through `OperationError.Detail`, the last stderr line. Rejected new sentinels: the script message is already the user-facing text.
- The CreateVM success message is the script's stdout, which includes the IP. The backend does not know the IP.
- `ip` is `""` when unknown, and the UI renders `-`. Rejected `"-"` from the backend (the display rule belongs in the UI) and `null` (a pointer type for no gain).
- Removing openssh-server is always blocked, in the service and again in `remove-package.sh` (exit 2). Every VM depends on SSH from creation onward.
- Breaking schema change: the old `stratus.db` is deleted by hand, documented in `.env.example`. Rejected migration code (out of scope in the spec).
- History records only mutating operations. Rejected recording `list-vms.sh` calls, which would push real operations out of the 50-row view.
- The private key travels on script stdout and stays only in backend memory. Rejected writing it to a backend file, which conflicts with P5.
- One Go package per layer with `prepare_*.go` files. `modernc.org/sqlite` (pure Go, no cgo). Plain controlled forms plus `validation.ts`, no zod or react-hook-form (P9). The UI reads the catalog from the backend (REQ-5). The regeneration confirmation (REQ-7) uses `user_key_generated` from the VM list.
- A failed `create-backend-key.sh` at startup is logged and does not stop the server.
- One creation at a time, through a service-wide `createMu.TryLock()` taken after validation and before `Insert`. Every creation registers or attaches the shared base disk. Two concurrent `storageattach` calls on the same medium can fail or race. A second create gets 409 "Ya hay una creación de VM en curso" right away. Rejected a queue: a request could wait up to 15 min and then hit the proxy timeouts. Rejected a per-name lock only: it does not protect the shared disk. Operations on other VMs (start, stop, packages) are not blocked (REQ-32, AC-46).
- AC mapping of the script tests:
  - Generic argument checks (argument count, VM name `VM1`, relative base path, bad bridge name, no command for `vm_exec`, any argument for `list-vms.sh`) are AC-48 tests.
  - Script tests that use the exact example values of another AC keep that AC and also cite AC-48 in "Covers": AC-7 injection or empty package, AC-15, AC-18 hostnames, AC-25, AC-33 `openssh-server`. They are the script side of the same P3 double check, and renaming them would hide their link to the AC that defines the values.
  - The `%q` quoting test of `vm_exec` is AC-14: an `/etc/issue` message with spaces and `;` must reach the guest literally.
- Timeout kill is graceful: SIGTERM to the process group, SIGKILL after `KillGrace` = 30 s. `create-vm.sh` maps TERM to exit 143, so its `EXIT` trap removes the half-created VM and its differencing disk. With an immediate SIGKILL the trap never ran and left a registered VM, which blocked the name. The response stays `TimedOut` and 504 (AC-20). `WriteTimeout` 16 min still covers 15 min + 30 s.
- `create-vm.sh` has a 14-minute budget (section 6). Every wait is bounded, including `guestcontrol run --timeout`, so the script normally ends with its own clear message before the backend's 15-minute timeout. Rejected keeping the 240 s waits and 24/18 retries: in the worst case, with unbounded `guestcontrol` calls and `ConnectTimeout` counted, they went past 15 min.
- `GET /healthz` stays outside `/api/prepare`. It is the liveness check for the deploy and has no system effect.
- Risk: `unregistervm --delete` could remove a multi-attach parent. Mitigation:
  - Both scripts detach and `closemedium` the differencing image (only a medium with a parent) before `unregistervm`.
  - The real-host demo compares `sha256sum` of the base disk before and after a delete (AC-38).
- Risk: the base disk is attached to another VM as a normal disk, or booted directly. Mitigation: `storageattach --mtype multiattach` fails, the cleanup removes the new VM, and creation returns exit 4. `.env.example` and the header of `create-vm.sh` state the requirement.
- Risk: the GA version property appears before guest control accepts sessions. Mitigation: `ga_root 10000 /bin/true` is retried 8 times, 5 s apart (up to 120 s), before the bootstrap.
- Risk: `guestcontrol run --timeout` bounds the guest process but may not bound session start or logon in every VirtualBox version. Mitigation: the backend's 15-minute SIGTERM still stops the script, and the cleanup trap runs within the 30 s grace. The AC-44 budget test checks the bounds that the script passes, not VirtualBox behaviour.
- Risk: the cleanup takes longer than the 30 s grace (for example a slow `unregistervm`), and SIGKILL interrupts it. Mitigation: the cleanup is a few VBoxManage calls plus `wait_stopped` (10 s). A VM left behind makes the next create of that name exit 6 "VM ya registrada en VirtualBox", and the user deletes it.
- Risk: a wrong root password in `APP_GUEST_PASSWORD_FILE`. Mitigation: the readiness retries end in exit 5 with the full cleanup (AC-44), and stderr shows the VBoxManage logon error.
- Risk: the LAN gives no DHCP lease to bridged VMs (Wi-Fi bridging limits, MAC filtering). Mitigation: the IP wait fails with exit 5 and a clear message, and `.env.example` recommends a wired interface for `APP_BRIDGE_ADAPTER`.
- Risk: the guest's first interface (`Net/0`) is not the bridged NIC. Mitigation: the base image has a single NIC (documented requirement).
- Risk: clones share a DHCP identity and get the same IP. Mitigation: the bootstrap regenerates `/etc/machine-id` and removes the dhclient leases and the dhcpcd DUID and leases before the power cycle. AC-40 is checked on the real host.
- Risk: an old `stratus.db` (with `ssh_port NOT NULL`) makes every insert fail. Mitigation: the deletion note in `.env.example`, and the demo step deletes it.
- Risk: VMs created with the old design remain in VirtualBox. Accepted: they are deleted by hand (out of scope).
- Risk: proxies cut 15-minute requests. Mitigation: nginx and the Vite proxy use 960 s timeouts, and `http.Server.WriteTimeout` is 16 min.
- Risk: the backend restarts while a key is pending, and the key is lost. Accepted: the user regenerates it (P5 forbids persisting it).
- Risk: bats stubs only check the logged commands, not a real guest. Mitigation: the real-host demo in section 7 (P8).

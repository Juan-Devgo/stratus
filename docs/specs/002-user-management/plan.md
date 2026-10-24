# Plan: User management

## 1. Metadata
- Spec: `docs/specs/002-user-management/spec.md`
- Updated: 2026-10-22
- Constitution version: 2.0.0

## 2. Constitution check
- P1: the backend validates input, writes the password to a host temp file, picks the script, passes argv and maps the exit code to a message. Every action on the VM (lookups, `useradd`, sudoers, `chpasswd`, keys, `pkill`, `find`, `userdel`) lives in `scripts/users/*.sh`. The temp file and the in-memory `KeyStore` are app bookkeeping on the host, not VM operations.
- P2: there are three scripts, one per operation (`list-users.sh`, `create-user.sh`, `delete-user.sh`). Each has `set -euo pipefail`, positional arguments, results on stdout, diagnostics on stderr, and the exit-code table of section 6 with a distinct code per failure type. The password arrives as the path of a secret file given in argv (REQ-9). Create rejects an existing user and delete rejects a missing user with their own codes, as the spec defines.
- P3: the backend calls `exec.CommandContext` with argv items and never uses `bash -c` locally. The same rules are in `stratus-ui/src/features/users/validation.ts`, `stratus-srv/internal/service/users_validation.go` and step "1. Validar argumentos" of each script. Remote commands go through `vm_exec`, which quotes each argument with `printf '%q '`. Constant remote `sh -c`/`bash -c` programs receive the username and UID as positional arguments, never interpolated.
- P4: every operation runs through `vm_exec` (SSH as root with the backend key to the VM IPv4). Key users get `passwd -l`. SSH password login stays off for everyone because 001 `10-stratus.conf` is never touched, so password users log in only at the console or with `su`.
- P5: the user's private key exists only in the script's temp dir (deleted on exit), then in the script's stdout, then in the users `KeyStore`. The `KeyStore` wipes it after one download, after 10 minutes, or when that user's delete succeeds. stdout is never logged or stored. `UserKeyDownload` shows the usage message.
- P6: every operation returns `{message}` or `{error}`. `operations` rows with module `users` keep the command, argv (secret file path only), exit code, redacted stderr, result and timestamp for create, delete and failed lists.
- P7: the home card "Gestión de usuarios" opens `/users`, the single screen of the module. The create form confirms its result with `OperationResult`, and every list load confirms with a toast.
- P8: each script runs standalone with argv and env, and has bats tests that use PATH stubs (`bats -r scripts/`). The before/after demo on a real VM covers what stubs cannot: AC-9, AC-10, AC-11, AC-18, AC-20, AC-21, AC-22, AC-23 and AC-31.
- P9: there are two new dependencies. `sonner` comes through the shadcn Sonner component, approved for the REQ-18 toast. No backend dependency is added. The only new abstraction is `operationRunner`, which holds the existing `runOperation` so both modules share one per-VM lock (AC-24).

## 3. Architecture
- Scripts: `scripts/users/` holds one script per operation. Each sources `../prepare/lib/vm-exec.sh` for `vm_ip` and `vm_exec`. The steps are the same as the 001 guest scripts: validate args (exit 2), `VBoxManage showvminfo <vm>` (exit 3), `vm_ip` (exit 7), then remote steps through a `guest <code> <cmd...>` wrapper that passes 2, 4 and 7 through.
- Password path (REQ-9, AC-12):
  1. `UsersService.CreateUser` writes the password with `os.CreateTemp("", "stratus-password-*")` (mode 0600, no trailing newline) and closes the file.
  2. It passes the file path as the last argv item and removes the file with `defer os.Remove`, on success or failure.
  3. `create-user.sh` reads the file into a shell variable with `IFS= read -r` and validates it.
  4. It runs `printf '%s:%s\n' "$username" "$password" | vm_exec <vm> chpasswd`. `printf` is a bash builtin, so the password is in no host process argv. ssh forwards its stdin to the remote `chpasswd`, whose argv is only `chpasswd`. The password is never printed.
- Data flow of a mutating operation (create, delete):
  1. The UI validates with `validation.ts` and calls `features/users/api.ts`.
  2. The handler decodes the request and calls `UsersService`.
  3. The service validates the VM name and the input, loads the VM row (`VMRepository.Get`, else `ErrVMNotFound`), and builds argv.
  4. `operationRunner.run` takes the shared `VMLocks` (any module, AC-24), runs the script through `ExecRunner` (15 min, process group, SIGTERM then SIGKILL), redacts stderr, writes the history row and logs a summary.
  5. The service maps exit codes to sentinel errors. Create with a key puts stdout into the `KeyStore` under `<vm>/<username>`. A successful delete discards that key.
  6. The handler maps the result to an HTTP status. The UI shows `OperationResult` and reloads the users list, which shows the toast, and the history.
- List: `GET /api/users/vms/{vm}/users` runs `list-users.sh` through `operationRunner.run` with `recordSuccess=false`. It still takes the VM lock. Only a failure (including no IP and timeout) writes a history row (REQ-19, AC-30). The UI loads the list on VM selection and after each create or delete. On success it shows the toast "Lista de usuarios actualizada". On failure it shows an `Alert` with the error text.
- Shared lock: `main.go` creates one `*service.VMLocks` and passes it to `NewPrepareService` and `NewUsersService`.
- State:
  - SQLite: the `operations` rows with module `users`. The schema does not change.
  - Backend memory: the shared `VMLocks` and the users `KeyStore`.
  - VM: the users, groups, homes, `/etc/sudoers.d/stratus-<username>` and `authorized_keys`.
  - UI: `Users.tsx` holds the selected VM, the users, the history and the list error. `CreateUserForm` holds the user whose key can still be downloaded.
- Base disk: it is expected to include the `sudo` package (documented with the 001 base disk requirements). `create-user.sh` installs `sudo` only as a fallback when the sudo option is `yes` and `command -v sudo` fails.

## 4. Design patterns
- Shared executor `operationRunner` in `internal/service/operation_runner.go`: one implementation of lock, run, history, log and exit-7 mapping for both modules. It also gives one per-VM lock across modules (REQ-17, AC-24) and lets successful lists skip the history (REQ-19).
- Strategy via `ScriptRunner` (existing): users service and handler tests use a fake runner.
- `KeyStore` (existing) reused as a second instance keyed `<vm>/<username>`, so private-key handling has one implementation.
- Sourced helper `scripts/prepare/lib/vm-exec.sh` (existing): it is reused from `scripts/users/` without moving it.
- Cleanup trap in `create-user.sh`: once `useradd` succeeds, an `EXIT` trap removes the partial user on any failure (REQ-12).

## 5. Code structure
Scripts (headers, comments and output in Spanish):
- create: `scripts/users/list-users.sh` - prints regular users with their sudo and key flags.
- create: `scripts/users/create-user.sh` - creates the user with optional sudo and either a password or a key. It has the cleanup trap.
- create: `scripts/users/delete-user.sh` - checks the UID range, kills the user's processes, optionally purges the user's files, then removes the sudo rule and the account.
- create: `scripts/users/test_helper.bash` - loads `../prepare/test_helper.bash` and adds `setup_users_stubs`, `setup_password_file` and `run_remote_locally` (section 6).
- create: `scripts/users/list-users.bats`, `scripts/users/create-user.bats`, `scripts/users/delete-user.bats` - tests of section 7.
- modify: `scripts/prepare/create-vm.sh` - header comment only: add "- sudo instalado." to the base disk requirements.

Backend:
- create: `stratus-srv/internal/service/operation_runner.go` - `operationRunner` with the logic of today's `PrepareService.runOperation`, plus `recordSuccess`. `redactAll` and `lastLine` move here.
- create: `stratus-srv/internal/service/operation_runner_test.go`.
- modify: `stratus-srv/internal/service/prepare_service.go` - replace the `locks VMLocks` field with `ops *operationRunner`. `NewPrepareService` takes `locks *VMLocks`. `runOperation` becomes a one-line wrapper over `s.ops.run(..., true, onSuccess)`, so the `prepare_*.go` call sites do not change.
- modify: `stratus-srv/internal/service/prepare_vms_test.go` and `stratus-srv/internal/handler/prepare_test.go` - pass `&VMLocks{}` to `NewPrepareService`.
- modify: `stratus-srv/internal/service/errors.go` - add `ErrUserExists`, `ErrGroupExists`, `ErrNotRegularUser`, `ErrUserKeyUnavailable` and `CleanupError`.
- create: `stratus-srv/internal/service/users_service.go` - `UsersConfig`, `UsersService`, `NewUsersService`, `History`, `getVM`.
- create: `stratus-srv/internal/service/users_validation.go` - `ValidateUsername`, `ValidatePassword`, `ValidateCreateUser`.
- create: `stratus-srv/internal/service/users_list.go` - `ListUsers`, `parseUsers`.
- create: `stratus-srv/internal/service/users_create.go` - `CreateUser`, `writeSecretFile`, `DownloadUserKey`.
- create: `stratus-srv/internal/service/users_delete.go` - `DeleteUser`.
- create: `stratus-srv/internal/service/users_validation_test.go`, `users_list_test.go`, `users_create_test.go`, `users_delete_test.go`, `users_service_test.go`.
- create: `stratus-srv/internal/handler/users.go` - `RegisterUsers` with the routes of section 6.
- create: `stratus-srv/internal/handler/users_test.go`.
- modify: `stratus-srv/internal/handler/respond.go` - map the new sentinels, and `CleanupError` to 500 with its own text.
- modify: `stratus-srv/cmd/server/main.go` - create one `&service.VMLocks{}`, pass it to both services, build `NewUsersService`, and call `handler.RegisterUsers`.
- modify: `stratus-srv/.env.example` - add "- sudo instalado." to the base disk requirements.

Frontend:
- create: `stratus-ui/src/components/ui/sonner.tsx` - from `pnpm dlx shadcn@latest add sonner`. It adds `sonner` to `stratus-ui/package.json`. If the generated file imports `next-themes`, it uses the fixed `theme="light"` instead and `next-themes` is not kept.
- modify: `stratus-ui/src/components/AppLayout.tsx` - mount `<Toaster />`.
- modify: `stratus-ui/src/test/setup.ts` - stub `window.matchMedia`, which Sonner needs in jsdom.
- modify: `stratus-ui/src/features/home/modules.ts` - `users`: `enabled: true`, with the description "Crear y eliminar usuarios de la VM, con sudo y llaves SSH opcionales."
- modify: `stratus-ui/src/App.tsx` - route `/users` to `<Users />`.
- modify: `stratus-ui/src/routes/Home.test.tsx` - the 001 test "AC-23 ..." becomes the 002 test "AC-27 ..." (REQ-21 supersedes 001 REQ-19 for this card).
- modify: `stratus-ui/src/features/prepare/api.ts` - export `request`, `operation`, `send` and `errorMessage` for reuse. Behavior does not change.
- create: `stratus-ui/src/features/users/types.ts`, `api.ts`, `validation.ts`.
- create: `stratus-ui/src/features/users/UserTable.tsx` - the table with a delete button per row.
- create: `stratus-ui/src/features/users/DeleteUserDialog.tsx` - the confirmation dialog with the "Eliminar todos sus archivos" checkbox.
- create: `stratus-ui/src/features/users/CreateUserForm.tsx` - the username field, two checkboxes, and password fields shown only when "Generar llaves SSH" is unchecked.
- create: `stratus-ui/src/features/users/UserKeyDownload.tsx` - the download button and usage message.
- create: `stratus-ui/src/routes/Users.tsx` - the module screen.
- create: tests `stratus-ui/src/features/users/{validation.test.ts,UserTable.test.tsx,DeleteUserDialog.test.tsx,CreateUserForm.test.tsx,UserKeyDownload.test.tsx}` and `stratus-ui/src/routes/Users.test.tsx`.
- keep and reuse: `features/prepare/{VmSelector,HistoryTable,useOperation}`, `components/OperationResult.tsx`, and the shadcn components `card`, `table`, `checkbox`, `field`, `input`, `alert`, `alert-dialog`, `button`, `badge`, `spinner`, `empty`.

## 6. Interfaces and data
Scripts (argv; env `APP_SSH_KEY_PATH` inherited; stdout is the result; stderr is diagnostics):
| Script | Arguments | stdout |
|---|---|---|
| `list-users.sh` | `<vm_name>` | one line per user `<username> <uid> <yes\|no> <yes\|no>` (sudo, key), ordered by UID |
| `create-user.sh` | `<vm_name> <username> <sudo yes\|no> <ssh_key yes\|no> [password_file]` | with key: only the private key; without key: `Usuario <username> creado en <vm>` |
| `delete-user.sh` | `<vm_name> <username> <remove_files yes\|no>` | `Usuario <username> eliminado de <vm>`, plus ` junto con todos sus archivos` when `remove_files` is `yes` |

Validation, step 1 (exit 2, no VBoxManage or ssh call):
- `vm_name` matches `^[a-z0-9][a-z0-9-]{0,31}$`.
- `username` matches `^[a-z][a-z0-9_-]{0,31}$`.
- `sudo`, `ssh_key` and `remove_files` are each `yes` or `no`.
- Argument count: `create-user.sh` takes exactly 5 args when `ssh_key=no` and exactly 4 when `ssh_key=yes`.
- `password_file` is a readable file. Its content, read with `LC_ALL=C`, matches `^[!-~][ -~]{6,70}[!-~]$`: 8-72 printable ASCII characters, no space at either end.
- `APP_SSH_KEY_PATH` is set.

Exit codes:
| Code | `list-users.sh` | `create-user.sh` | `delete-user.sh` |
|---|---|---|---|
| 0 | éxito | éxito | éxito |
| 2 | argumentos inválidos | argumentos inválidos o archivo de contraseña inválido | argumentos inválidos |
| 3 | VM no encontrada | VM no encontrada | VM no encontrada |
| 4 | fallo de SSH | fallo de SSH | fallo de SSH |
| 5 | fallo del comando remoto | fallo al crear (la limpieza terminó bien) | fallo al eliminar |
| 6 | - | el usuario ya existe en la VM | no es un usuario regular existente (falta o UID fuera de 1000-59999) |
| 7 | VM sin IP | VM sin IP | VM sin IP |
| 8 | - | existe un grupo con ese nombre | - |
| 9 | - | la creación falló y la limpieza también | - |
| 99 | operación no implementada | operación no implementada | operación no implementada |

`list-users.sh` remote program (one `vm_exec <vm> sh -c "$LIST" sh` call, `LIST` constant, `LC_ALL=C`):
1. `getent passwd` filtered with `awk -F:` to `1000 <= $3 <= 59999`. Fields used: name, UID, home.
2. Sudo is `yes` when `command -v sudo` succeeds and `sudo -n -l -U <name>` prints "may run the following commands". This covers the sudo group and sudoers rules.
3. Key is `yes` when `<home>/.ssh/authorized_keys` has a line that is not blank and does not start with `#` (`grep -qvE '^[[:space:]]*(#|$)'`).

`create-user.sh` steps:
1. Validate the arguments, then run `showvminfo` (exit 3) and `vm_ip` (exit 7).
2. Lookup: `vm_exec sh -c 'if getent passwd "$1" >/dev/null; then echo user; elif getent group "$1" >/dev/null; then echo group; else echo free; fi' sh <u>`. `user` exits 6 with "El usuario ya existe en la VM". `group` exits 8 with "Ya existe un grupo con ese nombre en la VM".
3. Only when `sudo=yes`: `vm_exec sh -c 'command -v sudo >/dev/null || DEBIAN_FRONTEND=noninteractive apt-get install -y sudo'`. Failure exits 5, and nothing has been created yet. This is a fallback, since the base disk ships `sudo`.
4. Only when `ssh_key=yes`: run `mktemp -d` with `trap` removal and `umask 077`, then `ssh-keygen -q -t ed25519 -N "" -C stratus-<vm>-<u> -f <tmp>/id_ed25519` with its output on stderr. Failure exits 5.
5. `vm_exec useradd -m -s /bin/bash -U <u>`. Failure exits 5. From here on the cleanup trap is active.
6. Only when `sudo=yes`: `vm_exec sh -c 'f=/etc/sudoers.d/stratus-$1; t=/etc/sudoers.d/.stratus-$1; printf "%s ALL=(ALL:ALL) NOPASSWD: ALL\n" "$1" >"$t" && chmod 440 "$t" && visudo -cf "$t" && mv "$t" "$f"' sh <u>`. The temp name contains a dot, so sudo ignores it.
7. Password mode: `printf '%s:%s\n' "$username" "$password" | vm_exec <vm> chpasswd`.
   Key mode, three calls:
   - `vm_exec install -d -m 700 -o <u> -g <u> /home/<u>/.ssh`
   - `vm_exec sh -c 'printf "%s\n" "$2" >"/home/$1/.ssh/authorized_keys" && chown "$1:$1" "/home/$1/.ssh/authorized_keys" && chmod 600 "/home/$1/.ssh/authorized_keys"' sh <u> <public key>`
   - `vm_exec passwd -l <u>`
8. Remove the trap. Print the private key (key mode) or the message (password mode).

Cleanup trap of `create-user.sh` (on a non-zero exit after step 5, including 143 on SIGTERM; its output goes only to stderr):
1. `userdel -r <u>`. This removes the account, the home and the mail spool. A missing mail spool is only a warning.
2. `rm -f /etc/sudoers.d/stratus-<u> /etc/sudoers.d/.stratus-<u>`.
3. `sh -c '! getent group "$1" >/dev/null || groupdel "$1"' sh <u>`.
4. Verify: `sh -c '! getent passwd "$1" >/dev/null && [ ! -e "/home/$1" ] && [ ! -e "/etc/sudoers.d/stratus-$1" ]' sh <u>`.
5. If steps 1-4 all succeed, keep the original exit code. If any fails, print `Limpieza fallida; puede quedar en la VM: cuenta <u>, /home/<u>, /etc/sudoers.d/stratus-<u>` as the last stderr line and exit 9. The original error was printed earlier, so stderr holds both errors (AC-33).

`delete-user.sh` steps:
1. Validate the arguments, then run `showvminfo` (exit 3) and `vm_ip` (exit 7).
2. UID: `vm_exec sh -c 'id -u "$1" 2>/dev/null || echo none' sh <u>`. If the result is `none` or outside 1000-59999, exit 6 with "Solo se pueden eliminar usuarios regulares existentes".
3. Processes: `vm_exec sh -c 'pkill -KILL -u "$1" || true; i=0; while pgrep -u "$1" >/dev/null; do i=$((i+1)); [ "$i" -le 10 ] || exit 1; sleep 1; done' sh <u>`. Failure exits 5 with "No se pudieron terminar los procesos de <u>".
4. Only when `remove_files=yes`:
   1. `vm_exec rm -rf -- /home/<u> /var/mail/<u>`.
   2. `vm_exec find / ( -path /proc -o -path /sys -o -path /dev -o -path /run -o -fstype nfs -o -fstype nfs4 -o -fstype cifs -o -fstype smb3 -o -fstype fuse.sshfs ) -prune -o -uid <uid> ! -type d -exec rm -f -- {} +`. tmpfs `/tmp` is included.
   3. `vm_exec bash -c '<same prune> -o -uid "$1" -type d -print0 | sort -rz | while IFS= read -r -d "" d; do rmdir -- "$d" 2>/dev/null || chown root:root -- "$d"; done' bash <uid>`. This goes deepest first. It removes empty directories and gives the rest to `root:root` with their permissions unchanged.
   Any failure exits 5.
5. `vm_exec rm -f /etc/sudoers.d/stratus-<u>`.
6. `vm_exec userdel <u>`. This removes the account and its primary group unless another user has it as primary group. Failure exits 5.
7. `vm_exec visudo -c`. Failure exits 5.
8. Print the message.

bats helper `scripts/users/test_helper.bash`:
```
load ../prepare/test_helper        # assert_script_structure, setup_stubs, stub_command, stub_vm_ip
setup_users_stubs <vm> <ip|->      # setup_stubs + stub_vm_ip. The ssh stub also appends its stdin to "$BATS_TEST_TMPDIR/ssh-stdin.log"
                                   # and answers from env: LOOKUP=user|group|free (create step 2), TARGET_UID=<n|none>
                                   # (delete step 2), FAIL_REMOTE=<ERE> (exit 1 when the remote command matches), and
                                   # FAIL_CLEANUP=<ERE> (exit 1 for cleanup commands)
setup_password_file <password>     # writes "$BATS_TEST_TMPDIR/password" with mode 600 and exports PASSWORD_FILE
run_remote_locally                 # list-users only: the ssh stub evals the remote program locally, with PATH stubs for
                                   # getent and sudo, and homes under "$BATS_TEST_TMPDIR/home" (read-only program)
```
Scripts under test run with `</dev/null` unless the test feeds stdin.

SQLite: no schema change. History rows use module `users`, and operation `list_users` (failures only), `create_user` or `delete_user`.

Go (package `service`):
```go
var (
  ErrUserExists         = errors.New("El usuario ya existe en la VM")
  ErrGroupExists        = errors.New("Ya existe un grupo con ese nombre en la VM")
  ErrNotRegularUser     = errors.New("Solo se pueden eliminar usuarios regulares existentes")
  ErrUserKeyUnavailable = errors.New("La llave ya no está disponible; elimine y vuelva a crear el usuario para obtener una nueva")
)
type CleanupError struct { Username, Detail string } // Error(): "No se pudo crear el usuario <u> y la limpieza falló; revise la VM. <Detail>"; Unwrap() returns ErrScriptFailed

type operationRunner struct { runner ScriptRunner; history *repository.HistoryRepository; logger *log.Logger; locks *VMLocks; module string }
func newOperationRunner(module string, runner ScriptRunner, history *repository.HistoryRepository, locks *VMLocks, logger *log.Logger) *operationRunner
// run: TryLock or ErrOperationInProgress; Run; redact; when recordSuccess is false and the result is "ok", insert no row;
// otherwise insert the row; log the summary (never stdout); map runErr, TimedOut, exit 7 (ErrVMNoIP) and non-zero (*OperationError);
// call onSuccess before unlocking.
func (o *operationRunner) run(ctx context.Context, operation, vm, script string, args []string, recordSuccess bool, onSuccess func(context.Context) error) (RunResult, error)

func NewPrepareService(cfg Config, runner ScriptRunner, vms *repository.VMRepository, history *repository.HistoryRepository, locks *VMLocks, logger *log.Logger) *PrepareService

const (
  exitUserExists     = 6 // create-user.sh
  exitNotRegularUser = 6 // delete-user.sh
  exitGroupExists    = 8
  exitCleanupFailed  = 9
)
func ValidateUsername(s string) error       // ^[a-z][a-z0-9_-]{0,31}$
func ValidatePassword(s string) error       // 8-72 bytes, each 0x20-0x7E, first and last not ' '
func ValidateCreateUser(in CreateUserInput) error // username; SSHKey && Password != "" -> "Con llaves SSH no se envía contraseña"; !SSHKey -> ValidatePassword

type CreateUserInput struct { Username string `json:"username"`; Sudo bool `json:"sudo"`; SSHKey bool `json:"ssh_key"`; Password string `json:"password,omitempty"` }
type UserView struct { Username string `json:"username"`; UID int `json:"uid"`; Sudo bool `json:"sudo"`; SSHKey bool `json:"ssh_key"` }

type UsersConfig struct { KeyTTL time.Duration /* 10m */ }
func NewUsersService(cfg UsersConfig, runner ScriptRunner, vms *repository.VMRepository, history *repository.HistoryRepository, locks *VMLocks, logger *log.Logger) *UsersService
func (s *UsersService) ListUsers(ctx context.Context, vm string) ([]UserView, error)  // argv [vm]; recordSuccess false; malformed stdout -> error "salida inesperada de list-users.sh"
func (s *UsersService) CreateUser(ctx context.Context, vm string, in CreateUserInput) (msg string, keyAvailable bool, err error)
  // validate; argv [vm u yes|no yes|no] + [secret path] when !SSHKey; secret file 0600 removed by defer;
  // exit 6 -> ErrUserExists, 8 -> ErrGroupExists, 9 -> &CleanupError{u, lastLine(stderr)}; ok with key -> keys.Put(vm+"/"+u, stdout);
  // msg with key: "Usuario <u> creado en <vm>. Descargue su llave privada ahora: se puede descargar una sola vez y se descarta en 10 minutos"
  // msg without key: "Usuario <u> creado en <vm>; inicia sesión con su contraseña en la consola o con su - <u>"
func (s *UsersService) DeleteUser(ctx context.Context, vm, username string, removeFiles bool) (string, error) // argv [vm u yes|no]; exit 6 -> ErrNotRegularUser; ok -> keys.Discard(vm+"/"+u); msg = trimmed stdout
func (s *UsersService) DownloadUserKey(vm, username string) ([]byte, error) // validates both; ErrKeyUnavailable -> ErrUserKeyUnavailable
func (s *UsersService) History(ctx context.Context) ([]repository.Operation, error) // module "users", limit 50
```
The exit-code mapping applies only when `errors.As(err, *OperationError)` holds. Exit 7 is already `ErrVMNoIP` from `operationRunner`.

Go (package `handler`): `func RegisterUsers(mux *http.ServeMux, svc *service.UsersService)`. Error to status mapping:
- `ErrUserExists`, `ErrGroupExists`, `ErrNotRegularUser`: 409 with the sentinel text.
- `ErrUserKeyUnavailable`: 410.
- `*CleanupError`: 500 with `Error()`.
- Everything else as in 001 (`ErrVMNotFound` 404, `ErrOperationInProgress` and `ErrVMNoIP` 409, `ErrTimeout` 504, `*OperationError` 500).

HTTP API (JSON `snake_case`):
| Method and path | Body / query | Success |
|---|---|---|
| `GET /api/users/vms/{vm}/users` | - | 200 `{"users": [{"username","uid","sudo","ssh_key"}]}` |
| `POST /api/users/vms/{vm}/users` | `{"username","sudo","ssh_key","password"?}` | 201 `{"message","key_available"}` |
| `DELETE /api/users/vms/{vm}/users/{username}` | `?remove_files=true\|false` (missing means false; any other value is 400) | 200 `{"message"}` |
| `GET /api/users/vms/{vm}/users/{username}/key/download` | - | 200 `text/plain`, `Content-Disposition: attachment; filename="id_ed25519_<vm>_<username>"`, `Cache-Control: no-store` |
| `GET /api/users/history` | - | 200 `{"operations": [...]}` (same row shape as 001) |

Frontend:
```ts
// features/users/types.ts
interface UserRow { username: string; uid: number; sudo: boolean; ssh_key: boolean }
interface CreateUserInput { username: string; sudo: boolean; ssh_key: boolean; password?: string } // password omitted when ssh_key
// features/users/api.ts (uses request/operation/send/errorMessage from features/prepare/api)
listUsers(vm): Promise<UserRow[]>; createUser(vm, input): Promise<{ message: string; key_available: boolean }>;
deleteUser(vm, username, removeFiles: boolean): Promise<string>; downloadUserKey(vm, username): Promise<Blob>; getUsersHistory(): Promise<OperationRow[]>;
// features/users/validation.ts - each returns a Spanish error string or null
validateUsername(s); validatePassword(s); validatePasswordConfirmation(password, confirmation); createUserErrors(form): Record<string, string>
// components
UserTable({ users: UserRow[]; vm: Vm | null; onDone(): void })          // columns Usuario, UID, Sudo (Sí/No), Llave (Sí/No), delete button per row
DeleteUserDialog({ vm: string; username: string | null; onClose(); onDone() }) // names the user; "Eliminar todos sus archivos" unchecked
CreateUserForm({ vm: Vm | null; onDone(): void })                       // with vm null: button disabled and "Seleccione una VM para continuar"
UserKeyDownload({ vm: Vm; username: string })                            // file id_ed25519_<vm>_<username>; usage: chmod 600, do not share or commit,
                                                                         // ssh -i id_ed25519_<vm>_<username> <username>@<ip | "<IP de la VM>">
```
`Users.tsx` loads `listVms` and `getUsersHistory` on mount. It calls `listUsers` on each VM selection and from `onDone`. Each successful `listUsers` calls `toast.success("Lista de usuarios actualizada")`. A failure sets an `Alert` "Usuarios no disponibles" with the API message. With no VM selected, the users list is empty and no request is made.

## 7. Test-first
- Framework and commands: Go `testing` (table-driven) with `cd stratus-srv && go test ./...`; Vitest + Testing Library with `cd stratus-ui && pnpm test`; bats with `bats -r scripts/`.
- Location and naming: tests sit next to the code. Go tests are `TestAC<n>_<Behavior>`, Vitest uses `it("AC-<n> ...")` and bats uses `@test "AC-<n> ..."`. Go tests use the fake `ScriptRunner` and a temp SQLite file, UI tests mock `fetch` with `vi.spyOn(globalThis, "fetch")`, and bats tests use `scripts/users/test_helper.bash`. Rows at level "manual" are steps of the real-VM before/after demo (P8).

| Test | Level | File | Covers (US / REQ / AC) | Given - when - then |
|---|---|---|---|---|
| AC-1 list-users imprime solo usuarios regulares con sudo y llave | unit | `scripts/users/list-users.bats` | US-1 / REQ-1, REQ-2 / AC-1 | given getent with root, www-data, ana 1001 (sudo, key), luis 1002, nobody 65534 - when the remote program runs locally - then stdout is `ana 1001 yes yes` and `luis 1002 no no` only |
| TestAC1_ListUsersParsesScriptOutput | unit | `stratus-srv/internal/service/users_list_test.go` | US-1 / REQ-1 / AC-1 | given that stdout - when ListUsers runs - then it returns two UserView with argv `[vm1]` |
| AC-1 shows each user with Sí/No | unit | `stratus-ui/src/features/users/UserTable.test.tsx` | US-1 / REQ-1, REQ-23 / AC-1 | given ana and luis rows - when rendered - then the rows read `ana 1001 Sí Sí` and `luis 1002 No No` |
| AC-2 no VM selected: empty list and disabled create | unit | `stratus-ui/src/routes/Users.test.tsx` | US-1 / REQ-1 / AC-2 | given no VM selected - when the screen opens - then no users request, empty list, create disabled with "Seleccione una VM para continuar" |
| AC-3 list-users / create-user / delete-user salen con 7 sin IP | unit | `scripts/users/{list-users,create-user,delete-user}.bats` | US-1..US-3 / REQ-17 / AC-3 | given `stub_vm_ip vm1 -` - when each script runs - then exit 7, the message, no ssh call |
| TestAC3_ListUsersMapsNoIP | unit | `stratus-srv/internal/service/users_list_test.go` | US-1 / REQ-17, REQ-18 / AC-3 | given exit 7 - when ListUsers runs - then ErrVMNoIP and one history row |
| TestAC3_CreateUserMapsNoIP | unit | `stratus-srv/internal/service/users_create_test.go` | US-2 / REQ-17, REQ-18 / AC-3 | given exit 7 - when CreateUser runs - then ErrVMNoIP, a history row, the secret file removed |
| TestAC3_DeleteUserMapsNoIP | unit | `stratus-srv/internal/service/users_delete_test.go` | US-3 / REQ-17, REQ-18 / AC-3 | given exit 7 - when DeleteUser runs - then ErrVMNoIP and a history row |
| AC-3 shows the no-IP message when the list fails | unit | `stratus-ui/src/routes/Users.test.tsx` | US-1 / REQ-18 / AC-3 | given 409 with the no-IP text - when a VM is selected - then the Alert shows "La VM no tiene dirección IP; verifique que esté encendida" |
| AC-4 create-user con sudo y llaves | unit | `scripts/users/create-user.bats` | US-2 / REQ-5, REQ-8 / AC-4 | given `vm1 ana yes yes` - when it runs - then calls.log has `useradd -m -s /bin/bash -U ana`, the sudoers step, authorized_keys and `passwd -l ana`, and stdout is only the private key |
| TestAC4_CreateUserPassesDiscreteArgs | unit | `stratus-srv/internal/service/users_create_test.go` | US-2 / REQ-3, REQ-10 / AC-4 | given sudo and keys - when CreateUser runs - then argv is `[vm1 ana yes yes]`, keyAvailable is true and the key is in the KeyStore |
| AC-4 submits with sudo and keys and shows download and usage | unit | `stratus-ui/src/features/users/CreateUserForm.test.tsx` | US-2 / REQ-3, REQ-11 / AC-4 | given ana, both checked - when submitted - then the body is `{username:"ana",sudo:true,ssh_key:true}` and the result, the download button and `ssh -i id_ed25519_vm1_ana ana@192.168.1.50` show |
| AC-5 rejects invalid usernames | unit | `stratus-ui/src/features/users/validation.test.ts` | US-2 / REQ-4 / AC-5 | given `Ana`, `1ana`, `ana;rm`, empty, 33 chars - when validated - then each returns an error |
| AC-5 invalid username shows error and sends nothing | unit | `stratus-ui/src/features/users/CreateUserForm.test.tsx` | US-2 / REQ-4 / AC-5 | given `ana;rm` - when submitted - then a field error and no fetch |
| TestAC5_CreateUserRejectsInvalidUsername | unit | `stratus-srv/internal/service/users_validation_test.go` | US-2 / REQ-4 / AC-5 | given the 5 names - when CreateUser runs - then ErrInvalidInput and no runner call |
| AC-6 create-user sale con 6 si el usuario existe | unit | `scripts/users/create-user.bats` | US-2 / REQ-4 / AC-6 | given LOOKUP=user - when it runs - then exit 6, "El usuario ya existe en la VM", no useradd |
| TestAC6_CreateUserExistingMapsErrUserExists | unit | `stratus-srv/internal/service/users_create_test.go` | US-2 / REQ-4 / AC-6 | given exit 6 - when CreateUser runs - then ErrUserExists |
| TestAC6_AC29_AC33_UsersHandlerMapsCreateErrors | unit | `stratus-srv/internal/handler/users_test.go` | US-2 / REQ-4, REQ-12 / AC-6, AC-29, AC-33 | given create exits 6, 8, 9 - when POST `/api/users/vms/vm1/users` - then 409, 409, 500 with the spec texts |
| TestAC17_UsersHandlerMapsDeleteError | unit | `stratus-srv/internal/handler/users_test.go` | US-3 / REQ-14 / AC-17 | given delete exit 6 - when DELETE `/api/users/vms/vm1/users/root` - then 409 "Solo se pueden eliminar usuarios regulares existentes"; `remove_files=si` gives 400 and no runner call |
| AC-7 rejects invalid passwords | unit | `stratus-ui/src/features/users/validation.test.ts` | US-2 / REQ-7 / AC-7 | given 7 chars, 73 chars, `ñ`, leading space, mismatched confirmation - when validated - then each returns an error |
| AC-7 invalid password shows error and sends nothing | unit | `stratus-ui/src/features/users/CreateUserForm.test.tsx` | US-2 / REQ-7 / AC-7 | given keys unchecked and a 7-char password - when submitted - then a field error and no fetch |
| TestAC7_ValidatePassword | unit | `stratus-srv/internal/service/users_validation_test.go` | US-2 / REQ-7 / AC-7 | given the same passwords - when CreateUser runs - then ErrInvalidInput and no runner call |
| AC-8 with keys shows no password fields and sends none | unit | `stratus-ui/src/features/users/CreateUserForm.test.tsx` | US-2 / REQ-3, REQ-8 / AC-8 | given keys checked - when rendered and submitted - then no password inputs and no `password` key in the body |
| TestAC8_CreateUserWithKeyRejectsPassword | unit | `stratus-srv/internal/service/users_validation_test.go` | US-2 / REQ-8 / AC-8 | given ssh_key true and a password - when CreateUser runs - then ErrInvalidInput, no runner call |
| AC-8 create-user con llaves rechaza password_file | unit | `scripts/users/create-user.bats` | US-2 / REQ-8 / AC-8 | given `vm1 ana no yes <file>` - when it runs - then exit 2, no calls |
| AC-9 create-user con sudo escribe la regla NOPASSWD validada | unit | `scripts/users/create-user.bats` | US-2 / REQ-6 / AC-9 | given sudo yes - when it runs - then the sudoers step has `NOPASSWD: ALL`, `chmod 440` and `visudo -cf`; with sudo no there is no sudoers step |
| AC-9 create-user instala sudo solo si falta | unit | `scripts/users/create-user.bats` | US-2 / REQ-6 / AC-9 | given sudo yes - when it runs - then the `command -v sudo \|\| apt-get install -y sudo` step precedes useradd; with sudo no it is absent |
| AC-9 sudo -n true with and without sudo | manual | real VM demo | US-2 / REQ-6 / AC-9 | given users created with and without sudo - when `sudo -n true` runs over SSH - then success and failure |
| AC-10 create-user con llaves instala authorized_keys y bloquea la contraseña | unit | `scripts/users/create-user.bats` | US-2 / REQ-8 / AC-10 | given keys yes - when it runs - then `install -d -m 700 -o ana -g ana /home/ana/.ssh`, the authorized_keys step with `chmod 600` and `passwd -l ana`, and no chpasswd |
| AC-10 key login works, su and password fail | manual | real VM demo | US-2 / REQ-8 / AC-10 | given a key user - when SSH with the key, `su - ana` from another user, SSH with a password - then only the key works |
| AC-11 create-user con contraseña usa chpasswd y no toca sshd | unit | `scripts/users/create-user.bats` | US-2 / REQ-7 / AC-11 | given a password file - when it runs - then a `chpasswd` call, no `passwd -l`, no sshd_config change |
| AC-11 console and su succeed, SSH refused | manual | real VM demo | US-2 / REQ-7 / AC-11 | given password `Secreta123` - when console login, `su - ana`, `ssh ana@<ip>` - then success, success, refused |
| AC-12 create-user envía la contraseña por stdin y nunca en argv | unit | `scripts/users/create-user.bats` | US-2 / REQ-9 / AC-12 | given password `Secreta123` - when it runs - then ssh-stdin.log has `ana:Secreta123`, and calls.log, stdout and stderr do not contain `Secreta123` |
| TestAC12_CreateUserPasswordOnlyInSecretFile | unit | `stratus-srv/internal/service/users_create_test.go` | US-2 / REQ-9, REQ-19 / AC-12 | given `Secreta123` - when CreateUser runs (ok and exit 5) - then during Run the last arg is a 0600 file with the password, no arg equals it, and afterwards the file is gone and history and logs lack it |
| TestAC13_UserKeyDownloadOnceAndExpires | unit | `stratus-srv/internal/service/users_create_test.go` | US-2 / REQ-10 / AC-13 | given a stored key and KeyTTL 50 ms - when downloaded twice, or after the TTL - then the first works and later calls return ErrUserKeyUnavailable |
| TestAC13_UserKeyDownloadHeadersAnd410 | unit | `stratus-srv/internal/handler/users_test.go` | US-2 / REQ-10 / AC-13 | given a stored key - when GET download twice - then 200 with `filename="id_ed25519_vm1_ana"` and `no-store`, then 410 with the spec text |
| AC-13 shows the unavailable message on 410 | unit | `stratus-ui/src/features/users/UserKeyDownload.test.tsx` | US-2 / REQ-10 / AC-13, AC-32 | given 410 - when download is clicked - then "La llave ya no está disponible; elimine y vuelva a crear el usuario para obtener una nueva" |
| TestAC14_UserKeyNeverInHistoryOrLogs | unit | `stratus-srv/internal/service/users_create_test.go` | US-2 / REQ-10, REQ-19 / AC-14 | given stdout with a private key block - when CreateUser runs - then no history column or log line contains it |
| AC-14 create-user no escribe la llave privada en stderr | unit | `scripts/users/create-user.bats` | US-2 / REQ-10 / AC-14 | given keys yes - when it runs - then stderr and calls.log have no `PRIVATE KEY` |
| AC-15 create-user limpia si falla tras crear la cuenta | unit | `scripts/users/create-user.bats` | US-2 / REQ-12 / AC-15 | given FAIL_REMOTE matching authorized_keys - when it runs - then exit 5 and calls.log has `userdel -r ana`, `rm -f /etc/sudoers.d/stratus-ana ...`, groupdel and the verify step |
| TestAC15_CreateUserFailureReturnsOperationError | unit | `stratus-srv/internal/service/users_create_test.go` | US-2 / REQ-12, REQ-18 / AC-15 | given exit 5 - when CreateUser runs - then *OperationError, no key stored, history row error |
| AC-16 delete asks for confirmation naming the user | unit | `stratus-ui/src/features/users/DeleteUserDialog.test.tsx` | US-3 / REQ-13 / AC-16 | given luis listed - when delete is clicked - then the dialog names luis with the checkbox unchecked, there is no fetch until Confirmar, then `DELETE .../users/luis?remove_files=false` |
| AC-17 delete-user rechaza root, www-data y usuarios inexistentes | unit | `scripts/users/delete-user.bats` | US-3 / REQ-14 / AC-17 | given TARGET_UID 0, 33, none - when it runs - then exit 6, the spec text, no pkill or userdel |
| TestAC17_DeleteUserNonRegularMapsError | unit | `stratus-srv/internal/service/users_delete_test.go` | US-3 / REQ-14 / AC-17 | given exit 6 - when DeleteUser runs for root - then ErrNotRegularUser |
| AC-18 delete-user termina procesos antes de eliminar | unit | `scripts/users/delete-user.bats` | US-3 / REQ-15 / AC-18 | given TARGET_UID 1002 - when it runs - then the pkill step comes before `userdel luis`, which comes before `visudo -c`, and stdout is the message |
| TestAC18_DeleteUserPassesArgs | unit | `stratus-srv/internal/service/users_delete_test.go` | US-3 / REQ-15 / AC-18 | given removeFiles false and true - when DeleteUser runs - then argv `[vm1 luis no]` / `[vm1 luis yes]` and the message is stdout |
| AC-18 running process is terminated | manual | real VM demo | US-3 / REQ-15 / AC-18 | given `luis` running `sleep 600` - when deleted - then the process is gone and the account is removed |
| AC-19 delete-user sin archivos no borra nada | unit | `scripts/users/delete-user.bats` | US-3 / REQ-16 / AC-19 | given remove_files no - when it runs - then no `rm -rf`, no `find` |
| AC-20 delete-user con archivos borra home, correo y archivos del UID | unit | `scripts/users/delete-user.bats` | US-3 / REQ-16 / AC-20 | given remove_files yes - when it runs - then `rm -rf -- /home/luis /var/mail/luis` and a `find` that prunes /proc /sys /dev /run and network fs, with `-uid 1002 ! -type d` |
| AC-20 files removed, others' files remain | manual | real VM demo | US-3 / REQ-16 / AC-20 | given the AC-20 files - when deleted with files - then they are gone and the other users' files in /tmp and /srv/datos remain |
| AC-21 create with sudo and keys, before/after | manual | real VM demo | US-2 / REQ-5, REQ-6, REQ-8 / AC-21 | `getent passwd`, `sudo -l -U ana`, `passwd -S ana` (L) and authorized_keys before and after |
| AC-22 delete with files, before/after | manual | real VM demo | US-3 / REQ-15, REQ-16 / AC-22 | `getent passwd`, `sudo -l -U ana`, `find / -xdev -user <uid>` empty after |
| AC-23 delete-user quita la regla sudo y verifica con visudo -c | unit | `scripts/users/delete-user.bats` | US-3 / REQ-15 / AC-23 | given any delete - when it runs - then `rm -f /etc/sudoers.d/stratus-luis` and `visudo -c` |
| AC-23 no sudoers rule remains | manual | real VM demo | US-2, US-3 / REQ-6, REQ-15 / AC-23 | `grep -r ana /etc/sudoers /etc/sudoers.d/` empty and `visudo -c` ok |
| TestAC24_ListUsersRejectedWhilePrepareRuns | integration | `stratus-srv/internal/service/users_service_test.go` | US-1 / REQ-17 / AC-24 | given PrepareService and UsersService sharing one VMLocks and an install_package blocked on vm1 - when ListUsers on vm1 runs - then ErrOperationInProgress and no list-users call |
| TestAC24_CreateUserBlocksPrepare | integration | `stratus-srv/internal/service/users_service_test.go` | US-2 / REQ-17 / AC-24 | given a create_user blocked on vm1 - when InstallPackage on vm1 runs - then ErrOperationInProgress |
| TestAC24_DeleteUserRejectedWhilePrepareRuns | integration | `stratus-srv/internal/service/users_service_test.go` | US-3 / REQ-17 / AC-24 | given a set_hostname blocked on vm1 - when DeleteUser on vm1 runs - then ErrOperationInProgress |
| AC-25 <script> sale con 2 ante argumentos inválidos | unit | `scripts/users/{list-users,create-user,delete-user}.bats` | US-1..US-3 / REQ-4, REQ-9, REQ-22 / AC-25 | given a missing arg, `Ana`, `sudo=si`, a missing or invalid secret file - when run - then exit 2 and an empty calls.log |
| TestAC26_UsersHistoryReturns50NewestOfModule | unit | `stratus-srv/internal/service/users_service_test.go` | US-4 / REQ-20 / AC-26 | given 55 users rows and prepare rows - when History runs - then 50 users rows, newest first |
| AC-26 shows the module history | unit | `stratus-ui/src/routes/Users.test.tsx` | US-4 / REQ-20 / AC-26 | given `/api/users/history` rows - when the screen opens - then the rows show with "Ver stderr" |
| AC-27 Gestión de usuarios navigates, the others are Próximamente | unit | `stratus-ui/src/routes/Home.test.tsx` | US-1..US-4 / REQ-21 / AC-27 | given the home - when it loads - then the users card links to `/users`, and the systemd and HAProxy cards show "Próximamente" (2 total) |
| AC-28 <script> tiene la estructura exigida | unit | `scripts/users/{list-users,create-user,delete-user}.bats` | US-1..US-3 / REQ-22 / AC-28 | given the file - when `assert_script_structure` runs - then it passes |
| AC-29 create-user sale con 8 si existe el grupo | unit | `scripts/users/create-user.bats` | US-2 / REQ-4 / AC-29 | given LOOKUP=group - when it runs - then exit 8, the spec text, no useradd or groupadd |
| TestAC29_CreateUserGroupExistsMapsError | unit | `stratus-srv/internal/service/users_create_test.go` | US-2 / REQ-4 / AC-29 | given exit 8 - when CreateUser runs - then ErrGroupExists |
| TestAC30_SuccessfulListsAreNotRecorded | unit | `stratus-srv/internal/service/users_list_test.go` | US-1, US-4 / REQ-19 / AC-30 | given 20 successful lists - then 0 rows; given exit 4 - then 1 row with code and stderr |
| TestAC30_OperationRunnerSkipsRecordOnlyOnSuccess | unit | `stratus-srv/internal/service/operation_runner_test.go` | US-4 / REQ-19 / AC-30 | given recordSuccess false - when ok, error, timeout - then 0, 1, 1 rows |
| AC-31 delete-user borra directorios vacíos y entrega los demás a root:root | unit | `scripts/users/delete-user.bats` | US-3 / REQ-16 / AC-31 | given remove_files yes - when it runs - then the directory step uses `-uid 1002 -type d`, `sort -rz`, `rmdir` and the `chown root:root` fallback |
| AC-31 shared directory kept as root:root | manual | real VM demo | US-3 / REQ-16 / AC-31 | given the AC-31 tree - when deleted with files - then `l.txt` and `/srv/vacio` are gone, and `/srv/proyecto` (root:root, same mode) and `ana.txt` remain |
| TestAC32_DeleteUserDiscardsPendingKey | unit | `stratus-srv/internal/service/users_delete_test.go` | US-2, US-3 / REQ-10 / AC-32 | given a pending key for vm1/ana - when DeleteUser succeeds - then DownloadUserKey returns ErrUserKeyUnavailable; when delete fails the key stays |
| AC-33 create-user sale con 9 si la limpieza falla | unit | `scripts/users/create-user.bats` | US-2 / REQ-12 / AC-33 | given FAIL_REMOTE and FAIL_CLEANUP on userdel - when it runs - then exit 9 and stderr has the original error and "Limpieza fallida; puede quedar en la VM: ..." |
| TestAC33_CreateUserCleanupFailureMessage | unit | `stratus-srv/internal/service/users_create_test.go` | US-2 / REQ-12 / AC-33 | given exit 9 and that stderr - when CreateUser runs - then *CleanupError text "No se pudo crear el usuario ana y la limpieza falló; revise la VM. Limpieza fallida; ..." and the history stderr has both lines |
| AC-34 toast on a successful list, message on failure | unit | `stratus-ui/src/routes/Users.test.tsx` | US-1 / REQ-18, REQ-23 / AC-34 | given vm1 selected - when the list loads - then "Lista de usuarios actualizada" appears; given a 500 - then the error Alert and no toast |
| AC-34 toast again after a create and after a delete | unit | `stratus-ui/src/routes/Users.test.tsx` | US-1..US-3 / REQ-18 / AC-34 | given vm1 selected - when a create succeeds, and then a delete succeeds - then the list is fetched again and the toast appears each time |
| TestAC1_UsersListRouteReturnsJSON | unit | `stratus-srv/internal/handler/users_test.go` | US-1 / REQ-1 / AC-1 | given list stdout for ana and luis - when GET `/api/users/vms/vm1/users` - then 200 `{"users":[{"username":"ana","uid":1001,"sudo":true,"ssh_key":true},...]}` |
| TestAC26_UsersHistoryRouteReturnsModuleRows | unit | `stratus-srv/internal/handler/users_test.go` | US-4 / REQ-20 / AC-26 | given one users row and one prepare row - when GET `/api/users/history` - then only the users row, snake_case fields |

Existing tests that keep passing after the `operationRunner` refactor: every `stratus-srv/internal/service/prepare_*_test.go` and `stratus-srv/internal/handler/prepare_test.go`, with only the constructor argument changed.

## 8. Traceability
| Requirement | Component / file | Verified by (AC) |
|---|---|---|
| REQ-1 | `list-users.sh`, `UsersService.ListUsers`, `Users.tsx`, `VmSelector`, `UserTable` | AC-1, AC-2, AC-34 |
| REQ-2 | `list-users.sh` remote program | AC-1 |
| REQ-3 | `CreateUserForm` | AC-4, AC-8 |
| REQ-4 | `validation.ts`, `users_validation.go`, `create-user.sh` steps 1-2 | AC-5, AC-6, AC-25, AC-29 |
| REQ-5 | `create-user.sh` step 5 | AC-4, AC-21 |
| REQ-6 | `create-user.sh` steps 3 and 6 | AC-9, AC-21, AC-23 |
| REQ-7 | `CreateUserForm`, `validation.ts`, `users_validation.go`, `create-user.sh` step 7 | AC-7, AC-11 |
| REQ-8 | `create-user.sh` steps 4 and 7, `CreateUserForm` | AC-8, AC-10, AC-21 |
| REQ-9 | `users_create.go` secret file, `create-user.sh` stdin to `chpasswd` | AC-12, AC-25 |
| REQ-10 | users `KeyStore`, `DownloadUserKey`, `DeleteUser`, download route | AC-13, AC-14, AC-32 |
| REQ-11 | `UserKeyDownload` | AC-4 |
| REQ-12 | `create-user.sh` cleanup trap, `CleanupError` | AC-15, AC-33 |
| REQ-13 | `UserTable`, `DeleteUserDialog` | AC-16 |
| REQ-14 | `delete-user.sh` step 2, `ErrNotRegularUser` | AC-17 |
| REQ-15 | `delete-user.sh` steps 3, 5-7 | AC-18, AC-22, AC-23 |
| REQ-16 | `delete-user.sh` step 4 | AC-19, AC-20, AC-22, AC-31 |
| REQ-17 | `operationRunner`, shared `VMLocks`, `ExecRunner`, `vm_exec` | AC-3, AC-24 |
| REQ-18 | `Users.tsx` toast and Alert, `useOperation`, `respond.go` | AC-3, AC-15, AC-34 |
| REQ-19 | `operationRunner.recordSuccess`, `Redact`, secret-file handling | AC-12, AC-14, AC-30 |
| REQ-20 | `UsersService.History`, `GET /api/users/history`, `HistoryTable` | AC-26 |
| REQ-21 | `modules.ts`, `App.tsx`, `Home.test.tsx` | AC-27 |
| REQ-22 | headers and sections of `scripts/users/*.sh` | AC-25, AC-28 |
| REQ-23 | shadcn components (`sonner`, `table`, `checkbox`, `alert-dialog`, ...), Spanish texts | AC-1, AC-16, AC-34 |

## 9. Decisions and risks
- Password through ssh stdin to `chpasswd`. Rejected: hashing on the host with `openssl passwd` and passing the hash to `usermod -p`, because the hash would show in the VM process list and the host would need openssl.
- Shared `operationRunner` with one `*VMLocks`. Rejected: duplicating `runOperation` in `UsersService`, which would give two copies of the history and lock logic.
- `sudo` is expected on the base disk, and `create-user.sh` installs it only as a fallback. Rejected: failing and adding `sudo` to the 001 catalog, which is an extra manual step.
- Sonner for the toast (`sonner` approved). Rejected: a custom auto-hiding Alert, which is custom code that shadcn already covers.
- `vm-exec.sh` stays in `scripts/prepare/lib/` and is sourced from `scripts/users/`. Rejected: moving it to `scripts/lib/`, which would edit every 001 script for no behavior change.
- The private key is generated on the host, as in 001 `generate-user-key.sh`. It never exists on the VM.
- Delete order: processes, then files, then the sudo rule, then `userdel`. If a file step fails, the account still exists and the delete can be retried.
- Exit 6 means "user exists" in create and "not a regular existing user" in delete. Each script documents its own table, and the service maps it per operation.
- Risk: `pnpm dlx shadcn add sonner` may generate `useTheme` from `next-themes`. Mitigation: replace it with `theme="light"` and do not keep `next-themes`.
- Risk: Sonner calls `window.matchMedia`, which jsdom lacks. Mitigation: a stub in `src/test/setup.ts`.
- Risk: key login with a locked password depends on `UsePAM yes`, the Debian default, because with PAM sshd ignores the `!` lock for key authentication. Mitigation: the AC-10 demo step checks it on the real VM.
- Risk: `su - <u>` run as root never asks for a password. Mitigation: the AC-10 and AC-11 demo steps run `su` from another non-root user.
- Risk: the `find /` purge can be slow on a large disk. Mitigation: the 15-minute script timeout, and the pruning of /proc, /sys, /dev, /run and network filesystems.
- Risk: sudoers rules written by hand outside `/etc/sudoers.d/stratus-<u>` are not removed. Mitigation: the app only creates that file, and AC-23 covers app-created users. `userdel` drops sudo-group membership.
- Risk: the systemd user manager can outlive `pkill` for a moment. Mitigation: the bounded `pgrep` wait (10 s) before `userdel`.
- Risk: the bats stubs check commands, not effects, for the file purge. Mitigation: the AC-20, AC-22 and AC-31 demo steps on a real VM.
- Risk: the 001 home test expects 3 "Próximamente" cards. Mitigation: it is rewritten as the AC-27 test in the same change that enables the card.

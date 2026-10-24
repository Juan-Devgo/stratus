# Plan: Services

## 1. Metadata
- Spec: `docs/specs/003-services/spec.md`
- Updated: 2026-10-22
- Constitution version: 2.0.0

## 2. Constitution check
- P1: the backend only does these things: validates the fields, checks the archive's `.tar.gz` suffix and its 200 MiB size, writes the archive and the variables to host temp files, picks the script, passes argv, and maps the exit code and stdout to a response. The scripts in `scripts/services/` do all the work: archive content checks, transfer, `useradd`, extraction, the env file, the unit file, `systemctl`, `journalctl`, backups and rollback.
- P2: there are 12 scripts, one per operation (REQ-26). Each has `set -euo pipefail`, positional arguments, results on stdout and diagnostics on stderr. They share the exit-code table of section 6 and use a distinct code per failure type. The archive and the variables arrive as paths of host temp files (REQ-12, REQ-13). The delete steps tolerate parts that are already gone, so delete can be retried (REQ-25).
- P3: the same allowlists are in `stratus-ui/src/features/services/validation.ts`, `stratus-srv/internal/service/services_validation.go` and step "1. Validar argumentos" of each script (helpers in `scripts/services/lib/service-common.sh`). The backend uses `exec.CommandContext` with argv items. Remote commands go through `vm_exec` (`printf '%q '`). Constant remote `sh -c` programs receive names as positional arguments.
- P4: every remote step, including the archive transfer, runs through `vm_exec`: SSH as root with the backend key against the VM IPv4. The archive is streamed over the ssh stdin into `tar` on the VM. No scp, no Guest Additions.
- P5: this module handles no private key. Environment values are secrets: they live only in a host temp file (0600, removed by `defer`), the script's memory, the ssh stdin and `/etc/<name>.env` (600 `root:root`). They are never in argv, stdout, stderr, the history, logs, the list or the UI after submission.
- P6: every operation returns `{message[, active_state]}`, `{output}` or `{error}`. `operations` rows with module `services` keep the command, argv (paths only), exit code, redacted stderr, result and timestamp. Mutating operations are always recorded; list, status and logs only on failure (REQ-27).
- P7: the home card "Servicios systemd" opens `/services`, the only screen of the module. Forms and dialogs confirm with `OperationResult` and, when the state is not `active`, with `StateWarning`.
- P8: each script runs standalone and has bats tests (PATH stubs and real `tar` fixtures). Rows at level "manual" in section 7 are the real-VM before/after demo (AC-31, AC-32 and what stubs cannot show).
- P9: no new npm or Go dependency. The shadcn `dialog` component is built on the installed `@base-ui/react`. Reused as-is: `operationRunner`, the shared `VMLocks`, `ExecRunner`, `Redact`, the history repository, `vm-exec.sh`, `VmSelector`, `HistoryTable`, `useOperation` and `OperationResult`. The only new shared code is the bash library `service-common.sh`, which avoids copying the validators into 12 scripts.

## 3. Architecture
- Scripts:
  - `scripts/services/<operation>-service.sh` source `../prepare/lib/vm-exec.sh`, then `lib/service-common.sh`.
  - Common steps: validate the args (exit 2); for create and redeploy, check the archive on the host (exit 10 or 11); `VBoxManage showvminfo` (exit 3); `vm_ip` (exit 7); for operations on an existing service, `require_managed` (exit 6); then the remote steps through `guest <code> <cmd...>`, which passes 2, 4 and 7 through.
- Archive path (REQ-4, REQ-5, REQ-12):
  1. The UI sends `multipart/form-data` with a `payload` part (JSON) first and an `archive` part second.
  2. The handler wraps the body in `http.MaxBytesReader` (limit `MaxArchiveBytes` + 1 MiB) and hands the `archive` part to the service as an `io.Reader`.
  3. The service validates `payload` and the VM first. It then checks the `.tar.gz` suffix and copies the part with `io.LimitReader(max+1)` into `os.CreateTemp("", "stratus-archive-*.tar.gz")`. More than `max` bytes gives "El archivo supera 200 MB".
  4. The path goes into argv. `defer os.Remove` deletes the file when the operation ends.
  5. The script checks the archive on the host (`check_archive`): `gzip -t`, then `tar -tzf` (names) and `tar -tvzf` (first character = type), zipped line by line. It allows only `-` and `d` entries, no name starting with `/`, and no `..` segment. Both listings must have the same line count. The executable, after stripping a leading `./`, must be a `-` entry. Failures give exit 10 or 11 before any VBoxManage or ssh call.
  6. The script streams the archive: `vm_exec <vm> tar -xzf - -C /opt/<name> --no-same-owner < <archive>`. No temp file is left on the VM.
- Secrets path (REQ-9, REQ-13):
  1. The service writes `KEY=VALUE\n` lines with `writeSecretFile("stratus-env-*", content)` (0600) and passes the path in argv. `defer os.Remove` deletes it.
  2. The script validates every line again.
  3. `encode_env` prints `KEY="<value>"`, with `\`, `"`, `` ` `` and `$` each escaped by `\`. That is the systemd EnvironmentFile double-quote rule, so the service sees the value exactly as typed (AC-10). It uses bash builtins only (`printf`, parameter expansion).
  4. The output is piped into `write_remote_file /etc/<name>.env 600`, which reads ssh stdin into `<dest>.tmp`, runs `chown root:root` and `chmod`, then `mv -f`.
- Data flow of a mutating operation:
  1. The UI validates and calls `features/services/api.ts`.
  2. The handler parses the JSON or multipart body.
  3. `ServicesService` validates, loads the VM (`VMRepository.Get`, else `ErrVMNotFound`), writes the temp files and builds argv.
  4. `operationRunner.run` takes the shared `VMLocks` (AC-29), runs the script (15 min), redacts stderr, writes the history row and logs a summary.
  5. The service maps exit codes to sentinels and parses stdout into `ServiceResult`.
  6. The UI shows `OperationResult` and `StateWarning`, then reloads the list (with its toast) and the history.
- State reading (REQ-15, REQ-17, REQ-23, REQ-24): after an operation started or restarted the service, the script runs `sleep 3` on the host and then `read_state`. It prints `active_state=<state>` as the second stdout line. `read_state` prints `ActiveState`, plus ` (<SubState>)` when `ActiveState` is `activating`, for example `activating (auto-restart)`. "Running" means `active` or `activating`. If the read fails after a successful change, the line is `active_state=desconocido`, which shows the warning, and the operation still succeeds.
- List (REQ-16, REQ-21): one call, `vm_exec <vm> sh -c "$LIST" sh "$MARKER" /etc/systemd/system /etc`. For each `*.service` in the first directory whose file has the exact marker line, it prints one tab-separated row (section 6). Env keys are read from `/etc/<name>.env` with `sed`. Only keys leave the VM. The edit and redeploy dialogs prefill from the latest list row.
- Rollback:
  - Create: an `EXIT` trap undoes everything after `useradd` succeeds (REQ-14).
  - Edit and redeploy: they back up to `/var/lib/stratus/backup/<name>/` (unit and env) and `/opt/.stratus-backup-<name>` (content, redeploy only). The trap restores the files, runs `daemon-reload` and restores the previous active state. Backups are always deleted (REQ-23, REQ-24).
  - Any failure of the cleanup or restore exits 9.
- Shared lock: `main.go` passes the existing `locks` to `NewServicesService`.
- State:
  - SQLite: `operations` rows with module `services`, no schema change.
  - VM: users, `/opt/<name>`, `/etc/<name>.env` and the unit files.
  - UI: `Services.tsx` holds the selected VM, the services, the list error, the history, the busy flag, the output panel and the open dialog.

## 4. Design patterns
- Shared executor `operationRunner` (existing) in `internal/service/operation_runner.go`: lock, run, history (with `recordSuccess=false` for list, status and logs) and logging, with no copy.
- Strategy via `ScriptRunner` (existing): service and handler tests use the fake runner.
- Sourced bash library `scripts/services/lib/service-common.sh`: one implementation of the allowlists, `require_managed`, `read_state`, `render_unit`, `encode_env`, `write_remote_file` and `check_archive` for 12 scripts.
- Cleanup trap (as in `create-user.sh`) in create, edit and redeploy: once the first change happens, any non-zero exit (including 143 on SIGTERM) runs the cleanup or restore.

## 5. Code structure
Scripts (headers, comments and output in Spanish):
- create: `scripts/services/lib/service-common.sh`.
  - Header and exit codes, as in `vm-exec.sh`.
  - `MARKER`, `fail`, the validators, `check_archive`, `guest`, `require_managed`, `read_state`, `render_unit`, `encode_env`, `write_remote_file`.
- create: `scripts/services/lib/service-common.bats` - validator, `check_archive`, `render_unit` and `encode_env` tests.
- create: `scripts/services/list-services.sh`, `create-service.sh`, `edit-service.sh`, `redeploy-service.sh`, `delete-service.sh`, `start-service.sh`, `stop-service.sh`, `restart-service.sh`, `enable-service.sh`, `disable-service.sh`, `status-service.sh`, `logs-service.sh`.
- create: one `.bats` next to each script.
- create: `scripts/services/test_helper.bash`.
  - Loads `../prepare/test_helper`.
  - Adds `setup_services_stubs`, `setup_env_file`, `make_archive`, `make_bad_archive` and `run_remote_locally` (section 6).

Backend:
- modify: `stratus-srv/internal/service/errors.go`.
  - Add `ErrServiceExists`, `ErrNotManagedService`, `ErrInvalidArchive` and `ErrExecutableMissing`.
  - Add `ServiceRollbackError`.
- modify: `stratus-srv/internal/service/users_create.go` - `writeSecretFile(pattern, content string)`. `CreateUser` passes `"stratus-password-*"`.
- create: `stratus-srv/internal/service/services_service.go`.
  - `ServicesConfig`, `ServicesService`, `NewServicesService`, `getVM`, `History`, `MaxArchiveBytes`.
  - `mapServiceError`, `parseResult`, the exit-code constants.
- create: `stratus-srv/internal/service/services_validation.go` - field validators, `ValidateCreateService`, `ValidateEditService`.
- create: `stratus-srv/internal/service/services_archive.go` - `saveArchive`, `envFileContent`.
- create: `stratus-srv/internal/service/services_list.go` - `ListServices`, `parseServices`.
- create: `stratus-srv/internal/service/services_create.go` - `CreateService`.
- create: `stratus-srv/internal/service/services_edit.go` - `EditService`.
- create: `stratus-srv/internal/service/services_redeploy.go` - `RedeployService`.
- create: `stratus-srv/internal/service/services_delete.go` - `DeleteService`.
- create: `stratus-srv/internal/service/services_actions.go` - `ServiceAction`, `ServiceStatus`, `ServiceLogs`.
- create: tests `services_validation_test.go`, `services_archive_test.go`, `services_list_test.go` (with `newServicesEnv`), `services_create_test.go`, `services_edit_test.go`, `services_redeploy_test.go`, `services_delete_test.go`, `services_actions_test.go`, `services_service_test.go`, all in `stratus-srv/internal/service/`.
- create: `stratus-srv/internal/handler/services.go` - `RegisterServices` with the routes of section 6 and `readUpload`.
- create: `stratus-srv/internal/handler/services_test.go`.
- modify: `stratus-srv/internal/handler/respond.go` - map the new sentinels and `*ServiceRollbackError`.
- modify: `stratus-srv/internal/handler/prepare.go` - `maxBodyBytes` goes from 64 KiB to 256 KiB, so 50 JSON-escaped values of 1024 characters fit.
- modify: `stratus-srv/cmd/server/main.go` - build `NewServicesService` with the shared `locks` and call `handler.RegisterServices`.

Infra:
- modify: `stratus-ui/nginx.conf` - add `client_max_body_size 210m;` in `location /api/`. The nginx default of 1 MB would reject uploads.

Frontend:
- create: `stratus-ui/src/components/ui/dialog.tsx` - from `pnpm dlx shadcn@latest add dialog`, with no new package.
- modify: `stratus-ui/src/features/home/modules.ts` - `services`: `enabled: true`, with the description "Desplegar aplicaciones como servicios systemd y controlarlos desde la consola."
- modify: `stratus-ui/src/App.tsx` - route `/services` to `<Services />`.
- modify: `stratus-ui/src/routes/Home.test.tsx` - the 002 test "AC-27 ..." becomes the 003 test "AC-35 ..." (REQ-30).
- create: `stratus-ui/src/features/services/types.ts`, `api.ts` (with a local `sendForm` for `FormData`) and `validation.ts`.
- create: `stratus-ui/src/features/services/EnvVarFields.tsx` - key and value rows. Values use `type="password"`. Rows can be added and removed.
- create: `stratus-ui/src/features/services/StateWarning.tsx` - warning when the state is set and not `active`.
- create: `stratus-ui/src/features/services/ServiceTable.tsx` - the table with 10 buttons per row.
- create: `stratus-ui/src/features/services/CreateServiceForm.tsx`.
- create: `stratus-ui/src/features/services/EditServiceDialog.tsx` - prefilled, with the "Reemplazar variables de entorno" checkbox.
- create: `stratus-ui/src/features/services/RedeployServiceDialog.tsx` - archive and executable, then a confirmation step.
- create: `stratus-ui/src/features/services/DeleteServiceDialog.tsx`.
- create: `stratus-ui/src/features/services/ServiceOutputPanel.tsx` - the "Salida" card with a `<pre>`.
- create: `stratus-ui/src/routes/Services.tsx`.
- create: tests `stratus-ui/src/features/services/{validation.test.ts,StateWarning.test.tsx,ServiceTable.test.tsx,CreateServiceForm.test.tsx,EditServiceDialog.test.tsx,RedeployServiceDialog.test.tsx,DeleteServiceDialog.test.tsx}` and `stratus-ui/src/routes/Services.test.tsx`.
- keep and reuse: `features/prepare/{api (request, send, errorMessage, ApiError), VmSelector, HistoryTable, useOperation}`, `components/OperationResult.tsx`, and the shadcn components `card`, `table`, `field`, `input`, `select`, `checkbox`, `alert`, `alert-dialog`, `button`, `badge`, `spinner`, `empty`.

## 6. Interfaces and data
Scripts (argv; env `APP_SSH_KEY_PATH` inherited):

| Script | Arguments | stdout |
|---|---|---|
| `list-services.sh` | `<vm>` | one row per app service: `name⇥active_state⇥enabled_state⇥restart⇥restart_sec⇥executable⇥args⇥env_keys⇥description` (`env_keys` comma-separated, may be empty) |
| `create-service.sh` | `<vm> <name> <description> <archive_file> <executable> <args> <restart> <restart_sec> <env_file>` | `Servicio <name> creado en <vm>` then `active_state=<state>` |
| `edit-service.sh` | `<vm> <name> <description> <executable> <args> <restart> <restart_sec> <env_file\|->` (`-` keeps `/etc/<name>.env`) | `Servicio <name> actualizado en <vm>`, then `active_state=<state>` only if it restarted |
| `redeploy-service.sh` | `<vm> <name> <archive_file> <executable>` | `Servicio <name> redesplegado en <vm>`, then `active_state=<state>` only if it started |
| `delete-service.sh` | `<vm> <name>` | `Servicio <name> eliminado de <vm>` |
| `start-service.sh`, `restart-service.sh` | `<vm> <name>` | `Servicio <name> iniciado\|reiniciado en <vm>` then `active_state=<state>` |
| `stop-service.sh`, `enable-service.sh`, `disable-service.sh` | `<vm> <name>` | `Servicio <name> detenido\|habilitado\|deshabilitado en <vm>` |
| `status-service.sh` | `<vm> <name>` | output of `systemctl status --no-pager --full <name>` |
| `logs-service.sh` | `<vm> <name>` | `journalctl -u <name>.service -n 100 --no-pager` |

`args` is one argv item: empty, or tokens separated by single spaces.

Validation, step 1 (exit 2, no VBoxManage or ssh call):
- `vm`: `^[a-z0-9][a-z0-9-]{0,31}$`.
- `name`: `^[a-z][a-z0-9-]{0,31}$`.
- `description`, checked with `LC_ALL=C.UTF-8`: 1-100 characters, no `[[:cntrl:]]`, `%`, `\` or `"`.
- `executable`: `^[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)*$`, at most 255 characters, no `.` or `..` segment.
- `args`: empty, or 1-16 tokens split on single spaces, each matching `^[A-Za-z0-9._/:=,@+-]{1,128}$`. A leading, trailing or double space is invalid.
- `restart`: `no`, `always` or `on-failure`.
- `restart_sec`: `^(0|[1-9][0-9]{0,3})$` and at most 3600.
- `env_file`: a readable file with at most 50 lines, each `KEY=VALUE`. The key matches `^[A-Z_][A-Z0-9_]{0,63}$` and keys are unique. The value, checked with `LC_ALL=C`, matches `^[ -~]*$` and has at most 1024 characters.
- `archive_file`: a readable file.
- `APP_SSH_KEY_PATH` is set.

Exit codes (one table, the same in every header; each script lists the codes it uses):

| Code | Meaning | Scripts |
|---|---|---|
| 0 | éxito | all |
| 2 | argumentos inválidos | all |
| 3 | VM no encontrada | all |
| 4 | fallo de SSH | all |
| 5 | falló un paso remoto (la limpieza o la restauración terminó bien) | all |
| 6 | la unidad no existe o no la creó Stratus | all except list and create |
| 7 | VM sin IP | all |
| 8 | ya existe un servicio, usuario, grupo, `/opt/<name>` o `/etc/<name>.env` con ese nombre | create |
| 9 | falló la operación y también la limpieza o la restauración | create, edit, redeploy |
| 10 | archivo comprimido inválido | create, redeploy |
| 11 | el ejecutable no existe en el directorio de la aplicación | create, edit, redeploy |
| 99 | operación no implementada | all |

`service-common.sh` (sourced after `vm-exec.sh`; it uses the globals `vm_name` and `name`):
```
MARKER='# stratus-managed: servicio creado por Stratus'
fail <code> <msg>                       # msg to stderr, exit code
validate_name|validate_description|validate_executable|validate_args|validate_restart|validate_restart_sec|validate_env_file <value>
                                        # return 1 with the reason on stderr; the caller exits 2
check_archive <file> <executable>       # host only; return 10 (gzip, listing, type, "/", "..") or 11 (executable not a "-" entry)
guest <code> <cmd...>                   # vm_exec; 2/4/7 pass through; other failure prints "${fail_msg:-Falló el paso <prog> en la VM (código N)}", exits <code>
require_managed                         # vm_exec sh -c 'grep -qxF "$2" "/etc/systemd/system/$1.service" 2>/dev/null && echo yes || echo no' sh <name> "$MARKER"
                                        # "no" -> exit 6 "Solo se pueden gestionar servicios creados por Stratus"
read_state                              # prints ActiveState, or "activating (<SubState>)" (systemctl show -p ... --value)
is_running <state>                      # true for active or activating*
render_unit <name> <desc> <exe> <args> <restart> <restart_sec>
                                        # prints MARKER and every REQ-11 line; ExecStart=/opt/<name>/<exe>[ <args>]
encode_env <env_file>                   # prints KEY="value" lines, escaping \ " ` $
write_remote_file <dest> <mode>         # stdin -> vm_exec sh -c 'umask 077; cat >"$1.tmp" && chown root:root "$1.tmp" && chmod "$2" "$1.tmp" && mv -f "$1.tmp" "$1"' sh <dest> <mode>
read_unit_fields                        # prints description⇥executable⇥args⇥restart⇥restart_sec parsed from the unit file
```

`create-service.sh` steps:
1. Validate, run `check_archive` (10 or 11), `showvminfo` (3) and `vm_ip` (7).
2. Name check: `sh -c 'if systemctl cat "$1.service" >/dev/null 2>&1 || [ -e "/etc/systemd/system/$1.service" ] || getent passwd "$1" >/dev/null || getent group "$1" >/dev/null || [ -e "/opt/$1" ] || [ -e "/etc/$1.env" ]; then echo exists; else echo free; fi' sh <name>`. `exists` exits 8 with "Ya existe un servicio o usuario con ese nombre en la VM".
3. `useradd --system --user-group --home-dir /opt/<name> --no-create-home --shell /usr/sbin/nologin <name>`. From here on the trap is active.
4. `install -d -m 755 -o <name> -g <name> /opt/<name>`.
5. Stream the archive into `tar -xzf - -C /opt/<name> --no-same-owner`.
6. `chown -R <name>:<name> /opt/<name>`, then `chmod u+x /opt/<name>/<exe>`.
7. `encode_env | write_remote_file /etc/<name>.env 600`.
8. `render_unit | write_remote_file /etc/systemd/system/<name>.service 644`.
9. `systemctl daemon-reload`, then `systemctl enable --now <name>`. Any failure in steps 3-9 exits 5 through the trap.
10. Disarm the trap, run `sleep 3`, then `read_state`, and print both lines.

Trap of `create-service.sh` (non-zero exit after step 3, including 143). Every step is tried even if one fails, and output goes to stderr:
1. `systemctl disable --now <name>` (tolerant).
2. `rm -f` of the unit, the env file and their `.tmp` files.
3. `rm -rf /opt/<name>`.
4. `userdel` if the user exists, then `groupdel` if the group exists.
5. `daemon-reload`.
6. Verify that none of them exists.
7. If any step fails, print `Limpieza fallida; puede quedar en la VM: <unit>, /etc/<name>.env, /opt/<name>, usuario y grupo <name>` and exit 9. Otherwise keep the original code.

`edit-service.sh` steps:
1. Validate, run `showvminfo` and `vm_ip`, then `require_managed`.
2. `sh -c '[ -f "/opt/$1/$2" ] && echo yes || echo no' sh <name> <exe>`. `no` exits 11.
3. Run `read_state`; `was_running` is the result of `is_running`.
4. Back up: `rm -rf` then `mkdir -m 700` of `/var/lib/stratus/backup/<name>`, and `cp -p` of the unit and, if present, the env file. From here on the trap is active.
5. Run `chmod u+x` on the executable.
6. `render_unit | write_remote_file` for the unit.
7. If `env_file` is not `-`: `encode_env | write_remote_file /etc/<name>.env 600`.
8. Run `daemon-reload`. If `was_running`, run `systemctl restart <name>`.
9. Delete the backup directory. A failure here exits 5 with "No se pudo borrar el respaldo ..." and makes no restore.
10. If it restarted, run `sleep 3` and `read_state`. Print the result.

Restore trap (edit):
1. `cp -p` the backups back, then `daemon-reload`.
2. If `was_running`, `systemctl restart`; otherwise `systemctl stop`.
3. `rm -rf` the backup directory.
4. If any step fails, print `Restauración fallida; puede quedar en la VM: ...` and exit 9.

`redeploy-service.sh` steps:
1. Validate, run `check_archive` (10 or 11), `showvminfo`, `vm_ip` and `require_managed`.
2. Run `read_unit_fields` and `read_state` (`was_running`).
3. Back up the unit to `/var/lib/stratus/backup/<name>/` and `rm -rf /opt/.stratus-backup-<name>`. From here on the trap is active.
4. If `was_running`, run `systemctl stop <name>`.
5. `mv /opt/<name> /opt/.stratus-backup-<name>`, then `install -d -m 755 -o <name> -g <name> /opt/<name>`.
6. Stream the archive into `tar`, run `chown -R`, then `chmod u+x <exe>`.
7. If the executable changed, `render_unit` with the new executable and the current description, args, Restart and RestartSec, write it, then `daemon-reload`.
8. If `was_running`, run `systemctl start <name>`.
9. `rm -rf` both backups. A failure exits 5 with no restore.
10. If it started, run `sleep 3` and `read_state`. Print the result.

Restore trap (redeploy):
1. `systemctl stop` (tolerant).
2. If step 5 ran: `rm -rf /opt/<name>`, then `mv /opt/.stratus-backup-<name> /opt/<name>`.
3. `cp -p` the unit back, then `daemon-reload`.
4. If `was_running`, `systemctl start`.
5. `rm -rf` the backups.
6. If any step fails, exit 9 as in edit.

`delete-service.sh` steps, each with `fail_msg="No se pudo eliminar el servicio <name>: falló el paso <n>"` and exit 5:
1. Validate, run `showvminfo` and `vm_ip`, then `require_managed`.
2. `systemctl disable --now <name>`.
3. `rm -f /etc/<name>.env`.
4. `rm -rf /opt/<name>`.
5. `sh -c '! getent passwd "$1" >/dev/null || userdel "$1"' sh <name>`.
6. `sh -c '! getent group "$1" >/dev/null || groupdel "$1"' sh <name>`.
7. `rm -f /etc/systemd/system/<name>.service`. This runs last on purpose.
8. `sh -c 'systemctl daemon-reload && { systemctl reset-failed "$1.service" 2>/dev/null || true; }' sh <name>`.
9. Print the message.

Action scripts:
1. Validate, run `showvminfo`, `vm_ip` and `require_managed`.
2. Run `systemctl <start|stop|restart|enable|disable> <name>` (exit 5 on failure).
3. Start and restart only: `sleep 3`, then `read_state`.
4. Print the result.

`status-service.sh` runs `sh -c 'systemctl status --no-pager --full "$1"; rc=$?; [ "$rc" -eq 0 ] || [ "$rc" -eq 3 ]' sh <name>`. Inactive and failed services exit 0 (REQ-19).

bats helper `scripts/services/test_helper.bash`:
```
load ../prepare/test_helper            # assert_script_structure, setup_stubs (sleep stubbed), stub_command, stub_vm_ip
setup_services_stubs <vm> <ip|->       # setup_stubs + stub_vm_ip; the ssh stub writes calls.log, remote.log (unquoted),
                                       # ssh-stdin.log and "$BATS_TEST_TMPDIR/stdin/<n>" (stdin of call n), and answers from env:
                                       # LOOKUP=exists|free (create name check, default free), MANAGED=yes|no (default yes),
                                       # ACTIVE=<state> (read_state, default active), EXE_PRESENT=yes|no (edit, default yes),
                                       # UNIT_FIELDS=<tsv> (read_unit_fields), FAIL_REMOTE=<ERE>, FAIL_RESTORE=<ERE>
                                       # (exit 1 only for cleanup or restore commands), STATUS_RC=<n> (systemctl status)
setup_env_file <KEY=VALUE>...          # "$BATS_TEST_TMPDIR/env" with mode 600; exports ENV_FILE
make_archive <out> <file>...           # real tar.gz built with "tar -C <dir> -czf <out> ." (so "./" prefixes)
make_bad_archive <kind> <out>          # dotdot, absolute (tar -P), symlink, hardlink, fifo, corrupt
run_remote_locally                     # list only: the ssh stub runs the program here, swapping the two directory args for
                                       # fixture dirs, with a systemctl stub (show, is-enabled)
```
Scripts under test run with `</dev/null` unless the test feeds stdin.

SQLite: no schema change. History rows use module `services`. Operations: `create_service`, `edit_service`, `redeploy_service`, `delete_service`, `start_service`, `stop_service`, `restart_service`, `enable_service` and `disable_service` (always recorded), and `list_services`, `service_status` and `service_logs` (failures only).

Go (package `service`):
```go
var (
  ErrServiceExists     = errors.New("Ya existe un servicio o usuario con ese nombre en la VM")
  ErrNotManagedService = errors.New("Solo se pueden gestionar servicios creados por Stratus")
  ErrInvalidArchive    = errors.New("Archivo comprimido inválido")
  ErrExecutableMissing = errors.New("El ejecutable no existe en el directorio de la aplicación")
)
// Operation "create" -> "No se pudo crear el servicio <n> y la limpieza falló; revise la VM. <Detail>"
// "edit" -> "No se pudo editar el servicio <n> y la restauración falló; revise la VM. <Detail>"
// "redeploy" -> "No se pudo redesplegar el servicio <n> y la restauración falló; revise la VM. <Detail>"
type ServiceRollbackError struct { Operation, Name, Detail string } // Unwrap() returns ErrScriptFailed

const ( exitNotManaged = 6; exitServiceExists = 8; exitRollbackFailed = 9; exitInvalidArchive = 10; exitExecutableMissing = 11 )
func mapServiceError(operation, name string, err error) error // only for *OperationError; 9 -> ServiceRollbackError for create/edit/redeploy
func parseResult(stdout []byte) ServiceResult                 // line 1 message; "active_state=<s>" line -> ActiveState

type EnvVar struct { Key string `json:"key"`; Value string `json:"value"` }
type CreateServiceInput struct { Name, Description, Executable, Args, Restart string; RestartSec int `json:"restart_sec"`; Env []EnvVar `json:"env"` } // snake_case tags
type EditServiceInput struct { Description, Executable, Args, Restart string; RestartSec int `json:"restart_sec"`; ReplaceEnv bool `json:"replace_env"`; Env []EnvVar `json:"env"` }
type ArchiveUpload struct { Filename string; Body io.Reader }
type ServiceView struct { Name, Description, ActiveState, EnabledState, Restart string; RestartSec int; Executable, Args string; EnvKeys []string }
  // json: name, description, active_state, enabled_state, restart, restart_sec, executable, args, env_keys
type ServiceResult struct { Message string `json:"message"`; ActiveState string `json:"active_state,omitempty"` }

func ValidateServiceName(s string) error
func ValidateDescription(s string) error   // utf8.ValidString, 1-100 runes, unicode.IsPrint, not % \ "
func ValidateExecutable(s string) error
func ValidateArgs(s string) error
func ValidateRestart(s string) error
func ValidateRestartSec(n int) error
func ValidateEnv(env []EnvVar) error       // <=50, keys regex and unique, values <=1024 bytes 0x20-0x7E
func ValidateCreateService(in CreateServiceInput) error
func ValidateEditService(in EditServiceInput) error // !ReplaceEnv && len(Env) > 0 -> "Sin reemplazar variables no se envían valores"

func saveArchive(a ArchiveUpload, max int64) (path string, err error)
  // suffix ".tar.gz" else ValidationError "El archivo debe ser .tar.gz"; > max bytes or *http.MaxBytesError
  // -> ValidationError "El archivo supera 200 MB" (file removed)
func envFileContent(env []EnvVar) string     // "KEY=VALUE\n" per row
func writeSecretFile(pattern, content string) (string, error) // users_create.go, now with a pattern

type ServicesConfig struct { MaxArchiveBytes int64 } // 200 << 20 in main.go
func NewServicesService(cfg ServicesConfig, runner ScriptRunner, vms *repository.VMRepository, history *repository.HistoryRepository, locks *VMLocks, logger *log.Logger) *ServicesService
func (s *ServicesService) MaxArchiveBytes() int64
func (s *ServicesService) ListServices(ctx context.Context, vm string) ([]ServiceView, error) // recordSuccess false; bad row -> "salida inesperada de list-services.sh"; [] when none
func (s *ServicesService) CreateService(ctx context.Context, vm string, in CreateServiceInput, a ArchiveUpload) (ServiceResult, error)
  // validate -> getVM -> saveArchive -> secret file -> argv of section 6; both files removed by defer
func (s *ServicesService) EditService(ctx context.Context, vm, name string, in EditServiceInput) (ServiceResult, error) // last arg "-" when !ReplaceEnv
func (s *ServicesService) RedeployService(ctx context.Context, vm, name, executable string, a ArchiveUpload) (ServiceResult, error)
func (s *ServicesService) DeleteService(ctx context.Context, vm, name string) (string, error)
func (s *ServicesService) ServiceAction(ctx context.Context, vm, name, action string) (ServiceResult, error) // action in start|stop|restart|enable|disable -> "services/<action>-service.sh"
func (s *ServicesService) ServiceStatus(ctx context.Context, vm, name string) (string, error) // recordSuccess false
func (s *ServicesService) ServiceLogs(ctx context.Context, vm, name string) (string, error)   // recordSuccess false
func (s *ServicesService) History(ctx context.Context) ([]repository.Operation, error)       // module "services", limit 50
```

Go (package `handler`): `func RegisterServices(mux *http.ServeMux, svc *service.ServicesService)`.
- `readUpload(w, r, svc, payload any) (service.ArchiveUpload, bool)` sets `MaxBytesReader(MaxArchiveBytes + 1 MiB)` and opens `r.MultipartReader()`.
- The first part must be `payload`: JSON, at most 256 KiB, decoded into `payload`. The second part must be `archive`, and becomes `ArchiveUpload{part.FileName(), part}`. Any other order or name is 400.

Error mapping:
- `ErrServiceExists`, `ErrNotManagedService`: 409.
- `ErrInvalidArchive`, `ErrExecutableMissing`: 422.
- `*ServiceRollbackError`: 500 with `Error()`.
- The rest as in 001 and 002.

HTTP API (JSON `snake_case`):

| Method and path | Body | Success |
|---|---|---|
| `GET /api/services/vms/{vm}/services` | - | 200 `{"services": [ServiceView]}` |
| `POST /api/services/vms/{vm}/services` | multipart: `payload` = CreateServiceInput, `archive` | 201 `{"message","active_state"}` |
| `PUT /api/services/vms/{vm}/services/{name}` | EditServiceInput | 200 `{"message","active_state"?}` |
| `POST /api/services/vms/{vm}/services/{name}/redeploy` | multipart: `payload` = `{"executable"}`, `archive` | 200 `{"message","active_state"?}` |
| `DELETE /api/services/vms/{vm}/services/{name}` | - | 200 `{"message"}` |
| `POST /api/services/vms/{vm}/services/{name}/{start\|stop\|restart\|enable\|disable}` (5 routes) | - | 200 `{"message","active_state"?}` |
| `GET /api/services/vms/{vm}/services/{name}/status` | - | 200 `{"output"}` |
| `GET /api/services/vms/{vm}/services/{name}/logs` | - | 200 `{"output"}` |
| `GET /api/services/history` | - | 200 `{"operations": [...]}` via `historyHandler` |

Frontend:
```ts
// features/services/types.ts
type Restart = "no" | "always" | "on-failure";
type ServiceAction = "start" | "stop" | "restart" | "enable" | "disable";
interface ServiceRow { name: string; description: string; active_state: string; enabled_state: string; restart: Restart; restart_sec: number; executable: string; args: string; env_keys: string[] }
interface EnvVar { key: string; value: string }
interface ServiceConfig { description: string; executable: string; args: string; restart: Restart; restart_sec: number }
interface CreateServiceInput extends ServiceConfig { name: string; env: EnvVar[] }
interface EditServiceInput extends ServiceConfig { replace_env: boolean; env: EnvVar[] }
interface ServiceResult { message: string; active_state?: string }
// features/services/api.ts (request, errorMessage, ApiError from features/prepare/api; sendForm posts FormData with payload then archive)
listServices(vm): Promise<ServiceRow[]>; createService(vm, input, archive: File): Promise<ServiceResult>;
editService(vm, name, input): Promise<ServiceResult>; redeployService(vm, name, executable, archive: File): Promise<ServiceResult>;
deleteService(vm, name): Promise<string>; runServiceAction(vm, name, action): Promise<ServiceResult>;
getServiceStatus(vm, name): Promise<string>; getServiceLogs(vm, name): Promise<string>; getServicesHistory(): Promise<OperationRow[]>;
// features/services/validation.ts - each returns a Spanish error string or null
validateServiceName; validateDescription; validateExecutable; validateArgs; validateRestartSec(text);
validateEnvRows(rows): (string | null)[] plus a list-level error; validateArchive(file | null) // > 200*1024*1024 or not ".tar.gz"
createServiceErrors(form); editServiceErrors(form); redeployErrors(form): Record<string, string>
// components
EnvVarFields({ rows: EnvVar[]; onChange(rows); errors; disabled; keysLocked?: boolean })
StateWarning({ name: string; state?: string })  // state set and not "active": "El servicio <name> quedó en estado <state>; revise Estado y Logs"
ServiceTable({ services: ServiceRow[]; busy: boolean; onAction(name, action); onStatus(name); onLogs(name); onEdit(row); onRedeploy(row); onDelete(name) })
  // columns Servicio, Descripción, Estado, Habilitado, Restart, RestartSec, Acciones; buttons Iniciar, Detener, Reiniciar, Habilitar,
  // Deshabilitar, Estado, Logs, Editar, Redesplegar, Eliminar with aria-label "<Botón> <name>", all disabled while busy
CreateServiceForm({ vm: Vm | null; busy: boolean; onBusyChange(b); onDone() })  // vm null: disabled with "Seleccione una VM para continuar"
EditServiceDialog({ vm: string; service: ServiceRow | null; onClose(); onDone() })
RedeployServiceDialog({ vm: string; service: ServiceRow | null; onClose(); onDone() }) // step 2 warns that all of /opt/<name> is replaced
DeleteServiceDialog({ vm: string; name: string | null; onClose(); onDone() })
ServiceOutputPanel({ title: string; output: string })  // "Estado de <name>" / "Logs de <name>"
```
`Services.tsx`:
- On mount it loads `listVms` and `getServicesHistory`.
- It calls `listServices` on each VM selection and after each operation. Success shows `toast.success("Lista de servicios actualizada")`. Failure shows an `Alert` "Servicios no disponibles".
- Row actions, status and logs run through one `useOperation` that sets `busy`. The result shows above the table with `StateWarning`. Status and logs output goes to the "Salida" card.
- With no VM, the list is empty and no request is made.

## 7. Test-first
- Framework and commands: Go `testing` (table-driven) with `cd stratus-srv && go test ./...`; Vitest + Testing Library with `cd stratus-ui && pnpm test`; bats with `bats -r scripts/`.
- Location and naming: tests sit next to the code. Go uses `TestAC<n>_<Behavior>`, Vitest `it("AC-<n> ...")`, and bats `@test "AC-<n> ..."`. Go tests use `fakeRunner` and a temp SQLite file (`newServicesEnv`). UI tests mock `fetch` with `vi.spyOn(globalThis, "fetch")` and mock `sonner`'s `toast.success`. bats tests use `scripts/services/test_helper.bash`. Level "manual" rows are steps of the real-VM before/after demo (P8).

| Test | Level | File | Covers (US / REQ / AC) | Given - when - then |
|---|---|---|---|---|
| AC-1 create-service ejecuta los pasos de REQ-10 en orden | unit | `scripts/services/create-service.bats` | US-1 / REQ-10, REQ-26 / AC-1 | given valid args, a `make_archive` with `demian-lee templates/x static/x data/x` and an env file - when it runs - then remote.log has, in order: `useradd --system --user-group --home-dir /opt/demian-lee --no-create-home --shell /usr/sbin/nologin demian-lee`, `install -d`, `tar -xzf - -C /opt/demian-lee`, `chown -R demian-lee:demian-lee`, `chmod u+x /opt/demian-lee/demian-lee`, the env write, the unit write, `daemon-reload`, `enable --now demian-lee`; the stdin of the tar call equals the archive; stdout is the message and `active_state=active` |
| TestAC1_CreateServicePassesDiscreteArgs | unit | `stratus-srv/internal/service/services_create_test.go` | US-1 / REQ-1, REQ-12, REQ-13 / AC-1 | given the AC-1 input - when CreateService runs - then argv is `[vm1 demian-lee <desc> <archive path> demian-lee "" on-failure 5 <env path>]` and the result is the message with `active` |
| TestAC1_CreateServiceRouteMultipart | unit | `stratus-srv/internal/handler/services_test.go` | US-1 / REQ-1 / AC-1 | given multipart `payload` then `archive` - when POST `/api/services/vms/vm1/services` - then 201 `{"message","active_state":"active"}`; `archive` before `payload` gives 400 |
| AC-1 submits each field and shows success and active | unit | `stratus-ui/src/features/services/CreateServiceForm.test.tsx` | US-1 / REQ-1, REQ-15, REQ-32 / AC-1 | given the AC-1 values - when submitted - then the FormData has `payload` (JSON with the 3 variables) before `archive`, and the success result shows with no warning |
| AC-2 la unidad tiene la marca y cada línea de REQ-11 | unit | `scripts/services/lib/service-common.bats` | US-1 / REQ-11 / AC-2 | given demian-lee values - when `render_unit` runs - then the output has `MARKER` and exactly the REQ-11 lines, with `ExecStart=/opt/demian-lee/demian-lee` |
| AC-2 create-service escribe el env 600 root:root | unit | `scripts/services/create-service.bats` | US-1 / REQ-10 / AC-2 | given an env file - when it runs - then the env write has `/etc/demian-lee.env`, `chown root:root`, `chmod 600` and `mv -f`, and its stdin has the 3 encoded lines |
| AC-2 inspection of user, ownership, env and unit | manual | real VM demo | US-1 / REQ-10, REQ-11 / AC-2 | `getent passwd demian-lee`, `ls -ln /opt/demian-lee`, `stat -c '%a %U:%G' /etc/demian-lee.env`, `systemctl cat demian-lee` |
| AC-3 rejects invalid names | unit | `stratus-ui/src/features/services/validation.test.ts` | US-1 / REQ-2 / AC-3 | given `Demian`, `1app`, `app;rm`, `app_x`, empty, 33 chars - when validated - then each returns an error |
| AC-3 invalid name shows error and sends nothing | unit | `stratus-ui/src/features/services/CreateServiceForm.test.tsx` | US-1 / REQ-2 / AC-3 | given `app_x` - when submitted - then a field error and no fetch |
| TestAC3_ValidateServiceName | unit | `stratus-srv/internal/service/services_validation_test.go` | US-1 / REQ-2 / AC-3 | given the 6 names - when CreateService runs - then ErrInvalidInput, no runner call, no temp file |
| AC-4 create-service sale con 8 si el nombre está en uso | unit | `scripts/services/create-service.bats` | US-1 / REQ-2 / AC-4 | given LOOKUP=exists - when it runs - then exit 8, the spec text, no `useradd`; the lookup program checks `systemctl cat`, the unit path, `getent passwd`, `getent group`, `/opt/$1` and `/etc/$1.env` |
| TestAC4_CreateServiceNameInUseMapsError | unit | `stratus-srv/internal/service/services_create_test.go` | US-1 / REQ-2 / AC-4 | given exit 8 - when CreateService runs - then ErrServiceExists |
| TestAC4_AC6_AC7_AC13_ServicesHandlerMapsCreateErrors | unit | `stratus-srv/internal/handler/services_test.go` | US-1 / REQ-2, REQ-5, REQ-6, REQ-14 / AC-4, AC-6, AC-7, AC-13 | given create exits 8, 10, 11, 9 - when POST - then 409, 422, 422, 500 with the spec texts |
| AC-4 name in use rejected without changes | manual | real VM demo | US-1 / REQ-2 / AC-4 | create `ssh`, `www-data`, `staff`, and `api` with `/opt/api` present - each shows the text and `ls /opt /etc` is unchanged |
| AC-5 rejects 201 MB and .zip | unit | `stratus-ui/src/features/services/validation.test.ts` | US-1 / REQ-4 / AC-5 | given a File with size 201 MiB, and `app.zip` - when validateArchive runs - then the two spec texts |
| AC-5 invalid archive shows error and sends nothing | unit | `stratus-ui/src/features/services/CreateServiceForm.test.tsx` | US-1 / REQ-4 / AC-5 | given `app.zip` - when submitted - then "El archivo debe ser .tar.gz" and no fetch |
| TestAC5_CreateServiceRejectsArchive | unit | `stratus-srv/internal/service/services_archive_test.go` | US-1, US-6 / REQ-4, REQ-12 / AC-5 | given MaxArchiveBytes 10 and an 11-byte body, or `app.zip` - when CreateService and RedeployService run - then the spec texts, no runner call, no `stratus-archive-*` file left |
| TestAC5_ServicesRouteRejectsOversizedBody | unit | `stratus-srv/internal/handler/services_test.go` | US-1 / REQ-4 / AC-5 | given a body over MaxArchiveBytes + 1 MiB - when POST - then 400 "El archivo supera 200 MB" and no runner call |
| AC-6 check_archive rechaza archivos inválidos | unit | `scripts/services/lib/service-common.bats` | US-1, US-6 / REQ-5 / AC-6 | given `make_bad_archive` dotdot, absolute, symlink, hardlink, fifo, corrupt - when `check_archive` runs - then each returns 10; a valid archive returns 0 |
| AC-6 create-service y redeploy-service salen con 10 sin tocar la VM | unit | `scripts/services/{create-service,redeploy-service}.bats` | US-1, US-6 / REQ-5 / AC-6 | given a symlink archive - when each runs - then exit 10, "Archivo comprimido inválido", and calls.log has no VBoxManage or ssh |
| TestAC6_InvalidArchiveMapsError | unit | `stratus-srv/internal/service/services_create_test.go` | US-1, US-6 / REQ-5 / AC-6 | given exit 10 - when CreateService and RedeployService run - then ErrInvalidArchive |
| AC-7 ejecutable ausente sale con 11 | unit | `scripts/services/{create-service,redeploy-service,edit-service}.bats` | US-1, US-5, US-6 / REQ-6 / AC-7 | given `bin/app` missing from the archive (create, redeploy: no ssh), or EXE_PRESENT=no (edit: no backup or write) - when run - then exit 11 and the spec text |
| AC-7 rejects `../x` and `/bin/sh` | unit | `stratus-ui/src/features/services/validation.test.ts` | US-1, US-5, US-6 / REQ-6 / AC-7 | given `../x`, `/bin/sh`, `a/./b`, 256 chars - when validated - then errors |
| TestAC7_ValidateExecutableAndMapMissing | unit | `stratus-srv/internal/service/services_validation_test.go` | US-1, US-5, US-6 / REQ-6 / AC-7 | given `../x` and `/bin/sh` - then ErrInvalidInput and no runner call; given exit 11 on create, edit, redeploy - then ErrExecutableMissing |
| AC-8 rejects description, args, Restart and RestartSec | unit | `stratus-ui/src/features/services/validation.test.ts` | US-1, US-5 / REQ-3, REQ-7, REQ-8 / AC-8 | given `%`, `\`, a line break, args with `;`, `$`, `"`, 17 tokens, `on-abort`, -1, 3601 - when validated - then errors; `Servidor de producción ñ` is valid |
| TestAC8_ValidateUnitFields | unit | `stratus-srv/internal/service/services_validation_test.go` | US-1, US-5 / REQ-3, REQ-7, REQ-8 / AC-8 | given the same values - when CreateService and EditService run - then ErrInvalidInput and no runner call |
| AC-8 create-service y edit-service salen con 2 ante campos inválidos | unit | `scripts/services/{create-service,edit-service}.bats` | US-1, US-5 / REQ-3, REQ-7, REQ-8 / AC-8 | given each invalid value - when run - then exit 2 and empty calls.log |
| AC-9 rejects invalid environment rows | unit | `stratus-ui/src/features/services/validation.test.ts` | US-1, US-5 / REQ-9 / AC-9 | given `port`, `1PORT`, a duplicate, a value with a line break, with `é`, 1025 chars, 51 rows - when validated - then errors |
| TestAC9_ValidateEnv | unit | `stratus-srv/internal/service/services_validation_test.go` | US-1, US-5 / REQ-9 / AC-9 | given the same rows - when CreateService and EditService run - then ErrInvalidInput and no runner call |
| AC-9 validate_env_file rechaza archivos inválidos | unit | `scripts/services/lib/service-common.bats` | US-1, US-5 / REQ-9 / AC-9 | given env files with those rows - when `validate_env_file` runs - then each returns 1 |
| AC-10 encode_env conserva el valor exacto | unit | `scripts/services/lib/service-common.bats` | US-1, US-5 / REQ-9 / AC-10 | given `K=a b "c" $HOME \x` - when `encode_env` runs - then the output is `K="a b \"c\" \$HOME \\x"` |
| AC-10 service sees the exact value | manual | real VM demo | US-1, US-5 / REQ-9 / AC-10 | `tr '\0' '\n' < /proc/$(systemctl show -p MainPID --value <n>)/environ \| grep ^K=` shows `K=a b "c" $HOME \x` |
| AC-11 Secreta123 no aparece en argv ni en la salida | unit | `scripts/services/{create-service,edit-service}.bats` | US-1, US-5 / REQ-13 / AC-11 | given `ADMIN_PASSWORD=Secreta123` - when run (ok, and failing with FAIL_REMOTE) - then calls.log, remote.log, stdout and stderr lack `Secreta123`; only ssh-stdin.log has it |
| TestAC11_EnvValuesOnlyInSecretFile | unit | `stratus-srv/internal/service/services_create_test.go` | US-1, US-5 / REQ-12, REQ-13, REQ-27 / AC-11 | given `Secreta123` - when CreateService and EditService run (ok and exit 5) - then during Run the env arg is a 0600 file with `ADMIN_PASSWORD=Secreta123` and no arg contains the value; afterwards the env and archive files are gone and the history and logs lack it |
| AC-11 list-services imprime solo las claves | unit | `scripts/services/list-services.bats` | US-2, US-5 / REQ-13, REQ-21 / AC-11 | given `run_remote_locally` with `/etc/demian-lee.env` holding `ADMIN_PASSWORD="Secreta123"` - when it runs - then the env_keys field is `PORT,SERVER_NAME,ADMIN_PASSWORD` and stdout lacks `Secreta123` |
| AC-11 value inputs are masked and edit shows no values | unit | `stratus-ui/src/features/services/EditServiceDialog.test.tsx` | US-1, US-5 / REQ-13, REQ-21 / AC-11 | given a row with 3 keys - when the dialog opens - then the keys show, no value input has text, and value inputs have `type="password"` |
| AC-12 create-service limpia si falla tras crear el usuario | unit | `scripts/services/create-service.bats` | US-1 / REQ-14 / AC-12 | given FAIL_REMOTE on `tar -xzf` - when it runs - then exit 5 and remote.log has `disable --now`, the `rm -f` of the unit and env, `rm -rf /opt/demian-lee`, userdel, groupdel, `daemon-reload` and the verify step |
| TestAC12_CreateServiceFailureReturnsOperationError | unit | `stratus-srv/internal/service/services_create_test.go` | US-1 / REQ-14, REQ-29 / AC-12 | given exit 5 - when CreateService runs - then *OperationError, a history row with result error, temp files gone |
| AC-13 create-service sale con 9 si la limpieza falla | unit | `scripts/services/create-service.bats` | US-1 / REQ-14 / AC-13 | given FAIL_REMOTE on tar and FAIL_RESTORE on userdel - when it runs - then exit 9 and stderr has the original error and "Limpieza fallida; puede quedar en la VM: ..." |
| TestAC13_CreateServiceCleanupFailureMessage | unit | `stratus-srv/internal/service/services_create_test.go` | US-1 / REQ-14 / AC-13 | given exit 9 and that stderr - when CreateService runs - then ServiceRollbackError text "No se pudo crear el servicio demian-lee y la limpieza falló; revise la VM. Limpieza fallida; ..." and the history stderr holds both lines |
| AC-14 create-service termina bien aunque la app falle | unit | `scripts/services/create-service.bats` | US-1 / REQ-15 / AC-14 | given ACTIVE=failed, then ACTIVE=`activating (auto-restart)` - when it runs - then exit 0, `sleep 3` before the state read, stdout `active_state=failed` / `active_state=activating (auto-restart)`, no cleanup |
| TestAC14_CreateServiceReturnsNonActiveState | unit | `stratus-srv/internal/service/services_create_test.go` | US-1 / REQ-15 / AC-14 | given stdout with `active_state=failed` - when CreateService runs - then no error and ActiveState `failed` |
| AC-14 shows the warning for non-active states | unit | `stratus-ui/src/features/services/StateWarning.test.tsx` | US-1, US-3, US-5, US-6 / REQ-15 / AC-14 | given `failed`, `activating (auto-restart)`, `active`, undefined - when rendered - then the warning naming Estado and Logs shows only for the first two |
| AC-14 crashing app stays installed | manual | real VM demo | US-1 / REQ-15 / AC-14 | an archive whose executable runs `exit 1`, with Restart `no` and with `on-failure` 5 - the UI shows `failed` / `activating (auto-restart)` and `systemctl cat` succeeds |
| AC-15 list-services lista solo unidades con la marca | unit | `scripts/services/list-services.bats` | US-2 / REQ-16 / AC-15 | given fixture units demian-lee and api (with marker) and ssh, cron (without), and systemctl answers - when it runs - then stdout has exactly the 2 rows with every field |
| TestAC15_ListServicesParsesRows | unit | `stratus-srv/internal/service/services_list_test.go` | US-2 / REQ-16 / AC-15 | given that stdout - when ListServices runs - then 2 ServiceView, argv `[vm1]`, no history row; empty stdout gives `[]`; a malformed row gives an error |
| TestAC15_ServicesListRouteReturnsJSON | unit | `stratus-srv/internal/handler/services_test.go` | US-2 / REQ-16 / AC-15 | given that stdout - when GET `/api/services/vms/vm1/services` - then 200 with snake_case fields and `env_keys` arrays |
| AC-15 shows each service row | unit | `stratus-ui/src/features/services/ServiceTable.test.tsx` | US-2 / REQ-16, REQ-32 / AC-15 | given demian-lee and api rows - when rendered - then each row shows name, description, `active`/`inactive`, `enabled`/`disabled`, Restart and RestartSec |
| AC-15 toast on a successful list, message on failure | unit | `stratus-ui/src/routes/Services.test.tsx` | US-2 / REQ-16, REQ-29 / AC-15 | given vm1 selected - when the list loads - then "Lista de servicios actualizada"; given a 500 - then the "Servicios no disponibles" Alert and no toast |
| AC-16 no VM selected: empty list and disabled create | unit | `stratus-ui/src/routes/Services.test.tsx` | US-2 / REQ-16 / AC-16 | given no VM - when the screen opens - then no `/api/services/vms` request, an empty list, and create disabled with "Seleccione una VM para continuar" |
| AC-17 cada script sale con 7 sin IP | unit | `scripts/services/*.bats` | US-1..US-7 / REQ-26 / AC-17 | given `stub_vm_ip vm1 -` - when each of the 12 scripts runs - then exit 7, the message, no ssh call |
| TestAC17_ServicesOperationsMapNoIP | unit | `stratus-srv/internal/service/services_service_test.go` | US-1..US-7 / REQ-26, REQ-27 / AC-17 | given exit 7 - when each service method runs - then ErrVMNoIP and one history row each |
| AC-17 shows the no-IP message | unit | `stratus-ui/src/routes/Services.test.tsx` | US-2 / REQ-29 / AC-17 | given 409 with the no-IP text - when a VM is selected - then the Alert shows "La VM no tiene dirección IP; verifique que esté encendida" |
| AC-18 cada acción llama a su systemctl | unit | `scripts/services/{start,stop,restart,enable,disable}-service.bats` | US-3 / REQ-17 / AC-18 | given a managed `api` - when each runs - then remote.log has `systemctl <action> api` after the marker check and stdout is the message |
| TestAC18_ServiceActionRunsItsScript | unit | `stratus-srv/internal/service/services_actions_test.go` | US-3 / REQ-17, REQ-26 / AC-18 | given each action - when ServiceAction runs - then script `services/<action>-service.sh`, argv `[vm1 api]`, operation `<action>_service`; `reload` gives ErrInvalidInput |
| TestAC18_ServiceActionRoutes | unit | `stratus-srv/internal/handler/services_test.go` | US-3 / REQ-17 / AC-18 | given each of the 5 routes - when POSTed - then 200 `{"message"}` |
| AC-18 row buttons call each action | unit | `stratus-ui/src/features/services/ServiceTable.test.tsx` | US-3 / REQ-17 / AC-18 | given api - when each of the 5 buttons is clicked - then `onAction("api", <action>)` |
| AC-18 start, enable, stop, disable, restart changes PID | manual | real VM demo | US-3 / REQ-17 / AC-18 | `systemctl is-active api`, `is-enabled api`, and `systemctl show -p MainPID api` before and after Reiniciar |
| AC-19 start-service sobre un servicio activo termina bien | unit | `scripts/services/start-service.bats` | US-3 / REQ-17 / AC-19 | given ACTIVE=active - when it runs - then exit 0 and `active_state=active` |
| AC-20 los scripts rechazan unidades sin la marca | unit | `scripts/services/{edit,redeploy,delete,start,stop,restart,enable,disable,status,logs}-service.bats` | US-3..US-7 / REQ-18 / AC-20 | given MANAGED=no and name `ssh` - when each runs - then exit 6, "Solo se pueden gestionar servicios creados por Stratus", and remote.log has only the marker check |
| TestAC20_NotManagedMapsError | unit | `stratus-srv/internal/service/services_service_test.go` | US-3..US-7 / REQ-18 / AC-20 | given exit 6 - when each method on an existing service runs - then ErrNotManagedService |
| TestAC20_ServicesHandlerMapsNotManaged | unit | `stratus-srv/internal/handler/services_test.go` | US-3..US-7 / REQ-18 / AC-20 | given exit 6 - when start, status, logs, PUT, redeploy, DELETE for `ssh` - then 409 with the spec text |
| AC-21 status-service acepta servicios inactivos o fallidos | unit | `scripts/services/status-service.bats` | US-4 / REQ-19 / AC-21 | given the status program exiting 3 with `failed` output (STATUS_RC=3) - when it runs - then exit 0 and stdout has `failed`; STATUS_RC=1 exits 5 |
| TestAC21_ServiceStatusReturnsOutput | unit | `stratus-srv/internal/service/services_actions_test.go` | US-4 / REQ-19, REQ-27 / AC-21 | given stdout with `failed` - when ServiceStatus runs - then that output, no error, no history row |
| AC-21 Estado shows the output panel | unit | `stratus-ui/src/routes/Services.test.tsx` | US-4 / REQ-19 / AC-21 | given status `{output}` with `failed` - when Estado is clicked - then "Estado de api" with the output and no failure Alert |
| AC-22 logs-service pide las últimas 100 líneas | unit | `scripts/services/logs-service.bats` | US-4 / REQ-20 / AC-22 | given a managed api - when it runs - then remote.log has `journalctl -u api.service -n 100 --no-pager` and no `-f` |
| TestAC22_ServiceLogsReturnsOutput | unit | `stratus-srv/internal/service/services_actions_test.go` | US-4 / REQ-20 / AC-22 | given stdout - when ServiceLogs runs - then that output and no history row; a status/logs route returns `{"output"}` |
| AC-22 Logs shows the output panel | unit | `stratus-ui/src/routes/Services.test.tsx` | US-4 / REQ-20 / AC-22 | given logs output - when Logs is clicked - then "Logs de api" with the output |
| AC-22 journal of 500 lines shows 100 | manual | real VM demo | US-4 / REQ-20 / AC-22 | with a service that logged 500 lines - the panel holds 100 lines |
| AC-23 edit prefills and saves Restart always | unit | `stratus-ui/src/features/services/EditServiceDialog.test.tsx` | US-5 / REQ-21, REQ-22 / AC-23 | given the demian-lee row - when opened - then the current values and the 3 keys without values, checkbox unchecked; when Restart `always` is saved - then PUT body has `restart:"always"`, `replace_env:false`, `env:[]` |
| AC-23 edit-service reescribe la unidad y reinicia si estaba activo | unit | `scripts/services/edit-service.bats` | US-5 / REQ-23 / AC-23 | given ACTIVE=active and env `-` - when it runs - then the backup step, a unit write with `Restart=always`, no env write, `daemon-reload`, `systemctl restart demian-lee`, no `enable`/`disable`, the backup removal, and `active_state=active` |
| TestAC23_EditServicePassesArgs | unit | `stratus-srv/internal/service/services_edit_test.go` | US-5 / REQ-21, REQ-22, REQ-23 / AC-23 | given replace_env false - when EditService runs - then argv `[vm1 demian-lee <desc> demian-lee "" always 5 -]`; env rows with replace_env false give ErrInvalidInput |
| TestAC23_EditServiceRoute | unit | `stratus-srv/internal/handler/services_test.go` | US-5 / REQ-23 / AC-23 | given a valid body - when PUT `/api/services/vms/vm1/services/demian-lee` - then 200 `{"message","active_state"}` |
| AC-23 Restart always applied | manual | real VM demo | US-5 / REQ-23 / AC-23 | `systemctl show -p Restart demian-lee`, `md5sum /etc/demian-lee.env` unchanged, `is-active` and `is-enabled` |
| AC-24 edit-service no inicia un servicio inactivo | unit | `scripts/services/edit-service.bats` | US-5 / REQ-23 / AC-24 | given ACTIVE=inactive - when it runs - then the unit write and `daemon-reload` but no restart and no `active_state` line |
| AC-25 edit-service reescribe el env completo | unit | `scripts/services/edit-service.bats` | US-5 / REQ-22 / AC-25 | given an env file with only `PORT=9090` - when it runs - then the env write to `/etc/demian-lee.env` with `chmod 600` and `chown root:root`, and its stdin is only `PORT="9090"` |
| TestAC25_EditServiceReplaceEnvUsesSecretFile | unit | `stratus-srv/internal/service/services_edit_test.go` | US-5 / REQ-13, REQ-22 / AC-25 | given replace_env true and `PORT=9090` - when EditService runs - then the last arg is a 0600 file with `PORT=9090\n`, removed afterwards |
| AC-25 checked box asks every value again | unit | `stratus-ui/src/features/services/EditServiceDialog.test.tsx` | US-5 / REQ-22 / AC-25 | given the checkbox checked - when rendered - then key rows are prefilled and values empty; when only `PORT=9090` is kept and saved - then the body has `replace_env:true, env:[{key:"PORT",value:"9090"}]` |
| AC-26 redeploy-service reemplaza el contenido y respeta el estado | unit | `scripts/services/redeploy-service.bats` | US-6 / REQ-24 / AC-26 | given ACTIVE=active - when it runs - then in order: unit backup, `systemctl stop`, `mv /opt/demian-lee /opt/.stratus-backup-demian-lee`, `install -d`, tar, `chown -R`, `systemctl start`, backup removal, `active_state=active`; given ACTIVE=inactive - then no stop or start and no state line |
| TestAC26_RedeployServicePassesArgs | unit | `stratus-srv/internal/service/services_redeploy_test.go` | US-6 / REQ-12, REQ-24 / AC-26 | given an archive and executable `demian-lee` - when RedeployService runs - then argv `[vm1 demian-lee <archive path> demian-lee]`, the archive file removed afterwards |
| TestAC26_RedeployRouteMultipart | unit | `stratus-srv/internal/handler/services_test.go` | US-6 / REQ-24 / AC-26 | given multipart `payload {"executable"}` then `archive` - when POST `.../demian-lee/redeploy` - then 200 |
| AC-26 content replaced and state kept | manual | real VM demo | US-6 / REQ-24 / AC-26 | `ls -ln /opt/demian-lee` shows only the new files owned by demian-lee; `is-active` before and after |
| AC-27 redeploy-service restaura si falla tras detener | unit | `scripts/services/redeploy-service.bats` | US-6 / REQ-24 / AC-27 | given ACTIVE=active and FAIL_REMOTE on `chown -R` (then on `systemctl start`) - when it runs - then exit 5 and remote.log has `rm -rf /opt/demian-lee`, `mv /opt/.stratus-backup-demian-lee /opt/demian-lee`, the unit restore, `daemon-reload`, `systemctl start` and the backup removal; given ACTIVE=failed and no failure - then exit 0 with `active_state=failed` |
| TestAC27_RedeployFailureReturnsOperationError | unit | `stratus-srv/internal/service/services_redeploy_test.go` | US-6 / REQ-24, REQ-29 / AC-27 | given exit 5 - then *OperationError and a history row; given ok with `active_state=failed` - then ActiveState `failed` |
| AC-27 failed redeploy restores content | manual | real VM demo | US-6 / REQ-24 / AC-27 | an archive whose extraction fails mid-way - previous files back, `ls /opt/.stratus-backup-* /var/lib/stratus/backup` empty |
| AC-28 delete asks for confirmation naming the service | unit | `stratus-ui/src/features/services/DeleteServiceDialog.test.tsx` | US-7 / REQ-25 / AC-28 | given api - when the dialog opens - then it names api and there is no fetch until Confirmar, then `DELETE /api/services/vms/vm1/services/api` |
| AC-28 delete-service quita todo con la unidad al final | unit | `scripts/services/delete-service.bats` | US-7 / REQ-25 / AC-28 | given a managed api - when it runs - then in order: `disable --now api`, `rm -f /etc/api.env`, `rm -rf /opt/api`, userdel, groupdel, `rm -f /etc/systemd/system/api.service`, `daemon-reload`; stdout is the message |
| TestAC28_DeleteServicePassesArgs | unit | `stratus-srv/internal/service/services_delete_test.go` | US-7 / REQ-25 / AC-28 | given api - when DeleteService runs - then argv `[vm1 api]`, the message is stdout, one history row |
| TestAC28_DeleteServiceRoute | unit | `stratus-srv/internal/handler/services_test.go` | US-7 / REQ-25 / AC-28 | given ok - when DELETE `.../services/api` - then 200 `{"message"}` |
| AC-28 nothing remains | manual | real VM demo | US-7 / REQ-25 / AC-28 | `systemctl cat api` fails, `ls /etc/api.env /opt/api` fails, `getent passwd api` and `getent group api` are empty |
| TestAC29_ServicesRejectedWhileOtherModuleRuns | integration | `stratus-srv/internal/service/services_service_test.go` | US-1..US-7 / REQ-26 / AC-29 | given PrepareService, UsersService and ServicesService sharing one VMLocks - when a prepare op is blocked on vm1 and CreateService or ServiceAction runs, and when a create_service is blocked and DeleteUser runs - then ErrOperationInProgress and no second script call |
| AC-30 <script> sale con 2 ante argumentos inválidos | unit | `scripts/services/*.bats` | US-1..US-7 / REQ-2, REQ-26, REQ-31 / AC-30 | given a missing arg, name `Demian`, a missing archive or env file - when each script runs - then exit 2 and an empty calls.log |
| AC-31 create demian-lee, before/after | manual | real VM demo | US-1 / REQ-10, REQ-15 / AC-31 | `getent passwd demian-lee`, `ls -l /opt/demian-lee`, `stat -c '%a %U:%G' /etc/demian-lee.env`, `systemctl is-enabled`, `is-active`, `curl http://localhost:8080/healthz` before and after |
| AC-32 delete demian-lee, before/after | manual | real VM demo | US-7 / REQ-25 / AC-32 | `systemctl list-unit-files demian-lee.service`, `getent passwd demian-lee`, `ls /opt/demian-lee /etc/demian-lee.env` before and after |
| TestAC33_ServicesRecordPolicy | unit | `stratus-srv/internal/service/services_service_test.go` | US-8 / REQ-27 / AC-33 | given 20 successful list, status and logs and 1 failed status - then 1 row; given each of the 9 mutating operations ok - then 9 rows; no row contains an env value |
| TestAC34_ServicesHistoryReturns50NewestOfModule | unit | `stratus-srv/internal/service/services_service_test.go` | US-8 / REQ-28 / AC-34 | given 55 services rows and users rows - when History runs - then 50 services rows, newest first |
| TestAC34_ServicesHistoryRoute | unit | `stratus-srv/internal/handler/services_test.go` | US-8 / REQ-28 / AC-34 | given one services row and one users row - when GET `/api/services/history` - then only the services row |
| AC-34 shows the module history | unit | `stratus-ui/src/routes/Services.test.tsx` | US-8 / REQ-28 / AC-34 | given `/api/services/history` rows - when the screen opens - then the rows show with "Ver stderr" |
| AC-35 Servicios systemd navigates, HAProxy is Próximamente | unit | `stratus-ui/src/routes/Home.test.tsx` | US-1..US-8 / REQ-30 / AC-35 | given the home - when it loads - then the systemd card links to `/services`, and only HAProxy shows "Próximamente" (1 total) |
| AC-36 <script> tiene la estructura exigida | unit | `scripts/services/*.bats` | US-1..US-7 / REQ-31 / AC-36 | given each of the 12 files - when `assert_script_structure` runs - then it passes |
| AC-37 create shows loading and disables the form | unit | `stratus-ui/src/features/services/CreateServiceForm.test.tsx` | US-1 / REQ-29 / AC-37 | given a pending fetch - when submitted - then a spinner shows and the inputs and button are disabled until it resolves |
| AC-37 row buttons disabled while an operation runs | unit | `stratus-ui/src/routes/Services.test.tsx` | US-3, US-5, US-6 / REQ-29 / AC-37 | given a pending Iniciar - when waiting - then a spinner shows and every row button is disabled |
| AC-37 edit and redeploy dialogs show loading | unit | `stratus-ui/src/features/services/{EditServiceDialog,RedeployServiceDialog}.test.tsx` | US-5, US-6 / REQ-29 / AC-37 | given a pending request - when saving or confirming - then a spinner shows and the fields and buttons are disabled |
| AC-38 redeploy prefills the executable and confirms | unit | `stratus-ui/src/features/services/RedeployServiceDialog.test.tsx` | US-6 / REQ-24 / AC-38 | given the demian-lee row - when opened - then executable `demian-lee`; when `bin/server` and an archive are set - then a warning that all of `/opt/demian-lee` is replaced, with no fetch until Confirmar, then a multipart POST with `{"executable":"bin/server"}` |
| AC-38 redeploy-service cambia ExecStart conservando los argumentos | unit | `scripts/services/redeploy-service.bats` | US-6 / REQ-24 / AC-38 | given UNIT_FIELDS with exe `demian-lee` and args `--port 8080`, and an archive with `bin/server` - when run with `bin/server` - then the unit write has `ExecStart=/opt/demian-lee/bin/server --port 8080` and `daemon-reload`; with the same exe there is no unit write |
| TestAC38_RedeployMapsExecutableMissing | unit | `stratus-srv/internal/service/services_redeploy_test.go` | US-6 / REQ-24 / AC-38 | given exit 11 - when RedeployService runs - then ErrExecutableMissing |
| AC-39 redeploy-service restaura la unidad anterior si el inicio falla | unit | `scripts/services/redeploy-service.bats` | US-6 / REQ-24 / AC-39 | given the exe changed and FAIL_REMOTE on `systemctl start` - when it runs - then exit 5 and remote.log has the content restore, the unit `cp -p` back, `daemon-reload`, and the backup removal |
| AC-39 previous ExecStart restored | manual | real VM demo | US-6 / REQ-24 / AC-39 | `systemctl cat demian-lee` shows the old ExecStart; no backup remains |
| AC-40 edit-service restaura si daemon-reload o el reinicio fallan | unit | `scripts/services/edit-service.bats` | US-5 / REQ-23 / AC-40 | given ACTIVE=active and FAIL_REMOTE on `systemctl restart` (then on `daemon-reload` once) - when it runs - then exit 5 and remote.log has `cp -p` of unit and env back, `daemon-reload`, `systemctl restart`, the backup removal; with FAIL_RESTORE too - then exit 9 and "Restauración fallida; ..."; given ACTIVE=failed after restart - then exit 0 with `active_state=failed` |
| TestAC40_EditAndRedeployRestoreFailureMessages | unit | `stratus-srv/internal/service/services_edit_test.go` | US-5, US-6 / REQ-23, REQ-24, REQ-29 / AC-40, AC-39 | given exit 9 - when EditService and RedeployService run - then ServiceRollbackError "No se pudo editar el servicio demian-lee y la restauración falló; revise la VM. ..." / "No se pudo redesplegar ..." and the handler returns 500 with that text |
| AC-40 failed edit restored | manual | real VM demo | US-5 / REQ-23 / AC-40 | an edit whose restart fails - `systemctl cat`, `md5sum /etc/demian-lee.env` as before, `is-active` active, `ls /var/lib/stratus/backup` empty |
| AC-41 delete-service falla con el nombre y se puede repetir | unit | `scripts/services/delete-service.bats` | US-7 / REQ-25 / AC-41 | given FAIL_REMOTE on userdel - when it runs - then exit 5, stderr "No se pudo eliminar el servicio api: ...", and no `rm -f /etc/systemd/system/api.service`; the env, `/opt`, user and group steps are tolerant (`rm -f`, `rm -rf`, `! getent ... \|\|`) |
| TestAC41_DeleteServiceFailureNamesService | unit | `stratus-srv/internal/service/services_delete_test.go` | US-7 / REQ-25, REQ-29 / AC-41 | given exit 5 with that stderr - when DeleteService runs - then *OperationError whose text has "No se pudo eliminar el servicio api" |
| AC-41 failed delete shows the message | unit | `stratus-ui/src/features/services/DeleteServiceDialog.test.tsx` | US-7 / REQ-25 / AC-41 | given a 500 with that text - when confirmed - then the failure result shows the text naming api |
| AC-41 retry after a failed delete | manual | real VM demo | US-7 / REQ-25 / AC-41 | with a process of `api` keeping userdel busy - the first delete fails and api stays listed; the second succeeds |
| AC-42 start y restart terminan bien con la app fallando | unit | `scripts/services/{start-service,restart-service}.bats` | US-3 / REQ-17 / AC-42 | given ACTIVE=`activating (auto-restart)` - when each runs - then exit 0, `sleep 3` before the state read, and `active_state=activating (auto-restart)` |
| TestAC42_StartReturnsNonActiveState | unit | `stratus-srv/internal/service/services_actions_test.go` | US-3 / REQ-17 / AC-42 | given that stdout - when ServiceAction start and restart run - then ActiveState `activating (auto-restart)` |
| AC-42 shows the warning after Iniciar | unit | `stratus-ui/src/routes/Services.test.tsx` | US-3 / REQ-17 / AC-42 | given start returning `active_state: "failed"` - when Iniciar is clicked - then the success result and the warning naming Estado and Logs |
| AC-43 activating cuenta como en ejecución | unit | `scripts/services/{redeploy-service,edit-service}.bats` | US-5, US-6 / REQ-23, REQ-24 / AC-43 | given ACTIVE=`activating (auto-restart)` - when redeploy runs - then `systemctl stop` comes before the `mv` and `systemctl start` after the extraction; when edit runs - then `systemctl restart` |
| AC-43 crashing service fixed by redeploy | manual | real VM demo | US-5, US-6 / REQ-23, REQ-24 / AC-43 | api in `activating (auto-restart)` - redeploy a fixed archive - UI shows `active` |

## 8. Traceability
| Requirement | Component / file | Verified by (AC) |
|---|---|---|
| REQ-1 | `CreateServiceForm`, `EnvVarFields`, `CreateServiceInput`, create route | AC-1 |
| REQ-2 | `validation.ts`, `services_validation.go`, `validate_name`, `create-service.sh` step 2 | AC-3, AC-4, AC-30 |
| REQ-3 | `validateDescription` / `ValidateDescription` / `validate_description` | AC-8 |
| REQ-4 | `validateArchive`, `saveArchive`, `readUpload` | AC-5 |
| REQ-5 | `check_archive`, the `tar -xzf -` stream | AC-6 |
| REQ-6 | executable validators, `check_archive`, edit step 2, `chmod u+x` | AC-7, AC-38 |
| REQ-7 | args validators, `render_unit` ExecStart | AC-8, AC-38 |
| REQ-8 | Restart/RestartSec validators, form defaults `on-failure` / 5 | AC-8 |
| REQ-9 | env validators, `encode_env` | AC-9, AC-10 |
| REQ-10 | `create-service.sh` steps 3-9 | AC-1, AC-2, AC-31 |
| REQ-11 | `render_unit`, `MARKER` | AC-2 |
| REQ-12 | `saveArchive` temp file plus `defer os.Remove` | AC-1, AC-5, AC-11, AC-26 |
| REQ-13 | `writeSecretFile`, `encode_env`, `write_remote_file`, keys-only list, password inputs | AC-1, AC-11, AC-25 |
| REQ-14 | `create-service.sh` trap, `ServiceRollbackError` | AC-12, AC-13 |
| REQ-15 | `sleep 3` plus `read_state`, `parseResult`, `StateWarning` | AC-1, AC-14, AC-31 |
| REQ-16 | `list-services.sh`, `ListServices`, `ServiceTable`, `Services.tsx` toast | AC-15, AC-16 |
| REQ-17 | 5 action scripts, `ServiceAction`, row buttons | AC-18, AC-19, AC-42 |
| REQ-18 | `require_managed`, `ErrNotManagedService` | AC-20 |
| REQ-19 | `status-service.sh` (exit 0 or 3), `ServiceStatus`, `ServiceOutputPanel` | AC-21 |
| REQ-20 | `logs-service.sh`, `ServiceLogs`, `ServiceOutputPanel` | AC-22 |
| REQ-21 | list fields `executable`, `args`, `env_keys`; `EditServiceDialog` prefill | AC-11, AC-23 |
| REQ-22 | "Reemplazar variables de entorno" checkbox, `-` argument, `ValidateEditService` | AC-23, AC-25 |
| REQ-23 | `edit-service.sh` steps and restore trap | AC-23, AC-24, AC-25, AC-40, AC-43 |
| REQ-24 | `redeploy-service.sh` steps and restore trap, `RedeployServiceDialog` | AC-6, AC-26, AC-27, AC-38, AC-39, AC-43 |
| REQ-25 | `delete-service.sh`, `DeleteServiceDialog` | AC-28, AC-32, AC-41 |
| REQ-26 | `vm_exec`, `ExecRunner` 15 min, shared `VMLocks`, one script per operation | AC-17, AC-29, AC-30 |
| REQ-27 | `operationRunner.recordSuccess`, argv with paths only | AC-11, AC-33 |
| REQ-28 | `ServicesService.History`, history route, `HistoryTable` | AC-34 |
| REQ-29 | `useOperation`, `OperationResult`, `respond.go`, busy state | AC-12, AC-17, AC-37, AC-41 |
| REQ-30 | `modules.ts`, `App.tsx`, `Home.test.tsx` | AC-35 |
| REQ-31 | headers and sections of the 12 scripts | AC-30, AC-36 |
| REQ-32 | shadcn components (`dialog`, `table`, `select`, `alert-dialog`, ...) and Spanish texts | AC-1, AC-15 |

## 9. Decisions and risks
- Archive transfer streams over the ssh stdin of `vm_exec` straight into `tar -xzf - -C /opt/<name>`. The user approved this in place of scp. Rejected: a `vm_copy` scp helper plus a temp file on the VM, which is one more helper and one more thing to clean. The spec clarification ("copied to the VM and extracted") still holds: the copy is the SSH stream.
- The archive content is checked on the host inside the script (`gzip -t`, `tar -tzf`, `tar -tvzf`) before VBoxManage or ssh. An invalid archive never reaches the VM, and bats tests use real archives. Rejected: checking on the VM after upload.
- The edit and redeploy dialogs prefill from the latest list row, because `list-services.sh` returns every field in one call (user's choice). Rejected: a `show-service.sh` read on each open.
- Layout: buttons per row; edit, redeploy and delete in dialogs (shadcn `dialog` on the installed `@base-ui/react`); Estado and Logs in a "Salida" card (user's choice).
- A failed restore in edit or redeploy (exit 9) shows its own text, "No se pudo editar|redesplegar el servicio <n> y la restauración falló; revise la VM" (user's choice). One `ServiceRollbackError` covers create, edit and redeploy.
- There are 12 scripts with a shared bash library, because REQ-26 asks for one script file per operation. Rejected: one `service-action.sh <action>`.
- 200 MB means 200 MiB (209,715,200 bytes) in the UI and the backend. The handler's body limit adds 1 MiB for the `payload` part and the multipart framing. nginx allows 210m.
- `maxBodyBytes` rises to 256 KiB for every JSON route. 50 values of 1024 characters can exceed 64 KiB once escaped, and no route is harmed by the larger limit.
- Env encoding uses the systemd EnvironmentFile double-quote rule, with `\ " ` $` escaped. Rejected: single quotes, which cannot hold a `'`.
- Backups go to `/var/lib/stratus/backup/<name>/` and `/opt/.stratus-backup-<name>`. Neither path is a unit file or a valid service name. A stale backup from a killed run is removed when the next edit or redeploy starts.
- Risk: systemd versions differ in EnvironmentFile parsing. Mitigation: the AC-10 demo step reads `/proc/<pid>/environ` on Debian 13.
- Risk: the host needs GNU tar and gzip, and the `C.UTF-8` locale for the description check. Mitigation: both are standard on the Linux host that runs the backend; the bats tests fail on a host without them.
- Risk: the 3-second state read keeps the VM lock 3 s longer. Mitigation: this is accepted by REQ-15 and is far below the 15-minute timeout.
- Risk: if delete fails after removing the unit file (only the final `daemon-reload` is left), a retry gets exit 6. Mitigation: the unit file is removed only after every other step succeeded, and the failure message names the service so the user can run `systemctl daemon-reload` on the VM.
- Risk: the bats stubs check commands, not effects, for extraction, rollback and systemd. Mitigation: the manual demo rows of section 7.
- Risk: the 002 home test expects 2 "Próximamente" cards. Mitigation: it becomes the AC-35 test in the same change that enables the card.

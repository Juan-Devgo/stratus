# Plan: HAProxy load balancing

## 1. Metadata
- Spec: `docs/specs/004-haproxy/spec.md`
- Updated: 2026-10-24
- Constitution version: 2.0.0

## 2. Constitution check
- P1: the backend validates input, reads the topology from SQLite, picks the script, builds argv and maps exit codes to messages. Every VirtualBox, DHCP, HAProxy, sshd, `jump` and ufw change happens only in `scripts/haproxy/*.sh` and in the 001-003 scripts. The backend computes only app bookkeeping: the reserved subnets, the next `server<N>` name and the stored server list.
- P2: there are 7 new scripts, one per operation (create, delete, assign, remove, save frontend, refresh, read state). Each has `set -euo pipefail`, positional arguments, results on stdout, diagnostics on stderr, and the exit-code table of section 6. Refresh, save frontend, remove and `jump_drop` are idempotent. The way to reach a server comes in as the explicit argument `--jump <lb>`, never from the environment.
- P3: the same allowlists (name, memory, CPUs, ports, server name, subnet, server spec) are in `stratus-ui/src/features/haproxy/validation.ts`, `stratus-srv/internal/service/haproxy_validation.go` and `scripts/haproxy/lib/haproxy-common.sh`. Constant remote `sh -c` programs get their values as positional arguments. haproxy.cfg and the sshd file are built on the host with `printf` and sent over ssh stdin.
- P4: the load balancer gets its key access in the 001 Guest Additions bootstrap, by reusing `create-vm.sh`. From then on every step is SSH with the backend key. Servers are reached only through `jump@<LB bridged IPv4>`. `jump` is created with `useradd --system`, has no usable password and no login shell, and its keys carry `restrict,port-forwarding`.
- P5: this module creates no private key. Only public keys are copied into `jump`'s `authorized_keys`. The 001 and 002 private-key flows do not change.
- P6: every operation returns `{message}` or `{error}`. `operations` rows with module `haproxy` keep command, argv, exit code, redacted stderr, result and timestamp. Read state is recorded only when it fails.
- P7: the home card "Backends HAProxy" opens `/haproxy`, the only screen of the module. Forms and dialogs confirm with `OperationResult`.
- P8: each script runs standalone and has bats tests with PATH stubs. The manual rows of section 7 (AC-30 to AC-33 and the inspections) are the before/after demo on the real host.
- P9: no new Go or npm dependency. Reused as they are: `ExecRunner`, `operationRunner`, `VMLocks`, the history repository, `Redact`, `create-vm.sh`, `delete-vm.sh`, `stop-vm.sh`, `vm-exec.sh`, `VmSelector`, `HistoryTable`, `useOperation` and `OperationResult`. The new shared code is two bash libraries and one repository, each with more than one user.

## 3. Architecture
- Topology source: SQLite. The table `load_balancers` holds the name, subnet `N` and frontend port. The table `lb_servers` holds `server<N>`, VM, port, last IP and the enabled flag. The backend decides from SQLite whether a VM is a load balancer, an assigned server or a plain VM. Scripts never infer the role from VirtualBox.
- Reaching a server (REQ-19):
  - When the target VM is a row in `lb_servers`, every guest script of 001, 002 and 003 gets `--jump <lb>` as its first two argv items. `targetArgs(ctx, vm)` builds them.
  - The script parses the option in step 1, validates the LB name and calls `vm_route <vm> <lb>`.
  - `vm-exec.sh` keeps a routing map `VM_ROUTES`:
    - `vm_ip` on a routed VM first resolves the IPv4 of adapter 1 of the load balancer. If that fails, it returns 12 with "El balanceador <lb> está apagado; enciéndalo para llegar al servidor".
    - It then resolves the IPv4 of adapter 1 of the server. If that fails, it returns 7.
    - `vm_exec` on a routed VM adds `-o ProxyCommand="ssh -i <key> ... -o HostKeyAlias=<lb> -W %h:%p jump@<lb ip>"` (what `ssh -J` does, but with the backend key and the pinned known_hosts for the hop).
    - Calls to unrouted VMs, the load balancer included, are direct as before.
- IP of adapter 1: `vm_nic_ip <vm> <nic>` reads `macaddress<nic>` from `showvminfo` and finds the matching `/VirtualBox/GuestInfo/Net/<i>/MAC`, then reads `/Net/<i>/V4/IP`. A VM with only one NIC (no `nic2` other than `none`) keeps the old `/Net/0/V4/IP` read. `list-vms.sh` uses the same function, so 001 shows the internal IPv4 of a server (REQ-13) and the bridged IPv4 of a load balancer.
- Create a load balancer (`create-lb.sh`):
  1. Validate the arguments.
  2. Check that `base_disk` exists. If not, exit 6 with "Disco base de HAProxy no encontrado".
  3. Pick the lowest `N` that is not in `reserved` and not used by `VBoxManage list dhcpservers`. No free `N` exits 8. An existing DHCP server for `stratus-<lb>` exits 13.
  4. `dhcpserver add --network=stratus-<lb> --server-ip=10.10.N.2 --netmask=255.255.255.0 --lower-ip=10.10.N.10 --upper-ip=10.10.N.200 --enable`. From here on the trap is active.
  5. Run `../prepare/create-vm.sh <lb> <mem> <cpus> <base_disk> <bridge> stratus-<lb>`. This does the 001 bootstrap with NIC 2 on the internal network.
  6. Over root SSH: give the adapter-2 interface (found by MAC) `10.10.N.1/24` through `/etc/network/interfaces.d/stratus-internal` and `ifup`.
  7. Create `jump` with `useradd --system --create-home --home-dir /var/lib/jump --shell /usr/sbin/nologin jump`.
  8. Write `/var/lib/jump/.ssh/authorized_keys` (700/600, owner `jump`) with `restrict,port-forwarding <backend key> stratus-backend`.
  9. Write `/etc/ssh/sshd_config.d/20-stratus-jump.conf`, then `sshd -t` and `systemctl reload ssh`.
  10. Render haproxy.cfg with port 80 and no servers, `apply_cfg`, then `systemctl enable --now haproxy`.
  11. Print the result.
  The trap runs `../prepare/delete-vm.sh <lb>` and `dhcpserver remove --network=stratus-<lb>`. The base disk is never modified (delete-vm.sh only closes children).
- Assign (`assign-server.sh`):
  1. Validate the arguments.
  2. Check that both VMs exist. The load balancer must be running with a bridged IPv4, otherwise exit 12.
  3. Save the previous power state of the VM. If it is running, run `../prepare/stop-vm.sh <vm>`.
  4. `modifyvm <vm> --nic1 intnet --intnet1 stratus-<lb> --nic2 nat`. No port forwarding. From here on the trap is active.
  5. Delete the guest IP properties, then `startvm`.
  6. Wait up to 60 polls of 2 s (120 s) for an adapter-1 IPv4 in `10.10.N.10`-`10.10.N.200`. If none arrives, exit 14.
  7. `vm_route <vm> <lb>`, then `jump_sync <lb> <vm>`.
  8. Render the existing specs plus the new line and run `apply_cfg`.
  9. Print the result and `ip=<ip>`.
  The trap runs `jump_drop <lb> <vm>`, powers the VM off, runs `modifyvm --nic1 bridged --bridgeadapter1 <bridge> --nic2 none`, and starts the VM only if it was running before. haproxy.cfg is already restored by `apply_cfg`, or it was never changed. A failed restore exits 9.
- Remove (`remove-server.sh`):
  1. Validate the arguments.
  2. The load balancer must be running with a bridged IPv4. If not, exit 12 and change nothing.
  3. Render the remaining specs and run `apply_cfg`.
  4. `jump_drop`.
  5. Save the power state of the server and run `stop-vm.sh` if it is running.
  6. `modifyvm --nic2 none --nic1 bridged --bridgeadapter1 <bridge>`.
  7. `startvm` only if the server was running.
  Every step tolerates parts that are already undone, so a retry finishes the removal.
- Save frontend (`save-frontend.sh`): `ufw allow <port>/tcp` when `command -v ufw` succeeds, then render with the stored specs and the new port, then `apply_cfg`.
- Refresh (`refresh-lb.sh`):
  1. For each spec whose VM is `running`, start a background poll of `vm_nic_ip <vm> 1`. All polls share one budget of 60 polls of 2 s, then `wait`.
  2. A server with an IPv4 in range gets that IP and `on`. Any other server keeps its last IP and gets `off` (`disabled`).
  3. Render, then `apply_cfg --if-changed`. When the uploaded file equals the current one (`cmp -s`), there is no reload.
  4. Print one `<name> <ip> <on|off>` line per server, then `reloaded=yes|no`.
- `apply_cfg`:
  1. Upload stdin to `/etc/haproxy/haproxy.cfg.new`.
  2. `haproxy -c -f .new`. On failure, remove `.new` and exit 10, with the check output on stderr.
  3. `cp -p haproxy.cfg haproxy.cfg.bak`, `mv .new haproxy.cfg`, then `systemctl reload haproxy`.
  4. Verify for up to 5 polls of 1 s that `ss -Hltnp "sport = :<port>"` shows `haproxy`.
  5. If the reload or the check fails, `mv .bak` back, run `systemctl reload haproxy`, and exit 11 with the reload output on stderr.
  6. Remove `.bak`.
- Read state (`lb-status.sh`): `vm_exec <lb> bash -c` with a constant program that opens `/dev/tcp/127.0.0.1/9999`, sends `show stat` and prints `<svname> <status>` for the rows of backend `stratus_back` other than `BACKEND`.
- `jump` keys (`scripts/haproxy/lib/jump-keys.sh`):
  - `jump_sync <lb> <vm> [exclude_user]`:
    1. Read every key line of `/root/.ssh/authorized_keys` and of `<home>/.ssh/authorized_keys` of the users with UID 1000-59999 on the server, with one routed `vm_exec`. An excluded user is skipped.
    2. Drop the backend key (compared by key type and blob with `$APP_SSH_KEY_PATH.pub`) and remove duplicates.
    3. Write each key as `restrict,port-forwarding <type> <blob> stratus-jump:<vm>`.
    4. Send the lines through a direct `vm_exec <lb>` program that replaces every line ending in ` stratus-jump:<vm>` atomically (tmp, chown `jump`, chmod 600, mv).
  - `jump_drop <lb> <vm>` writes an empty set. The backend line `stratus-backend` is never touched.
  - Users: `assign-server.sh`, `remove-server.sh`, and `generate-user-key.sh`, `create-user.sh` and `delete-user.sh` when they get `--jump`. Failure exits 15 after the server change is rolled back:
    - generate-user-key: restore the copy of `/root/.ssh/authorized_keys` taken before the change.
    - create-user: the existing cleanup trap removes the user.
    - delete-user: it runs `jump_sync ... <username>` before any change. If later steps fail, the trap resyncs.
- Backend data flow:
  1. The handler decodes the request.
  2. `HAProxyService` validates, reads `LBRepository` and builds argv with the full spec list.
  3. `operationRunner.runLocked` takes every lock in `lockVMs` at once (all or nothing), runs the script, records the row and maps the exit code.
  4. On success, `onSuccess` updates SQLite before the locks are released.
- Cross-module rules:
  - `PrepareService` gets `*repository.LBRepository` and `*HAProxyService`:
    - DeleteVM: a load balancer with servers gives ErrLBHasServers. A load balancer without servers runs `HAProxyService.DeleteLB`. An assigned server gives ErrVMIsServer.
    - StartVM: after a successful start of a server or a load balancer, it runs `HAProxyService.Refresh`. If that fails, it returns a `warning`.
    - GenerateUserKey: on an assigned server it uses `--jump` and locks both VMs.
    - Install and remove package: on a load balancer they add `--frontend <port>`. Removing `haproxy` from a load balancer gives ErrHAProxyRemovalBlocked.
    - Every guest operation gets `targetArgs`.
  - `UsersService` gets `LBRepository`: it uses `targetArgs`, and create and delete lock the server and its load balancer.
  - `ServicesService` gets `LBRepository`: it uses `targetArgs` and keeps one lock.
- Shared locks: `VMLocks` gains `TryLockAll` and `UnlockAll`, plus `TryLockCreate` and `UnlockCreate`. The create lock replaces `PrepareService.createMu`, so creating a VM and creating a load balancer exclude each other (AC-8).
- UI: `/haproxy` shows the load balancer table, then "Crear balanceador" and, for the selected load balancer:
  - actions "Actualizar IPs" and "Eliminar";
  - the frontend form;
  - the assign form;
  - the servers table;
  - the module history.
  The servers come from `GET .../servers` (SQLite). When the selected load balancer is `running`, `GET .../status` adds the states and shows the toast "Estado del balanceador actualizado". When it is off, there is no status call: the state shows "-" with the note "El balanceador está apagado".
- State:
  - SQLite: `vms`, `load_balancers`, `lb_servers`, `operations`.
  - VirtualBox: the VMs, adapters, internal networks and DHCP servers.
  - The load balancer: haproxy.cfg, `jump` and the sshd file.
  - Backend memory: `VMLocks`.
  - UI: `HAProxy.tsx` holds the selected load balancer, the servers, the states, the busy flag and the history.

## 4. Design patterns
- Routing map `VM_ROUTES` in `scripts/prepare/lib/vm-exec.sh`: one place decides direct or jump SSH for about 20 scripts. Each script only parses `--jump`.
- Shared bash libraries `scripts/haproxy/lib/haproxy-common.sh` (validators, `render_cfg`, `apply_cfg`, `wait_ips`) and `scripts/haproxy/lib/jump-keys.sh` (`jump_sync`, `jump_drop`). They hold one implementation for 7 scripts plus the 3 key scripts of 001 and 002.
- Full rendering from the stored list (haproxy.cfg is always rebuilt from argv): there is no in-place text editing, and rollback is a copy of `.bak`.
- Cleanup trap in `create-lb.sh` and `assign-server.sh`, as in 001 `create-vm.sh` and 002 `create-user.sh`.
- Repository `LBRepository`: the SQL for the topology stays out of the services. Tests use a temp SQLite file.
- All-or-nothing multi-lock in `VMLocks.TryLockAll`: two-VM operations (REQ-21) never hold only half their locks.

## 5. Code structure
Scripts (headers, comments and output in Spanish):
- create: `scripts/haproxy/lib/haproxy-common.sh` - `fail`, `validate_lb_name`, `validate_port`, `validate_frontend_port`, `validate_server_name`, `validate_subnet`, `validate_reserved`, `validate_spec`, `lb_reachable <lb>` (exit 12), `render_cfg <frontend_port> <spec...>`, `apply_cfg <lb> <port> [--if-changed]`, `wait_ips <subnet> <vm...>`.
- create: `scripts/haproxy/lib/jump-keys.sh` - `jump_sync`, `jump_drop`.
- create: `scripts/haproxy/create-lb.sh`, `delete-lb.sh`, `assign-server.sh`, `remove-server.sh`, `save-frontend.sh`, `refresh-lb.sh`, `lb-status.sh`.
- create: `scripts/haproxy/test_helper.bash` - loads `../prepare/test_helper` and adds the helpers of section 6.
- create: one `.bats` next to each script, plus `scripts/haproxy/lib/haproxy-common.bats` and `scripts/haproxy/lib/jump-keys.bats`.
- modify: `scripts/prepare/lib/vm-exec.sh` - add `vm_nic_ip`, `vm_route`, `VM_ROUTES`, the code 12 and the ProxyCommand. `vm_ip` uses `vm_nic_ip <vm> 1`.
- modify: `scripts/prepare/lib/vm-exec.bats` - routing, 12, 7 and MAC tests.
- modify: `scripts/prepare/test_helper.bash` - add `stub_vm_nics <vm> <state> <mac1> <ip1|-> [<mac2> <ip2|->]`. `stub_vm_ip` does not change.
- modify: `scripts/prepare/create-vm.sh` - optional 6th argument `intnet_name` (`^stratus-[a-z0-9][a-z0-9-]{0,31}$`) that adds `--nic2 intnet --intnet2 <name>`. Header updated.
- modify: `scripts/prepare/list-vms.sh` - source `lib/vm-exec.sh` and print `vm_nic_ip <name> 1`.
- modify: `scripts/prepare/{install-package,remove-package,generate-user-key,set-issue,set-hostname}.sh` - optional `--jump <lb>` and the code 12 in the header and the `guest` pass-through. `install-package.sh` and `remove-package.sh` also take an optional `--frontend <port>`: with `ufw` they also run `ufw allow <port>/tcp`, and with `haproxy` they exit 2 with "No se puede eliminar haproxy: el balanceador lo usa". `generate-user-key.sh` with `--jump` backs up authorized_keys, runs `jump_sync` and exits 15 after restoring.
- modify: `scripts/users/{list-users,create-user,delete-user}.sh` - optional `--jump <lb>` and code 12. `create-user.sh` (key mode) and `delete-user.sh` call `jump_sync` with code 15.
- modify: `scripts/services/lib/service-common.sh` - `guest` passes 12 through, and `parse_jump` is documented in the header.
- modify: `scripts/services/*-service.sh` (12 scripts) - optional `--jump <lb>` and code 12 in the header.
- modify: the `.bats` files of every script above - `--jump` and code 12 tests (section 7).

Backend:
- modify: `stratus-srv/internal/repository/db.go` - add the `load_balancers` and `lb_servers` tables (`CREATE TABLE IF NOT EXISTS`, additive, no migration).
- create: `stratus-srv/internal/repository/load_balancers.go` and `load_balancers_test.go` - `LBRepository`.
- modify: `stratus-srv/internal/service/vm_locks.go` and `vm_locks_test.go` - `TryLockAll`, `UnlockAll`, `TryLockCreate`, `UnlockCreate`.
- modify: `stratus-srv/internal/service/operation_runner.go` and `operation_runner_test.go` - `runLocked`, with `run` calling it with `[vm]`. Exit 12 becomes `*LBOffError`.
- modify: `stratus-srv/internal/service/errors.go` and `errors_test.go` - the new sentinels and `LBOffError`.
- create: `stratus-srv/internal/service/haproxy_service.go` - `HAProxyConfig`, `HAProxyService`, `NewHAProxyService`, `History`, `mapHAProxyError`, exit-code constants.
- create: `stratus-srv/internal/service/haproxy_validation.go` - `ValidatePort`, `ValidateFrontendPort`.
- create: `stratus-srv/internal/service/haproxy_topology.go` - `targetArgs`, `serverSpecs`, `nextServerName`, `parseSubnet`, `parseRefresh`, `parseStatus`.
- create: `stratus-srv/internal/service/haproxy_lbs.go` - `ListLBs`, `CreateLB`, `DeleteLB`.
- create: `stratus-srv/internal/service/haproxy_servers.go` - `ListServers`, `ServerStatus`, `AssignServer`, `RemoveServer`.
- create: `stratus-srv/internal/service/haproxy_frontend.go` - `SaveFrontend`, `Refresh`.
- create: tests `haproxy_validation_test.go`, `haproxy_topology_test.go`, `haproxy_lbs_test.go`, `haproxy_servers_test.go`, `haproxy_frontend_test.go`, `haproxy_service_test.go`, all in `stratus-srv/internal/service/`.
- modify: `stratus-srv/internal/service/prepare_service.go` - `NewPrepareService(cfg, runner, vms, lbs, history, locks, haproxy, logger)`. `createMu` is removed. `guestArgs` becomes `targetArgs`.
- modify: `stratus-srv/internal/service/prepare_vms.go` - `VMView` gains `Role`, `LB`, `LBIP`. `CreateVM` uses `locks.TryLockCreate`. `StartVM` returns `StartResult`. `DeleteVM` applies the LB and server rules.
- modify: `stratus-srv/internal/service/prepare_keys.go`, `prepare_packages.go`, `prepare_system.go` - `targetArgs`, two-VM lock for keys, `--frontend`, ErrHAProxyRemovalBlocked.
- modify: `stratus-srv/internal/service/users_service.go`, `users_list.go`, `users_create.go`, `users_delete.go` - `lbs` field, `targetArgs`, two-VM lock for create and delete, exit 15 mapping.
- modify: `stratus-srv/internal/service/services_service.go` and the `services_*.go` methods that build argv - `lbs` field and `targetArgs`.
- modify: the `prepare_*_test.go`, `users_*_test.go`, `services_*_test.go` files and `stratus-srv/internal/handler/{prepare,users,services}_test.go` - the new constructor arguments, plus the tests of section 7.
- create: `stratus-srv/internal/handler/haproxy.go` and `haproxy_test.go` - `RegisterHAProxy`.
- modify: `stratus-srv/internal/handler/prepare.go` - the start route returns `{message, warning?}`.
- modify: `stratus-srv/internal/handler/respond.go` and `respond_test.go` - map the new sentinels and `*LBOffError`.
- modify: `stratus-srv/cmd/server/config.go` and `config_test.go` - require `APP_HAPROXY_BASE_DISK`.
- modify: `stratus-srv/cmd/server/main.go` - build `LBRepository` and `HAProxyService` before `PrepareService`, pass `lbs` to every service, and call `handler.RegisterHAProxy`.
- modify: `stratus-srv/.env.example` - `APP_HAPROXY_BASE_DISK` and the HAProxy base disk requirements.

Frontend:
- modify: `stratus-ui/src/features/prepare/types.ts` - `Vm` gains `role`, `lb`, `lb_ip`.
- modify: `stratus-ui/src/features/prepare/VmSelector.tsx` - label `<name> - <Estado> - <ip|-> - Balanceador` when `role === "lb"`.
- modify: `stratus-ui/src/features/prepare/api.ts` - `startVm` returns `{message, warning?}`.
- modify: `stratus-ui/src/features/prepare/VmActions.tsx` - shows the warning `Alert` after a start.
- modify: `stratus-ui/src/features/prepare/UserKeyForm.tsx` and `stratus-ui/src/features/users/UserKeyDownload.tsx` - jump usage line for `role === "server"`, through a shared `sshCommand(vm, keyFile, user)` in `stratus-ui/src/features/prepare/ssh-command.ts`.
- create: `stratus-ui/src/features/haproxy/types.ts`, `api.ts`, `validation.ts`.
- create: `stratus-ui/src/features/haproxy/CreateLbForm.tsx` - name, memory and CPUs, validated with `createVmErrors`.
- create: `stratus-ui/src/features/haproxy/LbTable.tsx` - columns Nombre, Estado, IP, Subred, Puerto, Servidores, plus a select button per row.
- create: `stratus-ui/src/features/haproxy/LbActions.tsx` - "Actualizar IPs", and "Eliminar" with confirmation.
- create: `stratus-ui/src/features/haproxy/FrontendForm.tsx` - the port, with default 80 and the current value prefilled.
- create: `stratus-ui/src/features/haproxy/AssignServerForm.tsx` - a select of VMs with `role === "vm"`, the port (default 80), and a confirmation that warns the VM restarts.
- create: `stratus-ui/src/features/haproxy/ServerTable.tsx` - columns Servidor, VM, IP interna, Puerto, Estado, plus a "Quitar" button with a confirmation that warns the VM restarts.
- create: `stratus-ui/src/routes/HAProxy.tsx`.
- modify: `stratus-ui/src/App.tsx` - route `/haproxy`.
- modify: `stratus-ui/src/features/home/modules.ts` - `haproxy`: `enabled: true`, with the description "Crear balanceadores HAProxy, asignar servidores y ver su estado."
- modify: `stratus-ui/src/routes/Home.test.tsx` - the 003 test "AC-35 ..." becomes the 004 test "AC-36 ...".
- create: tests `stratus-ui/src/features/haproxy/{validation.test.ts,CreateLbForm.test.tsx,LbTable.test.tsx,LbActions.test.tsx,FrontendForm.test.tsx,AssignServerForm.test.tsx,ServerTable.test.tsx}`, `stratus-ui/src/routes/HAProxy.test.tsx` and `stratus-ui/src/features/prepare/ssh-command.test.ts`.
- modify: `stratus-ui/src/features/prepare/{VmSelector,VmActions,UserKeyForm}.test.tsx` and `stratus-ui/src/features/users/UserKeyDownload.test.tsx` - the fixtures gain the new `Vm` fields, plus the tests of section 7.

## 6. Interfaces and data
Environment: `APP_HAPROXY_BASE_DISK` (required) is the absolute path of the HAProxy base disk. It is passed as argv to `create-lb.sh`. HAProxy base disk requirements: the 001 base disk requirements (Debian 13, BIOS, one NIC, Guest Additions, openssh-server, sudo, root password = `APP_GUEST_PASSWORD_FILE`), plus the `haproxy` package enabled at boot.

SQLite (added to `schema`):
```sql
CREATE TABLE IF NOT EXISTS load_balancers (
  name TEXT PRIMARY KEY,                -- also a row in vms
  subnet INTEGER NOT NULL UNIQUE,       -- N of 10.10.N.0/24, 1-254
  frontend_port INTEGER NOT NULL DEFAULT 80
);
CREATE TABLE IF NOT EXISTS lb_servers (
  lb TEXT NOT NULL,
  name TEXT NOT NULL,                   -- server<N>
  vm TEXT NOT NULL UNIQUE,
  port INTEGER NOT NULL,
  last_ip TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  PRIMARY KEY (lb, name)
);
```
History: module `haproxy`. The operations `create_lb`, `delete_lb`, `assign_server`, `remove_server`, `save_frontend` and `refresh_lb` are always recorded. `lb_status` is recorded only when it fails. In every row the VM column is the load balancer.

Server spec (one argv item): `<server_name>,<vm>,<port>,<ip>,<on|off>`, for example `server1,web-a,80,10.10.1.11,on`.

Scripts (argv; env `APP_SSH_KEY_PATH`, and `APP_GUEST_PASSWORD_FILE` for create):
| Script | Arguments | stdout |
|---|---|---|
| `create-lb.sh` | `<lb> <memory_mb> <cpus> <base_disk> <bridge_adapter> <reserved>` (`-` or `1,3,...`) | `Balanceador <lb> creado y encendido; IP <ip>; subred 10.10.<N>.0/24`, then `subnet=<N>` |
| `delete-lb.sh` | `<lb>` | `Balanceador <lb> eliminado junto con su red interna` |
| `assign-server.sh` | `<lb> <subnet> <vm> <server_name> <port> <frontend_port> <bridge_adapter> [spec...]` | `Servidor <server_name> (<vm>) asignado a <lb>`, then `ip=<ip>` |
| `remove-server.sh` | `<lb> <vm> <frontend_port> <bridge_adapter> [spec...]` (remaining servers) | `Servidor <vm> quitado de <lb>` |
| `save-frontend.sh` | `<lb> <frontend_port> [spec...]` | `Frontend de <lb> en el puerto <port>` |
| `refresh-lb.sh` | `<lb> <subnet> <frontend_port> [spec...]` | `<server_name> <ip> <on\|off>` per server, then `reloaded=yes\|no` |
| `lb-status.sh` | `<lb>` | `<server_name> <status>` per server |
| 001-003 guest scripts | `[--jump <lb>] <existing args>`; package scripts also `[--frontend <port>]` | unchanged |
| `create-vm.sh` | `<vm> <memory_mb> <cpus> <base_disk> <bridge_adapter> [intnet_name]` | unchanged |

Validation, step 1 (exit 2, no VBoxManage or ssh call):
- `lb`, `vm`: `^[a-z0-9][a-z0-9-]{0,31}$`; `lb != vm`.
- `memory_mb` 512-8192 and `cpus` 1-4. `base_disk` is absolute and `bridge_adapter` matches `^[A-Za-z0-9._:-]{1,64}$`.
- `reserved`: `-` or `^[0-9]{1,3}(,[0-9]{1,3})*$`, each value 1-254. `subnet` is 1-254.
- `port`: `^[1-9][0-9]{0,4}$` and at most 65535. `frontend_port`: the same rule, and not 22.
- `server_name`: `^server([1-9]|10)$`.
- spec: 5 comma-separated fields, each checked with the rules above. The IP is `10.10.<subnet>.(10-200)`, or any IPv4 when the script has no subnet. There are at most 10 specs and the names are unique.
- `--jump <lb>`: same rule as `lb`. `--frontend <port>`: frontend rule.

Exit codes (the same table in every `scripts/haproxy` header; each script lists the codes it uses):
| Code | Meaning |
|---|---|
| 0 | éxito |
| 2 | argumentos inválidos |
| 3 | VM no encontrada en VirtualBox |
| 4 | fallo de VBoxManage o de SSH |
| 5 | falló un paso (la limpieza o la restauración terminó bien) |
| 6 | disco base de HAProxy no encontrado, interfaz de puente no encontrada o VM ya registrada |
| 7 | VM sin dirección IP |
| 8 | no hay subredes internas libres |
| 9 | falló la operación y también la limpieza o la restauración |
| 10 | configuración de HAProxy inválida; se mantuvo la anterior |
| 11 | no se pudo recargar HAProxy; se restauró la configuración anterior |
| 12 | el balanceador está apagado o sin IPv4 en puente |
| 13 | la red interna `stratus-<lb>` ya tiene un servidor DHCP en VirtualBox |
| 14 | el servidor no obtuvo una IPv4 interna en 120 segundos (se revirtió) |
| 15 | no se pudo actualizar la cuenta jump (se revirtió el cambio en el servidor) |
| 99 | operación no implementada |
The 001-003 guest scripts add 12, and the key scripts also add 15.

Generated haproxy.cfg (`render_cfg`):
```
global
  log /dev/log local0
  user haproxy
  group haproxy
  daemon
  stats socket ipv4@127.0.0.1:9999 level user
defaults
  mode http
  log global
  option httplog
  timeout connect 5s
  timeout client 50s
  timeout server 50s
frontend stratus_front
  bind :<frontend_port>
  default_backend stratus_back
backend stratus_back
  balance roundrobin
  server <name> <ip>:<port> check[ disabled]     # one line per spec, "disabled" when off
```

`20-stratus-jump.conf`:
```
Match User jump
  AllowTcpForwarding local
  PermitOpen 10.10.N.10:22 10.10.N.11:22 ... 10.10.N.200:22
  PermitTTY no
  X11Forwarding no
  AllowAgentForwarding no
  AllowStreamLocalForwarding no
  PermitTunnel no
  GatewayPorts no
  ForceCommand /usr/sbin/nologin
```

`vm-exec.sh`:
```
declare -A VM_ROUTES                 # vm -> lb
vm_route <vm> <lb>                   # registers the route
vm_nic_ip <vm> <nic>                 # IPv4 of adapter <nic> (MAC match; Net/0 with a single NIC); 7 on failure
vm_ip <vm>                           # routed: LB adapter 1 (else 12 + message), then server adapter 1 (else 7)
vm_exec <vm> <cmd...>                # routed: ssh ... -o ProxyCommand="ssh -i <key> -o BatchMode=yes -o ConnectTimeout=10
                                     #   -o StrictHostKeyChecking=accept-new -o UserKnownHostsFile=<key>.known_hosts
                                     #   -o HostKeyAlias=<lb> -W %h:%p jump@<lb ip>" -o HostKeyAlias=<vm> root@<server ip>
```
Option parsing in each guest script (step "1.0 Opciones"): `--jump` must be first. `--frontend` comes only in the package scripts. After sourcing the library, `[[ -z "$jump_lb" ]] || vm_route "$vm_name" "$jump_lb"`.

bats helper `scripts/haproxy/test_helper.bash`:
```
load ../prepare/test_helper                # assert_script_structure, setup_stubs, stub_command, stub_vm_ip, stub_vm_nics
setup_haproxy_stubs                        # setup_stubs plus a logging stub for ../prepare/{create-vm,delete-vm,stop-vm}.sh
                                           # (copies of the scripts dir in $BATS_TEST_TMPDIR); the ssh stub writes calls.log,
                                           # remote.log, ssh-stdin.log and stdin/<n>, and answers from env:
                                           # DHCPSERVERS=<text of list dhcpservers>, FAIL_REMOTE=<ERE>, FAIL_RESTORE=<ERE>,
                                           # HAPROXY_CHECK=ok|fail, SS_OUT=<ss output>, CMP=same|diff, STAT_CSV=<show stat>,
                                           # SERVER_KEYS=<lines>, UFW=yes|no
stub_ip_sequence <vm> <ip|->...            # successive answers of the IP property (polls)
```

Go (package `repository`):
```go
type LoadBalancer struct { Name string; Subnet, FrontendPort int }
type LBServer struct { LB, Name, VM string; Port int; LastIP string; Enabled bool }
var ErrLBNotFound error
func NewLBRepository(db *sql.DB) *LBRepository
func (r *LBRepository) InsertLB(ctx context.Context, lb LoadBalancer) error
func (r *LBRepository) GetLB(ctx context.Context, name string) (LoadBalancer, error)
func (r *LBRepository) ListLBs(ctx context.Context) ([]LoadBalancer, error)
func (r *LBRepository) DeleteLB(ctx context.Context, name string) error
func (r *LBRepository) Subnets(ctx context.Context) ([]int, error)
func (r *LBRepository) SetFrontendPort(ctx context.Context, name string, port int) error
func (r *LBRepository) Servers(ctx context.Context, lb string) ([]LBServer, error)      // ORDER BY name number
func (r *LBRepository) ServerByVM(ctx context.Context, vm string) (LBServer, bool, error)
func (r *LBRepository) InsertServer(ctx context.Context, s LBServer) error
func (r *LBRepository) DeleteServer(ctx context.Context, lb, vm string) error
func (r *LBRepository) UpdateServerIPs(ctx context.Context, lb string, rows []LBServer) error // last_ip, enabled; one transaction
```

Go (package `service`):
```go
var (
  ErrLBOff                 = errors.New("El balanceador está apagado; enciéndalo para llegar al servidor")
  ErrLBNotFound            = errors.New("El balanceador no existe")
  ErrLBHasServers          = errors.New("Quite primero los servidores del balanceador")
  ErrLBFull                = errors.New("El balanceador ya tiene 10 servidores")
  ErrVMNotAssignable       = errors.New("La VM no se puede asignar a este balanceador")
  ErrVMIsServer            = errors.New("La VM es servidor de un balanceador; quítela primero")
  ErrHAProxyRemovalBlocked = errors.New("No se puede eliminar haproxy: el balanceador lo usa")
  ErrNoFreeSubnet          = errors.New("No hay subredes internas libres")
  ErrIntnetExists          = errors.New("Red interna ya registrada en VirtualBox")
  ErrHAProxyConfigInvalid  = errors.New("Configuración de HAProxy inválida; se mantuvo la anterior")
  ErrHAProxyReloadFailed   = errors.New("No se pudo recargar HAProxy; se mantuvo la configuración anterior")
  ErrJumpUpdateFailed      = errors.New("No se pudo actualizar la cuenta jump del balanceador; se revirtió el cambio en el servidor")
)
type LBOffError struct { Message string } // Error() = Message (stderr line with <lb>); Unwrap() = ErrLBOff
const exitLBOff = 12                      // mapped in operationRunner for every module
const ( exitNoFreeSubnet = 8; exitConfigInvalid = 10; exitReloadFailed = 11; exitIntnetExists = 13; exitJumpFailed = 15 )
func mapHAProxyError(err error) error     // only *OperationError: 8, 10, 11, 13 -> sentinels; others unchanged

func (l *VMLocks) TryLockAll(vms ...string) bool // all or nothing
func (l *VMLocks) UnlockAll(vms ...string)
func (l *VMLocks) TryLockCreate() bool           // global create lock shared by create_vm and create_lb
func (l *VMLocks) UnlockCreate()
func (o *operationRunner) runLocked(ctx context.Context, operation, vm string, lockVMs []string, script string,
  args []string, recordSuccess bool, onSuccess func(context.Context) error) (RunResult, error)

func targetArgs(ctx context.Context, lbs *repository.LBRepository, vm string, extra ...string) (args []string, lb string, err error)
  // server -> ["--jump", lb, vm, extra...]; otherwise [vm, extra...]
func serverSpecs(rows []repository.LBServer) []string
func nextServerName(rows []repository.LBServer) (string, error) // lowest free server1..server10, else ErrLBFull
func ValidatePort(p int) error         // 1-65535: "El puerto debe ser un número entre 1 y 65535"
func ValidateFrontendPort(p int) error // ValidatePort and != 22: "El puerto 22 está reservado para SSH"

type HAProxyConfig struct { BaseDisk, BridgeAdapter string }
func NewHAProxyService(cfg HAProxyConfig, runner ScriptRunner, vms *repository.VMRepository, lbs *repository.LBRepository,
  history *repository.HistoryRepository, locks *VMLocks, logger *log.Logger) *HAProxyService
type LBView struct { Name, State, IP string; Subnet string /* "10.10.N.0/24" */; FrontendPort, ServerCount int } // snake_case json
type ServerView struct { Name, VM, IP string; Port int; State string /* UP|DOWN|MAINT|other|"-" */ }
func (s *HAProxyService) ListLBs(ctx context.Context) ([]LBView, string, error)          // runs prepare/list-vms.sh, not recorded
func (s *HAProxyService) CreateLB(ctx context.Context, in CreateVMInput) (string, error)  // ValidateCreateVM; TryLockCreate else ErrCreateInProgress;
  // vms.Insert (ErrDuplicateVM); argv of section 6 with reserved = Subnets; ok -> InsertLB(subnet from "subnet=", port 80); fail -> vms.Delete
func (s *HAProxyService) DeleteLB(ctx context.Context, lb string) (string, error)         // servers > 0 -> ErrLBHasServers (no runner); ok -> DeleteLB + vms.Delete
func (s *HAProxyService) ListServers(ctx context.Context, lb string) ([]ServerView, error) // SQLite only; State "-"
func (s *HAProxyService) ServerStatus(ctx context.Context, lb string) (map[string]string, error) // lock lb; recordSuccess false
func (s *HAProxyService) AssignServer(ctx context.Context, lb, vm string, port int) (string, error)
  // ValidatePort; vm registered, not an LB, not in lb_servers (ErrVMNotAssignable); ErrLBFull; name = nextServerName;
  // runLocked(vm=lb, lockVMs=[lb, vm]); ok -> InsertServer(last_ip from "ip=", enabled)
func (s *HAProxyService) RemoveServer(ctx context.Context, lb, vm string) (string, error)  // lock [lb, vm]; ok -> DeleteServer
func (s *HAProxyService) SaveFrontend(ctx context.Context, lb string, port int) (string, error) // lock lb; ok -> SetFrontendPort
func (s *HAProxyService) Refresh(ctx context.Context, lb string) (string, error)          // lock lb; ok -> UpdateServerIPs
func (s *HAProxyService) History(ctx context.Context) ([]repository.Operation, error)     // module "haproxy", limit 50

type StartResult struct { Message string `json:"message"`; Warning string `json:"warning,omitempty"` }
func (s *PrepareService) StartVM(ctx context.Context, name string) (StartResult, error)
  // after ok: if the VM is an LB or a server -> haproxy.Refresh(lb); error -> Warning
  // "VM iniciada; no se pudieron actualizar las IPs del balanceador <lb>, use Actualizar IPs"
type VMView struct { ...; Role string `json:"role"` /* vm|lb|server */; LB string `json:"lb"`; LBIP string `json:"lb_ip"` }
```
Exit 15 maps to ErrJumpUpdateFailed in `GenerateUserKey`, `CreateUser` and `DeleteUser`. Exit 12 maps to `*LBOffError` in `operationRunner`, which covers every module.

Go (package `handler`): `RegisterHAProxy(mux, svc)`. Status codes:
- `ErrLBNotFound`: 404.
- `ErrLBOff` / `*LBOffError`, `ErrLBHasServers`, `ErrLBFull`, `ErrVMNotAssignable`, `ErrVMIsServer`, `ErrHAProxyRemovalBlocked`, `ErrNoFreeSubnet`, `ErrIntnetExists`: 409.
- `ErrHAProxyConfigInvalid`, `ErrHAProxyReloadFailed`, `ErrJumpUpdateFailed`: 500, each with its own text.

HTTP API (JSON `snake_case`):
| Method and path | Body | Success |
|---|---|---|
| `GET /api/haproxy/lbs` | - | `{"lbs":[{name,state,ip,subnet,frontend_port,server_count}],"list_error":""}` |
| `POST /api/haproxy/lbs` | `{name,memory_mb,cpus}` | 201 `{message}` |
| `DELETE /api/haproxy/lbs/{lb}` | - | 200 `{message}` |
| `PUT /api/haproxy/lbs/{lb}/frontend` | `{port}` | 200 `{message}` |
| `POST /api/haproxy/lbs/{lb}/refresh` | - | 200 `{message}` |
| `GET /api/haproxy/lbs/{lb}/servers` | - | 200 `{"servers":[{name,vm,ip,port,state:"-"}]}` |
| `GET /api/haproxy/lbs/{lb}/status` | - | 200 `{"states":{"server1":"UP"}}` |
| `POST /api/haproxy/lbs/{lb}/servers` | `{vm,port}` | 201 `{message}` |
| `DELETE /api/haproxy/lbs/{lb}/servers/{vm}` | - | 200 `{message}` |
| `GET /api/haproxy/history` | - | `{"operations":[...]}` via `historyHandler` |
| `POST /api/prepare/vms/{name}/start` (changed) | - | 200 `{message, warning?}` |
| `GET /api/prepare/vms` (changed) | - | each VM adds `role`, `lb`, `lb_ip` |

Frontend:
```ts
// features/prepare/types.ts
type VmRole = "vm" | "lb" | "server";
interface Vm { name; state; ip; user_key_generated; role: VmRole; lb: string; lb_ip: string }
// features/prepare/ssh-command.ts
sshCommand(vm: Vm, keyFile: string, user: string): string
  // server: `ssh -J jump@${vm.lb_ip || "<IP del balanceador>"} -i ${keyFile} ${user}@${vm.ip || "<IP interna>"}`
  // otherwise: `ssh -i ${keyFile} ${user}@${vm.ip || "<IP de la VM>"}`
// features/haproxy/types.ts
interface Lb { name: string; state: VmState; ip: string; subnet: string; frontend_port: number; server_count: number }
interface LbServer { name: string; vm: string; ip: string; port: number; state: string }
// features/haproxy/api.ts (request, operation, send, errorMessage from features/prepare/api)
listLbs(); createLb(input); deleteLb(lb); saveFrontend(lb, port); refreshLb(lb); listServers(lb);
getStatus(lb): Promise<Record<string, string>>; assignServer(lb, vm, port); removeServer(lb, vm); getHaproxyHistory()
// features/haproxy/validation.ts
validatePort(text): string | null          // "El puerto debe ser un número entre 1 y 65535"
validateFrontendPort(text): string | null  // plus "El puerto 22 está reservado para SSH"
// components
CreateLbForm({ busy; onDone })
LbTable({ lbs; selected; onSelect })
LbActions({ lb: Lb | null; onDone })               // delete confirmation names the LB and its internal network
FrontendForm({ lb: Lb | null; onDone })
AssignServerForm({ lb: Lb | null; vms: Vm[]; onDone }) // options: vms with role "vm"
ServerTable({ servers: LbServer[]; lbOff: boolean; lb: string; onDone })
```
`HAProxy.tsx`:
- On mount it loads `listLbs`, `listVms` and `getHaproxyHistory`.
- On each selection and after each operation it calls `listServers`. When the load balancer is `running` it also calls `getStatus`: success shows `toast.success("Estado del balanceador actualizado")`, failure shows the error `Alert`.
- When the load balancer is off it makes no status call, shows the state "-" and the note "El balanceador está apagado; los datos son los últimos conocidos", and shows no toast.
- One `useOperation` sets `busy`, which disables every button.

## 7. Test-first
- Framework and commands: Go `testing` (table-driven) with `cd stratus-srv && go test ./...`; Vitest + Testing Library with `cd stratus-ui && pnpm test`; bats with `bats -r scripts/`.
- Location and naming: tests sit next to the code. Go uses `TestAC<n>_<Behavior>`, Vitest `it("AC-<n> ...")` and bats `@test "AC-<n> ..."`. Go tests use `fakeRunner` and a temp SQLite file (`newHAProxyEnv` in `haproxy_service_test.go`). UI tests mock `fetch` and `sonner`. bats tests use `scripts/haproxy/test_helper.bash`. Level "manual" rows are steps of the real-host demo (P8).

| Test | Level | File | Covers (US / REQ / AC) | Given - when - then |
|---|---|---|---|---|
| AC-1 create-lb crea la red y llama a create-vm con la red interna | unit | `scripts/haproxy/create-lb.bats` | US-1 / REQ-1, REQ-2, REQ-3 / AC-1 | given `lb1 1024 1 <disk> eth0 -` - when it runs - then calls.log has `dhcpserver add --network=stratus-lb1` before `create-vm.sh lb1 1024 1 <disk> eth0 stratus-lb1`, and stdout ends with `subnet=1` |
| TestAC1_CreateLBPassesDiscreteArgs | unit | `stratus-srv/internal/service/haproxy_lbs_test.go` | US-1 / REQ-1 / AC-1 | given valid input and stdout `...\nsubnet=1` - when CreateLB runs - then argv `[lb1 1024 1 <haproxy disk> eth0 -]`, a vms row and a load_balancers row `{lb1,1,80}` |
| TestAC1_CreateLBRoute | unit | `stratus-srv/internal/handler/haproxy_test.go` | US-1 / REQ-1 / AC-1 | given ok - when POST `/api/haproxy/lbs` - then 201 `{message}` |
| AC-1 submits name, memory and CPUs | unit | `stratus-ui/src/features/haproxy/CreateLbForm.test.tsx` | US-1 / REQ-1, REQ-27 / AC-1 | given valid fields - when submitted - then POST body `{name,memory_mb,cpus}` and the result shows |
| AC-2 invalid fields show the 001 messages and send nothing | unit | `stratus-ui/src/features/haproxy/CreateLbForm.test.tsx` | US-1 / REQ-1 / AC-2 | given `LB_1`, memory 256, CPUs 5 - when submitted - then the 001 field errors and no fetch |
| TestAC2_CreateLBRejectsInvalidOrDuplicate | unit | `stratus-srv/internal/service/haproxy_lbs_test.go` | US-1 / REQ-1 / AC-2 | given invalid input, or a name already in vms - when CreateLB runs - then ErrInvalidInput / ErrDuplicateVM and no runner call |
| AC-3 create-lb sale con 6 sin disco base de HAProxy | unit | `scripts/haproxy/create-lb.bats` | US-1 / REQ-2 / AC-3 | given a missing disk - when it runs - then exit 6, "Disco base de HAProxy no encontrado", no `dhcpserver` or create-vm call |
| TestAC3_CreateLBMissingDiskRegistersNothing | unit | `stratus-srv/internal/service/haproxy_lbs_test.go` | US-1 / REQ-2 / AC-3 | given exit 6 with that stderr - when CreateLB runs - then the error text has it, and no vms or load_balancers row remains |
| AC-4 create-lb configura adaptadores y DHCP | unit | `scripts/haproxy/create-lb.bats` | US-1 / REQ-1, REQ-3 / AC-4 | given N=1 - then `dhcpserver add` has `--lower-ip=10.10.1.10 --upper-ip=10.10.1.200` and create-vm gets `stratus-lb1` |
| AC-4 create-vm agrega la NIC 2 en la red interna | unit | `scripts/prepare/create-vm.bats` | US-1 / REQ-3 / AC-4 | given a 6th argument `stratus-lb1` - then `modifyvm` has `--nic2 intnet --intnet2 stratus-lb1`; without it there is no `--nic2`; `stratus-x;y` exits 2 |
| AC-4 inspect disk parent, adapters and DHCP | manual | real host demo | US-1 / REQ-1, REQ-3 / AC-4 | `showvminfo lb1 --machinereadable`, `showmediuminfo` parent, `list dhcpservers` |
| AC-5 create-lb elige la subred libre más baja | unit | `scripts/haproxy/create-lb.bats` | US-1 / REQ-3 / AC-5 | given reserved `1,3` - when it runs - then 10.10.2.0/24 and `subnet=2` |
| TestAC5_CreateLBPassesReservedSubnets | unit | `stratus-srv/internal/service/haproxy_lbs_test.go` | US-1 / REQ-3 / AC-5 | given LBs with subnets 1 and 3 - when CreateLB runs - then the last arg is `1,3`, and stdout `subnet=2` stores 2 |
| AC-6 create-lb configura IP fija, jump, sshd y haproxy por SSH | unit | `scripts/haproxy/create-lb.bats` | US-1 / REQ-2, REQ-4, REQ-7 / AC-6 | when it runs - then remote.log has the interfaces file with `10.10.1.1/24`, `useradd --system ... --shell /usr/sbin/nologin jump`, the jump authorized_keys write whose stdin is `restrict,port-forwarding <backend key> stratus-backend`, `sshd -t`, `haproxy -c`, `enable --now haproxy` |
| AC-6 inspect bootstrap, HAProxy, adapter 2 and jump | manual | real host demo | US-1 / REQ-2, REQ-4 / AC-6 | `systemctl is-active haproxy`, `ip -4 addr`, `getent shadow jump`, `getent passwd jump`, `cat /var/lib/jump/.ssh/authorized_keys` |
| AC-7 create-lb limpia si falla el paso remoto | unit | `scripts/haproxy/create-lb.bats` | US-1 / REQ-2 / AC-7 | given FAIL_REMOTE on `useradd` - when it runs - then exit 5, `delete-vm.sh lb1` and `dhcpserver remove --network=stratus-lb1`, no `modifymedium` or `closemedium` on the base disk |
| TestAC7_CreateLBFailureFreesName | unit | `stratus-srv/internal/service/haproxy_lbs_test.go` | US-1 / REQ-2, REQ-22 / AC-7 | given exit 5 - then *OperationError, the vms row removed, no LB row, a history row `create_lb` error |
| TestAC8_CreateLockSharedWithCreateVM | integration | `stratus-srv/internal/service/haproxy_service_test.go` | US-1 / REQ-2 / AC-8 | given a create_vm blocked - when CreateLB runs - then ErrCreateInProgress and no script call; and the reverse |
| TestAC8_VMLocksCreateLock | unit | `stratus-srv/internal/service/vm_locks_test.go` | US-1 / REQ-2 / AC-8 | TryLockCreate twice gives true then false; after UnlockCreate it gives true |
| TestAC9_ListVMsMarksRoles | unit | `stratus-srv/internal/service/prepare_vms_test.go` | US-1 / REQ-5, REQ-13 / AC-9 | given lb1 and web-a assigned to it - when ListVMs runs - then lb1 has role `lb`, web-a has `server` with `lb:"lb1"` and `lb_ip` from the lb1 line |
| TestAC9_SetHostnameOnLBIsDirect | unit | `stratus-srv/internal/service/prepare_system_test.go` | US-1 / REQ-5 / AC-9 | given lb1 - when SetHostname runs - then argv `[lb1 web-01]` with no `--jump` |
| AC-9 the selector shows the Balanceador label | unit | `stratus-ui/src/features/prepare/VmSelector.test.tsx` | US-1 / REQ-5 / AC-9 | given lb1 with role `lb` - when rendered - then the option reads `lb1 - Encendida - 192.168.1.60 - Balanceador` |
| TestAC10_DeleteLBRejectsWithServers | unit | `stratus-srv/internal/service/haproxy_lbs_test.go` | US-1 / REQ-6 / AC-10 | given one server - then ErrLBHasServers and no runner call; given none - then `haproxy/delete-lb.sh [lb1]` and the rows deleted |
| AC-10 delete-lb borra la VM y el servidor DHCP | unit | `scripts/haproxy/delete-lb.bats` | US-1 / REQ-6 / AC-10 | when it runs - then `delete-vm.sh lb1`, then `dhcpserver remove --network=stratus-lb1`; a missing DHCP server is tolerated |
| AC-10 delete asks for confirmation and shows the result | unit | `stratus-ui/src/features/haproxy/LbActions.test.tsx` | US-1 / REQ-6 / AC-10 | given lb1 - when Eliminar is clicked - then a dialog names lb1 and `stratus-lb1`, no fetch until Confirmar, and a 409 text shows |
| AC-11 save-frontend escribe el nuevo puerto | unit | `scripts/haproxy/save-frontend.bats` | US-3 / REQ-7 / AC-11 | given `lb1 8080 server1,...` - when it runs - then the uploaded cfg has `bind :8080` and no `bind :80`, then check and reload |
| AC-11 rejects 22, 0 and 65536 | unit | `stratus-ui/src/features/haproxy/validation.test.ts` | US-3 / REQ-7 / AC-11 | given those values - when validateFrontendPort runs - then each returns an error; 8080 is valid |
| AC-11 invalid port sends nothing | unit | `stratus-ui/src/features/haproxy/FrontendForm.test.tsx` | US-3 / REQ-7 / AC-11 | given 22 - when saved - then the error and no fetch; given 8080 - then PUT body `{port:8080}` |
| TestAC11_SaveFrontendArgsAndValidation | unit | `stratus-srv/internal/service/haproxy_frontend_test.go` | US-3 / REQ-7 / AC-11 | given 8080 - then argv `[lb1 8080 <specs>]` and frontend_port 8080 stored; given 22, 0, 65536 - then ErrInvalidInput and no runner call |
| AC-11 HAProxy listens on 8080 only | manual | real host demo | US-3 / REQ-7 / AC-11 | `ss -ltnp` on the LB before and after |
| AC-12 render_cfg sin servidores | unit | `scripts/haproxy/lib/haproxy-common.bats` | US-3 / REQ-7 / AC-12 | given port 80 and no specs - then the output has `bind :80`, `balance roundrobin`, the stats socket on 127.0.0.1:9999 and no `server` line |
| AC-12 curl returns 503 | manual | real host demo | US-3 / REQ-7 / AC-12 | `curl -i http://<LB IP>/` shows 503 |
| AC-13 assign-server reconfigura, espera IP, copia llaves y agrega la línea | unit | `scripts/haproxy/assign-server.bats` | US-2 / REQ-9, REQ-10, REQ-17 / AC-13 | given web-a running and an IP sequence `- 10.10.1.11` - then in order: `stop-vm.sh web-a`, `modifyvm web-a --nic1 intnet --intnet1 stratus-lb1 --nic2 nat`, `startvm`, the jump_sync write, the cfg with `server server1 10.10.1.11:80 check`, check and reload; stdout `ip=10.10.1.11` |
| TestAC13_AssignServerArgsAndRow | unit | `stratus-srv/internal/service/haproxy_servers_test.go` | US-2 / REQ-8, REQ-9, REQ-10 / AC-13 | given lb1 subnet 1 and no servers - when AssignServer(web-a, 80) runs - then script `haproxy/assign-server.sh`, argv `[lb1 1 web-a server1 80 80 eth0]`, row `server1 web-a 80 10.10.1.11 on` |
| AC-13 assign confirms that the VM restarts | unit | `stratus-ui/src/features/haproxy/AssignServerForm.test.tsx` | US-2 / REQ-10, REQ-27 / AC-13 | given web-a and port 80 - when Asignar is clicked - then a dialog warns that web-a restarts, no fetch until Confirmar, then POST `{vm:"web-a",port:80}` |
| AC-13 web-a balanced on the internal network | manual | real host demo | US-2 / REQ-10 / AC-13 | `showvminfo web-a` nic1/nic2, its 10.10.1.x IP, haproxy.cfg line |
| TestAC14_NextServerNameLowestFree | unit | `stratus-srv/internal/service/haproxy_topology_test.go` | US-2 / REQ-9 / AC-14 | given server2 only - then `server1`; given server1..server10 - then ErrLBFull |
| TestAC15_AssignRejectionsChangeNothing | unit | `stratus-srv/internal/service/haproxy_servers_test.go` | US-2 / REQ-8 / AC-15 | given an LB VM, a VM assigned to lb2, or lb1 with 10 servers - when AssignServer runs - then ErrVMNotAssignable, ErrVMNotAssignable, ErrLBFull, with no runner call and no row |
| TestAC15_HandlerMapsAssignErrors | unit | `stratus-srv/internal/handler/haproxy_test.go` | US-2 / REQ-8 / AC-15 | given each rejection - when POST `.../servers` - then 409 with the spec text |
| AC-16 rejects server ports 0, 65536 and 80a | unit | `stratus-ui/src/features/haproxy/validation.test.ts` | US-2 / REQ-8 / AC-16 | given those values - when validatePort runs - then errors |
| AC-16 invalid server port sends nothing | unit | `stratus-ui/src/features/haproxy/AssignServerForm.test.tsx` | US-2 / REQ-8 / AC-16 | given `80a` - when submitted - then the field error and no fetch |
| TestAC16_ValidatePort | unit | `stratus-srv/internal/service/haproxy_validation_test.go` | US-2 / REQ-8 / AC-16 | given 0 and 65536 - when AssignServer runs - then ErrInvalidInput and no runner call |
| AC-17 assign-server revierte si no hay IP interna | unit | `scripts/haproxy/assign-server.bats` | US-2 / REQ-10 / AC-17 | given an IP that never arrives and web-a poweroff before - then exit 14 after 60 sleeps; the trap runs `jump_drop`, `poweroff`, `modifyvm web-a --nic1 bridged --bridgeadapter1 eth0 --nic2 none` and no `startvm` afterwards; no cfg upload |
| TestAC17_AssignFailureStoresNothing | unit | `stratus-srv/internal/service/haproxy_servers_test.go` | US-2 / REQ-10, REQ-22 / AC-17 | given exit 14 - then *OperationError, no lb_servers row, a history row error |
| AC-18 remove-server quita la línea, las llaves y vuelve al puente | unit | `scripts/haproxy/remove-server.bats` | US-2 / REQ-11, REQ-17 / AC-18 | given web-a running - then in order: the cfg without web-a's line, check and reload, jump_drop for web-a, `stop-vm.sh web-a`, `modifyvm web-a --nic2 none --nic1 bridged --bridgeadapter1 eth0`, `startvm web-a` |
| TestAC18_RemoveServerDeletesRow | unit | `stratus-srv/internal/service/haproxy_servers_test.go` | US-2 / REQ-11 / AC-18 | given web-a and web-b - when RemoveServer(web-a) runs - then argv `[lb1 web-a 80 eth0 <web-b spec>]` and only the web-b row remains |
| AC-18 remove confirms that the VM restarts | unit | `stratus-ui/src/features/haproxy/ServerTable.test.tsx` | US-2 / REQ-11 / AC-18 | given server1 web-a - when Quitar is clicked - then a dialog warns that web-a restarts, then DELETE `.../servers/web-a` |
| AC-18 web-a back on the LAN | manual | real host demo | US-2 / REQ-11 / AC-18 | `showvminfo web-a` (nic2 none), its LAN IP, jump authorized_keys |
| TestAC19_DeleteAssignedVMRejected | unit | `stratus-srv/internal/service/prepare_vms_test.go` | US-2 / REQ-12 / AC-19 | given web-a assigned - when DeleteVM runs - then ErrVMIsServer and no runner call; the handler returns 409 with the text |
| AC-20 assign-server agrega NAT sin reenvío de puertos | unit | `scripts/haproxy/assign-server.bats` | US-2 / REQ-10 / AC-20 | then `--nic2 nat` and no `natpf` in calls.log |
| AC-20 nginx installs on a server | manual | real host demo | US-2, US-4 / REQ-10, REQ-19 / AC-20 | install nginx from 001 on web-a: success |
| AC-21 refresh-lb reescribe la IP nueva y recarga | unit | `scripts/haproxy/refresh-lb.bats` | US-4 / REQ-14 / AC-21 | given spec `server1,web-a,80,10.10.1.11,on` and web-a now at 10.10.1.15, CMP=diff - then the cfg has `10.10.1.15:80`, the reload runs, stdout `server1 10.10.1.15 on` and `reloaded=yes` |
| TestAC21_StartServerTriggersRefresh | unit | `stratus-srv/internal/service/prepare_vms_test.go` | US-4 / REQ-14 / AC-21 | given web-a assigned to lb1 - when StartVM runs - then `start-vm.sh`, then `haproxy/refresh-lb.sh [lb1 1 80 server1,web-a,80,10.10.1.11,on]`; the row gets the new IP and there is no warning |
| AC-22 refresh-lb no recarga si nada cambió | unit | `scripts/haproxy/refresh-lb.bats` | US-4 / REQ-14 / AC-22 | given the same IPs and CMP=same - then no `systemctl reload` and `reloaded=no` |
| TestAC22_RefreshRoute | unit | `stratus-srv/internal/handler/haproxy_test.go` | US-4 / REQ-14 / AC-22 | given ok - when POST `.../refresh` - then 200 `{message}` and a history row |
| AC-23 servidor apagado queda disabled con la última IP | unit | `scripts/haproxy/refresh-lb.bats` | US-4 / REQ-15 / AC-23 | given web-b poweroff - then `server server2 10.10.1.12:80 check disabled` and `server2 10.10.1.12 off`; given it running at 10.10.1.12 - then no `disabled` and `on` |
| TestAC23_RefreshStoresEnabledFlag | unit | `stratus-srv/internal/service/haproxy_frontend_test.go` | US-4 / REQ-15 / AC-23 | given stdout with `server2 10.10.1.12 off` - when Refresh runs - then the row has enabled false and the same IP |
| AC-24 apply_cfg conserva la configuración si falla la revisión | unit | `scripts/haproxy/lib/haproxy-common.bats` | US-2..US-4 / REQ-16 / AC-24 | given HAPROXY_CHECK=fail - then exit 10, the check output on stderr, `rm -f` of `.new`, no `mv` and no reload |
| TestAC24_ConfigInvalidMapsError | unit | `stratus-srv/internal/service/haproxy_service_test.go` | US-2..US-4 / REQ-16 / AC-24 | given exit 10 - when AssignServer, RemoveServer, SaveFrontend and Refresh run - then ErrHAProxyConfigInvalid, no SQLite change, stderr in history |
| AC-25 lb-status lee show stat | unit | `scripts/haproxy/lb-status.bats` | US-5 / REQ-20 / AC-25 | given STAT_CSV with server1 UP, server2 DOWN and BACKEND - then stdout is `server1 UP` and `server2 DOWN`, and the remote program opens `/dev/tcp/127.0.0.1/9999` |
| TestAC25_ServerStatusParses | unit | `stratus-srv/internal/service/haproxy_servers_test.go` | US-5 / REQ-20 / AC-25 | given that stdout - when ServerStatus runs - then `{server1:UP, server2:DOWN}` and no history row |
| AC-25 table shows UP and DOWN with the toast | unit | `stratus-ui/src/routes/HAProxy.test.tsx` | US-5 / REQ-20, REQ-27 / AC-25 | given lb1 running and those states - when selected - then rows `server1 web-a 10.10.1.11 80 UP` and `server2 ... DOWN`, and "Estado del balanceador actualizado" |
| AC-25 UP and DOWN on the real LB | manual | real host demo | US-5 / REQ-20 / AC-25 | nginx on web-a, nothing on web-b |
| AC-26 jump_sync copia las llaves sin la del backend y una sola vez | unit | `scripts/haproxy/lib/jump-keys.bats` | US-6 / REQ-17 / AC-26 | given SERVER_KEYS with the backend key, a root key twice and a user key - then the LB write stdin has two lines `restrict,port-forwarding <type> <blob> stratus-jump:web-a` and the program keeps other lines; jump_drop sends an empty set and keeps `stratus-backend` |
| AC-26 root key in jump once, removed on remove | manual | real host demo | US-6 / REQ-17 / AC-26 | `grep -c` of the key in jump authorized_keys after assign (1) and after remove (0); `stratus-backend` stays |
| AC-27 generate-user-key con --jump sincroniza jump | unit | `scripts/prepare/generate-user-key.bats` | US-6 / REQ-17, REQ-19 / AC-27 | given `--jump lb1 web-a` - then ssh calls to web-a use the ProxyCommand with `jump@<lb ip>`, and the jump_sync write runs after `systemctl restart ssh` |
| AC-27 create-user con --jump y llave sincroniza jump | unit | `scripts/users/create-user.bats` | US-6 / REQ-17 / AC-27 | given `--jump lb1 web-a ana no yes` - then the jump_sync write after authorized_keys |
| TestAC27_KeyOpsOnServerPassJump | unit | `stratus-srv/internal/service/prepare_keys_test.go` | US-6 / REQ-17, REQ-19 / AC-27 | given web-a assigned to lb1 - when GenerateUserKey and CreateUser run - then argv starts with `--jump lb1 web-a` |
| AC-27 usage message shows ssh -J | unit | `stratus-ui/src/features/prepare/ssh-command.test.ts` | US-6 / REQ-18 / AC-27 | given a server with lb_ip 192.168.1.60 and ip 10.10.1.11 - then `ssh -J jump@192.168.1.60 -i id_ed25519_web-a root@10.10.1.11`; a plain VM gives the direct command |
| AC-27 key forms use the jump command | unit | `stratus-ui/src/features/prepare/UserKeyForm.test.tsx`, `stratus-ui/src/features/users/UserKeyDownload.test.tsx` | US-6 / REQ-18 / AC-27 | given a server VM - when the key is generated or the user is created - then the usage line is the `ssh -J` command |
| AC-27 jump command opens a session | manual | real host demo | US-6 / REQ-18 / AC-27 | run the shown command with the downloaded key |
| AC-28 create-lb restringe jump en sshd | unit | `scripts/haproxy/create-lb.bats` | US-6 / REQ-4 / AC-28 | then the sshd file stdin has `Match User jump`, `AllowTcpForwarding local`, `PermitTTY no`, `ForceCommand /usr/sbin/nologin`, and PermitOpen with exactly `10.10.1.10:22` to `10.10.1.200:22` (191 items) |
| AC-28 shell, other networks and other ports refused | manual | real host demo | US-6 / REQ-4 / AC-28 | `ssh jump@lb`, `ssh -J jump@lb root@192.168.1.1`, `ssh -L 3306:10.10.1.11:3306 jump@lb` all refused |
| AC-29 vm_ip sale con 12 si el balanceador está apagado | unit | `scripts/prepare/lib/vm-exec.bats` | US-6 / REQ-19 / AC-29 | given `vm_route web-a lb1` and lb1 poweroff - when vm_ip and vm_exec run - then 12, "El balanceador lb1 está apagado; enciéndalo para llegar al servidor", no ssh |
| AC-29 install-package con --jump y balanceador apagado | unit | `scripts/prepare/install-package.bats` | US-6 / REQ-19 / AC-29 | given `--jump lb1 web-a nginx` and lb1 off - then exit 12 and no ssh call |
| TestAC29_OperationRunnerMapsLBOff | unit | `stratus-srv/internal/service/operation_runner_test.go` | US-6 / REQ-19, REQ-22 / AC-29 | given exit 12 and that stderr line - then *LBOffError with that text, errors.Is ErrLBOff, a history row; the handler returns 409 with the text |
| AC-30 balanced responses, before/after | manual | real host demo | US-1..US-4 / REQ-7, REQ-10 / AC-30 | before: no LB; after: `for i in 1 2 3 4; do curl -s <LB IP>; done` alternates web-a and web-b |
| AC-31 server restarted keeps receiving traffic | manual | real host demo | US-4 / REQ-14 / AC-31 | stop web-a, change its lease, start it from 001, curl through the frontend |
| AC-32 jump command with the downloaded key | manual | real host demo | US-6 / REQ-18 / AC-32 | root and user sessions open on the server |
| AC-33 delete removes intnet and DHCP | manual | real host demo | US-1 / REQ-6 / AC-33 | `VBoxManage list intnets` and `list dhcpservers` before and after |
| TestAC34_AssignLocksLBAndServer | integration | `stratus-srv/internal/service/haproxy_service_test.go` | US-1..US-6 / REQ-21 / AC-34 | given an assign blocked on lb1 and web-a - when InstallPackage on web-a, SaveFrontend on lb1 and Refresh on lb1 run - then ErrOperationInProgress and no script call |
| TestAC34_TryLockAllIsAllOrNothing | unit | `stratus-srv/internal/service/vm_locks_test.go` | US-2 / REQ-21 / AC-34 | given web-a locked - TryLockAll(lb1, web-a) is false and lb1 stays free |
| TestAC35_HAProxyHistoryAndRecordPolicy | unit | `stratus-srv/internal/service/haproxy_service_test.go` | US-7 / REQ-23, REQ-24 / AC-35 | given 55 haproxy rows and prepare rows - then 50 haproxy rows, newest first; 5 successful ServerStatus add 0 rows, a failed one adds 1 |
| AC-35 shows the module history | unit | `stratus-ui/src/routes/HAProxy.test.tsx` | US-7 / REQ-24 / AC-35 | given `/api/haproxy/history` rows - then the rows show with "Ver stderr" |
| AC-36 Backends HAProxy navigates | unit | `stratus-ui/src/routes/Home.test.tsx` | US-1..US-7 / REQ-25 / AC-36 | given the home - then the HAProxy card links to `/haproxy` and no card shows "Próximamente" |
| AC-37 <script> sale con 2 ante argumentos inválidos | unit | `scripts/haproxy/*.bats` | US-1..US-6 / REQ-22, REQ-26 / AC-37 | given a missing argument, frontend port `22`, a bad spec, `--jump LB` - when each of the 7 scripts runs - then exit 2 and an empty calls.log |
| AC-37 los scripts con --jump inválido salen con 2 | unit | `scripts/prepare/install-package.bats`, `scripts/users/create-user.bats`, `scripts/services/start-service.bats` | US-6 / REQ-19 / AC-37 | given `--jump` without a value or `--jump Lb1` - then exit 2 and an empty calls.log |
| AC-38 <script> tiene la estructura exigida | unit | `scripts/haproxy/*.bats` | US-1..US-6 / REQ-26 / AC-38 | given each of the 7 files - when `assert_script_structure` runs - then it passes |
| AC-39 refresh-lb espera en paralelo con un solo límite | unit | `scripts/haproxy/refresh-lb.bats` | US-4 / REQ-14, REQ-15 / AC-39 | given 10 running servers, 3 of them never reporting an IP - then the `sleep 2` calls in each poll log total at most 60 per server and the run ends once the shared budget ends; the 3 are `disabled` with their last IPs |
| AC-40 remove-server con balanceador apagado no cambia nada | unit | `scripts/haproxy/remove-server.bats` | US-2 / REQ-11 / AC-40 | given lb1 poweroff - then exit 12, the message, no `modifyvm`, no `stop-vm.sh`, no ssh |
| TestAC40_RemoveServerLBOffKeepsRow | unit | `stratus-srv/internal/service/haproxy_servers_test.go` | US-2 / REQ-11 / AC-40 | given exit 12 - then *LBOffError and the row remains |
| TestAC41_StartWithFailedRefreshWarns | unit | `stratus-srv/internal/service/prepare_vms_test.go` | US-4 / REQ-14 / AC-41 | given start ok and refresh exit 12 - when StartVM runs - then no error, Warning "VM iniciada; no se pudieron actualizar las IPs del balanceador lb1, use Actualizar IPs", a `refresh_lb` error row in module haproxy |
| AC-41 start shows the warning | unit | `stratus-ui/src/features/prepare/VmActions.test.tsx` | US-4 / REQ-14 / AC-41 | given start returns `{message, warning}` - when Iniciar is clicked - then the success result and the warning Alert |
| TestAC42_KeyAndUserOpsLockServerAndLB | integration | `stratus-srv/internal/service/users_service_test.go` | US-6 / REQ-21 / AC-42 | given a Refresh blocked on lb1 - when GenerateUserKey, CreateUser and DeleteUser on web-a run - then ErrOperationInProgress |
| AC-42 generate-user-key restaura si jump falla | unit | `scripts/prepare/generate-user-key.bats` | US-6 / REQ-17 / AC-42 | given `--jump` and FAIL_REMOTE on the jump write - then exit 15, the authorized_keys backup restored, stdout empty |
| AC-42 create-user y delete-user revierten si jump falla | unit | `scripts/users/create-user.bats`, `scripts/users/delete-user.bats` | US-6 / REQ-17 / AC-42 | create: the cleanup trap runs `userdel -r ana` and exits 15; delete: jump_sync with the exclusion fails first, exit 15, no pkill or userdel |
| TestAC42_JumpFailureMapsError | unit | `stratus-srv/internal/service/users_create_test.go` | US-6 / REQ-17 / AC-42 | given exit 15 - when GenerateUserKey, CreateUser and DeleteUser run - then ErrJumpUpdateFailed, no key stored |
| TestAC43_DeleteVMOnLBUsesDeleteLB | unit | `stratus-srv/internal/service/prepare_vms_test.go` | US-1 / REQ-6 / AC-43 | given lb1 with a server - when DeleteVM runs - then ErrLBHasServers; without servers - then `haproxy/delete-lb.sh [lb1]`, a `delete_lb` row in module haproxy, rows removed |
| AC-44 create-lb crea jump como cuenta de sistema | unit | `scripts/haproxy/create-lb.bats` | US-1 / REQ-4 / AC-44 | then the `useradd` call has `--system` |
| AC-44 jump UID and not listed in 002 | manual | real host demo | US-1 / REQ-4 / AC-44 | `id -u jump` < 1000; the 002 list of lb1 lacks jump |
| AC-45 apply_cfg restaura si la recarga no enlaza el puerto | unit | `scripts/haproxy/lib/haproxy-common.bats` | US-3 / REQ-16 / AC-45 | given SS_OUT with nginx on 8080 - then 5 polls, `mv` of `.bak` back, a second reload, exit 11, the output on stderr |
| TestAC45_ReloadFailedMapsError | unit | `stratus-srv/internal/service/haproxy_frontend_test.go` | US-3 / REQ-16 / AC-45 | given exit 11 - when SaveFrontend runs - then ErrHAProxyReloadFailed, frontend_port unchanged, stderr in history |
| AC-45 port in use keeps port 80 | manual | real host demo | US-3 / REQ-16 / AC-45 | nginx on 8080, save 8080, `ss -ltnp` shows haproxy on 80 |
| AC-46 off LB shows stored servers without a read | unit | `stratus-ui/src/routes/HAProxy.test.tsx` | US-5 / REQ-20 / AC-46 | given lb1 poweroff - when selected - then no `/status` request, the row `server1 web-a 10.10.1.11 80 -`, the note, no toast, no error Alert |
| TestAC46_ListServersFromSQLite | unit | `stratus-srv/internal/service/haproxy_servers_test.go` | US-5 / REQ-20 / AC-46 | given a stored server - when ListServers runs - then the row with state "-" and no runner call |
| AC-47 vm_ip con ruta: balanceador encendido y servidor apagado da 7 | unit | `scripts/prepare/lib/vm-exec.bats` | US-6 / REQ-19 / AC-47 | given lb1 running at 192.168.1.60 and web-a poweroff - then 7, "La VM no tiene dirección IP; verifique que esté encendida", no ssh |
| AC-47 vm_nic_ip elige la IP por MAC | unit | `scripts/prepare/lib/vm-exec.bats` | US-4, US-6 / REQ-13, REQ-19 / AC-47 | given two NICs with Net/0 = NAT IP and Net/1 = the adapter-1 MAC - then the IP of Net/1 |
| AC-48 assign-server deja encendida una VM que estaba apagada | unit | `scripts/haproxy/assign-server.bats` | US-2 / REQ-10 / AC-48 | given web-a poweroff - then no `stop-vm.sh`, `startvm web-a`, and no poweroff after success |
| TestAC48_ReassignSameLBRejected | unit | `stratus-srv/internal/service/haproxy_servers_test.go` | US-2 / REQ-8 / AC-48 | given web-a already in lb1 - when AssignServer(lb1, web-a) runs - then ErrVMNotAssignable and no runner call |
| TestAC49_PackageRulesOnLB | unit | `stratus-srv/internal/service/prepare_packages_test.go` | US-1 / REQ-5 / AC-49 | given lb1 - RemovePackage haproxy gives ErrHAProxyRemovalBlocked and no runner call; InstallPackage ufw has argv `[--frontend 80 lb1 ufw]` |
| AC-49 install-package y remove-package con --frontend | unit | `scripts/prepare/install-package.bats`, `scripts/prepare/remove-package.bats` | US-1 / REQ-5 / AC-49 | install: `ufw allow 22/tcp` and `ufw allow 80/tcp`; remove `--frontend 80 lb1 haproxy`: exit 2 and the text, no ssh |
| AC-49 save-frontend permite el puerto en ufw | unit | `scripts/haproxy/save-frontend.bats` | US-3 / REQ-7 / AC-49 | given UFW=yes - then `ufw allow 8080/tcp`; given UFW=no - then no ufw call |
| AC-50 create-lb evita subredes de VirtualBox y redes ya registradas | unit | `scripts/haproxy/create-lb.bats` | US-1 / REQ-3 / AC-50 | given DHCPSERVERS with 10.10.1.2 - then subnet 2; given DHCPSERVERS with `NetworkName: stratus-lb1` - then exit 13, "Red interna ya registrada en VirtualBox", no `dhcpserver add` or create-vm |
| TestAC50_CreateLBMapsSubnetErrors | unit | `stratus-srv/internal/service/haproxy_lbs_test.go` | US-1 / REQ-3 / AC-50 | given exit 13 or 8 - then ErrIntnetExists / ErrNoFreeSubnet and no rows |
| TestAC51_AssignHistoryRow | unit | `stratus-srv/internal/service/haproxy_servers_test.go` | US-7 / REQ-23 / AC-51 | given an assign of web-a to lb1 with port 80 - then the row has operation `assign_server`, VM `lb1`, and args containing `web-a` and `80` |
| TestAC1_LBRepository | unit | `stratus-srv/internal/repository/load_balancers_test.go` | US-1, US-2 / REQ-3, REQ-9 / AC-1, AC-13 | insert, get, list, subnets, servers ordered by number, ServerByVM, UpdateServerIPs, delete |

Existing tests that must keep passing: every 001-003 bats, Go and Vitest test. Only constructor arguments and `Vm` fixtures change. `stub_vm_ip` and the single-NIC path of `vm_nic_ip` keep the old behavior.

## 8. Traceability
| Requirement | Component / file | Verified by (AC) |
|---|---|---|
| REQ-1 | `CreateLbForm`, `HAProxyService.CreateLB`, `create-lb.sh`, `create-vm.sh` | AC-1, AC-2, AC-4 |
| REQ-2 | `create-lb.sh` steps 2, 5 and trap, shared create lock | AC-3, AC-6, AC-7, AC-8 |
| REQ-3 | `create-lb.sh` subnet choice and DHCP, `create-vm.sh` NIC 2, `LBRepository.Subnets` | AC-4, AC-5, AC-50 |
| REQ-4 | `create-lb.sh` jump steps, `20-stratus-jump.conf` | AC-6, AC-28, AC-44 |
| REQ-5 | `VMView.Role`, `VmSelector`, `HAProxyService.ListLBs`, `LbTable`, `--frontend` in the package scripts | AC-9, AC-49 |
| REQ-6 | `DeleteLB`, `PrepareService.DeleteVM`, `delete-lb.sh`, `LbActions` | AC-10, AC-33, AC-43 |
| REQ-7 | `render_cfg`, `save-frontend.sh`, `FrontendForm`, validators | AC-11, AC-12, AC-30, AC-49 |
| REQ-8 | `AssignServer` checks, `ValidatePort`, `AssignServerForm` | AC-13, AC-15, AC-16, AC-48 |
| REQ-9 | `nextServerName` | AC-13, AC-14 |
| REQ-10 | `assign-server.sh` and trap | AC-13, AC-17, AC-20, AC-30, AC-48 |
| REQ-11 | `remove-server.sh`, `RemoveServer`, `ServerTable` | AC-18, AC-40 |
| REQ-12 | `PrepareService.DeleteVM` | AC-19 |
| REQ-13 | `vm_nic_ip`, `list-vms.sh` | AC-9, AC-47 |
| REQ-14 | `refresh-lb.sh`, `Refresh`, `StartVM` warning, `VmActions` | AC-21, AC-22, AC-31, AC-39, AC-41 |
| REQ-15 | `refresh-lb.sh` disabled flag, `UpdateServerIPs` | AC-23, AC-39 |
| REQ-16 | `apply_cfg`, `mapHAProxyError` | AC-24, AC-45 |
| REQ-17 | `jump-keys.sh`, key scripts with `--jump` | AC-13, AC-18, AC-26, AC-27, AC-42 |
| REQ-18 | `sshCommand`, `UserKeyForm`, `UserKeyDownload` | AC-27, AC-32 |
| REQ-19 | `targetArgs`, `--jump` parsing, `VM_ROUTES`, `vm_ip` 12/7, `LBOffError` | AC-20, AC-27, AC-29, AC-47 |
| REQ-20 | `lb-status.sh`, `ServerStatus`, `ListServers`, `HAProxy.tsx` | AC-25, AC-46 |
| REQ-21 | `VMLocks.TryLockAll`, `runLocked`, two-VM locks in keys and users | AC-34, AC-42 |
| REQ-22 | `ExecRunner` 15 min, one script per operation, `useOperation` busy | AC-7, AC-17, AC-29, AC-37 |
| REQ-23 | `operationRunner` record policy, module `haproxy` | AC-35, AC-51 |
| REQ-24 | `HAProxyService.History`, history route, `HistoryTable` | AC-35 |
| REQ-25 | `modules.ts`, `App.tsx`, `Home.test.tsx` | AC-36 |
| REQ-26 | headers and sections of `scripts/haproxy/*.sh` | AC-37, AC-38 |
| REQ-27 | shadcn components and Spanish texts in `features/haproxy` | AC-1, AC-13, AC-25 |

## 9. Decisions and risks
- The topology lives in SQLite (user's choice). The backend passes `--jump <lb>` to the 001-003 guest scripts, so the scripts never infer the role from VirtualBox. Rejected: detecting the role from adapter 1. Rejected: an environment variable, which breaks P2 (inputs as explicit arguments).
- One routing map in `vm-exec.sh`. Each script only parses `--jump` and calls `vm_route`. Rejected: adding jump logic to every script.
- haproxy.cfg is rendered whole from the server list in SQLite, then checked, reloaded and restored from `.bak` (user's choice). Rejected: editing lines in place.
- The state is read from a stats socket on `127.0.0.1:9999` with bash `/dev/tcp` (user's choice). Rejected: `socat` with the unix socket, which needs one more package.
- `create-lb.sh` reuses `create-vm.sh` with an optional internal network, so the 001 bootstrap is not copied. NIC 2 exists from the first boot, so there is no extra power cycle.
- `jump` is limited by an sshd `Match User jump` block with `PermitOpen` over the lease range `10.10.N.10`-`.200` port 22 (a subset of the `/24`, because OpenSSH does not allow host wildcards), plus `restrict,port-forwarding` on every key. Rejected: `permitopen` on every key line, which would make 191 options per key.
- The `jump` lines are tagged `stratus-jump:<vm>`, and every update rewrites the whole set of that server (sync). Add, remove and user changes then use one idempotent path.
- A load balancer deleted from the 001 screen runs `haproxy/delete-lb.sh` and is recorded in the 004 history (user confirmed).
- `ProxyCommand` with the backend key and the pinned known_hosts replaces `ssh -J`, because `-J` does not pass `-i` or `HostKeyAlias` to the hop. The UI still shows the `ssh -J` form to the user (REQ-18).
- The DHCP server takes `10.10.N.2`, outside the lease range and different from the LB's `10.10.N.1`.
- The create lock moves into `VMLocks`, which every service already shares. Rejected: passing `PrepareService` to `HAProxyService`.
- Risk: `create-lb.sh` runs `create-vm.sh` (worst case 14 min) plus about 1 min of SSH steps, which can pass the 15-minute timeout. Mitigation: SIGTERM runs both traps (process group). The extra steps have short bounds (`ConnectTimeout=10`, 5 reload polls).
- Risk: `systemctl reload haproxy` returns 0 even when the new workers cannot bind the port. Mitigation: the `ss` check after the reload (AC-45).
- Risk: create-vm.sh waits for `/Net/0/V4/IP`. With NIC 2 unconfigured in the guest, Net/0 is adapter 1. Mitigation: later reads use `vm_nic_ip` by MAC, and the AC-4 demo step checks the IP.
- Risk: a removal that fails after the cfg change keeps the SQLite row, so a refresh would put the server back as `disabled`. Mitigation: every remove step is idempotent and the UI shows the failure, so a retry finishes it.
- Risk: the bats stubs check commands, not effects, for networking, sshd and HAProxy. Mitigation: the manual rows of section 7.
- Risk: the 003 home test expects one "Próximamente" card. Mitigation: it becomes the AC-36 test in the same change that enables the card.

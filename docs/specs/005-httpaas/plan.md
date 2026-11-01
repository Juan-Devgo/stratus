# Plan: HTTPaaS: web servers and DNS

## 1. Metadata
- Spec: `docs/specs/005-httpaas/spec.md`
- Updated: 2026-11-01
- Constitution version: 2.1.0

## 2. Constitution check
- P1: the backend validates input and the `APP_DNS_*` values, reads the web and DNS state from SQLite, reads IPs through `prepare/list-vms.sh`, picks the script, builds argv and maps exit codes. VM creation, site config, page copy and extraction, static IP, bind9 config, zone writes, resolver config, the IP-in-use probe and systemctl run only in `scripts/web/*.sh`, `scripts/dns/*.sh` and the 001 scripts.
- P2: 16 new scripts, one per operation (section 6). Each has `set -euo pipefail`, positional arguments, results on stdout, diagnostics on stderr and the exit-code table of section 6. The page archive arrives as a host temp file path argument. Refresh (`--if-changed`), remove record, set and unset resolver, and the unit actions are idempotent. The way to reach a server comes as `--jump <lb>` (004).
- P3: the same allowlists (name, memory, CPUs, type, CIDR, gateway, domain, forwarders, record spec, unit) are in `stratus-ui/src/features/{web,dns}/validation.ts`, `stratus-srv/internal/service/{web,dns}_validation.go` and `scripts/{web,dns}/lib/*-common.sh`. Constant remote `sh -c` programs get values as positional arguments. Zone, bind9 options and site files are rendered on the host with `printf` and sent over ssh stdin.
- P4: web and DNS servers get key access in the 001 Guest Additions bootstrap through `create-vm.sh`. Afterwards every step is SSH with the backend key; assigned web servers go through `jump` (`vm_exec` and the new `vm_scp` share `VM_ROUTES`).
- P5: no private key is created. 001 and 002 key flows do not change.
- P6: every operation returns `{message}` (plus `warning` or per-VM `results` where the spec asks) or `{error}`. `operations` rows with module `web` or `dns` keep command, argv, exit code, redacted stderr, result and timestamp. Reads are recorded only on failure.
- P7: home cards "Servidores web" (`/web`) and "DNS" (`/dns`), one screen each. Forms and dialogs confirm with `OperationResult`.
- P8: each script runs standalone with bats tests on PATH stubs. Manual rows of section 7 (AC-45 to AC-49 and the inspections) are the before/after demo on the real host.
- P9: no new Go or npm dependency. Reused: `create-vm.sh`, `delete-vm.sh`, `stop-vm.sh`, `start-vm.sh` logic, `vm-exec.sh`, `check_archive`, `saveArchive`, `ExecRunner`, `operationRunner.runLocked`, `VMLocks` (create lock included), history repository, `Redact`, `targetArgs`, `VmSelector`, `HistoryTable`, `useOperation`, `OperationResult`, `StateWarning`, `ServiceOutputPanel`, `createVmErrors`. New shared code: one bash library per module, one unit-action library used by both, two repositories.

## 3. Architecture
- State source: SQLite. `web_servers` holds name and type. `dns_server` holds the single DNS server name. `dns_records` holds VM and IP. `dns_resolvers` holds the VMs whose resolver the app set. The backend decides from SQLite whether a VM is a web server, the DNS server, an LB, an assigned server or a plain VM. Scripts never infer the role.
- Services and dependencies (no cycles):
  - `DNSService` (vms, lbs, dns repos): records, resolvers, DNS server create/delete/actions. Exposes hooks used by other services: `AddRecordsFor`, `RefreshVM`, `RefreshAll`, `AfterVMDeleted`.
  - `WebService` (vms, lbs, web repos, `*DNSService`).
  - `HAProxyService` gains `*DNSService` and `dns` repo (assign/remove hooks, DNS server not assignable, delete LB hook).
  - `PrepareService` gains `*WebService`, `*DNSService` and repos (delete delegation, start hooks, package `--role`, selector `kind`).
- Create web server (`create-web.sh`):
  1. Validate arguments.
  2. Base disk missing: exit 6 with "Disco base de Apache no encontrado" or "Disco base de Nginx no encontrado".
  3. Run `../prepare/create-vm.sh <vm> <mem> <cpus> <base_disk> <bridge>` (001 bootstrap, DHCP, SSH). From here on the trap is active.
  4. Over SSH write the site from `render_site <type> <fqdn>`: nginx `/etc/nginx/sites-available/stratus` linked in `sites-enabled` and `sites-enabled/default` removed; apache `/etc/apache2/sites-available/000-default.conf` replaced. Then `nginx -t` or `apache2ctl configtest`.
  5. `systemctl enable <unit>`, `systemctl reload-or-restart <unit>`, then `systemctl is-active <unit>` must be `active`.
  6. Print the message and `ip=<ip>`.
  The trap runs `../prepare/delete-vm.sh <vm>`. The base disk is never modified.
- Backend create web: `TryLockCreate`, `vms.Insert`, `runLocked(create_web, [vm])`, on success insert `web_servers`. After the locks are released, when a DNS server exists, `DNSService.AddRecordsFor(vm, ip)` runs as its own `add_records` operation; any error becomes the REQ-5 warning.
- Install page (`install-page.sh [--jump <lb>] <vm> <archive>`):
  1. Validate; `check_archive <archive> index.html "El archivo debe contener index.html en la raíz"` on the host (10 or 11) before any VM call.
  2. `vm_scp <vm> <archive> /var/tmp/stratus-page.tar.gz`.
  3. Remote: `rm -rf /var/www/.stratus-page-new /var/www/.stratus-page-bak`, `cp -a /var/www/html /var/www/.stratus-page-bak` (phase `backup`).
  4. Remote: `mkdir /var/www/.stratus-page-new`, `tar -xzf ... --no-same-owner -C .stratus-page-new`, `chown -R root:root`, `find -type d -exec chmod 755`, `find -type f -exec chmod 644`, then `rm -rf /var/www/html && mv /var/www/.stratus-page-new /var/www/html` (phase `swap`). No systemctl call.
  5. Remove backup, `.new` and the remote archive.
  The trap in phase `swap` restores: `rm -rf /var/www/html && mv .stratus-page-bak /var/www/html` (exit 5), or exit 9 when the restore fails. The backup and remote archive are removed on every exit. The backend deletes the host temp file in a `defer` (003 REQ-12).
- Unit actions (`scripts/prepare/lib/systemd-unit.sh`, `unit_action <vm> <unit> <action>`): `systemctl <start|stop|restart> <unit>`; after start or restart `sleep 3` and print `active_state=<state>` (003 pattern). `unit_status` prints `systemctl status --no-pager <unit>` and treats exit 3 (inactive or failed) as success. Web scripts pass `apache2` or `nginx`; bind scripts pass `named`.
- Create DNS server (`create-dns.sh`):
  1. Validate arguments (exit 2 "Configuración de DNS incompleta" for CIDR, gateway, domain or forwarders).
  2. Base disk missing: exit 6 "Disco base de DNS no encontrado".
  3. `ip_in_use <bridge> <ip>`: `arping -c 1 -w 1 -I <bridge> <ip>` and `ping -c 1 -W 1 <ip>`; any reply exits 13 "La IP del servidor DNS ya está en uso". `arping` missing exits 4 "arping no está instalado en el host".
  4. `../prepare/create-vm.sh <dns> <mem> <cpus> <base_disk> <bridge>` (bootstrap on DHCP). From here on the trap is active.
  5. Over SSH: find the adapter-1 interface by MAC; rewrite `/etc/network/interfaces` to `source /etc/network/interfaces.d/*` plus loopback; write `/etc/network/interfaces.d/stratus-static` (`auto <if>`, `iface <if> inet static`, `address <cidr>`, `gateway <gw>`); `/etc/resolv.conf` = `nameserver 127.0.0.1`.
  6. Write `/etc/bind/named.conf.options` (`render_options`) and `/etc/bind/named.conf.local` (zone `<domain>`, `type master`, `file "/var/lib/bind/db.<domain>"`), upload `render_zone` output to `/var/lib/bind/db.<domain>`, then `named-checkconf` and `named-checkzone`.
  7. `systemctl enable named`; `../prepare/stop-vm.sh <dns>`; delete guest IP properties; `startvm --type headless`.
  8. Wait up to 90 polls of 2 s for `vm_nic_ip <dns> 1` == static IP, then up to 8 SSH tries (`ConnectTimeout=10`); else exit 5 (trap).
  9. `dig @<ip> <dns>.<domain> A +short` over SSH on the VM must return the IP; print the message.
  The trap runs `../prepare/delete-vm.sh <dns>`.
- Backend create DNS: `ValidateDNSConfig` (else `ErrDNSConfigIncomplete`, no script), `dns_server` empty (else `ErrDNSExists`), `TryLockCreate`, `vms.Insert`. Initial records: web servers whose `list-vms.sh` IPv4 is set; the others are named in the result. On success insert `dns_server` and the initial `dns_records`.
- Zone writes (`scripts/dns/lib/dns-common.sh`):
  - `render_zone <dns> <domain> <dns_ip> [record...]` prints the whole zone: `$TTL 60`, SOA with serial `$(date +%s)` on its own line ending `; serial`, `@ NS <dns>.<domain>.`, `<dns> A <dns_ip>`, one `<vm> A <ip>` per record sorted by name.
  - `apply_zone <dns> <domain> [--if-changed]`, stdin = zone:
    1. With `--if-changed`, read the current file and compare both without the `; serial` line; equal prints `reloaded=no` and returns 0.
    2. Upload to `/var/lib/bind/db.<domain>.new`; `named-checkzone <domain> .new`. Failure: remove `.new`, check output on stderr, exit 10.
    3. `cp -p` current to `.bak`, `mv .new` over it, `chown bind:bind`, `rndc reload <domain>`.
    4. Reload failure: `mv .bak` back, `rndc reload <domain>`, exit 11. Else remove `.bak`, print `reloaded=yes`.
  - Record scripts reach the DNS server with `vm_exec`; a DNS server with no IPv4 exits 7, which `DNSService` maps to "El servidor DNS está apagado; enciéndalo para cambiar los registros".
- Records:
  - Backend add: DNS server exists (else `ErrNoDNSServer`), config valid, each selected VM registered, not the DNS server, without a record (else "La VM ya tiene un registro DNS"), with an IPv4 from `list-vms.sh` (else the 001 no-IP text). Accepted VMs go to one `add-records.sh` with the full new set; on success their rows are inserted. Result lists every VM.
  - Backend remove: no record succeeds without a script. Otherwise `remove-record.sh` with the remaining set; on success delete the row.
  - Refresh (`refresh-records.sh <dns> <domain> <dns_ip> <wait_vms|-> [record...]`):
    1. In one shared window of 60 polls of 2 s, background-poll the DNS server IP and `vm_nic_ip <vm> 1` of each listed VM that is `running`; then `wait`.
    2. A VM with an IPv4 gets it; any other keeps its stored IP.
    3. Render and `apply_zone --if-changed`.
    4. Print `<vm> <ip>` per record, then `reloaded=yes|no`.
  - `RefreshVM(vm)` passes `wait_vms=<vm>`; `RefreshAll` and the button pass every record VM. On success `UpdateRecordIPs` stores the printed IPs.
  - `AfterVMDeleted(vm)`: delete its `dns_resolvers` row; if it had a record, delete the row first, then run `remove-record.sh` with the remaining set. A failed script returns the REQ-24 warning; the next refresh renders from SQLite and drops the stale line.
- Resolver (`set-resolver.sh` / `unset-resolver.sh [--jump <lb>] <vm> <dns_ip>`), one remote program:
  - Detect the client: `command -v dhclient` → write a marked block `# stratus-resolver` + `prepend domain-name-servers <ip>;` into `/etc/dhcp/dhclient.conf`; else `command -v dhcpcd` → write `/usr/lib/dhcpcd/dhcpcd-hooks/99-stratus-resolver` that prepends `nameserver <ip>` to `/etc/resolv.conf` when missing; neither → exit 5 "No se encontró un cliente DHCP compatible".
  - Apply now: prepend `nameserver <ip>` to `/etc/resolv.conf` if it is not the first nameserver (tmp + mv).
  - Unset: remove the block or hook, then remove `nameserver <ip>` lines from `/etc/resolv.conf`. Missing pieces are not errors.
  - Backend: DNS server exists, VM registered and not the DNS server, `targetArgs` adds `--jump`; on success insert or delete the `dns_resolvers` row.
- Delete DNS server: `DNSService.DeleteDNS` from `/dns` and from `PrepareService.DeleteVM`. `dns_resolvers` non-empty → `ResolversSetError` listing VMs, no script. Else `prepare/delete-vm.sh` as `delete_dns` (module dns); on success delete `dns_records`, `dns_server` and the `vms` row in one transaction.
- Cross-module hooks (each runs after the main operation released its locks; failures become warnings and failed `dns` rows):
  - `PrepareService.StartVM`: DNS server → `RefreshAll`; VM with a record → `RefreshVM`. Warnings join the 004 warning with `\n`.
  - `PrepareService.DeleteVM`: web server → `WebService.DeleteWeb`; DNS server → `DNSService.DeleteDNS`; LB → `HAProxyService.DeleteLB`; plain VM → as before. Every successful path calls `AfterVMDeleted`. Return `ActionResult`.
  - `HAProxyService.AssignServer`/`RemoveServer`: after success, `RefreshVM(vm)` when it has a record. `AssignServer` rejects the DNS server with `ErrVMNotAssignable`. `DeleteLB` calls `AfterVMDeleted(lb)`.
  - Package scripts: `PrepareService` adds `--role web-apache|web-nginx|dns` for web servers and the DNS server, and rejects first in Go with `*PackageBlockedError` (REQ-7, REQ-18).
- Locks (REQ-34): web operations lock the web server; `--jump` operations keep the 004 one-VM lock. Add, remove, refresh and bind actions lock the DNS server. Resolver set and unset lock the target VM. Creation of web and DNS servers uses the shared create lock. Hooks run after the triggering operation unlocked, so they compete normally and a locked DNS server gives the warning.
- UI:
  - `/web`: `CreateWebForm`, `WebTable` (one row per web server: name, type, state, IPv4, FQDN, LB; buttons Iniciar, Detener, Reiniciar, Estado, Instalar página, Eliminar), `ServiceOutputPanel` for Estado, `HistoryTable`.
  - `/dns`: without a server `CreateDnsForm`; with one `DnsServerPanel` (name, state, IPv4, zone, unit buttons, "Actualizar registros", Eliminar), then `RecordTable` (checkbox per VM without record, "Agregar registros", "Quitar registro", `Switch` "Usar como resolvedor"), then `HistoryTable`.
  - `VmSelector` labels by `kind`. Start, delete (001), assign and remove (004) show `warning` in an `Alert`.
- State:
  - SQLite: `vms`, `load_balancers`, `lb_servers`, `web_servers`, `dns_server`, `dns_records`, `dns_resolvers`, `operations`.
  - VirtualBox: the VMs and adapters.
  - Guests: site config and `/var/www/html`; bind9 config, zone and static IP; resolver block or hook.
  - Backend memory: `VMLocks`.
  - UI: `Web.tsx` and `Dns.tsx` hold their lists, busy flag and history.

## 4. Design patterns
- Full rendering from SQLite in `render_zone` / `apply_zone`: the zone always equals the stored records (REQ-29); rollback is a `.bak` copy; `--if-changed` avoids needless reloads (as 004 `apply_cfg`).
- Post-operation hooks in `DNSService` (`RefreshVM`, `RefreshAll`, `AfterVMDeleted`, `AddRecordsFor`): one place turns a DNS failure into a warning plus a failed `dns` row for five triggers in three modules.
- Shared bash libraries: `scripts/web/lib/web-common.sh` (validators, `render_site`), `scripts/dns/lib/dns-common.sh` (validators, `render_zone`, `apply_zone`, `render_options`, `ip_in_use`, `wait_ips`), `scripts/prepare/lib/systemd-unit.sh` (`unit_action`, `unit_status`, used by 8 scripts of two modules).
- Routing map reuse: `vm_scp` uses the same `VM_ROUTES` ProxyCommand as `vm_exec`, so scp through `jump` has no extra code in `install-page.sh`.
- Role option `--role` in the package scripts: one function maps a role to its protected package, conflicting package, ufw ports and messages.
- Cleanup traps in `create-web.sh`, `create-dns.sh` and `install-page.sh`, as in 001 and 004.
- Repositories `WebRepository` and `DNSRepository`: SQL stays out of services; tests use a temp SQLite file.

## 5. Code structure
Scripts (headers, comments and output in Spanish):
- create: `scripts/prepare/lib/systemd-unit.sh` and `.bats` - `unit_action`, `unit_status`.
- create: `scripts/web/lib/web-common.sh` and `.bats` - `fail`, `validate_vm_name`, `validate_memory`, `validate_cpus`, `validate_type`, `validate_base_disk`, `validate_bridge`, `validate_domain`, `type_unit <type>`, `render_site <type> <fqdn>`, `parse_jump`.
- create: `scripts/web/create-web.sh`, `install-page.sh`, `start-web.sh`, `stop-web.sh`, `restart-web.sh`, `status-web.sh`, each with `.bats`.
- create: `scripts/dns/lib/dns-common.sh` and `.bats` - `fail`, validators of section 6, `ip_in_use`, `render_options`, `render_zone`, `apply_zone`, `wait_ips`.
- create: `scripts/dns/create-dns.sh`, `add-records.sh`, `remove-record.sh`, `refresh-records.sh`, `set-resolver.sh`, `unset-resolver.sh`, `start-bind.sh`, `stop-bind.sh`, `restart-bind.sh`, `status-bind.sh`, each with `.bats`.
- create: `scripts/web/test_helper.bash`, `scripts/dns/test_helper.bash` - load `../prepare/test_helper` and add the helpers of section 6.
- modify: `scripts/prepare/lib/vm-exec.sh` and `.bats` - `vm_scp <vm> <local> <remote>` (direct or routed).
- modify: `scripts/services/lib/service-common.sh` and `.bats` - `check_archive <file> <exe> [missing_msg]`; with the third argument a missing entry prints it (code 11 unchanged).
- modify: `scripts/prepare/install-package.sh`, `remove-package.sh` and `.bats` - optional `--role <web-apache|web-nginx|dns>` after `--frontend`; rules of section 6.
- modify: `scripts/prepare/test_helper.bash` - the ssh stub also logs `scp` calls (`stub_command scp`).

Backend:
- modify: `stratus-srv/internal/repository/db.go` - add the four tables of section 6.
- create: `stratus-srv/internal/repository/web_servers.go`, `dns.go` and their `_test.go`.
- modify: `stratus-srv/internal/service/errors.go` and `errors_test.go` - new sentinels, `PackageBlockedError`, `ResolversSetError`.
- create: `stratus-srv/internal/service/web_service.go` (`WebConfig`, `WebService`, `History`, exit-code constants, `mapWebError`), `web_validation.go`, `web_servers.go` (`ListWebServers`, `CreateWeb`, `DeleteWeb`), `web_page.go` (`InstallPage`), `web_actions.go` (`WebAction`, `WebStatus`), with `_test.go` for each.
- create: `stratus-srv/internal/service/dns_service.go` (`DNSConfig`, `DNSService`, `History`, `mapDNSError`), `dns_validation.go` (`ValidateDNSConfig`), `dns_server.go` (`Overview`, `CreateDNS`, `DeleteDNS`, `DNSAction`, `DNSStatus`), `dns_records.go` (`AddRecords`, `RemoveRecord`, `Refresh`, `AddRecordsFor`, `RefreshVM`, `RefreshAll`, `AfterVMDeleted`, `recordSpecs`, `parseRecordRefresh`), `dns_resolvers.go` (`SetResolver`, `UnsetResolver`), with `_test.go` for each.
- create: `stratus-srv/internal/service/vm_status.go` (moved from `prepare_vms.go`) - `queryVMStates` returns also the failed `RunResult` so `WebService` can record `list_web`.
- modify: `stratus-srv/internal/service/prepare_service.go`, `prepare_vms.go`, `prepare_packages.go` and tests - new constructor args, `VMView.Kind`, `ActionResult` (renamed from `StartResult`), delete delegation, start hooks, `--role`.
- modify: `stratus-srv/internal/service/haproxy_service.go`, `haproxy_servers.go`, `haproxy_lbs.go` and tests - `dns` and `dnsRepo` fields, DNS server not assignable, hooks, `ActionResult` returns.
- create: `stratus-srv/internal/handler/web.go`, `dns.go` and `_test.go` - `RegisterWeb`, `RegisterDNS`; `web.go` has `readArchive` (one multipart part `archive`).
- modify: `stratus-srv/internal/handler/prepare.go`, `haproxy.go`, `respond.go` and tests - `{message, warning?}` for delete, assign, remove and LB delete; new error mappings.
- modify: `stratus-srv/cmd/server/config.go`, `config_test.go` - optional `APP_APACHE_BASE_DISK`, `APP_NGINX_BASE_DISK`, `APP_DNS_BASE_DISK`, `APP_DNS_IP`, `APP_DNS_GATEWAY`, `APP_DNS_DOMAIN` (default `stratus.lan`), `APP_DNS_FORWARDERS`; the server starts without them.
- modify: `stratus-srv/cmd/server/main.go` - build `WebRepository`, `DNSRepository`, `DNSService`, `WebService`, then `HAProxyService` and `PrepareService` with them; register routes.
- modify: `stratus-srv/.env.example`, `docs/guide-environment.md`, `docs/guide-base-images.md` - the new variables, `arping` on the host (with `cap_net_raw`), and the Apache, Nginx and DNS base disk requirements.

Frontend:
- modify: `stratus-ui/src/features/prepare/types.ts` - `Vm.kind`.
- modify: `stratus-ui/src/features/prepare/VmSelector.tsx` - labels "Servidor web (Apache)", "Servidor web (Nginx)", "Servidor DNS".
- modify: `stratus-ui/src/features/prepare/api.ts`, `VmActions.tsx` - `deleteVm` returns `{message, warning?}`; warning `Alert` uses `whitespace-pre-line`.
- modify: `stratus-ui/src/features/haproxy/api.ts`, `AssignServerForm.tsx`, `ServerTable.tsx`, `LbActions.tsx` - show `warning`.
- modify: `stratus-ui/src/features/services/StateWarning.tsx` - optional `hint` prop (default unchanged text).
- create: `stratus-ui/src/features/web/{types.ts,api.ts,validation.ts,CreateWebForm.tsx,WebTable.tsx,InstallPageDialog.tsx,UnitButtons.tsx}`.
- create: `stratus-ui/src/features/dns/{types.ts,api.ts,CreateDnsForm.tsx,DnsServerPanel.tsx,RecordTable.tsx}`.
- create: `stratus-ui/src/routes/Web.tsx`, `stratus-ui/src/routes/Dns.tsx`.
- modify: `stratus-ui/src/App.tsx` - routes `/web`, `/dns`.
- modify: `stratus-ui/src/features/home/modules.ts` - cards `web` and `dns`.
- create and modify tests of section 7.

## 6. Interfaces and data
Environment (all optional at startup; checked when used):
- `APP_APACHE_BASE_DISK`, `APP_NGINX_BASE_DISK`, `APP_DNS_BASE_DISK`: absolute paths. Empty or missing file → the "Disco base de ... no encontrado" message (empty path: from the backend, no script).
- `APP_DNS_IP` (CIDR, prefix 8-30), `APP_DNS_GATEWAY` (IPv4 inside it, not the network or broadcast address), `APP_DNS_DOMAIN` (default `stratus.lan`), `APP_DNS_FORWARDERS` (1-3 comma-separated IPv4). Any invalid → "Configuración de DNS incompleta" on every DNS operation that needs it.
- Base disk requirements: the 001 requirements plus `apache2` (Apache), `nginx` (Nginx) or `bind9` and `bind9-dnsutils` (DNS), enabled at boot. Host: `arping` (iputils) with `cap_net_raw`.

SQLite (added to `schema`):
```sql
CREATE TABLE IF NOT EXISTS web_servers (
  name TEXT PRIMARY KEY,                 -- también es una fila de vms
  type TEXT NOT NULL CHECK (type IN ('apache','nginx'))
);
CREATE TABLE IF NOT EXISTS dns_server (
  id INTEGER PRIMARY KEY CHECK (id = 1), -- un solo servidor DNS
  name TEXT NOT NULL UNIQUE              -- también es una fila de vms
);
CREATE TABLE IF NOT EXISTS dns_records (
  vm TEXT PRIMARY KEY,                   -- un registro A por VM
  ip TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS dns_resolvers (
  vm TEXT PRIMARY KEY                    -- la app configuró el DNS como resolvedor
);
```
History:
- module `web`: `create_web`, `delete_web`, `install_page`, `start_web`, `stop_web`, `restart_web` always; `status_web` and `list_web` only on failure. VM column = the web server (`-` for `list_web`).
- module `dns`: `create_dns`, `delete_dns`, `add_records`, `remove_record`, `refresh_records`, `set_resolver`, `unset_resolver`, `start_bind`, `stop_bind`, `restart_bind` always; `status_bind` only on failure. For add, remove and refresh the VM column is the DNS server and the target VMs are in args.

Record spec (one argv item): `<vm>,<ipv4>`, for example `web-a,192.168.1.40`.

Scripts (argv; env `APP_SSH_KEY_PATH`, and `APP_GUEST_PASSWORD_FILE` for create):
| Script | Arguments | stdout |
|---|---|---|
| `web/create-web.sh` | `<vm> <memory_mb> <cpus> <apache\|nginx> <base_disk> <bridge_adapter> <domain>` | `Servidor web <vm> (<Apache\|Nginx>) creado; IP <ip>`, then `ip=<ip>` |
| `web/install-page.sh` | `[--jump <lb>] <vm> <archive_file>` | `Página instalada en <vm>` |
| `web/{start,restart}-web.sh` | `[--jump <lb>] <vm> <apache2\|nginx>` | message, then `active_state=<state>` |
| `web/stop-web.sh` | `[--jump <lb>] <vm> <apache2\|nginx>` | message |
| `web/status-web.sh` | `[--jump <lb>] <vm> <apache2\|nginx>` | `systemctl status` output |
| `dns/create-dns.sh` | `<dns> <memory_mb> <cpus> <base_disk> <bridge_adapter> <dns_cidr> <gateway> <domain> <forwarders> [record...]` | `Servidor DNS <dns> creado en <ip>; zona <domain>` |
| `dns/add-records.sh` | `<dns> <domain> <dns_ip> <record...>` (full set after the add) | `Registros aplicados`, `reloaded=yes` |
| `dns/remove-record.sh` | `<dns> <domain> <dns_ip> <vm> [record...]` (remaining set) | `Registro de <vm> quitado` |
| `dns/refresh-records.sh` | `<dns> <domain> <dns_ip> <wait_vms\|-> [record...]` (`wait_vms` = `a,b`) | `<vm> <ip>` per record, `reloaded=yes\|no` |
| `dns/set-resolver.sh`, `dns/unset-resolver.sh` | `[--jump <lb>] <vm> <dns_ip>` | message |
| `dns/{start,restart}-bind.sh`, `dns/stop-bind.sh`, `dns/status-bind.sh` | `<dns>` | as the web unit scripts, unit `named` |
| `prepare/{install,remove}-package.sh` | `[--jump <lb>] [--frontend <port>] [--role <role>] <vm> <package>` | unchanged |

Validation, step 1 (exit 2, no VBoxManage, ssh, scp, arping or ping call):
- `vm`, `dns`, `lb`: `^[a-z0-9][a-z0-9-]{0,31}$`. `memory_mb` 512-8192, `cpus` 1-4, `base_disk` absolute, `bridge_adapter` `^[A-Za-z0-9._:-]{1,64}$`.
- type `apache|nginx`; unit `apache2|nginx` (web) or fixed `named` (bind).
- `domain`: 1-4 labels `^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$` joined by dots. `dns_cidr`: IPv4 with prefix 8-30. `gateway`: IPv4 inside the subnet, not network or broadcast. `forwarders`: 1-3 comma-separated IPv4. `dns_ip`: IPv4. These print "Configuración de DNS incompleta" in `create-dns.sh`.
- record: `<vm>,<ipv4>`; at most 200; VM names unique; no record for `dns`.
- `wait_vms`: `-` or comma-separated VM names.
- `archive_file`: readable regular file.
- `--role`: `web-apache|web-nginx|dns`.

Role rules (`role_rules` in the package scripts; the backend rejects the same cases first):
| Role | Remove blocked | Install blocked | ufw extra |
|---|---|---|---|
| `web-apache` | `apache2`: "No se puede eliminar apache2: el servidor web lo usa" | `nginx`: "No se puede instalar nginx: el servidor web usa apache2" | `80/tcp` |
| `web-nginx` | `nginx`: "No se puede eliminar nginx: el servidor web lo usa" | `apache2`: "No se puede instalar apache2: el servidor web usa nginx" | `80/tcp` |
| `dns` | `bind9`: "No se puede eliminar bind9: el servidor DNS lo usa" | - | `53/tcp`, `53/udp` |
A blocked case exits 2 with the text.

Exit codes (same table in every `scripts/web` and `scripts/dns` header; each script lists the ones it uses):
| Code | Meaning |
|---|---|
| 0 | éxito |
| 2 | argumentos inválidos (también "Configuración de DNS incompleta") |
| 3 | VM no encontrada en VirtualBox |
| 4 | fallo de VBoxManage, de SSH/scp, o arping no instalado |
| 5 | falló un paso (la limpieza o la restauración terminó bien) |
| 6 | disco base no encontrado, interfaz de puente no encontrada o VM ya registrada |
| 7 | VM sin dirección IP (en los scripts de registros: servidor DNS apagado) |
| 9 | falló la operación y también la limpieza o la restauración |
| 10 | archivo comprimido inválido (install-page) / zona DNS inválida, se mantuvo la anterior (dns) |
| 11 | el archivo no tiene index.html en la raíz (install-page) / no se pudo recargar bind9, se restauró la zona (dns) |
| 12 | el balanceador del servidor está apagado o sin IPv4 en puente |
| 13 | la IP del servidor DNS ya está en uso |
| 99 | operación no implementada |

Rendered files:
```
# nginx /etc/nginx/sites-available/stratus
server {
  listen 80 default_server;
  listen [::]:80 default_server;
  server_name <vm>.<domain>;
  root /var/www/html;
  index index.html index.nginx-debian.html;
  location / { try_files $uri $uri/ =404; }
}
# apache /etc/apache2/sites-available/000-default.conf
<VirtualHost *:80>
  ServerName <vm>.<domain>
  DocumentRoot /var/www/html
</VirtualHost>
# /etc/bind/named.conf.options
acl stratus_clients { <lan subnet>; 10.10.0.0/16; localhost; };
options {
  directory "/var/cache/bind";
  recursion yes;
  allow-query { stratus_clients; };
  allow-recursion { stratus_clients; };
  forwarders { <f1>; <f2>; };
  forward only;
  dnssec-validation auto;
  listen-on { any; };
  listen-on-v6 { none; };
};
# /var/lib/bind/db.<domain>
$TTL 60
@ IN SOA <dns>.<domain>. hostmaster.<domain>. (
  <epoch> ; serial
  3600 600 86400 60 )
@ IN NS <dns>.<domain>.
<dns> IN A <dns_ip>
<vm> IN A <ip>          ; one per record, sorted
```

`vm-exec.sh`:
```
vm_scp <vm> <local> <remote>   # scp -i <key> -o BatchMode=yes -o ConnectTimeout=10 -o StrictHostKeyChecking=accept-new
                               #   -o UserKnownHostsFile=<key>.known_hosts -o HostKeyAlias=<vm> [-o ProxyCommand=... when routed]
                               #   <local> root@<ip>:<remote>; 7 and 12 as vm_exec
```

bats helpers:
```
# scripts/web/test_helper.bash
load ../prepare/test_helper           # assert_script_structure, setup_stubs, stub_command, stub_vm_ip, stub_vm_nics
setup_web_stubs                       # setup_stubs + logging stubs for ../prepare/{create-vm,delete-vm}.sh (copied dir);
                                      # ssh and scp stubs write calls.log, remote.log, stdin/<n>; env FAIL_REMOTE=<ERE>,
                                      # FAIL_RESTORE=<ERE>, CONFIGTEST=ok|fail, ACTIVE_STATE=<state>
make_archive <name> <entry...>        # builds a .tar.gz in $BATS_TEST_TMPDIR
# scripts/dns/test_helper.bash
setup_dns_stubs                       # as setup_web_stubs plus stop-vm.sh; arping and ping stubs: ARPING=reply|none|missing,
                                      # PING=reply|none; env ZONE_CHECK=ok|fail, RNDC=ok|fail, CURRENT_ZONE=<text>,
                                      # DHCP_CLIENT=dhclient|dhcpcd|none
stub_ip_sequence <vm> <ip|->...       # successive IP answers (polls), from 004
```

Go (package `repository`):
```go
type WebServer struct { Name, Type string }            // type apache|nginx
func NewWebRepository(db *sql.DB) *WebRepository
func (r *WebRepository) Insert(ctx context.Context, w WebServer) error
func (r *WebRepository) Get(ctx context.Context, name string) (WebServer, bool, error)
func (r *WebRepository) List(ctx context.Context) ([]WebServer, error)
func (r *WebRepository) Delete(ctx context.Context, name string) error

type DNSRecord struct { VM, IP string }
func NewDNSRepository(db *sql.DB) *DNSRepository
func (r *DNSRepository) Server(ctx context.Context) (name string, ok bool, err error)
func (r *DNSRepository) InsertServer(ctx context.Context, name string, initial []DNSRecord) error // one transaction
func (r *DNSRepository) DeleteServer(ctx context.Context) error   // dns_server, dns_records and the vms row; one transaction
func (r *DNSRepository) Records(ctx context.Context) ([]DNSRecord, error) // ORDER BY vm
func (r *DNSRepository) Record(ctx context.Context, vm string) (DNSRecord, bool, error)
func (r *DNSRepository) InsertRecords(ctx context.Context, recs []DNSRecord) error
func (r *DNSRepository) DeleteRecord(ctx context.Context, vm string) error
func (r *DNSRepository) UpdateRecordIPs(ctx context.Context, recs []DNSRecord) error
func (r *DNSRepository) Resolvers(ctx context.Context) ([]string, error)
func (r *DNSRepository) SetResolver(ctx context.Context, vm string, on bool) error
```

Go (package `service`):
```go
var (
  ErrWebBaseDiskMissing  // built per type: "Disco base de Apache no encontrado" / "Disco base de Nginx no encontrado"
  ErrDNSBaseDiskMissing  = errors.New("Disco base de DNS no encontrado")
  ErrDNSExists           = errors.New("Ya existe un servidor DNS")
  ErrNoDNSServer         = errors.New("No hay servidor DNS")
  ErrDNSConfigIncomplete = errors.New("Configuración de DNS incompleta")
  ErrDNSIPInUse          = errors.New("La IP del servidor DNS ya está en uso")
  ErrDNSOff              = errors.New("El servidor DNS está apagado; enciéndalo para cambiar los registros")
  ErrZoneInvalid         = errors.New("Zona DNS inválida; se mantuvo la anterior")
  ErrRecordExists        = errors.New("La VM ya tiene un registro DNS")
  ErrIndexMissing        = errors.New("El archivo debe contener index.html en la raíz")
  ErrPackageBlocked      = errors.New("paquete protegido")
)
type PackageBlockedError struct { Message string }   // Unwrap() = ErrPackageBlocked; 409
type ResolversSetError struct { VMs []string }       // "Quite primero el resolvedor DNS de las VMs: vm1, vm2"; 409
func ValidateWebType(t string) error                 // "Seleccione el tipo de servidor web"
func ValidateDNSConfig(c DNSConfig) error            // ErrDNSConfigIncomplete
type ActionResult struct { Message string `json:"message"`; Warning string `json:"warning,omitempty"` } // renamed StartResult

type WebConfig struct { ApacheBaseDisk, NginxBaseDisk, BridgeAdapter, Domain string; MaxArchiveBytes int64 }
func NewWebService(cfg WebConfig, runner ScriptRunner, vms *repository.VMRepository, lbs *repository.LBRepository,
  web *repository.WebRepository, dnsRepo *repository.DNSRepository, history *repository.HistoryRepository,
  locks *VMLocks, dns *DNSService, logger *log.Logger) *WebService
type WebServerView struct { Name, Type, State, IP, FQDN, LB string } // json snake_case; FQDN/LB "" shown as "-"
func (s *WebService) ListWebServers(ctx context.Context) ([]WebServerView, string, error) // list_web row on failure
func (s *WebService) CreateWeb(ctx context.Context, in CreateWebInput) (ActionResult, error)
  // ValidateCreateVM + ValidateWebType; empty disk -> ErrWebBaseDiskMissing; TryLockCreate; vms.Insert;
  // runLocked(create_web,[vm]); ok -> web.Insert; fail -> vms.Delete; then dns.AddRecordsFor when a DNS server exists
  // -> Warning "Servidor web creado; no se pudo registrar en DNS, agréguelo desde DNS"
func (s *WebService) DeleteWeb(ctx context.Context, vm string) (ActionResult, error) // ErrVMIsServer; prepare/delete-vm.sh as delete_web; dns.AfterVMDeleted
func (s *WebService) InstallPage(ctx context.Context, vm string, a ArchiveUpload) (string, error) // saveArchive; defer os.Remove; targetArgs
func (s *WebService) WebAction(ctx context.Context, vm, action string) (ServiceResult, error)    // start|stop|restart; targetArgs + unit
func (s *WebService) WebStatus(ctx context.Context, vm string) (string, error)                    // recordSuccess false
func (s *WebService) History(ctx context.Context) ([]repository.Operation, error)                // module "web", 50
func (s *WebService) MaxArchiveBytes() int64

type DNSConfig struct { BaseDisk, BridgeAdapter, IP /* CIDR */, Gateway, Domain, Forwarders string }
func NewDNSService(cfg DNSConfig, runner ScriptRunner, vms *repository.VMRepository, lbs *repository.LBRepository,
  web *repository.WebRepository, dnsRepo *repository.DNSRepository, history *repository.HistoryRepository,
  locks *VMLocks, logger *log.Logger) *DNSService
type DNSOverview struct {
  Server *DNSServerView `json:"server"`  // nil when none: {name,state,ip,zone}
  VMs    []DNSVMView    `json:"vms"`     // every VM except the DNS server: {name,kind,role,state,ip,fqdn,record_ip,resolver}
  ListError string      `json:"list_error"`
}
type RecordResult struct { VM string `json:"vm"`; OK bool `json:"ok"`; Message string `json:"message"` }
func (s *DNSService) Overview(ctx context.Context) (DNSOverview, error)
func (s *DNSService) CreateDNS(ctx context.Context, in CreateVMInput) (string, error) // message names skipped web servers
func (s *DNSService) DeleteDNS(ctx context.Context) (string, error)                    // ResolversSetError; delete_dns
func (s *DNSService) DNSAction(ctx context.Context, action string) (ServiceResult, error)
func (s *DNSService) DNSStatus(ctx context.Context) (string, error)
func (s *DNSService) AddRecords(ctx context.Context, vms []string) (string, []RecordResult, error) // error only when none added
func (s *DNSService) RemoveRecord(ctx context.Context, vm string) (string, error)
func (s *DNSService) Refresh(ctx context.Context) (string, error)                       // all record VMs; reloaded=no -> "Registros sin cambios"
func (s *DNSService) SetResolver(ctx context.Context, vm string) (string, error)
func (s *DNSService) UnsetResolver(ctx context.Context, vm string) (string, error)
func (s *DNSService) AddRecordsFor(ctx context.Context, vm, ip string) error            // hook: no-op without DNS server
func (s *DNSService) RefreshVM(ctx context.Context, vm string) error                    // hook: no-op without record
func (s *DNSService) RefreshAll(ctx context.Context) error
func (s *DNSService) AfterVMDeleted(ctx context.Context, vm string) (warning string)
func (s *DNSService) IsDNSServer(ctx context.Context, vm string) (bool, error)
func (s *DNSService) History(ctx context.Context) ([]repository.Operation, error)       // module "dns", 50
func recordSpecs(recs []repository.DNSRecord) []string
func mapDNSError(err error) error // ErrVMNoIP -> ErrDNSOff (record scripts only); exit 10 -> ErrZoneInvalid; 13 -> ErrDNSIPInUse

type VMView struct { ...; Kind string `json:"kind"` /* "", apache, nginx, dns */ }
func (s *PrepareService) StartVM(ctx context.Context, name string) (ActionResult, error)
  // DNS warning: "Operación completada; no se pudo actualizar el registro DNS de <vm>, use Actualizar registros"
func (s *PrepareService) DeleteVM(ctx context.Context, name string) (ActionResult, error)
  // warning "VM eliminada; no se pudo quitar su registro DNS, use Actualizar registros"
func (s *HAProxyService) AssignServer(ctx context.Context, lb, vm string, port int) (ActionResult, error)
func (s *HAProxyService) RemoveServer(ctx context.Context, lb, vm string) (ActionResult, error)
func (s *HAProxyService) DeleteLB(ctx context.Context, lb string) (ActionResult, error)
```

Go (package `handler`): `RegisterWeb(mux, svc)`, `RegisterDNS(mux, svc)`. Status codes:
- `ErrDNSExists`, `ErrNoDNSServer`, `ErrDNSOff`, `ErrRecordExists`, `*ResolversSetError`, `*PackageBlockedError`, `ErrDNSIPInUse`: 409.
- `ErrDNSConfigIncomplete`, `ErrIndexMissing`, `ErrWebBaseDiskMissing`, `ErrDNSBaseDiskMissing`: 400 or 409 as the 001 disk messages (409).
- `ErrZoneInvalid`: 500 with its text.

HTTP API (JSON `snake_case`):
| Method and path | Body | Success |
|---|---|---|
| `GET /api/web/servers` | - | `{"servers":[{name,type,state,ip,fqdn,lb}],"list_error":""}` |
| `POST /api/web/servers` | `{name,memory_mb,cpus,type}` | 201 `{message, warning?}` |
| `DELETE /api/web/servers/{vm}` | - | 200 `{message, warning?}` |
| `POST /api/web/servers/{vm}/page` | multipart part `archive` | 200 `{message}` |
| `POST /api/web/servers/{vm}/{start\|stop\|restart}` | - | 200 `{message, active_state?}` |
| `GET /api/web/servers/{vm}/status` | - | 200 `{output}` |
| `GET /api/web/history` | - | `{"operations":[...]}` |
| `GET /api/dns` | - | `DNSOverview` |
| `POST /api/dns/server` | `{name,memory_mb,cpus}` | 201 `{message}` |
| `DELETE /api/dns/server` | - | 200 `{message}` |
| `POST /api/dns/server/{start\|stop\|restart}` | - | 200 `{message, active_state?}` |
| `GET /api/dns/server/status` | - | 200 `{output}` |
| `POST /api/dns/records` | `{vms:[...]}` | 200 `{message, results:[{vm,ok,message}]}`; none added → 409 `{error}` |
| `DELETE /api/dns/records/{vm}` | - | 200 `{message}` |
| `POST /api/dns/records/refresh` | - | 200 `{message}` |
| `PUT /api/dns/resolvers/{vm}` / `DELETE /api/dns/resolvers/{vm}` | - | 200 `{message}` |
| `GET /api/dns/history` | - | `{"operations":[...]}` |
| `DELETE /api/prepare/vms/{name}` (changed) | - | 200 `{message, warning?}` |
| `POST /api/haproxy/lbs/{lb}/servers`, `DELETE .../servers/{vm}`, `DELETE /api/haproxy/lbs/{lb}` (changed) | as before | `{message, warning?}` |
| `GET /api/prepare/vms` (changed) | - | each VM adds `kind` |

Frontend:
```ts
// features/prepare/types.ts
type VmKind = "" | "apache" | "nginx" | "dns";
interface Vm { ...; kind: VmKind }
// features/web/types.ts
type WebType = "apache" | "nginx";
interface WebServer { name: string; type: WebType; state: VmState; ip: string; fqdn: string; lb: string }
// features/web/api.ts
listWebServers(); createWeb(input); deleteWeb(vm); installPage(vm, file); webAction(vm, action); webStatus(vm); getWebHistory()
// features/web/validation.ts
createWebErrors(input): Record<string,string>  // createVmErrors + type required
validatePageFile(file): string | null          // ".tar.gz" and <= 200 MB, 003 texts
// features/dns/types.ts
interface DnsServer { name: string; state: VmState; ip: string; zone: string }
interface DnsVm { name; kind; role; state; ip; fqdn; record_ip; resolver: boolean }
// features/dns/api.ts
getDnsOverview(); createDns(input); deleteDns(); dnsAction(action); dnsStatus(); addRecords(vms);
removeRecord(vm); refreshRecords(); setResolver(vm, on); getDnsHistory()
// components
CreateWebForm({ busy; onDone })
WebTable({ servers; busy; onDone })               // per row: UnitButtons, Instalar página, Eliminar (confirmation names the VM)
InstallPageDialog({ vm; onDone })                 // file input + confirmation "Se reemplazará todo /var/www/html"
UnitButtons({ onAction(action); onStatus() })     // Iniciar, Detener, Reiniciar, Estado; StateWarning hint "revise Estado"
CreateDnsForm({ busy; onDone })
DnsServerPanel({ server; busy; onDone })          // UnitButtons, Actualizar registros, Eliminar
RecordTable({ vms; busy; onDone })                // checkboxes, Agregar registros, Quitar registro, resolver Switch
```
`Web.tsx` and `Dns.tsx` load their list, `listVms` (DNS labels) and history on mount and after each operation; one `useOperation` disables every button.

## 7. Test-first
- Framework and commands: Go `testing` (table-driven) with `cd stratus-srv && go test ./...`; Vitest + Testing Library with `cd stratus-ui && pnpm test`; bats with `bats -r scripts/`.
- Location and naming: tests sit next to the code. Go `TestAC<n>_<Behavior>`, Vitest `it("AC-<n> ...")`, bats `@test "AC-<n> ..."`. Go tests use `fakeRunner` and a temp SQLite file (`newWebEnv` in `web_service_test.go`, `newDNSEnv` in `dns_service_test.go`). UI tests mock `fetch` and `sonner`. Level "manual" rows are steps of the real-host demo (P8).

| Test | Level | File | Covers (US / REQ / AC) | Given - when - then |
|---|---|---|---|---|
| AC-1 create-web llama a create-vm con el disco de Nginx y escribe el sitio | unit | `scripts/web/create-web.bats` | US-1 / REQ-1, REQ-2, REQ-4 / AC-1 | given `web-a 1024 1 nginx <disk> eth0 stratus.lan` - then calls.log has `create-vm.sh web-a 1024 1 <disk> eth0`, the site stdin has `server_name web-a.stratus.lan;`, then `nginx -t`, `enable nginx`; stdout ends `ip=<ip>` |
| TestAC1_CreateWebPassesDiscreteArgs | unit | `stratus-srv/internal/service/web_servers_test.go` | US-1 / REQ-1, REQ-2 / AC-1 | given valid input type nginx - then script `web/create-web.sh`, argv `[web-a 1024 1 nginx <nginx disk> eth0 stratus.lan]`, rows in vms and web_servers |
| TestAC1_CreateWebRoute | unit | `stratus-srv/internal/handler/web_test.go` | US-1 / REQ-1 / AC-1 | when POST `/api/web/servers` - then 201 `{message}` |
| AC-1 submits name, memory, CPUs and type | unit | `stratus-ui/src/features/web/CreateWebForm.test.tsx` | US-1 / REQ-1, REQ-40 / AC-1 | given valid fields and Nginx - then POST body `{name,memory_mb,cpus,type:"nginx"}` and the result shows |
| AC-2 invalid fields or no type send nothing | unit | `stratus-ui/src/features/web/CreateWebForm.test.tsx` | US-1 / REQ-1 / AC-2 | given `Web_1`, 256, 5 and no type - then the 001 errors plus "Seleccione el tipo de servidor web", no fetch |
| TestAC2_CreateWebRejectsInvalid | unit | `stratus-srv/internal/service/web_servers_test.go` | US-1 / REQ-1 / AC-2 | given invalid fields, duplicate name, type `iis` or empty - then ErrInvalidInput / ErrDuplicateVM, no runner call |
| AC-3 create-web sale con 6 sin disco base de Apache | unit | `scripts/web/create-web.bats` | US-1 / REQ-2, REQ-3 / AC-3 | given type apache and a missing disk - then exit 6, "Disco base de Apache no encontrado", no create-vm call |
| TestAC3_CreateWebMissingDiskRegistersNothing | unit | `stratus-srv/internal/service/web_servers_test.go` | US-1 / REQ-3 / AC-3 | given exit 6 - then the error text, no vms or web_servers row; given empty `ApacheBaseDisk` - then ErrWebBaseDiskMissing and no runner call |
| AC-4 create-web deja el servicio activo y habilitado | unit | `scripts/web/create-web.bats` | US-1 / REQ-4 / AC-4 | given apache - then `000-default.conf` stdin has `ServerName web-a.stratus.lan` and `DocumentRoot /var/www/html`, `apache2ctl configtest`, `enable apache2`, `is-active apache2` |
| AC-4 create-web limpia si falla la configuración | unit | `scripts/web/create-web.bats` | US-1 / REQ-3 / AC-4 | given CONFIGTEST=fail - then exit 5 and `delete-vm.sh web-a` |
| AC-4 inspect bootstrap and default page | manual | real host demo | US-1 / REQ-3, REQ-4 / AC-4 | `systemctl is-enabled/is-active`, `curl -H 'Host: anything' http://<ip>/` 200 |
| TestAC5_CreateWebAddsRecord | unit | `stratus-srv/internal/service/web_servers_test.go` | US-1, US-5 / REQ-5, REQ-22 / AC-5 | given a DNS server and stdout `ip=192.168.1.40` - then a second call `dns/add-records.sh` with `web-a,192.168.1.40`, a dns_records row, no warning |
| AC-5 record resolves | manual | real host demo | US-1, US-5 / REQ-5 / AC-5 | `dig @<APP_DNS_IP> web-a.stratus.lan A` |
| TestAC6_CreateWebDNSFailureWarns | unit | `stratus-srv/internal/service/web_servers_test.go` | US-1, US-5 / REQ-5 / AC-6 | given add-records exit 7 - then ActionResult with the REQ-5 warning, a failed `add_records` row in module dns; without DNS server - then one script call and no warning |
| AC-6 create shows the warning | unit | `stratus-ui/src/features/web/CreateWebForm.test.tsx` | US-1 / REQ-5 / AC-6 | given `{message, warning}` - then the success and the warning Alert |
| TestAC7_CreateWebSharesCreateLock | integration | `stratus-srv/internal/service/web_service_test.go` | US-1 / REQ-3 / AC-7 | given a create_vm blocked - when CreateWeb runs - then ErrCreateInProgress, no script call |
| TestAC8_ListWebServersOnly | unit | `stratus-srv/internal/service/web_servers_test.go` | US-1, US-8 / REQ-6 / AC-8 | given web-a nginx (record), web-b apache assigned to lb1, vm1 - then two rows with type, state, ip, fqdn `web-a.stratus.lan` / "", lb "" / `lb1` |
| TestAC8_ListWebRecordsFailedRead | unit | `stratus-srv/internal/service/web_servers_test.go` | US-9 / REQ-35 / AC-8 | given list-vms exit 4 - then list_error and one `list_web` error row; given ok - then no row |
| TestAC8_ListVMsKind | unit | `stratus-srv/internal/service/prepare_vms_test.go` | US-8 / REQ-6, REQ-17 / AC-8 | given web-a nginx, web-b apache, dns1 - then kind `nginx`, `apache`, `dns`, vm1 `""` |
| AC-8 selector labels | unit | `stratus-ui/src/features/prepare/VmSelector.test.tsx` | US-8 / REQ-6, REQ-17 / AC-8 | given kinds - then options end with "Servidor web (Nginx)", "Servidor web (Apache)", "Servidor DNS" |
| AC-8 web table lists the servers | unit | `stratus-ui/src/features/web/WebTable.test.tsx` | US-1 / REQ-6 / AC-8 | given two servers - then rows with Nombre, Tipo, Estado, IP, FQDN, Balanceador ("-" when empty) |
| AC-9 role web-nginx bloquea quitar nginx e instalar apache2 | unit | `scripts/prepare/remove-package.bats`, `scripts/prepare/install-package.bats` | US-8 / REQ-7 / AC-9 | given `--role web-nginx web-a nginx` remove - then exit 2 and the text, no ssh; install apache2 - exit 2 and the text; install ufw - `ufw allow 22/tcp` and `ufw allow 80/tcp` |
| TestAC9_PackageRulesOnWebServer | unit | `stratus-srv/internal/service/prepare_packages_test.go` | US-8 / REQ-7 / AC-9 | given web-a nginx - RemovePackage nginx and InstallPackage apache2 give PackageBlockedError with the texts and no runner call; InstallPackage ufw argv `[--role web-nginx web-a ufw]` |
| TestAC10_DeleteWebRemovesRecord | unit | `stratus-srv/internal/service/web_servers_test.go` | US-1 / REQ-8, REQ-30 / AC-10 | given web-a with a record - then `prepare/delete-vm.sh [web-a]` as `delete_web`, then `dns/remove-record.sh`, rows removed; given web-a assigned - ErrVMIsServer and no runner call |
| AC-10 delete confirms with the name | unit | `stratus-ui/src/features/web/WebTable.test.tsx` | US-1 / REQ-8 / AC-10 | when Eliminar - then dialog names web-a, no fetch until Confirmar, 409 text shows |
| AC-10 VM and disk removed, base unchanged | manual | real host demo | US-1 / REQ-8 / AC-10 | `list vms`, `showmediuminfo` of the base disk |
| AC-11 install-page reemplaza el contenido con permisos | unit | `scripts/web/install-page.bats` | US-2 / REQ-9, REQ-10 / AC-11 | given an archive with `index.html` and `css/site.css` - then `scp ... root@<ip>:/var/tmp/stratus-page.tar.gz`, remote.log has `cp -a /var/www/html /var/www/.stratus-page-bak`, `tar -xzf`, `chown -R root:root`, `chmod 755`, `chmod 644`, `mv` into `/var/www/html`, no `systemctl` |
| TestAC11_InstallPageArgsAndCleanup | unit | `stratus-srv/internal/service/web_page_test.go` | US-2 / REQ-9 / AC-11, AC-17 | given an upload - then argv `[web-a <tmp>]`, `install_page` row ok, and `<tmp>` no longer exists after return (success and failure) |
| AC-11 page installed, PID unchanged | manual | real host demo | US-2 / REQ-10 / AC-11 | `stat`, `curl`, `systemctl show -p MainPID` before and after |
| AC-11 install confirms the replacement | unit | `stratus-ui/src/features/web/InstallPageDialog.test.tsx` | US-2 / REQ-10 / AC-11 | given a file - then the dialog warns that /var/www/html is replaced; multipart POST only after Confirmar |
| AC-12 install-page sale con 11 sin index.html en la raíz | unit | `scripts/web/install-page.bats` | US-2 / REQ-9 / AC-12 | given only `site/index.html` - then exit 11, "El archivo debe contener index.html en la raíz", no scp or ssh |
| AC-12 check_archive con mensaje propio | unit | `scripts/services/lib/service-common.bats` | US-2 / REQ-9 / AC-12 | given a third argument - then the missing entry prints it with 11; without it the 003 text |
| TestAC12_InstallPageMapsIndexMissing | unit | `stratus-srv/internal/service/web_page_test.go` | US-2 / REQ-9 / AC-12 | given exit 11 - then ErrIndexMissing |
| AC-13 install-page rechaza archivos inválidos | unit | `scripts/web/install-page.bats` | US-2 / REQ-9 / AC-13 | given `../evil`, `/abs`, a symlink or a corrupt gzip - then exit 10 "Archivo comprimido inválido", no scp |
| TestAC13_InstallPageSizeAndExtension | unit | `stratus-srv/internal/service/web_page_test.go` | US-2 / REQ-9 / AC-13 | given `site.zip` or 201 MB - then the 003 ValidationError texts and no runner call |
| AC-13 file checks before sending | unit | `stratus-ui/src/features/web/validation.test.ts` | US-2 / REQ-9 / AC-13 | given `site.zip` or a 201 MB file - then the 003 texts |
| AC-14 install-page restaura si falla la extracción | unit | `scripts/web/install-page.bats` | US-2 / REQ-10 / AC-14 | given FAIL_REMOTE on `tar -xzf` - then exit 5, `mv /var/www/.stratus-page-bak /var/www/html`, `rm -rf` of the backup and the remote archive; given FAIL_RESTORE too - exit 9 |
| AC-15 segunda instalación deja solo el segundo archivo | unit | `scripts/web/install-page.bats` | US-2 / REQ-10 / AC-15 | then the swap is `rm -rf /var/www/html && mv /var/www/.stratus-page-new /var/www/html` (no merge) |
| AC-15 only second archive remains | manual | real host demo | US-2 / REQ-10 / AC-15 | `ls -R /var/www/html` after two installs |
| AC-16 install-page con --jump usa scp por el balanceador | unit | `scripts/web/install-page.bats` | US-2, US-8 / REQ-11 / AC-16 | given `--jump lb1 web-a` - then the scp call has `ProxyCommand` with `jump@<lb ip>`; given lb1 off - exit 12, the 004 text, no scp |
| AC-16 vm_scp directo y por ruta | unit | `scripts/prepare/lib/vm-exec.bats` | US-2 / REQ-11 / AC-16 | given no route - direct scp with HostKeyAlias; given a route - ProxyCommand; given no IP - 7 |
| TestAC16_InstallPageOnServerPassesJump | unit | `stratus-srv/internal/service/web_page_test.go` | US-2 / REQ-11 / AC-16 | given web-a assigned to lb1 - then argv starts `--jump lb1 web-a`; exit 12 gives *LBOffError |
| AC-18 unit_action start, stop y restart | unit | `scripts/prepare/lib/systemd-unit.bats` | US-3 / REQ-12 / AC-18 | given start - `systemctl start nginx`, `sleep 3`, `active_state=active`; stop - no sleep; restart - `systemctl restart` |
| AC-18 start-web/stop-web/restart-web pasan la unidad | unit | `scripts/web/start-web.bats`, `stop-web.bats`, `restart-web.bats` | US-3 / REQ-12 / AC-18 | given `web-a nginx` - then remote `systemctl <action> nginx`; unit `sshd` exits 2 |
| TestAC18_WebActionUsesTypeUnit | unit | `stratus-srv/internal/service/web_actions_test.go` | US-3 / REQ-12 / AC-18 | given web-a nginx - start argv `[web-a nginx]` script `web/start-web.sh`, row `start_web`; apache gives `apache2`; action `enable` is ErrInvalidInput |
| AC-18 buttons call the actions | unit | `stratus-ui/src/features/web/UnitButtons.test.tsx` | US-3 / REQ-12 / AC-18 | when Iniciar, Detener, Reiniciar - then POST `.../start`, `/stop`, `/restart` |
| AC-18 stop, start and restart on the real VM | manual | real host demo | US-3 / REQ-12 / AC-18 | `systemctl is-active`, `curl`, MainPID |
| AC-19 start-bind y stop-bind actúan sobre named | unit | `scripts/dns/start-bind.bats`, `stop-bind.bats`, `restart-bind.bats` | US-3 / REQ-12 / AC-19 | given `dns1` - then `systemctl <action> named` |
| TestAC19_DNSActionLocksDNSServer | unit | `stratus-srv/internal/service/dns_server_test.go` | US-3 / REQ-12 / AC-19 | then script `dns/stop-bind.sh [dns1]`, row `stop_bind`; without DNS server - ErrNoDNSServer |
| AC-19 dig times out after Detener | manual | real host demo | US-3 / REQ-12 / AC-19 | `dig @<ip>` before and after |
| AC-20 status-web devuelve la salida aunque el servicio falló | unit | `scripts/web/status-web.bats`, `scripts/dns/status-bind.bats` | US-3 / REQ-12 / AC-20 | given systemctl status exit 3 with `failed` - then exit 0 and the output on stdout |
| TestAC20_WebStatusRecordsOnlyFailures | unit | `stratus-srv/internal/service/web_actions_test.go` | US-3, US-9 / REQ-12, REQ-35 / AC-20 | given ok - output and no row; given exit 4 - error and a `status_web` row |
| AC-20 Estado opens the panel | unit | `stratus-ui/src/features/web/UnitButtons.test.tsx` | US-3 / REQ-12 / AC-20 | when Estado - then the panel shows the output with `failed` and no error Alert |
| AC-21 start shows the state warning | unit | `stratus-ui/src/routes/Web.test.tsx` | US-3 / REQ-12 / AC-21 | given `{message, active_state:"failed"}` - then success plus "quedó en estado failed; revise Estado" |
| AC-22 create-dns configura IP fija, bind9 y reinicia | unit | `scripts/dns/create-dns.bats` | US-4 / REQ-13, REQ-14, REQ-16 / AC-22 | given valid args, ARPING=none, PING=none - then in order: `create-vm.sh dns1 ...`, `stratus-static` stdin with `address 192.168.1.53/24` and `gateway 192.168.1.1`, `named.conf.options` and zone uploads, `named-checkconf`, `named-checkzone`, `enable named`, `stop-vm.sh dns1`, `startvm`; the wait accepts IP 192.168.1.53 |
| AC-22 create-dns limpia si no responde en la IP fija | unit | `scripts/dns/create-dns.bats` | US-4 / REQ-14 / AC-22 | given the IP never equals the static one - then exit 5 after 90 polls and `delete-vm.sh dns1` |
| TestAC22_CreateDNSArgsAndRows | unit | `stratus-srv/internal/service/dns_server_test.go` | US-4 / REQ-13, REQ-14 / AC-22 | given valid config - then argv `[dns1 1024 1 <dns disk> eth0 192.168.1.53/24 192.168.1.1 stratus.lan 1.1.1.1,8.8.8.8]`, dns_server row |
| AC-22 address kept after restart | manual | real host demo | US-4 / REQ-14 / AC-22 | restart from 001; `ssh root@192.168.1.53` |
| AC-22 create form | unit | `stratus-ui/src/features/dns/CreateDnsForm.test.tsx` | US-4 / REQ-13 / AC-22 | given valid fields - then POST `/api/dns/server` and the result |
| TestAC23_SecondDNSRejected | unit | `stratus-srv/internal/service/dns_server_test.go` | US-4 / REQ-13 / AC-23 | given dns1 registered - then ErrDNSExists, no runner call; handler 409 |
| AC-23 form hidden with a server | unit | `stratus-ui/src/routes/Dns.test.tsx` | US-4 / REQ-13 / AC-23 | given a server - then no "Crear servidor DNS" form, the panel shows |
| TestAC24_ValidateDNSConfig | unit | `stratus-srv/internal/service/dns_validation_test.go` | US-4 / REQ-15 / AC-24 | given empty IP, `192.168.1.53`, gateway `10.0.0.1`, domain `Stratus_Lan`, forwarders `1.1.1`, prefix 31 - then ErrDNSConfigIncomplete; valid set passes |
| TestAC24_CreateDNSIncompleteConfig | unit | `stratus-srv/internal/service/dns_server_test.go` | US-4 / REQ-15 / AC-24 | given an invalid config - then ErrDNSConfigIncomplete, no runner call, no vms row |
| AC-24 create-dns valida la configuración | unit | `scripts/dns/create-dns.bats` | US-4 / REQ-15 / AC-24 | given each invalid value - then exit 2, "Configuración de DNS incompleta", empty calls.log |
| AC-25 create-dns sale con 13 si la IP responde | unit | `scripts/dns/create-dns.bats` | US-4 / REQ-15 / AC-25 | given ARPING=reply PING=none (blocks ICMP) - then exit 13, the text, no create-vm; given ARPING=none PING=reply - 13; given ARPING=missing - exit 4 |
| TestAC25_IPInUseMapsError | unit | `stratus-srv/internal/service/dns_server_test.go` | US-4 / REQ-15 / AC-25 | given exit 13 - then ErrDNSIPInUse, no vms row |
| AC-26 render_zone y render_options | unit | `scripts/dns/lib/dns-common.bats` | US-4, US-7 / REQ-16, REQ-22 / AC-26, AC-32 | given dns1, stratus.lan, 192.168.1.53 and `vm1,192.168.1.40` - then `$TTL 60`, `@ IN NS dns1.stratus.lan.`, `dns1 IN A 192.168.1.53`, `vm1 IN A 192.168.1.40`; options have `acl stratus_clients { 192.168.1.0/24; 10.10.0.0/16; localhost; }`, `forwarders { 1.1.1.1; 8.8.8.8; }` |
| AC-26 NS, A and forwarding | manual | real host demo | US-4, US-7 / REQ-16 / AC-26 | `dig NS`, `dig dns1.stratus.lan`, `dig debian.org` from a LAN client |
| AC-27 create-dns registra los servidores web iniciales | unit | `scripts/dns/create-dns.bats` | US-4, US-5 / REQ-20 / AC-27 | given records `web-a,...` and `web-b,...` - then the zone stdin has both lines |
| TestAC27_CreateDNSSkipsWebWithoutIP | unit | `stratus-srv/internal/service/dns_server_test.go` | US-4, US-5 / REQ-20 / AC-27 | given web-a and web-b with IPs and web-c without - then argv ends with two specs, two dns_records rows, the message names web-c |
| TestAC28_DNSServerRules | unit | `stratus-srv/internal/service/prepare_packages_test.go`, `haproxy_servers_test.go` | US-8 / REQ-18, REQ-19 / AC-28 | RemovePackage bind9 on dns1 - PackageBlockedError "No se puede eliminar bind9: el servidor DNS lo usa"; InstallPackage ufw argv `[--role dns dns1 ufw]`; AssignServer(lb1, dns1) - ErrVMNotAssignable, no runner call |
| AC-28 role dns en los scripts de paquetes | unit | `scripts/prepare/install-package.bats`, `remove-package.bats` | US-8 / REQ-18 / AC-28 | install ufw - `ufw allow 22/tcp`, `53/tcp`, `53/udp`; remove bind9 - exit 2 and the text |
| TestAC29_DeleteDNSRejectsWithResolvers | unit | `stratus-srv/internal/service/dns_server_test.go` | US-4, US-7 / REQ-21, REQ-33 / AC-29 | given vm1 resolver set (powered off) - then ResolversSetError "Quite primero el resolvedor DNS de las VMs: vm1", no runner call; given none - `prepare/delete-vm.sh [dns1]` as `delete_dns`, no dns rows remain |
| TestAC29_PrepareDeleteDelegatesDNS | unit | `stratus-srv/internal/service/prepare_vms_test.go` | US-4 / REQ-21 / AC-29 | given dns1 - when DeleteVM runs - then the `delete_dns` row in module dns |
| AC-29 after delete the form returns | unit | `stratus-ui/src/routes/Dns.test.tsx` | US-4 / REQ-21 / AC-29 | given delete ok and overview without server - then "Crear servidor DNS" shows and no records |
| TestAC30_AddRecordsPerVMResult | unit | `stratus-srv/internal/service/dns_records_test.go` | US-5 / REQ-22, REQ-23 / AC-30 | given vm1 and lb1 running, vm2 off - then one `dns/add-records.sh` with `vm1,<ip>` and `lb1,<bridged ip>`, results vm1 ok, lb1 ok, vm2 "La VM no tiene dirección IP; verifique que esté encendida", no vm2 row; vm1 again - "La VM ya tiene un registro DNS"; all rejected - 409 |
| AC-30 add-records aplica el conjunto completo | unit | `scripts/dns/add-records.bats` | US-5 / REQ-23, REQ-29 / AC-30 | given two records - then the zone stdin has both, `named-checkzone`, `rndc reload stratus.lan` |
| AC-30 select and add | unit | `stratus-ui/src/features/dns/RecordTable.test.tsx` | US-5 / REQ-23 / AC-30 | given three VMs without record - when selected and "Agregar registros" - then POST `{vms:[...]}` and the per-VM results |
| AC-30 dig before/after | manual | real host demo | US-5 / REQ-23 / AC-30 | `dig vm1.stratus.lan`, `dig lb1.stratus.lan` |
| TestAC31_AssignedServerRecordUsesInternalIP | unit | `stratus-srv/internal/service/dns_records_test.go` | US-5 / REQ-22 / AC-31 | given web-a in lb1 with list-vms IP 10.10.1.11 - then spec `web-a,10.10.1.11` |
| AC-32 TTL 60 | manual | real host demo | US-5 / REQ-22 / AC-32 | `dig` shows TTL 60 |
| AC-33 remove-record aplica el conjunto restante | unit | `scripts/dns/remove-record.bats` | US-5 / REQ-24 / AC-33 | given `vm1` and remaining `lb1,...` - then the zone has no `vm1` line |
| TestAC33_RemoveRecordIdempotent | unit | `stratus-srv/internal/service/dns_records_test.go` | US-5 / REQ-24 / AC-33 | given vm1 with a record - script runs and the row is gone; again - success, no runner call |
| AC-33 remove confirms | unit | `stratus-ui/src/features/dns/RecordTable.test.tsx` | US-5 / REQ-24 / AC-33 | when "Quitar registro" - dialog, then DELETE `/api/dns/records/vm1` |
| TestAC34_DeleteVMRemovesRecord | unit | `stratus-srv/internal/service/prepare_vms_test.go` | US-5 / REQ-24 / AC-34 | given vm1 with a record - then `delete-vm.sh`, then `dns/remove-record.sh` without vm1; given remove exit 7 - success with the REQ-24 warning, a failed `remove_record` row, the dns_records row already gone |
| AC-34 delete shows the warning | unit | `stratus-ui/src/features/prepare/VmActions.test.tsx` | US-5 / REQ-24 / AC-34 | given `{message, warning}` on delete - then the warning Alert |
| TestAC35_DNSOffMapsError | unit | `stratus-srv/internal/service/dns_records_test.go` | US-5, US-6 / REQ-25 / AC-35 | given exit 7 from add, remove and refresh - then ErrDNSOff, no SQLite change; handler 409 |
| AC-35 add-records sale con 7 si el DNS está apagado | unit | `scripts/dns/add-records.bats`, `refresh-records.bats` | US-5, US-6 / REQ-25 / AC-35 | given dns1 poweroff - exit 7, no ssh |
| AC-36 refresh-records escribe la IP nueva | unit | `scripts/dns/refresh-records.bats` | US-6 / REQ-26, REQ-27 / AC-36 | given `vm1,192.168.1.40`, wait `vm1`, new IP .45, CURRENT_ZONE old - then the zone has `vm1 IN A 192.168.1.45`, stdout `vm1 192.168.1.45` and `reloaded=yes` |
| TestAC36_StartVMRefreshesRecord | unit | `stratus-srv/internal/service/prepare_vms_test.go` | US-6 / REQ-26 / AC-36 | given vm1 with a record - then `start-vm.sh`, then `dns/refresh-records.sh [dns1 stratus.lan 192.168.1.53 vm1 vm1,192.168.1.40]`, the row gets the new IP, a `refresh_records` row |
| TestAC36_StartDNSRefreshesAll | unit | `stratus-srv/internal/service/prepare_vms_test.go` | US-6 / REQ-27 / AC-36 | given dns1 started - then refresh with wait `vm1,lb1` and all specs |
| AC-36 restart changes lease | manual | real host demo | US-6 / REQ-26 / AC-36 | stop, change lease, start, `dig` |
| TestAC37_AssignAndRemoveRefreshRecord | unit | `stratus-srv/internal/service/haproxy_servers_test.go` | US-6 / REQ-26 / AC-37 | given web-a with a record - AssignServer and RemoveServer each run `dns/refresh-records.sh` with wait `web-a` after the haproxy script; refresh exit 7 - success with the REQ-26 warning |
| AC-37 assign shows the DNS warning | unit | `stratus-ui/src/features/haproxy/AssignServerForm.test.tsx`, `ServerTable.test.tsx` | US-6 / REQ-26 / AC-37 | given `{message, warning}` - then the warning Alert |
| TestAC38_StartWithDNSOffWarns | unit | `stratus-srv/internal/service/prepare_vms_test.go` | US-6 / REQ-26 / AC-38 | given refresh exit 7 - then no error, Warning "Operación completada; no se pudo actualizar el registro DNS de vm1, use Actualizar registros", a failed `refresh_records` row |
| AC-39 refresh-records no recarga si nada cambió | unit | `scripts/dns/refresh-records.bats` | US-6 / REQ-27 / AC-39 | given the same IPs and CURRENT_ZONE equal except serial - then no upload, no `rndc`, `reloaded=no`; given vm2 poweroff - it keeps its IP and is not polled |
| TestAC39_RefreshRoute | unit | `stratus-srv/internal/handler/dns_test.go` | US-6 / REQ-27 / AC-39 | when POST `/api/dns/records/refresh` - then 200 `{message}` and a history row |
| AC-39 refresh button | unit | `stratus-ui/src/features/dns/DnsServerPanel.test.tsx` | US-6 / REQ-27 / AC-39 | when "Actualizar registros" - then POST and the result |
| AC-40 refresh-records espera en paralelo con un solo límite | unit | `scripts/dns/refresh-records.bats` | US-6 / REQ-27 / AC-40 | given 10 running VMs, 3 never reporting - then each poll log has at most 60 `sleep 2` and the run ends at the shared budget; the 3 keep their IPs |
| AC-41 apply_zone conserva la zona si falla la revisión | unit | `scripts/dns/lib/dns-common.bats` | US-5, US-6 / REQ-28 / AC-41 | given ZONE_CHECK=fail - exit 10, check output on stderr, `rm -f` of `.new`, no `mv`, no `rndc`; given RNDC=fail - `.bak` restored, second reload, exit 11 |
| TestAC41_ZoneInvalidMapsError | unit | `stratus-srv/internal/service/dns_records_test.go` | US-5, US-6 / REQ-28 / AC-41 | given exit 10 on add, remove and refresh - then ErrZoneInvalid, no SQLite change, stderr in history |
| AC-42 set-resolver con dhclient y con dhcpcd | unit | `scripts/dns/set-resolver.bats` | US-7 / REQ-31 / AC-42 | given DHCP_CLIENT=dhclient - the remote program writes `prepend domain-name-servers 192.168.1.53;` and prepends `nameserver 192.168.1.53`; dhcpcd - the hook file; none - exit 5 |
| AC-42 unset-resolver quita la configuración | unit | `scripts/dns/unset-resolver.bats` | US-7 / REQ-31 / AC-42 | then the block or hook is removed and `nameserver 192.168.1.53` lines are deleted; a second run succeeds |
| TestAC42_SetUnsetResolverFlag | unit | `stratus-srv/internal/service/dns_resolvers_test.go` | US-7 / REQ-31, REQ-33 / AC-42 | Set - argv `[vm1 192.168.1.53]`, row `set_resolver`, flag on; Unset - `unset_resolver`, flag off |
| AC-42 resolver toggle | unit | `stratus-ui/src/features/dns/RecordTable.test.tsx` | US-7 / REQ-31 / AC-42 | when the Switch turns on - PUT `/api/dns/resolvers/vm1`; off - DELETE |
| AC-42 resolv.conf, getent, apt and reboot | manual | real host demo | US-7 / REQ-31 / AC-42 | `cat /etc/resolv.conf`, `getent hosts web-a.stratus.lan`, `apt update`, reboot |
| TestAC43_ResolverOnServerAndOffVM | unit | `stratus-srv/internal/service/dns_resolvers_test.go` | US-7 / REQ-32 / AC-43 | given web-a in lb1 - argv starts `--jump lb1 web-a`; given exit 7 - ErrVMNoIP text |
| AC-43 set-resolver con VM apagada no llama a ssh | unit | `scripts/dns/set-resolver.bats` | US-7 / REQ-32 / AC-43 | given vm1 poweroff - exit 7, no ssh |
| TestAC44_DeleteVMClearsResolver | unit | `stratus-srv/internal/service/prepare_vms_test.go` | US-7 / REQ-33 / AC-44 | given vm1 with the flag - DeleteVM ok - then no dns_resolvers row, and DeleteDNS proceeds |
| AC-45 Nginx and Apache pages before/after | manual | real host demo | US-1, US-2 / REQ-2, REQ-4, REQ-10 / AC-45 | `curl` default page, install, `curl` new page |
| AC-46 both pages through lb1 | manual | real host demo | US-2, US-8 / REQ-6, REQ-11 / AC-46 | assign web-a, web-b; repeated `curl` on the frontend |
| AC-47 dig NXDOMAIN then IPv4 | manual | real host demo | US-4, US-5 / REQ-13, REQ-23 / AC-47 | `dig @<ip> vm1.stratus.lan A` before/after |
| AC-48 new address after restart | manual | real host demo | US-6 / REQ-26 / AC-48 | restart VM, `dig` |
| AC-49 getent fails then succeeds | manual | real host demo | US-7 / REQ-31 / AC-49 | `getent hosts lb1.stratus.lan` before/after set |
| TestAC50_CrossModuleLocks | integration | `stratus-srv/internal/service/dns_service_test.go` | US-1..US-8 / REQ-34 / AC-50 | given an install_page blocked on web-a - WebAction, InstallPackage and SetResolver on web-a give ErrOperationInProgress; given add_records blocked on dns1 - DNSAction gives ErrOperationInProgress |
| TestAC51_RecordPolicy | unit | `stratus-srv/internal/service/web_service_test.go`, `dns_service_test.go` | US-9 / REQ-35 / AC-51 | 20 ok WebStatus + 1 failed - 1 row; each always-recorded operation adds one row in its module |
| TestAC52_AddRecordsHistoryRow | unit | `stratus-srv/internal/service/dns_records_test.go` | US-9 / REQ-35 / AC-52 | given add of vm1 and lb1 - row VM `dns1`, args contain `vm1,` and `lb1,` |
| TestAC53_HistoryLimit | unit | `stratus-srv/internal/service/web_service_test.go`, `dns_service_test.go` | US-9 / REQ-36 / AC-53 | 55 rows - 50 newest first |
| AC-53 module history shows | unit | `stratus-ui/src/routes/Web.test.tsx`, `Dns.test.tsx` | US-9 / REQ-36 / AC-53 | given history rows - then "Ver stderr" per row |
| AC-54 six enabled cards | unit | `stratus-ui/src/routes/Home.test.tsx` | US-1..US-9 / REQ-37 / AC-54 | six cards; "Servidores web" links `/web`, "DNS" links `/dns` |
| AC-55 <script> sale con 2 ante argumentos inválidos | unit | `scripts/web/*.bats`, `scripts/dns/*.bats` | US-1..US-8 / REQ-34, REQ-39 / AC-55 | given a missing argument, type `iis`, name `Web1`, domain `Stratus_Lan`, unit `sshd`, bad record, `--jump Lb1` - exit 2 and empty calls.log, for each of the 16 scripts |
| AC-55 --role inválido sale con 2 | unit | `scripts/prepare/install-package.bats` | US-8 / REQ-7 / AC-55 | given `--role iis` - exit 2, empty calls.log |
| AC-56 <script> tiene la estructura exigida | unit | `scripts/web/*.bats`, `scripts/dns/*.bats` | US-1..US-8 / REQ-39 / AC-56 | `assert_script_structure` passes for each of the 16 scripts and the 3 libraries |
| TestAC1_WebAndDNSRepositories | unit | `stratus-srv/internal/repository/web_servers_test.go`, `dns_test.go` | US-1, US-4, US-5, US-7 / REQ-2, REQ-13, REQ-22, REQ-33 / AC-1, AC-22, AC-30, AC-42 | insert, get, list, delete; single dns_server row; records ordered; UpdateRecordIPs; resolvers; DeleteServer transaction |
| TestAC24_ConfigOptionalDNS | unit | `stratus-srv/cmd/server/config_test.go` | US-4 / REQ-15 / AC-24 | the server config loads without the new variables; `APP_DNS_DOMAIN` defaults to `stratus.lan` |

Note on REQ-38 and REQ-40: covered by every UI test above (success/failure `OperationResult`, busy buttons through `useOperation`, shadcn components and Spanish texts).

Existing tests that must keep passing: every 001-004 bats, Go and Vitest test. Changes there are constructor arguments, `Vm.kind` in fixtures, `StartResult` → `ActionResult`, and the delete/assign/remove response shape `{message, warning?}`.

## 8. Traceability
| Requirement | Component / file | Verified by (AC) |
|---|---|---|
| REQ-1 | `CreateWebForm`, `createWebErrors`, `WebService.CreateWeb`, `create-web.sh` validation | AC-1, AC-2 |
| REQ-2 | `create-web.sh` disk check, `WebConfig` disks | AC-1, AC-3, AC-45 |
| REQ-3 | `create-web.sh` → `create-vm.sh`, trap, shared create lock | AC-3, AC-4, AC-7 |
| REQ-4 | `render_site`, `create-web.sh` steps 4-5 | AC-4, AC-45 |
| REQ-5 | `CreateWeb` → `DNSService.AddRecordsFor`, warning | AC-5, AC-6 |
| REQ-6 | `ListWebServers`, `WebTable`, `VMView.Kind`, `VmSelector` | AC-8, AC-46 |
| REQ-7 | `--role web-*` in package scripts, `PackageBlockedError` | AC-9, AC-55 |
| REQ-8 | `DeleteWeb`, `WebTable` delete dialog, `PrepareService.DeleteVM` delegation | AC-10 |
| REQ-9 | `InstallPageDialog`, `validatePageFile`, `saveArchive`, `check_archive` with message | AC-11, AC-12, AC-13, AC-17 |
| REQ-10 | `install-page.sh` backup, swap, trap | AC-11, AC-14, AC-15, AC-45 |
| REQ-11 | `vm_scp` routed, `targetArgs` | AC-16, AC-46 |
| REQ-12 | `systemd-unit.sh`, `*-web.sh`, `*-bind.sh`, `WebAction`, `DNSAction`, `UnitButtons`, `StateWarning` | AC-18, AC-19, AC-20, AC-21 |
| REQ-13 | `CreateDnsForm`, `DNSService.CreateDNS`, `create-dns.sh` | AC-22, AC-23, AC-47 |
| REQ-14 | `create-dns.sh` steps 5, 7, 8 | AC-22 |
| REQ-15 | `ValidateDNSConfig`, `config.go`, `ip_in_use` | AC-24, AC-25 |
| REQ-16 | `render_options`, `render_zone` NS | AC-26 |
| REQ-17 | `VMView.Kind`, `VmSelector`, `DnsServerPanel` | AC-8 |
| REQ-18 | `--role dns`, `PackageBlockedError` | AC-28 |
| REQ-19 | `HAProxyService.AssignServer` DNS check | AC-28 |
| REQ-20 | `CreateDNS` initial records, `create-dns.sh` records | AC-27 |
| REQ-21 | `DeleteDNS`, `ResolversSetError`, `PrepareService.DeleteVM` delegation | AC-29 |
| REQ-22 | `recordSpecs`, `render_zone`, IP from `list-vms.sh` | AC-5, AC-26, AC-31, AC-32 |
| REQ-23 | `AddRecords`, `add-records.sh`, `RecordTable` | AC-30, AC-47 |
| REQ-24 | `RemoveRecord`, `remove-record.sh`, `AfterVMDeleted`, delete warnings | AC-33, AC-34 |
| REQ-25 | `mapDNSError` → `ErrDNSOff`, `ErrNoDNSServer`, UI hides actions without server | AC-35 |
| REQ-26 | `RefreshVM` hooks in `StartVM`, `AssignServer`, `RemoveServer` | AC-36, AC-37, AC-38, AC-48 |
| REQ-27 | `refresh-records.sh`, `RefreshAll`, `Refresh`, `wait_ips` | AC-36, AC-39, AC-40 |
| REQ-28 | `apply_zone`, `ErrZoneInvalid` | AC-41 |
| REQ-29 | full render from `dns_records` in every record script | AC-30, AC-33 |
| REQ-30 | `DeleteWeb` → `AfterVMDeleted` | AC-10 |
| REQ-31 | `set-resolver.sh`, `unset-resolver.sh`, `RecordTable` switch | AC-42, AC-49 |
| REQ-32 | `targetArgs`, `vm_ip` 7 and 12 | AC-43 |
| REQ-33 | `dns_resolvers`, `SetResolver`, `UnsetResolver`, `AfterVMDeleted` | AC-29, AC-42, AC-44 |
| REQ-34 | one script per operation, `ExecRunner` 15 min, `runLocked` locks, hooks after unlock | AC-50, AC-55 |
| REQ-35 | `operationRunner` record policy, modules `web` and `dns`, `list_web` row | AC-8, AC-20, AC-51, AC-52 |
| REQ-36 | `WebService.History`, `DNSService.History`, `HistoryTable` | AC-53 |
| REQ-37 | `modules.ts`, `App.tsx`, `Home.test.tsx` | AC-54 |
| REQ-38 | `useOperation`, `OperationResult`, error mapping in `respond.go` | AC-1, AC-6, AC-21, AC-30 |
| REQ-39 | headers and sections of `scripts/web/*.sh`, `scripts/dns/*.sh` | AC-55, AC-56 |
| REQ-40 | shadcn components and Spanish texts in `features/web`, `features/dns` | AC-1, AC-22, AC-30 |

## 9. Decisions and risks
- State in new SQLite tables (user's choice). Rejected: a `kind` column on `vms`, which needs a migration the schema does not have.
- The zone is rendered whole from `dns_records` and applied with `named-checkzone`, `.bak` and `rndc reload` (user's choice). Rejected: `nsupdate`, which makes bind own the zone (`.jnl`) and needs a diff to prove REQ-29.
- Resolver per DHCP client: dhclient `prepend`, dhcpcd hook (user's choice). Rejected: a systemd path watcher (window without the resolver) and a static resolv.conf (ignores later DHCP nameservers).
- Package protections through `--role` (user's choice). Rejected: generic `--protect/--conflict/--allow` options.
- IPs for records come from `list-vms.sh` in the backend for add and create (the VM must already report one); only refresh waits, inside the script, because waiting polls VirtualBox (P1).
- On VM delete the record row is removed before the zone script runs, so a failed removal is fixed by the next refresh, as the REQ-24 warning says.
- The DNS server bootstraps on DHCP through `create-vm.sh` and switches to the static IP with one power cycle. Rejected: changing `create-vm.sh` to boot with a static IP, which would touch the 001 bootstrap.
- `refresh-records.sh` also polls the DNS server inside the same 120 s window, so the refresh after starting the DNS server does not fail while it boots.
- Hooks run after the main operation releases its locks (REQ-34); a DNS server locked by another operation gives the warning instead of waiting.
- Several warnings (004 refresh and DNS refresh) are joined with `\n` in one `warning` field, shown with `whitespace-pre-line`. Rejected: changing the field to an array, which breaks 004 clients.
- `named` is the Debian 13 unit name for bind9 (`bind9` is an alias).
- Risk: `create-dns.sh` runs `create-vm.sh` (worst case 14 min) plus a power cycle (up to 4 min), which can pass the 15-minute timeout. Mitigation: SIGTERM runs the trap (process group); the demo measures it; the static-IP wait is bounded at 180 s.
- Risk: `arping` needs `CAP_NET_RAW`. Mitigation: `docs/guide-environment.md` documents `setcap cap_net_raw+ep $(command -v arping)`; a missing `arping` exits 4 with a clear message.
- Risk: the dhcpcd hooks directory differs between packages. Mitigation: the script uses `/usr/lib/dhcpcd/dhcpcd-hooks` and checks it exists, else exits 5; the base image guide states the expected client; AC-42 demo checks after reboot.
- Risk: bats stubs check commands, not effects, for bind9, networking and resolv.conf. Mitigation: the manual rows of section 7.
- Risk: the 004 home test expects four cards. Mitigation: it becomes the AC-54 test in the same change that adds the cards.

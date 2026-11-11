# Plan: HTTPaaS: web servers and DNS

## 1. Metadata
- Spec: `docs/specs/005-httpaas/spec.md`
- Updated: 2026-11-11 (amendment: the DNS records table drops the "IP del registro" column; REQ-23, REQ-43, AC-58, AC-63). Earlier on 2026-11-11 (amendment: FQDN links also in the web servers table; REQ-45, AC-65). Earlier on 2026-11-11 (amendment: abandoned requests are neither recorded nor logged as internal errors; REQ-50, AC-71). Earlier on 2026-11-11 (amendment: copy buttons on IPs, links on DNS record names and the "Zona" tab; US-10, US-11, REQ-43 to REQ-49, AC-63 to AC-70). Earlier on 2026-11-11 (amendment: the DNS server is not set as resolver of the VMs; US-7, REQ-31 to REQ-33, AC-42 to AC-44 and AC-49 removed). Previous: 2026-11-10 (A records only with LAN addresses; REQ-41, REQ-42, AC-57 to AC-62)
- Constitution version: 2.1.0

## 2. Constitution check
- P1: the backend validates input and the `APP_DNS_*` values, reads the web and DNS state from SQLite, reads IPs through `prepare/list-vms.sh`, picks the script, builds argv and maps exit codes. VM creation, site config, page copy and extraction, static IP, bind9 config, zone writes, the IP-in-use probe and systemctl run only in `scripts/web/*.sh`, `scripts/dns/*.sh` and the 001 scripts.
- P2: 14 new scripts, one per operation (section 6). Each has `set -euo pipefail`, positional arguments, results on stdout, diagnostics on stderr and the exit-code table of section 6. The page archive arrives as a host temp file path argument. Refresh (`--if-changed`), remove record and the unit actions are idempotent. The way to reach a server comes as `--jump <lb>` (004).
- P3: the same allowlists (name, memory, CPUs, type, CIDR, gateway, domain, forwarders, record spec, unit) are in `stratus-ui/src/features/{web,dns}/validation.ts`, `stratus-srv/internal/service/{web,dns}_validation.go` and `scripts/{web,dns}/lib/*-common.sh`. Constant remote `sh -c` programs get values as positional arguments. Zone, bind9 options and site files are rendered on the host with `printf` and sent over ssh stdin. The LAN rule of REQ-22 is checked twice: `DNSConfig.OnLAN` plus `lb_servers` in the backend, and `validate_lan_records` / `wait_ips <dns_cidr>` in the scripts.
- P4: web and DNS servers get key access in the 001 Guest Additions bootstrap through `create-vm.sh`. Afterwards every step is SSH with the backend key; assigned web servers go through `jump` (`vm_exec` and the new `vm_scp` share `VM_ROUTES`).
- P5: no private key is created. 001 and 002 key flows do not change.
- P6: every operation returns `{message}` (plus `warning`, `notice` or per-VM `results` where the spec asks) or `{error}`. `operations` rows with module `web` or `dns` keep command, argv, exit code, redacted stderr, result and timestamp. Reads are recorded only on failure.
- P7: home cards "Servidores web" (`/web`) and "DNS" (`/dns`), one screen each. Forms and dialogs confirm with `OperationResult`.
- P8: each script runs standalone with bats tests on PATH stubs. Manual rows of section 7 (AC-45 to AC-48 and the inspections) are the before/after demo on the real host.
- P9: no new Go or npm dependency. Reused: `create-vm.sh`, `delete-vm.sh`, `stop-vm.sh`, `start-vm.sh` logic, `vm-exec.sh`, `check_archive`, `saveArchive`, `ExecRunner`, `operationRunner.runLocked`, `VMLocks` (create lock included), history repository, `Redact`, `targetArgs`, `VmSelector`, `HistoryTable`, `useOperation`, `OperationResult`, `StateWarning`, `ServiceOutputPanel`, `createVmErrors`. New shared code: one bash library per module, one unit-action library used by both, two repositories.

## 3. Architecture
- State source: SQLite. `web_servers` holds name and type. `dns_server` holds the single DNS server name. `dns_records` holds VM and IP. The backend decides from SQLite whether a VM is a web server, the DNS server, an LB, an assigned server or a plain VM. Scripts never infer the role.
- Services and dependencies (no cycles):
  - `DNSService` (vms, lbs, dns repos): records, DNS server create/delete/actions. Exposes hooks used by other services: `AddRecordsFor`, `RefreshVM`, `RefreshAll`, `AfterVMDeleted`.
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
- Backend create DNS: `ValidateDNSConfig` (else `ErrDNSConfigIncomplete`, no script), `dns_server` empty (else `ErrDNSExists`), `TryLockCreate`, `vms.Insert`. Initial records (REQ-20): web servers that are not in `lb_servers` and whose `list-vms.sh` IPv4 passes `OnLAN`; assigned ones and the ones without a LAN IPv4 are named in the result. Load balancers are not registered. On success insert `dns_server` and the initial `dns_records`.
- Zone writes (`scripts/dns/lib/dns-common.sh`):
  - `render_zone <dns> <domain> <dns_ip> [record...]` prints the whole zone: `$TTL 60`, SOA with serial `$(date +%s)` on its own line ending `; serial`, `@ NS <dns>.<domain>.`, `<dns> A <dns_ip>`, one `<vm> A <ip>` per record sorted by name.
  - `apply_zone <dns> <domain> [--if-changed]`, stdin = zone:
    1. With `--if-changed`, read the current file and compare both without the `; serial` line; equal prints `reloaded=no` and returns 0.
    2. Upload to `/var/lib/bind/db.<domain>.new`; `named-checkzone <domain> .new`. Failure: remove `.new`, check output on stderr, exit 10.
    3. `cp -p` current to `.bak`, `mv .new` over it, `chown bind:bind`, `rndc reload <domain>`.
    4. Reload failure: `mv .bak` back, `rndc reload <domain>`, exit 11. Else remove `.bak`, print `reloaded=yes`.
  - Record scripts reach the DNS server with `vm_exec`; a DNS server with no IPv4 exits 7, which `DNSService` maps to "El servidor DNS está apagado; enciéndalo para cambiar los registros".
- LAN rule (REQ-22, amended 2026-11-10): an address is on the LAN when the VM is not in `lb_servers` and the address is inside the masked prefix of `APP_DNS_IP`. The backend checks both (`lbTopology`, `DNSConfig.OnLAN`). The record scripts receive `<dns_cidr>` instead of `<dns_ip>` and check the subnet of every record they write (`validate_lan_records`) and of every polled address (`wait_ips`). The role reaches the refresh script as `drop_vms`, because scripts never infer it.
- Records:
  - Backend add: DNS server exists (else `ErrNoDNSServer`), config valid. Each selected VM is checked in this order: registered; not the DNS server; not in `lb_servers` (else `*ServerRecordError`, "La VM es servidor del balanceador <lb>; registre el balanceador"); without a record (else "La VM ya tiene un registro DNS"); with an IPv4 from `list-vms.sh` (else the 001 no-IP text); IPv4 passes `OnLAN` (else `ErrVMNotOnLAN`). Accepted VMs go to one `add-records.sh` with the zone set plus the new ones; on success their rows are inserted. Result lists every VM.
  - Zone set of add and remove (`zoneRecords`): the stored records that pass the LAN rule. A row stored before 2026-11-10 that fails it stays in SQLite and out of the zone until a refresh removes it.
  - Backend remove: no record succeeds without a script. Otherwise `remove-record.sh` with the remaining zone set; on success delete the row.
  - Refresh (`refresh-records.sh <dns> <domain> <dns_cidr> <wait_vms|-> <drop_vms|-> [record...]`, records = every stored row, any IPv4):
    1. In one shared window of 60 polls of 2 s, background-poll the DNS server IP and `vm_nic_ip <vm> 1` of each listed VM that is `running` and not in `drop_vms`; a polled address outside `<dns_cidr>` does not end the poll (REQ-27: ignored as not reported); then `wait`.
    2. A record in `drop_vms` is removed. A VM with a polled LAN IPv4 gets it. Any other keeps its stored IP when that IP is inside `<dns_cidr>`, else the record is removed (also when the VM is off).
    3. Render the kept records and `apply_zone --if-changed` (a removed record or a zone line without stored record changes the zone, so it reloads).
    4. Print `<vm> <ip>` per kept record, `removed=<vm>` per removed record, then `reloaded=yes|no`. With removed records, stderr gets `Registros quitados: <vm>, <vm>`, which the history row keeps.
  - `RefreshVM(vm)` passes `wait_vms=<vm>`; `RefreshAll` and the button pass every record VM. `drop_vms` = stored record VMs that are in `lb_servers`, for every refresh. On success `UpdateRecordIPs` stores the printed IPs and `DeleteRecords` deletes the removed rows. The button message names the removed VMs.
  - `DropServerRecord(vm)` (REQ-41, REQ-42): no DNS server or no stored record is a no-op. Else delete the row first, then run `remove-record.sh` with the remaining zone set as its own `remove_record` operation. A lock or config rejection is saved by `recordRejected`.
  - `AfterVMDeleted(vm)`: if it had a record, delete the row first, then run `remove-record.sh` with the remaining set. A failed script returns the REQ-24 warning; the next refresh renders from SQLite and drops the stale line.
- Delete DNS server: `DNSService.DeleteDNS` from `/dns` and from `PrepareService.DeleteVM`. `prepare/delete-vm.sh` as `delete_dns` (module dns); on success delete `dns_records`, `dns_server` and the `vms` row in one transaction.
- Cross-module hooks (each runs after the main operation released its locks; failures become warnings and failed `dns` rows):
  - `PrepareService.StartVM`: DNS server → `RefreshAll`; VM with a record → `RefreshVM`. Warnings join the 004 warning with `\n`.
  - `PrepareService.DeleteVM`: web server → `WebService.DeleteWeb`; DNS server → `DNSService.DeleteDNS`; LB → `HAProxyService.DeleteLB`; plain VM → as before. Every successful path calls `AfterVMDeleted`. Return `ActionResult`.
  - `HAProxyService.AssignServer` (REQ-41, REQ-26): after success, `DropServerRecord(vm)`; a removed record gives the notice "Registro DNS de <vm> quitado: los servidores de un balanceador no se registran"; an error gives the warning "Operación completada; no se pudo quitar el registro DNS de <vm>, use Actualizar registros" (the row is already deleted). When `assign-server.sh` ran and failed, `RefreshVM(vm)` runs, its error is only logged, and the assign error is returned. `AssignServer` rejects the DNS server with `ErrVMNotAssignable`.
  - `HAProxyService.RemoveServer` (REQ-42): after success, `DropServerRecord(vm)` (it only finds a row stored before 2026-11-10; an error gives the REQ-41 warning), then, when a DNS server exists, the notice "<vm> no tiene registro DNS; agréguelo desde DNS". No record is added. `remove-server.sh` deletes the Guest Additions properties `/VirtualBox/GuestInfo/Net/0/V4/IP` and `/Net/1/V4/IP` after `modifyvm` and before `startvm`, as `assign-server.sh` does.
  - The record of a load balancer is never touched by assign or remove (REQ-22, AC-62). `DeleteLB` calls `AfterVMDeleted(lb)`.
  - Package scripts: `PrepareService` adds `--role web-apache|web-nginx|dns` for web servers and the DNS server, and rejects first in Go with `*PackageBlockedError` (REQ-7, REQ-18).
- Locks (REQ-34): web operations lock the web server; `--jump` operations keep the 004 one-VM lock. Add, remove, refresh and bind actions lock the DNS server. Creation of web and DNS servers uses the shared create lock. Hooks run after the triggering operation unlocked, so they compete normally and a locked DNS server gives the warning.
- UI:
  - `/web`: `CreateWebForm`, `WebTable` (one row per web server: name, type, state, IPv4, FQDN, LB; buttons Iniciar, Detener, Reiniciar, Estado, Instalar página, Eliminar), `ServiceOutputPanel` for Estado, `HistoryTable`.
  - `/dns`: without a server `CreateDnsForm`; with one `DnsServerPanel` (name, state, IPv4, zone, unit buttons, "Actualizar registros", Eliminar), then `RecordTable` (checkbox per VM without record, "Agregar registros", "Quitar registro"), then `HistoryTable`.
  - `VmSelector` labels by `kind`. Start, delete (001), assign and remove (004) show `warning` in an `Alert`; assign and remove also show `notice` in `NoticeAlert` (title "Aviso").
  - Assigned servers (REQ-6, REQ-23): `ListWebServers` returns `fqdn` "" and `Overview` returns `fqdn` "" and `record_ip` "" for a VM with role `server`, also when a row is still stored. `RecordTable` shows no checkbox for role `server`; with `record_ip` "" it has no "Quitar registro".
- State:
  - SQLite: `vms`, `load_balancers`, `lb_servers`, `web_servers`, `dns_server`, `dns_records`, `operations`.
  - VirtualBox: the VMs and adapters.
  - Guests: site config and `/var/www/html`; bind9 config, zone and static IP.
  - Backend memory: `VMLocks`.
  - UI: `Web.tsx` and `Dns.tsx` hold their lists, busy flag and history.

## 4. Design patterns
- Full rendering from SQLite in `render_zone` / `apply_zone`: the zone always equals the stored records (REQ-29); rollback is a `.bak` copy; `--if-changed` avoids needless reloads (as 004 `apply_cfg`).
- Post-operation hooks in `DNSService` (`RefreshVM`, `RefreshAll`, `AfterVMDeleted`, `AddRecordsFor`): one place turns a DNS failure into a warning plus a failed `dns` row for five triggers in three modules.
- Shared bash libraries: `scripts/web/lib/web-common.sh` (validators, `render_site`), `scripts/dns/lib/dns-common.sh` (validators, `render_zone`, `apply_zone`, `render_options`, `ip_in_use`, `wait_ips`), `scripts/prepare/lib/systemd-unit.sh` (`unit_action`, `unit_status`, used by 8 scripts of two modules).
- Routing map reuse: `vm_scp` uses the same `VM_ROUTES` ProxyCommand as `vm_exec`, so scp through `jump` has no extra code in `install-page.sh`.
- Role option `--role` in the package scripts: one function maps a role to its protected package, conflicting package, ufw ports and messages.
- Row first, zone second in `DropServerRecord` and `AfterVMDeleted` (one private helper): a failed zone change never leaves a stored record for a VM that must not have one; the next refresh fixes the zone (REQ-41).
- Cleanup traps in `create-web.sh`, `create-dns.sh` and `install-page.sh`, as in 001 and 004.
- Repositories `WebRepository` and `DNSRepository`: SQL stays out of services; tests use a temp SQLite file.

## 5. Code structure
Scripts (headers, comments and output in Spanish):
- create: `scripts/prepare/lib/systemd-unit.sh` and `.bats` - `unit_action`, `unit_status`.
- create: `scripts/web/lib/web-common.sh` and `.bats` - `fail`, `validate_vm_name`, `validate_memory`, `validate_cpus`, `validate_type`, `validate_base_disk`, `validate_bridge`, `validate_domain`, `type_unit <type>`, `render_site <type> <fqdn>`, `parse_jump`.
- create: `scripts/web/create-web.sh`, `install-page.sh`, `start-web.sh`, `stop-web.sh`, `restart-web.sh`, `status-web.sh`, each with `.bats`.
- create: `scripts/dns/lib/dns-common.sh` and `.bats` - `fail`, validators of section 6, `ip_in_use`, `render_options`, `render_zone`, `apply_zone`, `wait_ips`.
- create: `scripts/dns/create-dns.sh`, `add-records.sh`, `remove-record.sh`, `refresh-records.sh`, `start-bind.sh`, `stop-bind.sh`, `restart-bind.sh`, `status-bind.sh`, each with `.bats`.
- create: `scripts/web/test_helper.bash`, `scripts/dns/test_helper.bash` - load `../prepare/test_helper` and add the helpers of section 6.
- modify: `scripts/prepare/lib/vm-exec.sh` and `.bats` - `vm_scp <vm> <local> <remote>` (direct or routed).
- modify: `scripts/services/lib/service-common.sh` and `.bats` - `check_archive <file> <exe> [missing_msg]`; with the third argument a missing entry prints it (code 11 unchanged).
- modify: `scripts/prepare/install-package.sh`, `remove-package.sh` and `.bats` - optional `--role <web-apache|web-nginx|dns>` after `--frontend`; rules of section 6.
- modify: `scripts/prepare/test_helper.bash` - the ssh stub also logs `scp` calls (`stub_command scp`).

Backend:
- modify: `stratus-srv/internal/repository/db.go` - add the four tables of section 6.
- create: `stratus-srv/internal/repository/web_servers.go`, `dns.go` and their `_test.go`.
- modify: `stratus-srv/internal/service/errors.go` and `errors_test.go` - new sentinels, `PackageBlockedError`.
- create: `stratus-srv/internal/service/web_service.go` (`WebConfig`, `WebService`, `History`, exit-code constants, `mapWebError`), `web_validation.go`, `web_servers.go` (`ListWebServers`, `CreateWeb`, `DeleteWeb`), `web_page.go` (`InstallPage`), `web_actions.go` (`WebAction`, `WebStatus`), with `_test.go` for each.
- create: `stratus-srv/internal/service/dns_service.go` (`DNSConfig`, `DNSService`, `History`, `mapDNSError`), `dns_validation.go` (`ValidateDNSConfig`), `dns_server.go` (`Overview`, `CreateDNS`, `DeleteDNS`, `DNSAction`, `DNSStatus`), `dns_records.go` (`AddRecords`, `RemoveRecord`, `Refresh`, `AddRecordsFor`, `RefreshVM`, `RefreshAll`, `AfterVMDeleted`, `recordSpecs`, `parseRecordRefresh`), with `_test.go` for each.
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

Amendment 2026-11-10 (every file below exists except `NoticeAlert`; tests next to each):
- modify: `scripts/dns/lib/dns-common.sh` - `ip_in_subnet`, `validate_lan_records`, `wait_ips <dns_cidr> <vm...>`.
- modify: `scripts/dns/add-records.sh`, `remove-record.sh` - third argument `<dns_cidr>`; records outside it exit 2.
- modify: `scripts/dns/create-dns.sh` - initial records outside `<dns_cidr>` exit 2.
- modify: `scripts/dns/refresh-records.sh` - `<dns_cidr>`, `<drop_vms|->`, LAN filter, `removed=<vm>` lines.
- modify: `scripts/haproxy/remove-server.sh` - delete the two guest IP properties before `startvm`.
- modify: `stratus-srv/internal/repository/dns.go` - `DeleteRecords`.
- modify: `stratus-srv/internal/service/errors.go` - `ErrVMNotOnLAN`, `ServerRecordError`.
- modify: `stratus-srv/internal/service/dns_validation.go` - `DNSConfig.OnLAN`.
- modify: `stratus-srv/internal/service/dns_records.go` - `recordCandidate` order and LAN rule, `zoneRecords`, CIDR argv, `refresh` with `drop_vms` and removed rows, `DropServerRecord`, notice and warning texts, `userMessage`.
- modify: `stratus-srv/internal/service/dns_server.go` - `webRecords` (REQ-20), `Overview` (REQ-23).
- modify: `stratus-srv/internal/service/web_servers.go` - `ListWebServers` FQDN (REQ-6).
- modify: `stratus-srv/internal/service/prepare_vms.go` - `ActionResult.Notice`.
- modify: `stratus-srv/internal/service/haproxy_servers.go` - assign and remove hooks; `afterServerChange` removed.
- modify: `stratus-srv/internal/handler/respond.go`, `haproxy.go` - 409 for the two new errors; `notice` in the assign and remove responses.
- modify: `stratus-ui/src/features/prepare/api.ts` - `ActionResult.notice`.
- create: `stratus-ui/src/components/NoticeAlert.tsx` - shadcn `Alert` with title "Aviso".
- modify: `stratus-ui/src/features/haproxy/AssignServerForm.tsx`, `ServerTable.tsx` - show `notice`.
- modify: `stratus-ui/src/features/dns/RecordTable.tsx` - no checkbox for role `server`.
- modify: `docs/guide-environment.md` - upgrade note of REQ-27.

Amendment 2026-11-11, zone view and table helpers (tests next to each):
- create: `scripts/dns/show-zone.sh` and `.bats` - reads the zone (REQ-48).
- modify: `scripts/dns/test_helper.bash` - `ZONE_DUMP` answer of the ssh stub for `named-checkzone -D`.
- modify: `stratus-srv/internal/service/errors.go` - `ErrDNSOffZone`; `stratus-srv/internal/handler/respond.go` - 409.
- create: `stratus-srv/internal/service/dns_zone.go` - `ZoneRecord`, `Zone`, `parseZone`.
- modify: `stratus-srv/internal/handler/dns.go` - `GET /api/dns/zone`.
- create: `stratus-ui/src/components/CopyButton.tsx`, `IpCell.tsx` - copy button and IP cell ("-" without button).
- create: `stratus-ui/src/components/ui/tabs.tsx` - shadcn `tabs` (CLI).
- modify: `stratus-ui/src/features/web/WebTable.tsx`, `features/haproxy/LbTable.tsx`, `ServerTable.tsx`, `features/dns/RecordTable.tsx` - `IpCell`; FQDN link in `RecordTable` and `WebTable`.
- modify: `stratus-ui/src/features/dns/RecordTable.tsx` - no "IP del registro" column; `record_ip` only drives the checkbox and "Quitar registro".
- create: `stratus-ui/src/components/FqdnLink.tsx` - FQDN link to `http://<fqdn>/` in a new tab, "-" without link; used by `RecordTable` and `WebTable`.
- modify: `stratus-ui/src/features/dns/{types,api}.ts` - `ZoneRecord`, `getZone`.
- create: `stratus-ui/src/features/dns/ZoneTable.tsx` - the "Zona" tab.
- modify: `stratus-ui/src/routes/Dns.tsx` - tabs "VMs" and "Zona" in the "Registros" card; `zoneVersion` counter bumped by `reload`.

Amendment 2026-11-11, abandoned requests (tests next to each):
- modify: `stratus-srv/internal/service/vm_status.go` - `queryVMStates` returns no failed row when `ctx.Err() != nil` after `list-vms.sh` (REQ-50); `listErr` stays set, nobody reads the answer.
- modify: `stratus-srv/internal/handler/respond.go` - `writeError` returns without logging or writing when `errors.Is(err, context.Canceled)` (REQ-50).

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
DROP TABLE IF EXISTS dns_resolvers;     -- tabla de US-7, quitada el 2026-11-11
```
History:
- module `web`: `create_web`, `delete_web`, `install_page`, `start_web`, `stop_web`, `restart_web` always; `status_web` and `list_web` only on failure. VM column = the web server (`-` for `list_web`).
- module `dns`: `create_dns`, `delete_dns`, `add_records`, `remove_record`, `refresh_records`, `start_bind`, `stop_bind`, `restart_bind` always; `status_bind` and `show_zone` (2026-11-11) only on failure. For add, remove and refresh the VM column is the DNS server and the target VMs are in args.

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
| `dns/add-records.sh` | `<dns> <domain> <dns_cidr> <record...>` (full zone set after the add) | `Registros aplicados`, `reloaded=yes` |
| `dns/remove-record.sh` | `<dns> <domain> <dns_cidr> <vm> [record...]` (remaining zone set) | `Registro de <vm> quitado` |
| `dns/refresh-records.sh` | `<dns> <domain> <dns_cidr> <wait_vms\|-> <drop_vms\|-> [record...]` (`wait_vms`, `drop_vms` = `a,b`; records = every stored row) | `<vm> <ip>` per kept record, `removed=<vm>` per removed record, `reloaded=yes\|no`; stderr `Registros quitados: a, b` |
| `dns/{start,restart}-bind.sh`, `dns/stop-bind.sh`, `dns/status-bind.sh` | `<dns>` | as the web unit scripts, unit `named` |
| `prepare/{install,remove}-package.sh` | `[--jump <lb>] [--frontend <port>] [--role <role>] <vm> <package>` | unchanged |
| `dns/show-zone.sh` (2026-11-11) | `<dns> <domain>` | one line per record `<type>\t<name>\t<ttl>\t<value>`, name without the final dot; exit 0, 2, 3, 4, 5, 7, 99 |

Validation, step 1 (exit 2, no VBoxManage, ssh, scp, arping or ping call):
- `vm`, `dns`, `lb`: `^[a-z0-9][a-z0-9-]{0,31}$`. `memory_mb` 512-8192, `cpus` 1-4, `base_disk` absolute, `bridge_adapter` `^[A-Za-z0-9._:-]{1,64}$`.
- type `apache|nginx`; unit `apache2|nginx` (web) or fixed `named` (bind).
- `domain`: 1-4 labels `^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$` joined by dots. `dns_cidr`: IPv4 with prefix 8-30. `gateway`: IPv4 inside the subnet, not network or broadcast. `forwarders`: 1-3 comma-separated IPv4. `dns_ip`: IPv4. These print "Configuración de DNS incompleta" in `create-dns.sh`.
- record: `<vm>,<ipv4>`; at most 200; VM names unique; no record for `dns`. In `add-records.sh`, `remove-record.sh` and `create-dns.sh` the IPv4 must be inside `<dns_cidr>` ("Registro inválido: la dirección no está en la LAN"); `refresh-records.sh` accepts any IPv4 as input and writes only LAN ones.
- `dns_cidr` in the record scripts: `validate_cidr` ("IP del servidor DNS inválida"); the scripts take `dns_ip` from it.
- `wait_vms`, `drop_vms`: `-` or comma-separated VM names.
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

`show-zone.sh` (amendment 2026-11-11):
1. Validate `<dns>` (`validate_vm_name`), `<domain>` (`validate_domain`) and `APP_SSH_KEY_PATH` (2).
2. `VBoxManage showvminfo <dns>`; missing: 3.
3. `vm_exec <dns> named-checkzone -D -o - <domain> /var/lib/bind/db.<domain>`; 2, 4 and 7 pass through, any other failure is 5 with the tool output on stderr.
4. On the host, `awk` keeps the canonical lines (`<name> <ttl> IN <type> <rdata...>`; the "loaded serial" and "OK" lines some bind9 versions print on stdout are dropped), strips the final dot of the name and prints the tab-separated line.

`dns-common.sh` (amendment 2026-11-10):
```
ip_in_subnet <ip/prefix> <ip>              # 0 when the IPv4 is inside the subnet of the CIDR, else 1; no output
validate_lan_records <ip/prefix> [record...]  # 1 and "Registro inválido: la dirección no está en la LAN" when an IPv4 is outside
wait_ips <ip/prefix> <vm...>               # as before; an IPv4 outside the subnet is not accepted and the poll goes on
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
func (r *DNSRepository) DeleteRecords(ctx context.Context, vms []string) error // one transaction; empty list is a no-op
func (r *DNSRepository) UpdateRecordIPs(ctx context.Context, recs []DNSRecord) error
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
func ValidateWebType(t string) error                 // "Seleccione el tipo de servidor web"
func ValidateDNSConfig(c DNSConfig) error            // ErrDNSConfigIncomplete
type ActionResult struct { Message string `json:"message"`; Notice string `json:"notice,omitempty"`; Warning string `json:"warning,omitempty"` } // renamed StartResult; Notice added 2026-11-10
var ErrVMNotOnLAN = errors.New("La VM no tiene una dirección en la LAN")                        // 409
type ServerRecordError struct { LB string } // "La VM es servidor del balanceador <lb>; registre el balanceador"; 409
func (c DNSConfig) OnLAN(ip string) bool    // ip is an IPv4 inside the masked prefix of c.IP; false when either does not parse
var ErrDNSOffZone = errors.New("El servidor DNS está apagado; enciéndalo para ver la zona") // 409; 2026-11-11
type ZoneRecord struct { Type string `json:"type"`; Name string `json:"name"`; Value string `json:"value"`; TTL int `json:"ttl"` }
func (s *DNSService) Zone(ctx context.Context) ([]ZoneRecord, error) // ErrNoDNSServer; ops.run(show_zone, dns, recordSuccess false); ErrVMNoIP -> ErrDNSOffZone
func parseZone(stdout []byte) ([]ZoneRecord, error)                    // 4 tab-separated fields, TTL integer; otherwise an error

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
  VMs    []DNSVMView    `json:"vms"`     // every VM except the DNS server: {name,kind,role,state,ip,fqdn,record_ip}
  ListError string      `json:"list_error"`
}
type RecordResult struct { VM string `json:"vm"`; OK bool `json:"ok"`; Message string `json:"message"` }
func (s *DNSService) Overview(ctx context.Context) (DNSOverview, error)
func (s *DNSService) CreateDNS(ctx context.Context, in CreateVMInput) (string, error) // message names skipped web servers
func (s *DNSService) DeleteDNS(ctx context.Context) (string, error)                    // delete_dns
func (s *DNSService) DNSAction(ctx context.Context, action string) (ServiceResult, error)
func (s *DNSService) DNSStatus(ctx context.Context) (string, error)
func (s *DNSService) AddRecords(ctx context.Context, vms []string) (string, []RecordResult, error) // error only when none added
func (s *DNSService) RemoveRecord(ctx context.Context, vm string) (string, error)
func (s *DNSService) Refresh(ctx context.Context) (string, error)                       // all record VMs; nothing reloaded or removed -> "Registros sin cambios";
  // else "Registros actualizados", plus "; registros quitados: <vm>, <vm>" when records were removed
func (s *DNSService) DropServerRecord(ctx context.Context, vm string) (removed bool, err error) // hook of assign and remove; row first, then remove_record
func (s *DNSService) zoneRecords(ctx context.Context) ([]repository.DNSRecord, error)   // stored records that pass the LAN rule
func parseRecordRefresh(stdout []byte) (kept []repository.DNSRecord, removed []string, reloaded bool)
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
- `ErrDNSExists`, `ErrNoDNSServer`, `ErrDNSOff`, `ErrRecordExists`, `*PackageBlockedError`, `ErrDNSIPInUse`: 409.
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
| `POST /api/dns/records/refresh` | - | 200 `{message}` (names the removed VMs) |
| `GET /api/dns/history` | - | `{"operations":[...]}` |
| `GET /api/dns/zone` (2026-11-11) | - | 200 `{"records":[{type,name,value,ttl}]}`; DNS server off → 409 `{error}` |
| `DELETE /api/prepare/vms/{name}` (changed) | - | 200 `{message, warning?}` |
| `POST /api/haproxy/lbs/{lb}/servers`, `DELETE .../servers/{vm}` (changed) | as before | `{message, notice?, warning?}` |
| `DELETE /api/haproxy/lbs/{lb}` (changed) | as before | `{message, warning?}` |
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
interface DnsVm { name; kind; role; state; ip; fqdn; record_ip }
// features/dns/api.ts
getDnsOverview(); createDns(input); deleteDns(); dnsAction(action); dnsStatus(); addRecords(vms);
removeRecord(vm); refreshRecords(); getDnsHistory()
// components
CreateWebForm({ busy; onDone })
WebTable({ servers; busy; onDone })               // per row: UnitButtons, Instalar página, Eliminar (confirmation names the VM)
InstallPageDialog({ vm; onDone })                 // file input + confirmation "Se reemplazará todo /var/www/html"
UnitButtons({ onAction(action); onStatus() })     // Iniciar, Detener, Reiniciar, Estado; StateWarning hint "revise Estado"
CreateDnsForm({ busy; onDone })
DnsServerPanel({ server; busy; onDone })          // UnitButtons, Actualizar registros, Eliminar
RecordTable({ vms; busy; onDone })                // checkboxes (none for role "server"), Agregar registros, Quitar registro
// amendment 2026-11-10
interface ActionResult { message: string; notice?: string; warning?: string }   // features/prepare/api.ts
NoticeAlert({ notice })                           // components/NoticeAlert.tsx; renders nothing when empty; title "Aviso"
// amendment 2026-11-11
interface ZoneRecord { type: string; name: string; value: string; ttl: number }   // features/dns/types.ts
getZone(): Promise<ZoneRecord[]>                  // features/dns/api.ts
CopyButton({ value })                             // components; ghost icon-xs Button "Copiar <value>"; navigator.clipboard.writeText,
                                                  // else hidden textarea + document.execCommand("copy"); CheckIcon 2 s; toast.error on failure
IpCell({ ip })                                    // components; "-" when empty, else the IP and CopyButton
ZoneTable({ version })                            // features/dns; reads on mount and when version changes; Recargar; Tipo, Nombre, Valor, TTL
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
| AC-26 render_zone y render_options | unit | `scripts/dns/lib/dns-common.bats` | US-4 / REQ-16, REQ-22 / AC-26, AC-32 | given dns1, stratus.lan, 192.168.1.53 and `vm1,192.168.1.40` - then `$TTL 60`, `@ IN NS dns1.stratus.lan.`, `dns1 IN A 192.168.1.53`, `vm1 IN A 192.168.1.40`; options have `acl stratus_clients { 192.168.1.0/24; 10.10.0.0/16; localhost; }`, `forwarders { 1.1.1.1; 8.8.8.8; }` |
| AC-26 NS, A and forwarding | manual | real host demo | US-4 / REQ-16 / AC-26 | `dig NS`, `dig dns1.stratus.lan`, `dig debian.org` from a LAN client |
| AC-27 create-dns registra los servidores web iniciales | unit | `scripts/dns/create-dns.bats` | US-4, US-5 / REQ-20 / AC-27 | given records `web-a,...` and `web-b,...` - then the zone stdin has both lines |
| TestAC27_CreateDNSSkipsWebWithoutLANIP (replaces TestAC27_CreateDNSSkipsWebWithoutIP) | unit | `stratus-srv/internal/service/dns_server_test.go` | US-4, US-5 / REQ-20 / AC-27 | given web-a and web-b with LAN IPs, web-c without IP, web-d running and assigned to lb1 (IP 10.10.1.11), lb1 running - then argv ends with the specs of web-a and web-b, two dns_records rows, the message names web-c and web-d, no row for web-d or lb1 |
| AC-27 create-dns rechaza un registro inicial fuera de la LAN | unit | `scripts/dns/create-dns.bats` | US-4, US-5 / REQ-20, REQ-22 / AC-27 | given the record `web-d,10.10.1.11` and CIDR `192.168.1.53/24` - then exit 2 and empty calls.log |
| TestAC28_DNSServerRules | unit | `stratus-srv/internal/service/prepare_packages_test.go`, `haproxy_servers_test.go` | US-8 / REQ-18, REQ-19 / AC-28 | RemovePackage bind9 on dns1 - PackageBlockedError "No se puede eliminar bind9: el servidor DNS lo usa"; InstallPackage ufw argv `[--role dns dns1 ufw]`; AssignServer(lb1, dns1) - ErrVMNotAssignable, no runner call |
| AC-28 role dns en los scripts de paquetes | unit | `scripts/prepare/install-package.bats`, `remove-package.bats` | US-8 / REQ-18 / AC-28 | install ufw - `ufw allow 22/tcp`, `53/tcp`, `53/udp`; remove bind9 - exit 2 and the text |
| TestAC29_DeleteDNS | unit | `stratus-srv/internal/service/dns_server_test.go` | US-4 / REQ-21 / AC-29 | given dns1 with records - `prepare/delete-vm.sh [dns1]` as `delete_dns`, no dns rows remain |
| TestAC29_PrepareDeleteDelegatesDNS | unit | `stratus-srv/internal/service/prepare_vms_test.go` | US-4 / REQ-21 / AC-29 | given dns1 - when DeleteVM runs - then the `delete_dns` row in module dns |
| AC-29 after delete the form returns | unit | `stratus-ui/src/routes/Dns.test.tsx` | US-4 / REQ-21 / AC-29 | given delete ok and overview without server - then "Crear servidor DNS" shows and no records |
| TestAC30_AddRecordsPerVMResult | unit | `stratus-srv/internal/service/dns_records_test.go` | US-5 / REQ-22, REQ-23 / AC-30 | given vm1 and lb1 running, vm2 off - then one `dns/add-records.sh` with `vm1,<ip>` and `lb1,<bridged ip>`, results vm1 ok, lb1 ok, vm2 "La VM no tiene dirección IP; verifique que esté encendida", no vm2 row; vm1 again - "La VM ya tiene un registro DNS"; all rejected - 409 |
| AC-30 add-records aplica el conjunto completo | unit | `scripts/dns/add-records.bats` | US-5 / REQ-23, REQ-29 / AC-30 | given two records - then the zone stdin has both, `named-checkzone`, `rndc reload stratus.lan` |
| AC-30 select and add | unit | `stratus-ui/src/features/dns/RecordTable.test.tsx` | US-5 / REQ-23 / AC-30 | given three VMs without record - when selected and "Agregar registros" - then POST `{vms:[...]}` and the per-VM results |
| AC-30 dig before/after | manual | real host demo | US-5 / REQ-23 / AC-30 | `dig vm1.stratus.lan`, `dig lb1.stratus.lan` |
| TestAC31_AssignedServerRejected (replaces TestAC31_AssignedServerRecordUsesInternalIP) | unit | `stratus-srv/internal/service/dns_records_test.go` | US-5 / REQ-22, REQ-23 / AC-31 | given web-a in lb1 with IP 10.10.1.11 and running vm1 - when AddRecords(web-a, vm1) - then web-a result "La VM es servidor del balanceador lb1; registre el balanceador", vm1 ok, argv has only the vm1 spec, no web-a row; web-a alone gives `*ServerRecordError` (409) |
| AC-31 no selection for a server of a load balancer | unit | `stratus-ui/src/features/dns/RecordTable.test.tsx` | US-5 / REQ-23 / AC-31 | given web-a role `server` without record and vm1 - then no checkbox "Seleccionar web-a", one for vm1 |
| AC-32 TTL 60 | manual | real host demo | US-5 / REQ-22 / AC-32 | `dig` shows TTL 60 |
| AC-33 remove-record aplica el conjunto restante | unit | `scripts/dns/remove-record.bats` | US-5 / REQ-24 / AC-33 | given `vm1` and remaining `lb1,...` - then the zone has no `vm1` line |
| TestAC33_RemoveRecordIdempotent | unit | `stratus-srv/internal/service/dns_records_test.go` | US-5 / REQ-24 / AC-33 | given vm1 with a record - script runs and the row is gone; again - success, no runner call |
| AC-33 remove confirms | unit | `stratus-ui/src/features/dns/RecordTable.test.tsx` | US-5 / REQ-24 / AC-33 | when "Quitar registro" - dialog, then DELETE `/api/dns/records/vm1` |
| TestAC34_DeleteVMRemovesRecord | unit | `stratus-srv/internal/service/prepare_vms_test.go` | US-5 / REQ-24 / AC-34 | given vm1 with a record - then `delete-vm.sh`, then `dns/remove-record.sh` without vm1; given remove exit 7 - success with the REQ-24 warning, a failed `remove_record` row, the dns_records row already gone |
| AC-34 delete shows the warning | unit | `stratus-ui/src/features/prepare/VmActions.test.tsx` | US-5 / REQ-24 / AC-34 | given `{message, warning}` on delete - then the warning Alert |
| TestAC35_DNSOffMapsError | unit | `stratus-srv/internal/service/dns_records_test.go` | US-5, US-6 / REQ-25 / AC-35 | given exit 7 from add, remove and refresh - then ErrDNSOff, no SQLite change; handler 409 |
| AC-35 add-records sale con 7 si el DNS está apagado | unit | `scripts/dns/add-records.bats`, `refresh-records.bats` | US-5, US-6 / REQ-25 / AC-35 | given dns1 poweroff - exit 7, no ssh |
| AC-36 refresh-records escribe la IP nueva | unit | `scripts/dns/refresh-records.bats` | US-6 / REQ-26, REQ-27 / AC-36 | given `vm1,192.168.1.40`, wait `vm1`, new IP .45, CURRENT_ZONE old - then the zone has `vm1 IN A 192.168.1.45`, stdout `vm1 192.168.1.45` and `reloaded=yes` |
| TestAC36_StartVMRefreshesRecord | unit | `stratus-srv/internal/service/prepare_vms_test.go` | US-6 / REQ-26 / AC-36 | given vm1 with a record - then `start-vm.sh`, then `dns/refresh-records.sh [dns1 stratus.lan 192.168.1.53/24 vm1 - vm1,192.168.1.40]`, the row gets the new IP, a `refresh_records` row |
| TestAC36_StartDNSRefreshesAll | unit | `stratus-srv/internal/service/prepare_vms_test.go` | US-6 / REQ-27 / AC-36 | given dns1 started - then refresh with wait `vm1,lb1` and all specs |
| AC-36 restart changes lease | manual | real host demo | US-6 / REQ-26 / AC-36 | stop, change lease, start, `dig` |
| TestAC37_AssignRemovesRecordRemoveNotices (replaces TestAC37_AssignAndRemoveRefreshRecord) | unit | `stratus-srv/internal/service/haproxy_servers_test.go` | US-6 / REQ-41, REQ-42 / AC-37 | given web-a with a record - AssignServer runs `haproxy/assign-server.sh`, then `dns/remove-record.sh` with `web-a` and the remaining set, no refresh; the row is gone, a `remove_record` row, Notice "Registro DNS de web-a quitado: los servidores de un balanceador no se registran"; RemoveServer runs only `haproxy/remove-server.sh`, no dns_records row for web-a, Notice "web-a no tiene registro DNS; agréguelo desde DNS"; without DNS server - no Notice in either |
| TestAC37_DropServerRecord | unit | `stratus-srv/internal/service/dns_records_test.go` | US-6 / REQ-41, REQ-34 / AC-37 | given vm1 with a record - then removed true and the row is deleted before the script call; no record or no DNS server - removed false, no runner call; DNS server locked - error, row gone, a rejected `remove_record` row |
| AC-37 assign and remove show the notice | unit | `stratus-ui/src/features/haproxy/AssignServerForm.test.tsx`, `ServerTable.test.tsx` (replace the old AC-37 warning tests) | US-6 / REQ-41, REQ-42 / AC-37 | given `{message, notice}` - then the "Aviso" Alert with the notice text |
| AC-37 NoticeAlert | unit | `stratus-ui/src/components/NoticeAlert.test.tsx` | US-6 / REQ-41, REQ-42 / AC-37 | given a notice - title "Aviso" and the text; given "" - nothing rendered |
| AC-37 handler writes notice | unit | `stratus-srv/internal/handler/haproxy_test.go` | US-6 / REQ-41, REQ-42 / AC-37 | when assign and remove return a Notice - then the JSON has `notice` |
| AC-37 assign, remove and add again | manual | real host demo | US-6 / REQ-41, REQ-42 / AC-37, AC-61, AC-62 | `dig web-a` NXDOMAIN after assign; after remove no record; add; `dig` gives the new LAN IPv4; `dig lb1` unchanged |
| TestAC38_StartWithDNSOffWarns | unit | `stratus-srv/internal/service/prepare_vms_test.go` | US-6 / REQ-26 / AC-38 | given refresh exit 7 - then no error, Warning "Operación completada; no se pudo actualizar el registro DNS de vm1, use Actualizar registros", a failed `refresh_records` row |
| AC-39 refresh-records no recarga si nada cambió | unit | `scripts/dns/refresh-records.bats` | US-6 / REQ-27 / AC-39 | given the same IPs and CURRENT_ZONE equal except serial - then no upload, no `rndc`, `reloaded=no`; given vm2 poweroff - it keeps its IP and is not polled |
| TestAC39_RefreshRoute | unit | `stratus-srv/internal/handler/dns_test.go` | US-6 / REQ-27 / AC-39 | when POST `/api/dns/records/refresh` - then 200 `{message}` and a history row |
| AC-39 refresh button | unit | `stratus-ui/src/features/dns/DnsServerPanel.test.tsx` | US-6 / REQ-27 / AC-39 | when "Actualizar registros" - then POST and the result |
| AC-40 refresh-records espera en paralelo con un solo límite | unit | `scripts/dns/refresh-records.bats` | US-6 / REQ-27 / AC-40 | given 10 running VMs, 3 never reporting - then each poll log has at most 60 `sleep 2` and the run ends at the shared budget; the 3 keep their IPs |
| AC-41 apply_zone conserva la zona si falla la revisión | unit | `scripts/dns/lib/dns-common.bats` | US-5, US-6 / REQ-28 / AC-41 | given ZONE_CHECK=fail - exit 10, check output on stderr, `rm -f` of `.new`, no `mv`, no `rndc`; given RNDC=fail - `.bak` restored, second reload, exit 11 |
| TestAC41_ZoneInvalidMapsError | unit | `stratus-srv/internal/service/dns_records_test.go` | US-5, US-6 / REQ-28 / AC-41 | given exit 10 on add, remove and refresh - then ErrZoneInvalid, no SQLite change, stderr in history |
| AC-45 Nginx and Apache pages before/after | manual | real host demo | US-1, US-2 / REQ-2, REQ-4, REQ-10 / AC-45 | `curl` default page, install, `curl` new page |
| AC-46 both pages through lb1 | manual | real host demo | US-2, US-8 / REQ-6, REQ-11 / AC-46 | assign web-a, web-b; repeated `curl` on the frontend |
| AC-47 dig NXDOMAIN then IPv4 | manual | real host demo | US-4, US-5 / REQ-13, REQ-23 / AC-47 | `dig @<ip> vm1.stratus.lan A` before/after |
| AC-48 new address after restart | manual | real host demo | US-6 / REQ-26 / AC-48 | restart VM, `dig` |
| TestAC50_CrossModuleLocks | integration | `stratus-srv/internal/service/dns_service_test.go` | US-1..US-8 / REQ-34 / AC-50 | given an install_page blocked on web-a - WebAction and InstallPackage on web-a give ErrOperationInProgress; given add_records blocked on dns1 - DNSAction gives ErrOperationInProgress |
| TestAC51_RecordPolicy | unit | `stratus-srv/internal/service/web_service_test.go`, `dns_service_test.go` | US-9 / REQ-35 / AC-51 | 20 ok WebStatus + 1 failed - 1 row; each always-recorded operation adds one row in its module |
| TestAC52_AddRecordsHistoryRow | unit | `stratus-srv/internal/service/dns_records_test.go` | US-9 / REQ-35 / AC-52 | given add of vm1 and lb1 - row VM `dns1`, args contain `vm1,` and `lb1,` |
| TestAC53_HistoryLimit | unit | `stratus-srv/internal/service/web_service_test.go`, `dns_service_test.go` | US-9 / REQ-36 / AC-53 | 55 rows - 50 newest first |
| AC-53 module history shows | unit | `stratus-ui/src/routes/Web.test.tsx`, `Dns.test.tsx` | US-9 / REQ-36 / AC-53 | given history rows - then "Ver stderr" per row |
| AC-54 six enabled cards | unit | `stratus-ui/src/routes/Home.test.tsx` | US-1..US-9 / REQ-37 / AC-54 | six cards; "Servidores web" links `/web`, "DNS" links `/dns` |
| AC-55 <script> sale con 2 ante argumentos inválidos | unit | `scripts/web/*.bats`, `scripts/dns/*.bats` | US-1..US-8 / REQ-34, REQ-39 / AC-55 | given a missing argument, type `iis`, name `Web1`, domain `Stratus_Lan`, unit `sshd`, bad record, `--jump Lb1` - exit 2 and empty calls.log, for each of the 16 scripts |
| AC-55 --role inválido sale con 2 | unit | `scripts/prepare/install-package.bats` | US-8 / REQ-7 / AC-55 | given `--role iis` - exit 2, empty calls.log |
| AC-56 <script> tiene la estructura exigida | unit | `scripts/web/*.bats`, `scripts/dns/*.bats` | US-1..US-8 / REQ-39 / AC-56 | `assert_script_structure` passes for each of the 16 scripts and the 3 libraries |
| TestAC1_WebAndDNSRepositories | unit | `stratus-srv/internal/repository/web_servers_test.go`, `dns_test.go` | US-1, US-4, US-5 / REQ-2, REQ-13, REQ-22 / AC-1, AC-22, AC-30 | insert, get, list, delete; single dns_server row; records ordered; UpdateRecordIPs; DeleteServer transaction |
| TestAC24_ConfigOptionalDNS | unit | `stratus-srv/cmd/server/config_test.go` | US-4 / REQ-15 / AC-24 | the server config loads without the new variables; `APP_DNS_DOMAIN` defaults to `stratus.lan` |
| TestAC8_ListWebServersOnly (new case) | unit | `stratus-srv/internal/service/web_servers_test.go` | US-1, US-8 / REQ-6 / AC-8 | given web-b assigned to lb1 with a stored record - then fqdn "" |
| AC-8 web table shows - as FQDN of an assigned server | unit | `stratus-ui/src/features/web/WebTable.test.tsx` | US-1 / REQ-6 / AC-8 | given a server with fqdn "" and lb `lb1` - then FQDN cell "-" |
| TestAC57_AssignWithDNSOffWarns | unit | `stratus-srv/internal/service/haproxy_servers_test.go` | US-6 / REQ-41 / AC-57 | given web-a with a record and remove-record exit 7 - then no error, Warning "Operación completada; no se pudo quitar el registro DNS de web-a, use Actualizar registros", no Notice, a failed `remove_record` row, no dns_records row; Overview shows no record for web-a |
| AC-57 assign shows the DNS warning | unit | `stratus-ui/src/features/haproxy/AssignServerForm.test.tsx` | US-6 / REQ-41 / AC-57 | given `{message, warning}` - then the warning Alert |
| AC-57 refresh-records recarga si la zona tiene una entrada sin registro guardado | unit | `scripts/dns/refresh-records.bats` | US-6 / REQ-27, REQ-41 / AC-57 | given records `vm1,192.168.1.40` and CURRENT_ZONE with also a `web-a` line - then upload without `web-a`, `rndc reload`, `reloaded=yes` |
| AC-57 NXDOMAIN after the DNS server starts | manual | real host demo | US-6 / REQ-41 / AC-57 | assign with DNS off, start DNS, `dig web-a` |
| AC-58 refresh-records quita los registros de drop_vms y los de dirección fuera de la LAN | unit | `scripts/dns/refresh-records.bats` | US-6 / REQ-27 / AC-58 | given CIDR `192.168.1.53/24`, drop `web-a`, records `web-a,10.10.1.11`, `vm3,10.10.2.15` (poweroff) and `vm1,192.168.1.40` - then web-a is not polled, the zone has only `vm1`, stdout `vm1 192.168.1.40`, `removed=web-a`, `removed=vm3`, `reloaded=yes`, stderr "Registros quitados: web-a, vm3" |
| TestAC58_RefreshRemovesServerAndNonLANRecords | unit | `stratus-srv/internal/service/dns_records_test.go` | US-6 / REQ-27, REQ-35 / AC-58 | given those three rows and web-a in lb1 - then argv `[dns1 stratus.lan 192.168.1.53/24 vm1,vm3 web-a vm1,192.168.1.40 vm3,10.10.2.15 web-a,10.10.1.11]`; with stdout `vm1 192.168.1.40`, `removed=web-a`, `removed=vm3`, `reloaded=yes` - then only the vm1 row remains, message "Registros actualizados; registros quitados: web-a, vm3", the `refresh_records` row has web-a and vm3 in args and the stderr line; RefreshVM(vm1) also passes drop `web-a` |
| TestAC58_ZoneSetSkipsStoredNonLANRecords | unit | `stratus-srv/internal/service/dns_records_test.go` | US-5, US-6 / REQ-22 / AC-58 | given stored `web-a,10.10.1.11` (assigned) and `vm3,10.10.2.15` - when vm1 is added and when a record is removed - then the script argv has neither spec |
| TestAC58_OverviewHidesServerRecord | unit | `stratus-srv/internal/service/dns_server_test.go` | US-5 / REQ-23 / AC-58 | given web-a in lb1 with a stored record - then its view has role `server`, fqdn "" and record_ip "" |
| TestAC1_WebAndDNSRepositories_DNS (new case) | unit | `stratus-srv/internal/repository/dns_test.go` | US-6 / REQ-27 / AC-58 | DeleteRecords deletes the listed rows in one transaction; an empty list changes nothing |
| AC-58 hidden record has no Quitar registro | unit | `stratus-ui/src/features/dns/RecordTable.test.tsx` | US-5 / REQ-23 / AC-58 | given web-a role `server`, fqdn "" and record_ip "" - then FQDN "-", no "Quitar registro" and no checkbox in its row |
| AC-58 refresh result names the removed VMs | unit | `stratus-ui/src/features/dns/DnsServerPanel.test.tsx` | US-6 / REQ-27 / AC-58 | given the message "Registros actualizados; registros quitados: web-a, vm3" - then it shows |
| AC-58 upgrade cleanup | manual | real host demo | US-6 / REQ-27 / AC-58 | stored internal records, "Actualizar registros", `dig` |
| AC-59 ip_in_subnet y validate_lan_records | unit | `scripts/dns/lib/dns-common.bats` | US-5, US-6 / REQ-22 / AC-59 | given `192.168.1.53/24` - `192.168.1.40` passes, `10.0.2.15` and `169.254.3.7` fail; given `10.0.0.53/24` - `10.0.0.40` passes |
| AC-59 wait_ips ignora una dirección fuera de la LAN | unit | `scripts/dns/lib/dns-common.bats` | US-6 / REQ-27 / AC-59 | given answers `10.0.2.15` then `192.168.1.45` - then `vm1 192.168.1.45`; given only `10.0.2.15` - then `vm1 -` after 60 polls |
| AC-59 add-records sale con 2 si un registro no está en la LAN | unit | `scripts/dns/add-records.bats`, `scripts/dns/remove-record.bats` | US-5 / REQ-22 / AC-59 | given CIDR `192.168.1.53/24` and the record `vm1,10.0.2.15` - then exit 2 and empty calls.log (no VBoxManage, no ssh); given `10.0.0.53/24` and `vm1,10.0.0.40` - then the zone is applied |
| AC-59 refresh-records conserva la IP guardada si la VM informa una dirección fuera de la LAN | unit | `scripts/dns/refresh-records.bats` | US-6 / REQ-27 / AC-59 | given `vm1,192.168.1.40` running and reporting `10.0.2.15` - then stdout `vm1 192.168.1.40`, no `removed=` line, `reloaded=no` |
| TestAC59_OnLAN | unit | `stratus-srv/internal/service/dns_validation_test.go` | US-5, US-6 / REQ-22 / AC-59 | table: `192.168.1.53/24` with `192.168.1.40` true, `10.0.2.15`, `169.254.3.7`, "" and `abc` false; `10.0.0.53/24` with `10.0.0.40` true |
| TestAC59_AddRecordRejectsNonLAN | unit | `stratus-srv/internal/service/dns_records_test.go`, `errors_test.go`, `stratus-srv/internal/handler/respond_test.go` | US-5 / REQ-22, REQ-23 / AC-59 | given vm1 unassigned reporting `10.0.2.15` - then `ErrVMNotOnLAN` "La VM no tiene una dirección en la LAN", no runner call, 409; `AddRecordsFor` with that IP - error and a rejected `add_records` row |
| TestAC60_FailedAssignRefreshesRecord | unit | `stratus-srv/internal/service/haproxy_servers_test.go` | US-6 / REQ-26 / AC-60 | given vm1 with a record and assign-server exit 14 - then the assign error is returned, `dns/refresh-records.sh` ran after it with wait `vm1`, the row has the printed IP, a `refresh_records` row; given the refresh exits 7 - the same assign error and a failed `refresh_records` row; given a rejection before the script (LB full) - no refresh |
| TestAC61_AddAfterRemoveNeverInternal | unit | `stratus-srv/internal/service/dns_records_test.go` | US-5, US-6 / REQ-42 / AC-61 | given web-a unassigned reporting `10.10.1.11` or `10.0.2.15` - `ErrVMNotOnLAN`; reporting "" - `ErrVMNoIP`; reporting `192.168.1.47` - spec `web-a,192.168.1.47` |
| AC-61 remove-server borra las propiedades de IP antes de arrancar | unit | `scripts/haproxy/remove-server.bats` | US-5, US-6 / REQ-42 / AC-61 | given a running server - then calls.log has `guestproperty delete web-a /VirtualBox/GuestInfo/Net/0/V4/IP` and `.../Net/1/V4/IP` after `modifyvm` and before `startvm` |
| TestAC62_RemoveServerKeepsLBRecord | unit | `stratus-srv/internal/service/haproxy_servers_test.go` | US-5 / REQ-22 / AC-62 | given lb1 with a record and its only server web-a - when RemoveServer runs - then the lb1 row is unchanged and no dns script names lb1 as removed |
| AC-66 show-zone imprime cada registro como tipo, nombre, TTL y valor | unit | `scripts/dns/show-zone.bats` | US-11 / REQ-46, REQ-48 / AC-66 | given ZONE_DUMP with SOA, NS, two A and the "loaded serial"/"OK" lines - then four tab-separated lines, names without final dot; remote.log has `named-checkzone -D -o - stratus.lan /var/lib/bind/db.stratus.lan` |
| AC-68 show-zone sale con 7 si el servidor DNS no tiene IP | unit | `scripts/dns/show-zone.bats` | US-11 / REQ-49 / AC-68 | given dns1 without IP - exit 7, no ssh; given ZONE_CHECK=fail - exit 5 with the tool output on stderr |
| AC-69 show-zone sale con 2 ante argumentos inválidos | unit | `scripts/dns/show-zone.bats` | US-11 / REQ-48 / AC-69, AC-56 | missing argument, `DNS_1`, `Stratus_Lan`, extra argument, no `APP_SSH_KEY_PATH` - exit 2, empty calls.log; `assert_script_structure` |
| TestAC66_ZoneParsesRecords | unit | `stratus-srv/internal/service/dns_zone_test.go` | US-11 / REQ-46, REQ-48 / AC-66 | given that stdout - then four `ZoneRecord`, argv `[dns1 stratus.lan]`; malformed line - error; no DNS server - `ErrNoDNSServer` without runner call |
| TestAC68_ZoneDNSOff | unit | `stratus-srv/internal/service/dns_zone_test.go`, `errors_test.go`, `stratus-srv/internal/handler/respond_test.go` | US-11 / REQ-49 / AC-68 | exit 7 - `ErrDNSOffZone` (409) and one failed `show_zone` row; 20 good reads - no row |
| TestAC66_ZoneRoute | unit | `stratus-srv/internal/handler/dns_test.go` | US-11 / REQ-46 / AC-66 | GET `/api/dns/zone` - 200 `{records:[...]}` |
| AC-63 copies the IP / AC-64 toast on failure | unit | `stratus-ui/src/components/CopyButton.test.tsx` | US-10 / REQ-44 / AC-63, AC-64 | click "Copiar 192.168.1.40" - writeText called and check icon; writeText rejects - toast "No se pudo copiar 192.168.1.40" |
| AC-63 copy button next to each IP | unit | `WebTable.test.tsx`, `LbTable.test.tsx`, `ServerTable.test.tsx`, `RecordTable.test.tsx` | US-10 / REQ-43 / AC-63 | an IP cell has "Copiar <ip>"; a "-" cell has none |
| AC-65 the FQDN is a link | unit | `stratus-ui/src/features/dns/RecordTable.test.tsx`, `stratus-ui/src/features/web/WebTable.test.tsx` | US-10 / REQ-45 / AC-65 | link `http://web-a.stratus.lan/`, `target=_blank`, `rel=noopener noreferrer`; no link for "-" |
| AC-66, AC-67, AC-68 zone tab | unit | `stratus-ui/src/features/dns/ZoneTable.test.tsx`, `stratus-ui/src/routes/Dns.test.tsx` | US-11 / REQ-46, REQ-47, REQ-49 / AC-66, AC-67, AC-68 | "VMs" default; "Zona" lists the rows with copy buttons on A values; Recargar and an operation with the tab open read again; error text shown; empty zone text |
| AC-70 zone before and after removing a record | manual | real host demo | US-11 / REQ-46 / AC-70 | open "Zona", remove `vm1`'s record, open again, `dig` |
| TestAC71_CancelledListNotRecorded | unit | `stratus-srv/internal/service/vm_status_test.go` | US-9 / REQ-50 / AC-71 | cancelled context and a failing runner - no failed row; live context and the same failure - failed row |
| TestAC71_CancelledRequestNotLogged | unit | `stratus-srv/internal/handler/respond_test.go` | US-9 / REQ-50 / AC-71 | wrapped `context.Canceled` - no "error interno" log line and no body; another error - 500 and the log line |

Note on REQ-38 and REQ-40: covered by every UI test above (success/failure `OperationResult`, busy buttons through `useOperation`, shadcn components and Spanish texts).

Existing tests that must keep passing: every 001-004 bats, Go and Vitest test. Changes there are constructor arguments, `Vm.kind` in fixtures, `StartResult` → `ActionResult`, and the delete/assign/remove response shape `{message, warning?}`.

Amendment 2026-11-10, existing 005 tests whose assertions change (changed before the code, with the rows above): every bats and Go test that passes or asserts the third argument of `add-records.sh`, `remove-record.sh` and `refresh-records.sh` (`192.168.1.53` → `192.168.1.53/24`, plus the `drop_vms` item for refresh), the AC-40 `wait_ips` test (new first argument), `TestAC27_CreateDNSSkipsWebWithoutIP`, `TestAC31_AssignedServerRecordUsesInternalIP`, `TestAC37_AssignAndRemoveRefreshRecord`, `TestAC23_DNSOverview` (a server row), the Vitest "AC-31 shows each VM with its record" and the two "AC-37 shows the warning when the DNS record could not be updated". AC-36, AC-38, AC-39 and AC-40 keep their rows with the new argv.

## 8. Traceability
| Requirement | Component / file | Verified by (AC) |
|---|---|---|
| REQ-1 | `CreateWebForm`, `createWebErrors`, `WebService.CreateWeb`, `create-web.sh` validation | AC-1, AC-2 |
| REQ-2 | `create-web.sh` disk check, `WebConfig` disks | AC-1, AC-3, AC-45 |
| REQ-3 | `create-web.sh` → `create-vm.sh`, trap, shared create lock | AC-3, AC-4, AC-7 |
| REQ-4 | `render_site`, `create-web.sh` steps 4-5 | AC-4, AC-45 |
| REQ-5 | `CreateWeb` → `DNSService.AddRecordsFor`, warning | AC-5, AC-6 |
| REQ-6 | `ListWebServers` (FQDN "" while assigned), `WebTable`, `VMView.Kind`, `VmSelector` | AC-8, AC-46 |
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
| REQ-20 | `CreateDNS` initial records (`webRecords`: unassigned, `OnLAN`), `create-dns.sh` records with `validate_lan_records` | AC-27 |
| REQ-21 | `DeleteDNS`, `PrepareService.DeleteVM` delegation | AC-29 |
| REQ-22 | `recordSpecs`, `render_zone`, IP from `list-vms.sh`, `DNSConfig.OnLAN`, `zoneRecords`, `ip_in_subnet`, `validate_lan_records` | AC-5, AC-26, AC-31, AC-32, AC-58, AC-59, AC-62 |
| REQ-23 | `AddRecords` (`recordCandidate`, `ServerRecordError`, `ErrVMNotOnLAN`), `add-records.sh`, `Overview`, `RecordTable` | AC-30, AC-31, AC-47, AC-58, AC-59 |
| REQ-24 | `RemoveRecord`, `remove-record.sh`, `AfterVMDeleted`, delete warnings | AC-33, AC-34 |
| REQ-25 | `mapDNSError` → `ErrDNSOff`, `ErrNoDNSServer`, UI hides actions without server | AC-35 |
| REQ-26 | `RefreshVM` hooks in `StartVM` and after a failed `assign-server.sh` in `AssignServer` | AC-36, AC-38, AC-48, AC-60 |
| REQ-27 | `refresh-records.sh` (`drop_vms`, LAN filter, `removed=`), `RefreshAll`, `Refresh`, `wait_ips <dns_cidr>`, `DeleteRecords`, `docs/guide-environment.md` | AC-36, AC-39, AC-40, AC-57, AC-58, AC-59 |
| REQ-41 | `DropServerRecord`, `AssignServer` hook, `ActionResult.Notice`, `NoticeAlert`, `AssignServerForm` | AC-37, AC-57 |
| REQ-42 | `RemoveServer` hook and notice, `remove-server.sh` guest property delete, `ServerTable` | AC-37, AC-61, AC-62 |
| REQ-28 | `apply_zone`, `ErrZoneInvalid` | AC-41 |
| REQ-29 | full render from `dns_records` in every record script | AC-30, AC-33 |
| REQ-30 | `DeleteWeb` → `AfterVMDeleted` | AC-10 |
| REQ-34 | one script per operation, `ExecRunner` 15 min, `runLocked` locks, hooks after unlock (also `DropServerRecord` and the refresh after a failed assign) | AC-37, AC-50, AC-55 |
| REQ-35 | `operationRunner` record policy, modules `web` and `dns`, `list_web` row | AC-8, AC-20, AC-51, AC-52 |
| REQ-36 | `WebService.History`, `DNSService.History`, `HistoryTable` | AC-53 |
| REQ-37 | `modules.ts`, `App.tsx`, `Home.test.tsx` | AC-54 |
| REQ-38 | `useOperation`, `OperationResult`, error mapping in `respond.go` | AC-1, AC-6, AC-21, AC-30 |
| REQ-39 | headers and sections of `scripts/web/*.sh`, `scripts/dns/*.sh` | AC-55, AC-56 |
| REQ-40 | shadcn components and Spanish texts in `features/web`, `features/dns` | AC-1, AC-22, AC-30 |
| REQ-43 | `IpCell` in `WebTable`, `LbTable`, `ServerTable`, `RecordTable`, `ZoneTable` | AC-63, AC-66 |
| REQ-44 | `CopyButton` | AC-63, AC-64 |
| REQ-45 | `FqdnLink` in `RecordTable` and `WebTable` | AC-65 |
| REQ-46 | `show-zone.sh`, `DNSService.Zone`, `GET /api/dns/zone`, `ZoneTable`, tabs in `Dns.tsx` | AC-66, AC-70 |
| REQ-47 | `ZoneTable` version and Recargar, `Dns.tsx` `zoneVersion` | AC-67 |
| REQ-48 | `show-zone.sh`, `ops.run` DNS server lock | AC-66, AC-69 |
| REQ-49 | `ErrDNSOffZone`, record policy `show_zone` | AC-68 |
| REQ-50 | `queryVMStates` cancellation check, `context.Canceled` in `respond.go` | AC-71 |

## 9. Decisions and risks
- State in new SQLite tables (user's choice). Rejected: a `kind` column on `vms`, which needs a migration the schema does not have.
- The zone is rendered whole from `dns_records` and applied with `named-checkzone`, `.bak` and `rndc reload` (user's choice). Rejected: `nsupdate`, which makes bind own the zone (`.jnl`) and needs a diff to prove REQ-29.
- Package protections through `--role` (user's choice). Rejected: generic `--protect/--conflict/--allow` options.
- IPs for records come from `list-vms.sh` in the backend for add and create (the VM must already report one); only refresh waits, inside the script, because waiting polls VirtualBox (P1).
- On VM delete the record row is removed before the zone script runs, so a failed removal is fixed by the next refresh, as the REQ-24 warning says.
- The DNS server bootstraps on DHCP through `create-vm.sh` and switches to the static IP with one power cycle. Rejected: changing `create-vm.sh` to boot with a static IP, which would touch the 001 bootstrap.
- `refresh-records.sh` also polls the DNS server inside the same 120 s window, so the refresh after starting the DNS server does not fail while it boots.
- Hooks run after the main operation releases its locks (REQ-34); a DNS server locked by another operation gives the warning instead of waiting.
- Several warnings (004 refresh and DNS refresh) are joined with `\n` in one `warning` field, shown with `whitespace-pre-line`. Rejected: changing the field to an array, which breaks 004 clients.
- A cancelled request answers nothing: the client is gone, so `writeError` neither writes nor logs. Rejected: the 499 status of nginx, which no client would read. The check lives in `queryVMStates` so every caller (`list_web`, `list_dns` and the unrecorded lists) behaves the same.
- `named` is the Debian 13 unit name for bind9 (`bind9` is an alias).
- Risk: `create-dns.sh` runs `create-vm.sh` (worst case 14 min) plus a power cycle (up to 4 min), which can pass the 15-minute timeout. Mitigation: SIGTERM runs the trap (process group); the demo measures it; the static-IP wait is bounded at 180 s.
- Risk: `arping` needs `CAP_NET_RAW`. Mitigation: `docs/guide-environment.md` documents `setcap cap_net_raw+ep $(command -v arping)`; a missing `arping` exits 4 with a clear message.
- Risk: bats stubs check commands, not effects, for bind9 and networking. Mitigation: the manual rows of section 7.
- Risk: the 004 home test expects four cards. Mitigation: it becomes the AC-54 test in the same change that adds the cards.

Amendment 2026-11-10:
- The record scripts take `<dns_cidr>` in place of `<dns_ip>`. Rejected: a separate subnet argument (two values that must agree) and a check only in the backend (AC-59 asks the script to exit 2).
- The role check stays in the backend; scripts check only the subnet and get the assigned servers as `drop_vms`. Rejected: reading the adapter type in the scripts, which makes them infer the role (section 3).
- `refresh-records.sh` is the only script that accepts a stored non-LAN IPv4, because it is the one that removes it. Add and remove get `zoneRecords`, so no script ever writes such an address (REQ-22). Rejected: deleting those rows inside add or remove, which the spec gives only to refresh (REQ-27). Until the first refresh after upgrading, a non-LAN row of an unassigned VM is listed on the DNS screen and is not in the zone.
- Removed VMs reach the history row through the args (`drop_vms`, record specs) and one stderr line of the script. Rejected: a new history column.
- `notice` is a new optional field of `ActionResult`, shown by `NoticeAlert`. Rejected: appending it to `message` (changes the 004 success text) or to `warning` (it is not a failure).
- `RemoveServer` also calls `DropServerRecord`, so "the VM has no record" (REQ-42) holds for a row stored before 2026-11-10; a failure there shows the REQ-41 warning.
- The refresh after a failed assign runs only when `assign-server.sh` was invoked; a rejection before the script changed nothing. Its error is logged and kept as its history row, never shown (REQ-26).
- `Refresh` reports "Registros actualizados" when a row was removed even with `reloaded=no`.
- Risk: a running VM that only reports a non-LAN address keeps its poll open for the whole 120 s window. Mitigation: the window is shared and bounded (AC-40); the stored LAN IP is kept (AC-59).
- Risk: with `APP_DNS_IP` inside `10.10.0.0/16` the subnet check alone cannot tell an internal address. Mitigation: the backend role check and `drop_vms` still apply; `docs/guide-environment.md` tells the team to keep `APP_DNS_IP` outside `10.10.0.0/16`.
- Risk: AC-37, AC-57, AC-58, AC-61 and AC-62 depend on VirtualBox, DHCP and bind9. Mitigation: the manual rows of section 7.

Amendment 2026-11-11:
- The resolver feature (US-7) is removed whole: `set-resolver.sh`, `unset-resolver.sh` and their bats, `resolver_program` and its test helpers, `dns_resolvers.go`, the `dns_resolvers` repository methods, `ResolversSetError`, the `PUT`/`DELETE /api/dns/resolvers/{vm}` routes, the `resolver` field of `DNSVMView` and `DnsVm`, and the `Switch` of `RecordTable`. Rejected: hiding the switch and keeping the backend (dead code that the spec no longer requires).
- The schema runs `DROP TABLE IF EXISTS dns_resolvers`, so an existing `stratus.db` loses the flags without a manual step. Rejected: leaving the table (an orphan the code never reads).
- `DeleteDNS` no longer checks resolvers: it runs `prepare/delete-vm.sh` directly (REQ-21, AC-29).
- The shadcn `switch.tsx` is deleted when no other component imports it.
- Risk: a VM whose resolver was set before the upgrade keeps `APP_DNS_IP` in its resolver list. Mitigation: bind9 keeps answering and forwarding, and the DHCP nameservers follow it; after the DNS server is deleted the VM falls back to them. Undoing the guest configuration by hand is out of scope.

Amendment 2026-11-11, zone view and table helpers:
- The zone is read from the file with `named-checkzone -D -o -` (canonical, absolute names). Rejected: AXFR (REQ-16 sets no `allow-transfer`) and `rndc dumpdb` (writes a file on the VM and needs a second read).
- The script prints tab-separated fields and the service only splits them, as `parseRecordRefresh`. The `awk` filter keeps only lines whose third field is `IN`, so the "loaded serial" lines some bind9 versions print on stdout never reach the backend.
- One `IpCell` renders every IP cell, so the "-" rule and the button live in one place. The copy fallback (`execCommand`) is kept because the UI may be opened over plain HTTP from the LAN, where `navigator.clipboard` does not exist.
- The "Zona" tab is mounted only while selected; `Dns.tsx` bumps `zoneVersion` in `reload`, so an open tab reads again after an operation (REQ-47).
- Risk: TXT values with several spaces would be collapsed by `awk`. Mitigation: the zone written by REQ-28 has no TXT records.
- Risk: the read holds the DNS server lock, so it fails while a refresh runs (up to 120 s). Mitigation: the "Operación en curso en esta VM" text tells the user to retry.

# Progress: 005 HTTPaaS

## Phase 1: Backend foundations
- Files: repository/{db,web_servers,dns}.go; service/{errors,dns_validation,web_validation,vm_status,prepare_vms}.go; handler/respond.go; cmd/server/config.go; .env.example; tests next to each.
- Decisions: `ErrWebBaseDiskMissing` split into `ErrApacheBaseDiskMissing` and `ErrNginxBaseDiskMissing` (fixed texts, simpler mapping). `ErrIndexMissing` → 422 like 003 `ErrExecutableMissing`. `ValidateDNSConfig` checks only IP, gateway, domain and forwarders; empty base disk is `ErrDNSBaseDiskMissing`. Gateway also may not equal the DNS IP.
- Decisions: `queryVMStates` returns `*repository.Operation` (row without module/operation/VM) instead of `RunResult`, so a start error and a timeout can be recorded too. `DNSConfig.Address()` gives the IP without prefix.
- Notes: repository tests are `TestAC1_WebAndDNSRepositories_Web`/`_DNS`. `ActionResult` keeps the JSON shape `{message, warning?}`. `WebTypeApache`/`WebTypeNginx` constants live in web_validation.go.

## Phase 2: Shared script libraries
- Files: prepare/lib/{vm-exec.sh,systemd-unit.sh}, services/lib/service-common.sh, prepare/{install,remove}-package.sh, web/lib/web-common.sh, dns/lib/dns-common.sh, web/ and dns/ test_helper.bash, prepare/test_helper.bash (scp stub), .bats next to each.
- Decisions: `dns-common.sh` sources `web-common.sh` for fail, parse_jump and the shared validators (no duplicate allowlists). The 3 new libraries carry `set -euo pipefail` so `assert_script_structure` passes (callers are already strict). `unit_action` prints "Servicio <unidad> <iniciado|detenido|reiniciado> en <vm>" itself. `apply_zone` returns 9 when the restore reload also fails; `ip_in_use` returns 4 when arping exits >1 (no cap_net_raw). `validate_cidr`/`validate_gateway` also reject IP = network/broadcast and gateway = IP, like Go.
- Notes: "arping missing" is tested with `hide_arping` (PATH of /usr/bin links without arping) because the host has a real arping; `ARPING=missing` from the plan is not used. RNDC=fail fails only the first reload, `failall` all. DNS stubs copy scripts/web too. `bats -r -j 8 scripts/` runs the suite in ~1 min.

## Phase 3: Create, list and delete web servers
- Files: scripts/web/create-web.{sh,bats}; service/{web_service,web_servers,prepare_service,prepare_vms,prepare_packages}.go + tests; handler/{web,prepare_test,web_test}.go; cmd/server/main.go.
- Decisions: `NewWebService` has no `*DNSService` parameter yet; it is added in Phase 6/7 (T080/T088). `NewPrepareService` takes `*WebService` (not the web repository): it reads the type through the package-private `WebService.webType`. create-web.sh also exits 6 when the VM is already registered in VirtualBox, so its cleanup never deletes a foreign VM. Exit 6 maps to the Apache/Nginx disk sentinel only when stderr starts with "Disco base de"; other 6s keep the script text.
- Decisions: CreateWeb frees the name only if the script did not succeed (the hook flags success), so a failed web_servers insert never orphans a live VM. Package argv order is `--jump`, `--frontend`, `--role`, vm, pkg, as in the scripts. DeleteVM still returns a string until T090.
- Notes: a non-web VM in DeleteWeb gives ErrVMNotFound. Web history ops: create_web, delete_web, list_web (VM "-"). Page archive limit reuses `maxArchiveBytes` (200 MiB).

## Phase 4: Install a web page
- Files: scripts/web/install-page.{sh,bats}; service/web_page{,_test}.go, web_service.go (`getWeb`); handler/{web,web_test,prepare_test}.go.
- Decisions: each remote step is its own `vm_exec` (greppable, one exit code each); swap and restore use `sh -c 'rm -rf /var/www/html && mv <src> /var/www/html'` with constant paths. On exit 9 the backup `/var/www/.stratus-page-bak` is KEPT (not removed) so the content can be recovered by hand; every other exit removes `.new`, the backup and the remote archive. Restore runs only after the backup exists (phase `armed`). A non-web VM gives ErrVMNotFound. The route reads a single multipart part `archive` (`readArchive`, new 400 texts for a missing part); size and extension texts come from 003 `saveArchive`.
- Notes: AC-13 bats reuse `make_bad_archive` by sourcing scripts/services/test_helper.bash inside that test only (it redefines `make_archive`). The handler test server now uses `maxTestArchive` (1 MiB) for web too.

## Phase 5: Web and bind9 service actions
- Files: scripts/web/{start,stop,restart,status}-web.{sh,bats}, scripts/dns/{start,stop,restart,status}-bind.{sh,bats}; service/web_actions{,_test}.go, web_service_test.go (`assignToLB`); handler/web{,_test}.go.
- Decisions: the 8 scripts are thin wrappers: validate, `--jump` route (web only), `showvminfo` → 3, then `unit_action`/`unit_status` whose code is the script's code. Bind scripts take only `<dns>` (the DNS server is never behind a LB), so `--jump` there is exit 2. `WebAction` rejects other actions with a ValidationError ("Acción no válida: debe ser start, stop o restart"); `unitActions` is shared for DNSAction later. No `mapWebError` on actions/status (no own codes).
- Notes: history ops `start_web`, `stop_web`, `restart_web` always; `status_web` only on failure. Routes reply `ServiceResult` (`{message, active_state?}`) and `{output}`.

## Phase 6: DNS server (US-4)
- Files: `scripts/dns/create-dns.{sh,bats}`, `scripts/dns/test_helper.bash` (`stub_dns_vm`); `service/dns_service.go`, `dns_server.go` (+ tests); `handler/dns.go` (+ test); constructors of Web, HAProxy, Prepare; `main.go`.
- Decisions: `CreateDNS` maps its codes with `mapCreateDNSError` (6 disk, 13 IP in use); `mapDNSError` (7 → DNS off) is only for the record scripts of Phase 7. Initial records come from `list-vms.sh`; the DNS server's own A line is rendered by `render_zone`, never stored in `dns_records`.
- Decisions: `lbTopology` extracted to `haproxy_topology.go`, shared by `ListVMs` and `Overview`. `DNSVMView.role` is the 004 role (vm, lb, server); `kind` is the web type.
- Notes: `NewHAProxyService` takes `dnsRepo` after `lbs`; `NewWebService` and `NewPrepareService` take `*DNSService` before the logger (`WebService.dns` is still unused until T088). Test envs: `newDNSEnv` wraps `webEnv` (`env.svc` is the DNSService, `env.dnsSvc` is shared); handler `testServer.dns` and `registerDNSServer`.
- Notes: `DeleteDNS`/`DeleteVM` still return string; `ActionResult` comes in T090.

## Phase 7: Add and remove A records (US-5)
- Files: `scripts/dns/{add-records,remove-record}.{sh,bats}`; `service/dns_records{,_test}.go`; `web_servers.go`, `prepare_vms.go`, `haproxy_lbs.go`, `haproxy_service.go` (+ tests); `handler/{dns,prepare,haproxy}.go` (+ test); `main.go`.
- Decisions: `AddRecords` sends one `add-records.sh` with the full set (stored + accepted, sorted by VM); if none is accepted it returns the first rejection's error (409/404), else 200 `{message, results}`. `RemoveRecord` on a VM without record succeeds without a script.
- Decisions: `CreateWeb` split into `createWeb` (holds the create lock) + DNS hook after unlock (`AddRecordsFor`); failure or missing `ip=` with a DNS server → warning. `AfterVMDeleted` clears the resolver, deletes the row first, then runs `remove-record.sh` (nil hook); failure → REQ-24 warning, next refresh renders from SQLite.
- Decisions: hook rejections before the script (lock taken, config invalid) are saved by `recordRejected` as failed history rows (exit -1).
- Notes: `NewHAProxyService(..., locks, dns *DNSService, logger)`; `main.go` builds DNSService before HAProxy. `DeleteVM` and `DeleteLB` return `ActionResult` (`{message, warning?}`).

## Phase 8: Follow DHCP changes (US-6)
- Files: `scripts/dns/refresh-records.{sh,bats}`; `service/dns_records.go` (`Refresh`, `RefreshVM`, `RefreshAll`, `parseRecordRefresh`, `refreshWarning`, `joinWarnings`), `prepare_vms.go`, `haproxy_servers.go` (+ tests); `handler/{dns,haproxy}.go` (+ test).
- Decisions: `refresh-records.sh` exits 7 at once if the DNS server is not `running` (no ssh, no polls); otherwise `wait_ips <dns> <running wait VMs>` shares one 60×2 s window. Wait VMs without a record are ignored. Output keeps the received record order.
- Decisions: `RefreshVM` is a no-op without DNS server, without record, or for the DNS server itself; `RefreshAll` waits for every record VM (`-` if none). Hook rejections (lock, config) go to `recordRejected` like Phase 7. `Refresh` message: "Registros actualizados" / "Registros sin cambios".
- Decisions: `AssignServer`/`RemoveServer` split into private `assignServer`/`removeServer` + `afterServerChange` (hook after unlock), returning `ActionResult`; handlers write `{message, warning?}`.
- Notes: the REQ-26 warning names the started/assigned VM, also for the DNS server start; joined to the 004 warning with `\n`. Test helper `fakeRunner.scriptOrder()` lives in `prepare_vms_test.go`.

## Phase 9: DNS server as resolver (US-7)
- Files: `scripts/dns/{set,unset}-resolver.{sh,bats}`, `scripts/dns/lib/dns-common.sh` (section 8 `resolver_program`), `scripts/dns/test_helper.bash` (`resolver_program_file`, `run_resolver_program`); `service/dns_resolvers{,_test}.go`, `prepare_vms_test.go`; `handler/dns{,_test}.go`.
- Decisions: one POSIX sh program sent by stdin (`vm_exec <vm> sh -s stratus-resolver <set|unset> <ip>`, root over ssh, no sudo). dhclient block = marker line + `prepend` line (removed with `sed '/^# stratus-resolver$/,+1d'`); dhcpcd hook prepends only when not first. Remote "no client" exits 3, which `vm_exec` turns into 5.
- Decisions: bats run the captured program on a fake root (paths rewritten, PATH limited to basic tools plus an empty dhclient/dhcpcd), so the files it writes are checked without a VM.
- Decisions: `SetResolver`/`UnsetResolver` need the DNS server and valid config, a registered VM that is not the DNS server ("El servidor DNS no puede ser su propio resolvedor"); `targetArgs` adds `--jump`; lock = target VM; exit 7/12 keep the generic 001/004 texts (no `mapDNSError`).
- Notes: T109 passed at once: `AfterVMDeleted` (Phase 7) already clears the flag. Routes `PUT`/`DELETE /api/dns/resolvers/{vm}` reply `{message}`.

## Phase 10: Locks and history across modules (US-8, US-9)
- Files: `service/dns_service_test.go` (TestAC50_CrossModuleLocks, TestAC51_DNSRecordPolicy, TestAC53_DNSHistoryLimit, `countOps`), `service/web_service_test.go` (TestAC51_WebRecordPolicy, TestAC53_WebHistoryLimit).
- Decisions: AC-51 and AC-53 tests are split per module (`_Web…`/`_DNS…`) because both files share the package. The policy tests also run 20 successful list reads (`ListWebServers`, `Overview`) to check `list_web`/`list_dns` stay out.
- Notes: T114 needed no code change: every test passed on the first run (also with `-race`). No script changed, so bats were not rerun in this phase (0 failures at Phase 9).

## Phase 11: Kinds and warnings in the 001 and 004 screens (US-5, US-6, US-8)
- Files: `features/prepare/{types,api}.ts`, `VmSelector.tsx`, `VmActions.tsx`; `features/haproxy/{api.ts,AssignServerForm,ServerTable,LbActions}.tsx`; `features/services/StateWarning.tsx`; new `components/WarningAlert.tsx`; tests and every `Vm` fixture (`kind: ""`).
- Decisions: `StartResult` renamed `ActionResult` (start, delete VM, delete LB, assign, remove). New shared `WarningAlert` (title "Advertencia", `whitespace-pre-line`) replaces the inline Alert of `VmActions`; Phases 12-13 reuse it. `KIND_LABELS` exported from `VmSelector.tsx`; an LB keeps " - Balanceador", other kinds add " - Servidor web (Nginx|Apache)" / " - Servidor DNS".
- Notes: `StateWarning` `hint` defaults to "revise Estado y Logs" (003); web/dns pass "revise Estado". Each component clears its warning when a new action starts.

## Phase 12: Web servers screen (US-1, US-2, US-3)
- Files: new `features/web/{types,api,validation}.ts`, `CreateWebForm`, `UnitButtons`, `InstallPageDialog`, `WebTable` (+ tests), `routes/Web.tsx` (+ test); `App.tsx` route `/web`.
- Decisions: `UnitButtons` takes `name`, `unit` (apache2|nginx|named, used in the StateWarning text), `busy`, `onAction`, `onStatus`, `onDone` and `children` (extra buttons in the same `role="group"` "Acciones de <vm>"), so Phase 13 reuses it with `dnsAction`/`dnsStatus`. A failed Estado shows a failure Alert; a good one only the panel.
- Decisions: `InstallPageDialog({vm|null, onClose, onDone})` copies the 003 two-step redeploy dialog (file, then warning "Se reemplazará todo /var/www/html de <vm>"); multipart has only the `archive` part. `validatePageFile` reuses `validateArchive` with its own missing-file text.
- Decisions: `WebTable` uses a data row plus a full-width actions row per server; each component owns its `useOperation`, and the screen's `reloading` flag is the shared `busy` (the backend locks reject overlaps anyway).
- Notes: UI 247 tests pass; typecheck and build OK. Phase 13 needs a shadcn `Switch` (not installed yet).

## Phase 13: DNS screen and home (US-4, US-5, US-6, US-7)
- Files: new `features/dns/{types,api}.ts`, `CreateDnsForm`, `DnsServerPanel`, `RecordTable` (+ tests), `routes/Dns.tsx` (+ test), `components/ui/switch.tsx` (shadcn); `App.tsx` route `/dns`; `features/home/modules.ts` and `Home.test.tsx` (six cards).
- Decisions: `Dns.tsx` reads only `GET /api/dns` and the history (the overview already has every VM, so no `listVms`); it waits for the first read before choosing form or panel. Without a server the records card is hidden.
- Decisions: `DnsServerPanel` reuses `UnitButtons` (unit `named`) with "Actualizar registros" and "Eliminar" as extra buttons; a successful delete also calls `toast.success` because the panel unmounts on reload. `RecordTable` has a checkbox only for VMs without record, sends them in table order, lists `results` as "<vm>: <message>" under `aria-label="Resultado por VM"`, and uses a controlled `Switch` "Usar como resolvedor en <vm>" (PUT on, DELETE off).
- Notes: shadcn generated `switch.tsx` with `import { cn } from "cn"`, the same as the other ui files. UI 262 tests pass; typecheck and build OK.

## Phase 14: Docs and checks (polish)
- Files: `docs/guide-base-images.md` (five images; new sections 8 Apache/Nginx and 9 DNS with verification and exit codes), `docs/guide-environment.md` (`arping` + `cap_net_raw` prerequisite, the seven 005 variables, sections 2.3 and 2.4, checks).
- Decisions: the new image sections are appended as 8 and 9 instead of renumbering, so the existing "Go to section 5" links stay valid. Web images start from the server image (they can be assigned to an LB); the DNS image from the common part.
- Notes: T148 green: gofmt clean, golangci-lint 0 issues, `go test ./...` ok, bats 682 ok / 0 not ok, UI typecheck, 262 tests and build ok.
- Notes: T149 (web demo) and T150 (DNS demo) are manual real-host demos and stay pending: they need VirtualBox, the three new base disks, `APP_DNS_*` and `arping` with `cap_net_raw` on the host.

## Phase 15: Amendment 2026-11-10: documents and LAN rule foundations
- Files: `scripts/dns/lib/dns-common.{sh,bats}` (`ip_in_subnet`, `validate_lan_records`); `service/{dns_validation,errors}.go` (`DNSConfig.OnLAN`, `ErrVMNotOnLAN`, `ServerRecordError`) + tests; `handler/respond.go` (+ test); `repository/dns.go` (`DeleteRecords`) + test. The three amended docs went in their own commit (T151).
- Decisions: `ip_in_subnet` prints nothing and returns 1 for any non-IPv4 or a bad prefix (0-32 accepted); `validate_lan_records` reads the IP after the first comma and prints the 003-style text on stderr. `ServerRecordError` is matched with `errors.As` before the sentinel loop in `writeError`.
- Notes: `wait_ips` still has its old `<vm...>` signature; Phase 17 changes it. In this shell the default `go` mismatches GOROOT: put `~/.local/share/mise/installs/go/1.27.3/bin` first in PATH to run tests.

## Phase 16: Records only with LAN addresses on add, remove and DNS creation
- Files: `scripts/dns/{add-records,remove-record,create-dns}.{sh,bats}`; `service/dns_records.go`, `dns_server.go` (+ tests); argv assertions in `web_servers_test.go`, `prepare_vms_test.go`, `haproxy_lbs_test.go`.
- Decisions: add and remove take `<dns_cidr>` (`validate_cidr`, then `dns_ip` is the part before `/`); the services pass `s.cfg.IP`. `zoneRecords` filters by `OnLAN(ip)` only (not by `lb_servers`), as the plan says. `AddRecordsFor` with a non-LAN IP records a rejected `add_records` row and returns `ErrVMNotOnLAN`. The `CreateDNS` message now reads "Servidores web sin registro DNS (sin IPv4 en la LAN o asignados a un balanceador): ...".
- Notes: refresh still uses `s.cfg.Address()` and the old argv (the three refresh argv assertions keep `192.168.1.53`); Phase 17 changes them. `bats -r scripts/dns/` 93 ok.

## Phase 17: Refresh keeps LAN addresses and removes the other records
- Files: `scripts/dns/lib/dns-common.{sh,bats}` (`wait_ips <ip/prefix> <vm...>`); `scripts/dns/refresh-records.{sh,bats}`; `service/dns_records.go` + tests (`dns_records_test.go`, `prepare_vms_test.go`, `haproxy_servers_test.go` argv assertions).
- Decisions: new argv `<dns> <domain> <dns_cidr> <wait_vms|-> <drop_vms|-> [record...]`. A record in drop_vms is removed and never polled; a polled LAN IP replaces the stored one; otherwise the stored IP stays if it is in the LAN, else the record is removed (also when the VM is off). stdout: kept `<vm> <ip>`, then `removed=<vm>` in received order, then `reloaded=`; stderr `Registros quitados: a, b`.
- Go: `refresh()` builds drop_vms from `lbTopology` (record VMs in `serverLB`) and passes `s.cfg.IP`; `parseRecordRefresh` also returns removed VMs; the save hook runs `UpdateRecordIPs` then `DeleteRecords`. `Refresh` message: "Registros actualizados; registros quitados: vm2, vm3".
- Notes: `TestAC37_AssignAndRemoveRefreshRecord` now expects `web-a` also in drop_vms (the server is already assigned when the hook runs); Phase 18 rewrites it. `s.cfg.Address()` is no longer used. `bats -r scripts/dns/` 97 ok.

## Phase 18: Records on assign and remove
- Files: `scripts/haproxy/remove-server.{sh,bats}` (deletes both guest IP properties after `modifyvm`, before `startvm`); `service/dns_records.go` (`DropServerRecord`, `dropRecordRowFirst` shared with `AfterVMDeleted`, three text helpers); `service/prepare_vms.go` (`ActionResult.Notice`); `service/haproxy_servers.go` (assign and remove hooks, `afterServerChange` removed); tests in `dns_records_test.go`, `haproxy_servers_test.go`, `handler/haproxy_test.go`.
- Decisions: `DropServerRecord` returns `removed=true` whenever the row was deleted, even if the zone step failed (the caller then shows the warning, never the notice). `assignServer` also returns `ran` (false for validation errors or `ErrOperationInProgress`); only `ran && err` triggers `RefreshVM`. The remove hook shows the "no tiene registro" notice whenever a DNS server exists, also next to the REQ-41 warning.
- Notes: T188 needed no handler code: the handlers already `writeJSON` the whole `ActionResult`, so `notice` appears via its JSON tag; T187 only adds the test. `refreshWarning` is still used by the start hook. Phase 19 (UI, views) is next.

## Phase 19: Screens: hidden records of assigned servers and notices
- Files: `service/web_servers.go`, `dns_server.go` (+ tests); `stratus-ui/src/components/NoticeAlert.{tsx,test.tsx}`; `features/prepare/api.ts` (`notice?`); `features/dns/RecordTable.{tsx,test.tsx}`; `features/haproxy/{AssignServerForm,ServerTable}.{tsx,test.tsx}`; `features/dns/DnsServerPanel.test.tsx`; `features/web/WebTable.test.tsx`.
- Decisions: `ListWebServers` checks the assignment first and only looks up the record when the server is not assigned (`fqdn` "" otherwise). `Overview` clears `record_ip` for role `server` (so `fqdn` stays ""). `RecordTable` uses one `selectable` predicate (no record and role not `server`) for the checkbox and the chosen list. Assign and remove forms keep separate `notice` and `warning` state.
- Notes: T200 needed no component change: `DnsServerPanel` already shows the whole refresh message and `WebTable` already prints "-" for an empty `fqdn`. I wrote the UI notice and warning tests together with the component edits, so I did not watch those UI tests fail first; the Go tests and the AC-31 RecordTable test were seen failing first. `pnpm test` 269 ok. Phase 20 remains (guide, final verification, manual demos T149/T150/T204).

## Phase 20: Amendment verification
- Files: `docs/guide-environment.md` (section 2.4: records only inside the subnet of `APP_DNS_IP`, `APP_DNS_IP` outside `10.10.0.0/16`, one "Actualizar registros" after upgrading removes records stored with internal addresses).
- Decisions: none beyond the guide text.
- Notes: T203 green: gofmt clean, golangci-lint 0 issues, `go test ./...` ok, `pnpm typecheck`, 269 tests and `pnpm build` ok, `bats -r -j 8 scripts/` exit 0 with 694 ok / 0 not ok. T204 (real-host demo of the amendment) is manual and stays unchecked, like T149 and T150: it needs VirtualBox, the base disks, `APP_DNS_*` and `arping` with `cap_net_raw`.

## Phase 21: Amendment 2026-11-11: remove the DNS server as resolver
- Files: `repository/{db,dns,dns_test}.go`, new `repository/db_test.go`; `service/{dns_server,dns_records,dns_service,errors,prepare_vms}.go` + tests, `dns_resolvers{,_test}.go` deleted; `handler/{dns,respond}.go` + tests; `scripts/dns/{set,unset}-resolver.{sh,bats}` deleted, `lib/dns-common.sh` (section 8 gone), `test_helper.bash`; `features/dns/{types,api}.ts`, `RecordTable.{tsx,test.tsx}`, `DnsServerPanel.test.tsx`, `routes/Dns.{tsx,test.tsx}`, `features/home/modules.ts`; `components/ui/switch.tsx` deleted (no other user).
- Decisions: the schema runs `DROP TABLE IF EXISTS dns_resolvers` (TestOpenDropsDNSResolvers). TestNoResolverRoutes accepts 404 or 405 for `PUT`/`DELETE /api/dns/resolvers/vm1`. The DNS screen and home card texts no longer mention the resolver; the records card is titled "Registros". The AC-29 panel test now uses a generic 409 text.
- Notes: gofmt clean, golangci-lint 0 issues, `go test ./...` ok, `bats -r -j 8 scripts/dns/` 84 ok, UI typecheck, 269 tests and build ok. VMs whose resolver was set before the upgrade keep their guest configuration (out of scope, plan section 9).


## Phase 22: Amendment 2026-11-11: zone read script and backend
- Files: `scripts/dns/show-zone.{sh,bats}`, `scripts/dns/test_helper.bash` (`ZONE_DUMP`); `service/{errors,dns_zone}.go` + tests; `handler/{dns,respond}.go` + tests. The amended docs went in their own commit (T215).
- Decisions: `show-zone.sh` reads with `named-checkzone -D -o -` and filters with `awk` on the host (third field `IN`, numeric TTL), so "loaded serial"/"OK" lines never reach the backend. `Zone` returns `[]` (never nil) for an empty zone. A malformed line is an error, not a partial zone. `ErrDNSOffZone` is a separate sentinel because `ErrDNSOff` talks about changing records.
- Notes: the exit-7 and exit-3 bats rebuild the stubs first (stub answers are appended, the first match wins). shellcheck is not installed on this host. `bats -r -j 8 scripts/dns/` 90 ok; gofmt clean, golangci-lint 0 issues, `go test ./...` ok.

## Phase 23: Amendment 2026-11-11: copy buttons and links
- Files: new `stratus-ui/src/components/{CopyButton,IpCell}.tsx` and `CopyButton.test.tsx`; `features/web/WebTable`, `features/haproxy/{LbTable,ServerTable}`, `features/dns/RecordTable` (`.tsx` and `.test.tsx`).
- Decisions: `CopyButton` exposes `data-copied` for the check state (tests read it; the accessible name stays "Copiar <ip>"). The fallback without `navigator.clipboard` focuses a hidden readonly `textarea` and calls `execCommand("copy")`; a false return or a rejected `writeText` shows the toast. `IpCell` renders a bare "-" so the existing "-" cell assertions still hold. The FQDN link uses the `link` button classes without `Button` (a plain `<a>`).
- Notes: UI typecheck ok, 279 tests pass.

## Phase 24: Amendment 2026-11-11: zone tab
- Files: new `stratus-ui/src/components/ui/tabs.tsx` (shadcn CLI), `features/dns/ZoneTable.{tsx,test.tsx}`; `features/dns/{types,api}.ts` (`ZoneRecord`, `getZone`); `routes/Dns.{tsx,test.tsx}`.
- Decisions: the tabs panel unmounts when hidden, so `ZoneTable` reads the zone on mount, which is when "Zona" opens. `Dns.reload` increments `zoneVersion`; `ZoneTable` reads again on each new version, so an operation with "Zona" open re-reads it and a closed tab does nothing. `ZoneTable` drops a response that arrives after a newer read. The A values use `IpCell`, so they get the copy button. A failed read clears the table and shows the error in an `Alert`.
- Notes: the spinner inside "Recargar" adds "Cargando" to its accessible name while reading, so the loading test matches `/Recargar/`. The AC-67 route test uses "Actualizar registros" as the operation, since add and remove live in the hidden "VMs" tab. UI typecheck ok, 287 tests and build ok.

## Phase 25: Amendment 2026-11-11 verification
- Files: none beyond `tasks.md` and this file.
- Decisions: none.
- Notes: T238 green: gofmt clean, golangci-lint 0 issues, `go test ./...` ok, `bats -r -j 8 scripts/dns/` 90 ok / 0 not ok, UI typecheck, 287 tests and `pnpm build` ok. T239 (real-host demo AC-70) is manual and stays unchecked, like T149, T150 and T204: it needs VirtualBox, the DNS server VM and `APP_DNS_*`.

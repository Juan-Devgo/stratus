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

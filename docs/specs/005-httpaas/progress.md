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

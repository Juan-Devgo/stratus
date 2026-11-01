# Progress: 005 HTTPaaS

## Phase 1: Backend foundations
- Files: repository/{db,web_servers,dns}.go; service/{errors,dns_validation,web_validation,vm_status,prepare_vms}.go; handler/respond.go; cmd/server/config.go; .env.example; tests next to each.
- Decisions: `ErrWebBaseDiskMissing` split into `ErrApacheBaseDiskMissing` and `ErrNginxBaseDiskMissing` (fixed texts, simpler mapping). `ErrIndexMissing` → 422 like 003 `ErrExecutableMissing`. `ValidateDNSConfig` checks only IP, gateway, domain and forwarders; empty base disk is `ErrDNSBaseDiskMissing`. Gateway also may not equal the DNS IP.
- Decisions: `queryVMStates` returns `*repository.Operation` (row without module/operation/VM) instead of `RunResult`, so a start error and a timeout can be recorded too. `DNSConfig.Address()` gives the IP without prefix.
- Notes: repository tests are `TestAC1_WebAndDNSRepositories_Web`/`_DNS`. `ActionResult` keeps the JSON shape `{message, warning?}`. `WebTypeApache`/`WebTypeNginx` constants live in web_validation.go.

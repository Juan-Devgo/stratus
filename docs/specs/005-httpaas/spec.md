# HTTPaaS: web servers and DNS

## 1. Metadata
- Status: Approved
- Updated: 2026-11-01
- Constitution version: 2.1.0
- Required by: none

## 2. Constitution relationship
- Applies:
  - P1: the backend only validates input (including the uploaded archive's size and extension), invokes one script per operation and reports the result; VM creation, page transfer and extraction, bind9 zone changes, resolver changes and systemctl run only in scripts.
  - P2: each operation has its own script file with `set -euo pipefail`, explicit arguments and a documented exit-code table; the page archive reaches the script as the path of a temporary host file given as an argument (as 003 REQ-12); record refresh, resolver set and unset, and service actions are idempotent.
  - P3: VM name, memory, CPUs, web server type, selected VMs, and the `APP_DNS_*` values are validated against an allowlist in the UI/backend and again in the script, and passed as discrete arguments.
  - P4: web and DNS servers are bootstrapped by Guest Additions as in 001 REQ-30 and afterwards reached only by SSH with the backend key; web servers assigned to a load balancer are reached through its `jump` (004 REQ-19).
  - P5: no private key is created by this module; keys of 001 and 002 keep their rules.
  - P6: every operation ends with a success or failure message; the history keeps command, exit code, stderr and timestamp of every change and every failed read.
  - P7: two home cards, "Servidores web" and "DNS", each open one screen; forms confirm the result.
  - P8: each script runs on its own with bats tests; a before/after demonstration on the real host covers what stubs cannot (AC-45 to AC-49).
  - P9: reuses the VM creation, VM selector, script runner, operation runner, per-VM locks, history, archive upload and archive validation of 001-004; one site per web server, one DNS server, one zone.
- Conflicts: none with constitution 2.1.0 (mission amended on 2026-10-31 to add these two modules). This spec supersedes these requirements of the approved specs 001-004 for web servers, the DNS server and VMs with a DNS record; the supersessions are declared only here and 001-004 stay unchanged:
  - 001 REQ-2 (selector labels): extended by REQ-6 and REQ-17.
  - 001 REQ-3 (delete): extended by REQ-24 and REQ-30.
  - 001 REQ-19, 002 AC-27, 003 AC-35, 004 AC-36 (four home cards): superseded by REQ-37.
  - 001 REQ-25 (package protections): superseded by REQ-7 for web servers and REQ-18 for the DNS server.
  - 001 REQ-1 and AC-26 (bridged adapter with DHCP): superseded by REQ-14 for the DNS server.
  - 001 start (US-1) and 004 REQ-10, REQ-11 (assign and remove): extended by REQ-26.
  - 004 REQ-8 (VMs that can be assigned): superseded by REQ-19.

## 3. User stories
| ID | Priority | Actor | Story |
|---|---|---|---|
| US-1 | P1 | Student admin | As a student admin, I need to create and delete Apache or Nginx web server VMs from their base disks, so I can host sites without installing a web server by hand |
| US-2 | P1 | Student admin | As a student admin, I need to install a static web page on a web server from a `.tar.gz` archive and replace it later, so I can publish and update a site without a terminal |
| US-3 | P1 | Student admin | As a student admin, I need start, stop, restart and status for the web service and for bind9, so I can control them without a terminal |
| US-4 | P1 | Student admin | As a student admin, I need to create one DNS server with a static IP that is authoritative for the local zone, so VMs have stable names |
| US-5 | P1 | Student admin | As a student admin, I need A records added automatically for new web servers and manually for any VM I select (including load balancers), and to remove them, so every VM can be reached by name |
| US-6 | P1 | Student admin | As a student admin, I need A records to follow the VMs' DHCP addresses after every start, assign or remove, so names keep resolving without manual edits |
| US-7 | P2 | Student admin | As a student admin, I need to set or unset the DNS server as resolver of each VM, so VMs resolve the local zone and still reach internet names |
| US-8 | P1 | Student admin | As a student admin, I need web servers and the DNS server to work with modules 001-004 like any other VM, so I can prepare them, manage users and services on them and put web servers behind a load balancer |
| US-9 | P2 | Student admin | As a student admin, I need to see the history of operations of each module, so I can diagnose failures |

## 4. Requirements

### Web servers
- REQ-1 (US-1): The "Servidores web" screen has a "Crear servidor web" form with name, memory and CPUs, validated as in 001 REQ-1 (same regex, ranges, uniqueness among registered VMs and messages), and a type `Apache` or `Nginx` (no default; required).
- REQ-2 (US-1): A web server is a VM whose disk is a differencing disk on top of the multi-attach base disk `APP_APACHE_BASE_DISK` (type Apache) or `APP_NGINX_BASE_DISK` (type Nginx), each Debian 13 with Guest Additions, openssh-server and the web server installed, enabled at boot, prebuilt by the team with the same root password as the 001 base disk.
- REQ-3 (US-1): Creating a web server applies the 001 creation rules: bridged adapter with DHCP (001 REQ-1), bootstrap of 001 REQ-30 with `APP_GUEST_PASSWORD_FILE` (001 REQ-22), precondition checks and cleanup of 001 REQ-31 (with "Disco base de Apache no encontrado" or "Disco base de Nginx no encontrado" when the matching disk does not exist), and the single-creation rule of 001 REQ-32 shared with every VM and load balancer creation.
- REQ-4 (US-1, US-2): Each web server serves one site on port 80 for every request, with server name `<vm name>.<APP_DNS_DOMAIN>`, from document root `/var/www/html`. A new web server serves the base disk's default page. HTTPS is not configured.
- REQ-5 (US-1, US-5): When a DNS server exists, creating a web server adds its A record (REQ-22) in the same operation after the VM succeeds. If adding the record fails or is rejected (for example, the DNS server is off or locked), the creation is reported as a success, the record add is recorded as its own failed row in the DNS history, and the UI shows the warning "Servidor web creado; no se pudo registrar en DNS, agréguelo desde DNS". When no DNS server exists, no record is added and no warning appears.
- REQ-6 (US-1, US-8): The "Servidores web" screen lists only web servers with name, type, state, IPv4 as shown by 001 ("-" when off), FQDN (`<vm name>.<APP_DNS_DOMAIN>` when it has a record, otherwise "-") and the load balancer it is assigned to ("-" when none). Web servers appear in the 001 VM selector with the label "Servidor web (Apache)" or "Servidor web (Nginx)", so every 001, 002 and 003 operation works on them.
- REQ-7 (US-8): On a web server, 001 rejects removing its web server package with "No se puede eliminar apache2: el servidor web lo usa" or "No se puede eliminar nginx: el servidor web lo usa", rejects installing the other web server package with "No se puede instalar nginx: el servidor web usa apache2" or "No se puede instalar apache2: el servidor web usa nginx", and installing ufw also allows 80/tcp (supersedes 001 REQ-25 for web servers).
- REQ-8 (US-1): Each listed web server has a delete button that, after a confirmation naming it, follows 001 REQ-3 and removes its A record (REQ-30). It is rejected with "La VM es servidor de un balanceador; quítela primero" while it is assigned (004 REQ-12).

### Web page
- REQ-9 (US-2): Each listed web server has an "Instalar página" form with one `.tar.gz` archive uploaded from the browser, at most 200 MB, with the size, extension and invalid-archive rules and messages of 003 REQ-4 and REQ-5; the backend stores it in a temporary host file passed to the script and deletes it when the operation ends (003 REQ-12). The archive must hold a regular file `index.html` at its root; otherwise the install is rejected with "El archivo debe contener index.html en la raíz" and nothing changes on the VM.
- REQ-10 (US-2): Installing a page runs after a confirmation warning that the whole content of `/var/www/html` is replaced; it copies the archive to the VM with scp, replaces the whole content of `/var/www/html` with the archive's content, owned by `root:root` with directories 755 and files 644, without stopping or restarting the web service. Before replacing, it backs up the current content on the VM; if copy, extraction or ownership fails, the previous content is restored, and the backup is deleted when the operation ends, success or failure.
- REQ-11 (US-2): Installing a page on a web server assigned to a load balancer reaches it through the jump of 004 REQ-19 with the same off-state messages.

### Service actions
- REQ-12 (US-3): Each listed web server has buttons Iniciar, Detener, Reiniciar and Estado for its web service (`apache2` or `nginx`); the DNS screen has the same buttons for bind9 on the DNS server. An action that leaves the state unchanged succeeds. About 3 seconds after Iniciar or Reiniciar succeeds, the active state is read and shown; any state other than `active` is shown as a warning pointing to Estado (as 003 REQ-15). Estado shows the output of `systemctl status` in a panel; an inactive or failed service is shown, not treated as an operation failure.

### DNS server
- REQ-13 (US-4): The "DNS" screen has a "Crear servidor DNS" form with name, memory and CPUs, validated as in 001 REQ-1. At most one DNS server exists; while one is registered, creation is rejected with "Ya existe un servidor DNS" and no script is invoked. The DNS server is a VM on a differencing disk on top of `APP_DNS_BASE_DISK` (Debian 13 with Guest Additions, openssh-server and bind9 installed, enabled at boot, same root password, prebuilt by the team), with the creation rules of REQ-3 ("Disco base de DNS no encontrado" when the disk does not exist).
- REQ-14 (US-4): The DNS server has one adapter bridged to `APP_BRIDGE_ADAPTER` with the static IPv4 `APP_DNS_IP` (CIDR, for example `192.168.1.53/24`) and gateway `APP_DNS_GATEWAY`, kept across reboots; the team chooses an address outside the LAN router's DHCP pool. The creation succeeds only when the backend key opens an SSH session at that address (supersedes the DHCP address of 001 REQ-1 for the DNS server).
- REQ-15 (US-4): Creation fails without registering any VM with "Configuración de DNS incompleta" when `APP_DNS_IP`, `APP_DNS_GATEWAY`, `APP_DNS_DOMAIN` or `APP_DNS_FORWARDERS` is missing or invalid (`APP_DNS_IP` not an IPv4 CIDR with prefix 8-30, gateway not an IPv4 inside that subnet, domain not 1-4 labels matching `^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$` joined by dots, forwarders not 1-3 comma-separated IPv4 addresses), and with "La IP del servidor DNS ya está en uso" when the address of `APP_DNS_IP` answers on the LAN before creation: the script sends one ARP probe on `APP_BRIDGE_ADAPTER` and one ICMP echo, each with a 1-second timeout, and any reply means in use.
- REQ-16 (US-4, US-7): bind9 is authoritative for the zone `APP_DNS_DOMAIN` (default `stratus.lan`), whose NS record names the DNS server's own A record. For names outside the zone it forwards queries to `APP_DNS_FORWARDERS`, answering only clients in the DNS server's LAN subnet and `10.10.0.0/16` (004 internal networks reached through their NAT adapter); other clients are refused.
- REQ-17 (US-4, US-8): The DNS server appears in the 001 VM selector with the label "Servidor DNS", so every 001, 002 and 003 operation works on it; the DNS screen shows its name, state, IPv4 and zone.
- REQ-18 (US-8): On the DNS server, 001 rejects removing bind9 with "No se puede eliminar bind9: el servidor DNS lo usa", and installing ufw also allows 53/tcp and 53/udp (supersedes 001 REQ-25 for the DNS server).
- REQ-19 (US-8): The DNS server cannot be assigned to a load balancer; the request is rejected with "La VM no se puede asignar a este balanceador" and nothing changes (supersedes 004 REQ-8 for the DNS server). Web servers can be assigned like any other VM.
- REQ-20 (US-4, US-5): Creating the DNS server, in the same operation, adds its own A record (with `APP_DNS_IP`) and an A record for every existing web server that reports an IPv4; web servers without an IPv4 are skipped and named in the result, and the creation still succeeds.
- REQ-21 (US-4): Deleting the DNS server (from the DNS screen or from 001) follows 001 REQ-3 after a confirmation and drops every stored record. It is rejected with "Quite primero el resolvedor DNS de las VMs: <vm list>" while any VM uses it as resolver (REQ-33), including a powered-off one, and changes nothing; the user starts that VM and unsets its resolver, or deletes it.

### A records
- REQ-22 (US-5): An A record is named `<vm name>.<APP_DNS_DOMAIN>` with TTL 60 seconds and points to the VM's IPv4 as shown by 001 (the internal `10.10.N.x` address for a server assigned to a load balancer, 004 REQ-13; the bridged address for a load balancer). A VM has at most one A record; the record name does not change when the VM's hostname changes.
- REQ-23 (US-5): The DNS screen lists every registered VM except the DNS server with name, type label, FQDN ("-" when it has no record), IP in its record ("-" when none) and resolver (Sí/No). The user can select one or more VMs without a record and click "Agregar registros"; each selected VM that reports no IPv4 is rejected with "La VM no tiene dirección IP; verifique que esté encendida" while the others are added, and the result lists the outcome per VM. A VM that already has a record is rejected with "La VM ya tiene un registro DNS".
- REQ-24 (US-5): Each VM with a record has a "Quitar registro" button that, after a confirmation, removes it; removing a record already gone succeeds. Deleting a VM in 001 or in the web or HAProxy modules removes its record after the VM is deleted; if removing the record fails, the delete is reported as a success, the failure is recorded as its own row in the DNS history, and the UI shows the warning "VM eliminada; no se pudo quitar su registro DNS, use Actualizar registros".
- REQ-25 (US-5, US-6): Adding, removing or refreshing records ("Actualizar registros") when no DNS server exists is not offered; when the DNS server is off, the operation fails with "El servidor DNS está apagado; enciéndalo para cambiar los registros" and changes nothing.
- REQ-26 (US-6): After every successful 001 start of a VM with a record, and after every successful 004 assign or remove of a VM with a record, the backend waits up to 120 seconds for the VM's IPv4 and updates its record if it changed. When the start, assign or remove succeeds but the update fails or is rejected, the operation is reported as a success, the update is recorded as its own failed row in the DNS history, and the UI shows the warning "Operación completada; no se pudo actualizar el registro DNS de <vm>, use Actualizar registros" (extends 001 start and 004 REQ-10, REQ-11).
- REQ-27 (US-6): The DNS screen has an "Actualizar registros" button, and every successful 001 start of the DNS server runs the same refresh: it waits for all running VMs with a record in parallel, in one shared window of at most 120 seconds, rewrites every record whose IP changed and reloads the zone only if something changed. A VM that is off or reports no IPv4 when the window ends keeps its last known IP.
- REQ-28 (US-5, US-6): Every change to the zone is checked with bind9's zone check before reloading; a failed check keeps the previous zone and the UI shows "Zona DNS inválida; se mantuvo la anterior" with the check output in the history stderr. After a successful change, `dig @<APP_DNS_IP> <vm name>.<APP_DNS_DOMAIN> A` returns the stored IP.
- REQ-29 (US-5): The records shown are those stored by the app; every successful add, remove or refresh leaves the zone on the DNS server matching them.
- REQ-30 (US-1, US-5): Deleting a web server removes its record as in REQ-24.

### Resolver
- REQ-31 (US-7): Each VM listed on the DNS screen has a "Usar como resolvedor" toggle. Setting it makes the VM's resolver list hold `APP_DNS_IP` first and the DHCP-provided nameservers after it, kept across reboots and DHCP renewals; unsetting it restores the DHCP-provided resolver list. Setting an already set resolver, or unsetting an unset one, succeeds without changes.
- REQ-32 (US-7): Setting or unsetting the resolver of a powered-off VM fails with the 001 REQ-29 message and runs no remote command; on a server assigned to a load balancer it goes through the jump of 004 REQ-19.
- REQ-33 (US-7): The backend stores per VM whether the app set the DNS server as its resolver, set on a successful set and cleared on a successful unset or when the VM is deleted.

### Common
- REQ-34 (US-1..US-8): Every operation is executed by its own script file with a 15-minute timeout (001 REQ-14). Operations on one VM hold its per-VM lock (001 REQ-15); record add, remove, refresh and every bind9 action hold the DNS server's lock; the record update of REQ-26 runs after the start, assign or remove released their locks; a second request on a locked VM is rejected with "Operación en curso en esta VM".
- REQ-35 (US-9): Web history: create, delete, install page, Iniciar, Detener and Reiniciar are always recorded; Estado and the list read are recorded only when they fail. DNS history: create, delete, add record, remove record, refresh (manual or automatic), set and unset resolver, Iniciar, Detener and Reiniciar are always recorded; Estado is recorded only when it fails. Each row has operation name, VM, command and arguments, exit code, stderr, result and timestamp. For add, remove and refresh, the VM column records the DNS server and the target VMs appear in the arguments column.
- REQ-36 (US-9): Each module screen shows the last 50 recorded operations of the module; each row can expand to show its stderr.
- REQ-37 (US-1..US-9): The home screen shows six module cards, all enabled: Preparar sistema, Gestión de usuarios, Servicios systemd, Backends HAProxy, Servidores web and DNS (supersedes the four cards of 001 REQ-19).
- REQ-38 (US-1..US-8): Every operation ends with a success or failure message in the UI; failure messages are understandable and do not show raw stack traces; while an operation runs, the UI shows a loading indicator and disables the buttons.
- REQ-39 (US-1..US-8): Each script file starts with a header documenting usage, arguments and exit-code table (including reserved code 99 "Operación no implementada"), followed by `set -euo pipefail`, with comments organized in sections and numbered steps.
- REQ-40 (US-1..US-9): The UI is built with shadcn/ui components and all its text is in Spanish.

## 5. Acceptance criteria

### Web servers
- AC-1 (US-1): Given name `web-a`, 1024 MB, 1 CPU and type Nginx, when the user submits "Crear servidor web", then the create script is invoked with each value as a separate argument, the VM's disk is a differencing child of `APP_NGINX_BASE_DISK`, and the UI shows the result.
- AC-2 (US-1): Given an invalid or duplicate name, memory or CPUs out of range, or no type selected, when the user submits, then the form shows a validation error and no script is invoked.
- AC-3 (US-1): Given `APP_APACHE_BASE_DISK` points to a missing file, when the user creates an Apache web server, then the UI shows "Disco base de Apache no encontrado" and no VM is registered.
- AC-4 (US-1): Given a web server created, when it is inspected over SSH, then the 001 bootstrap state holds (001 AC-40), the web service is active and enabled, and `curl -H 'Host: anything' http://<IPv4>/` returns 200 with the default page.
- AC-5 (US-1, US-5): Given a running DNS server, when web server `web-a` is created, then `dig @<APP_DNS_IP> web-a.stratus.lan A` returns its IPv4 and the DNS screen shows the record.
- AC-6 (US-1, US-5): Given the DNS server is off, when web server `web-b` is created, then the creation succeeds, the UI shows "Servidor web creado; no se pudo registrar en DNS, agréguelo desde DNS" and the DNS history has a failed row; given no DNS server exists, then no warning appears.
- AC-7 (US-1): Given a 001 VM creation in progress, when the user creates a web server, then the UI shows "Ya hay una creación de VM en curso" and no script is invoked.
- AC-8 (US-1, US-8): Given web servers `web-a` (Nginx) and `web-b` (Apache) and VM `vm1`, when the user opens the web screen, then only `web-a` and `web-b` are listed with their type, state, IPv4, FQDN and load balancer; and in the 001 selector they show "Servidor web (Nginx)" and "Servidor web (Apache)".
- AC-9 (US-8): Given an Nginx web server, when the user removes nginx or installs apache2 from 001, then the UI shows "No se puede eliminar nginx: el servidor web lo usa" or "No se puede instalar apache2: el servidor web usa nginx" and nothing changes; when the user installs ufw, then 22/tcp and 80/tcp are allowed.
- AC-10 (US-1): Given web server `web-a` with a record, when the user confirms delete, then the VM and its disk are removed, its record is gone from the zone, and the base disk is unchanged; given `web-a` is assigned to `lb1`, then the UI shows "La VM es servidor de un balanceador; quítela primero" and nothing changes.

### Web page
- AC-11 (US-2): Given a valid archive holding `index.html` and `css/site.css`, when the user confirms "Instalar página", then `/var/www/html` holds only those files owned by `root:root` with directories 755 and files 644, `curl http://<IPv4>/` returns the new `index.html`, and the web service's main PID did not change.
- AC-12 (US-2): Given an archive without `index.html` at its root (for example only `site/index.html`), when the user installs it, then the UI shows "El archivo debe contener index.html en la raíz" and `/var/www/html` is unchanged.
- AC-13 (US-2): Given a 201 MB file, a file named `site.zip`, or an archive holding `../evil`, an absolute path, a symlink or a corrupt gzip, when the user installs it, then the UI shows the matching 003 message and nothing changes on the VM.
- AC-14 (US-2): Given an install that fails after the backup (for example extraction fails), when it finishes, then `/var/www/html` holds the previous content, no backup remains on the VM, and the UI shows a failure.
- AC-15 (US-2): Given a page installed, when the user installs a second archive, then only the second archive's files remain in `/var/www/html`.
- AC-16 (US-2, US-8): Given `web-a` assigned to `lb1` and both running, when the user installs a page on `web-a`, then the copy goes through the jump and succeeds; given `lb1` is off, then the UI shows "El balanceador lb1 está apagado; enciéndalo para llegar al servidor" and nothing changes.
- AC-17 (US-2): Given an install, when it ends, then the temporary archive file no longer exists on the host.

### Service actions
- AC-18 (US-3): Given web server `web-a` active, when the user clicks Detener, then `systemctl is-active nginx` is `inactive` and `curl` to port 80 fails; when the user clicks Iniciar, then it is `active`; when the user clicks Reiniciar, then its main PID changes; when the user clicks Iniciar on an active service, then the operation succeeds and the state stays `active`.
- AC-19 (US-3): Given bind9 active on the DNS server, when the user clicks Detener, then `dig @<APP_DNS_IP>` times out; when the user clicks Iniciar, then it answers again.
- AC-20 (US-3): Given a failed web service, when the user clicks Estado, then the panel shows the `systemctl status` output including `failed`, and the UI does not report an operation failure.
- AC-21 (US-3): Given a web service that fails at start, when the user clicks Iniciar, then the operation succeeds and about 3 seconds later the UI shows the non-`active` state with a warning pointing to Estado.

### DNS server
- AC-22 (US-4): Given valid name, memory and CPUs and valid `APP_DNS_*` values, when the user submits "Crear servidor DNS", then the DNS server is created from `APP_DNS_BASE_DISK`, answers SSH at `APP_DNS_IP`, and keeps that address after a restart.
- AC-23 (US-4): Given a DNS server registered, when the user submits "Crear servidor DNS" again, then the UI shows "Ya existe un servidor DNS" and no script is invoked.
- AC-24 (US-4): Given `APP_DNS_IP` empty or `192.168.1.53` without prefix, `APP_DNS_GATEWAY` outside the subnet, `APP_DNS_DOMAIN` `Stratus_Lan`, or `APP_DNS_FORWARDERS` `1.1.1`, when the user creates the DNS server, then the UI shows "Configuración de DNS incompleta" and no VM is registered.
- AC-25 (US-4): Given another host answers ARP or ICMP at the address of `APP_DNS_IP` (including a host that blocks ICMP), when the user creates the DNS server, then the UI shows "La IP del servidor DNS ya está en uso" and no VM is registered.
- AC-26 (US-4, US-7): Given the DNS server `dns1`, when `dig @<APP_DNS_IP> stratus.lan NS` and `dig @<APP_DNS_IP> dns1.stratus.lan A` run from the host, then the NS record names `dns1.stratus.lan` and the A record is the address of `APP_DNS_IP`; when `dig @<APP_DNS_IP> debian.org A` runs from a LAN client, then it returns an address.
- AC-27 (US-4): Given running web servers `web-a` and `web-b` and a powered-off web server `web-c`, when the DNS server is created, then `web-a` and `web-b` have records, the result names `web-c` as skipped, and the creation succeeds.
- AC-28 (US-8): Given the DNS server, when the user removes bind9 from 001, then the UI shows "No se puede eliminar bind9: el servidor DNS lo usa"; when the user installs ufw, then 22/tcp, 53/tcp and 53/udp are allowed; when the user assigns it to a load balancer, then the UI shows "La VM no se puede asignar a este balanceador" and nothing changes.
- AC-29 (US-4, US-7): Given VM `vm1` uses the DNS server as resolver, when the user deletes the DNS server, then the UI shows "Quite primero el resolvedor DNS de las VMs: vm1" and nothing changes; given `vm1` uses it and is powered off, then the same rejection applies; given no VM uses it, then the VM and its disk are removed and the DNS screen shows no records and offers "Crear servidor DNS".

### A records
- AC-30 (US-5): Given running VM `vm1`, running load balancer `lb1` and powered-off VM `vm2`, none with a record, when the user selects all three and clicks "Agregar registros", then `vm1.stratus.lan` and `lb1.stratus.lan` resolve to their 001 IPv4 (the bridged one for `lb1`), the result shows `vm2` rejected with "La VM no tiene dirección IP; verifique que esté encendida", and `vm2` has no record.
- AC-31 (US-5): Given `web-a` assigned to `lb1` with internal IPv4 `10.10.1.11`, when its record is added, then `web-a.stratus.lan` resolves to `10.10.1.11`.
- AC-32 (US-5): Given any record, when `dig @<APP_DNS_IP>` returns it, then its TTL is 60.
- AC-33 (US-5): Given `vm1` with a record, when the user confirms "Quitar registro", then `dig @<APP_DNS_IP> vm1.stratus.lan A` returns NXDOMAIN; when a direct API call removes it again, then the operation succeeds.
- AC-34 (US-5): Given `vm1` with a record, when it is deleted from 001, then its record is gone; given the DNS server is off at that moment, then the delete succeeds, the UI shows "VM eliminada; no se pudo quitar su registro DNS, use Actualizar registros" and the DNS history has a failed row.
- AC-35 (US-5): Given the DNS server is off, when the user adds or removes a record or clicks "Actualizar registros", then the UI shows "El servidor DNS está apagado; enciéndalo para cambiar los registros" and nothing changes.
- AC-36 (US-6): Given `vm1` with a record `192.168.1.40`, when it is stopped, its lease changes and it is started from 001, then after the start `vm1.stratus.lan` resolves to its new IPv4 and the DNS history has a refresh row.
- AC-37 (US-6): Given `web-a` with a record on the bridge, when it is assigned to `lb1`, then its record becomes its internal IPv4; when it is removed from `lb1`, then its record becomes its new bridged IPv4.
- AC-38 (US-6): Given the DNS server is off, when `vm1` with a record is started from 001, then the start succeeds, the UI shows "Operación completada; no se pudo actualizar el registro DNS de vm1, use Actualizar registros" and the DNS history has a failed row.
- AC-39 (US-6): Given records whose IPs did not change, when the user clicks "Actualizar registros", then the operation succeeds and the zone is not reloaded; given `vm2` off with a record, then its record keeps its last IP.
- AC-40 (US-6): Given 10 running VMs with records, when the refresh runs, then it waits for all of them in parallel for at most 120 seconds in total.
- AC-41 (US-5, US-6): Given a change whose zone fails bind9's check, when the operation runs, then the zone served is unchanged and the UI shows "Zona DNS inválida; se mantuvo la anterior".

### Resolver
- AC-42 (US-7): Given running VM `vm1`, when the user sets "Usar como resolvedor", then `/etc/resolv.conf` on `vm1` lists `APP_DNS_IP` first followed by the DHCP nameservers, `getent hosts web-a.stratus.lan` and `apt update` succeed on `vm1`, and after a reboot the list is the same; when the user unsets it, then `/etc/resolv.conf` holds only the DHCP nameservers.
- AC-43 (US-7): Given `web-a` assigned to `lb1`, when the user sets its resolver, then the change goes through the jump and `getent hosts vm1.stratus.lan` succeeds on `web-a`; given a powered-off VM, then the UI shows "La VM no tiene dirección IP; verifique que esté encendida".
- AC-44 (US-7): Given `vm1` with the resolver set, when `vm1` is deleted, then the stored resolver flag is cleared and the DNS server can be deleted.

### Demonstration and common
- AC-45 (US-1, US-2): Given the real host, when an Nginx and an Apache web server are created and a page is installed on each, then before/after `curl http://<IPv4>/` shows the default page and then the installed page.
- AC-46 (US-2, US-8): Given the real host, when `web-a` and `web-b` with different pages are assigned to `lb1`, then repeated requests to the frontend port return both pages in turn.
- AC-47 (US-4, US-5): Given the real host, when the DNS server is created and records are added for `vm1` and `lb1`, then before/after `dig @<APP_DNS_IP> vm1.stratus.lan A` shows NXDOMAIN and then the IPv4.
- AC-48 (US-6): Given the real host, when a VM with a record is restarted and its address changes, then `dig` returns the new address after the start.
- AC-49 (US-7): Given the real host, when the resolver is set on a VM, then before/after `getent hosts lb1.stratus.lan` on that VM fails and then succeeds.
- AC-50 (US-1..US-8): Given an operation running on a VM, when another operation of any module is requested on the same VM, then it is rejected with "Operación en curso en esta VM"; given a record add running, when a bind9 action is requested, then it is rejected the same way.
- AC-51 (US-9): Given 20 successful Estado requests and one failed Estado, when the history is inspected, then it holds only the failed row; given create, delete, install page, Iniciar, Detener, Reiniciar, add, remove, refresh, set and unset resolver, then each adds one row in its module.
- AC-52 (US-9): Given an add of `vm1` and `lb1`, when the DNS history is inspected, then the row has the DNS server as VM and `vm1` and `lb1` in the arguments.
- AC-53 (US-9): Given more than 50 recorded operations of a module, when the user opens its screen, then only the 50 most recent are listed, newest first, each with expandable stderr.
- AC-54 (US-1..US-9): Given the home screen, when it loads, then six enabled cards appear and "Servidores web" and "DNS" navigate to their screens.
- AC-55 (US-1..US-8): Given a script run directly with a missing argument or a value outside its allowlist (for example type `iis`, name `Web1` or domain `Stratus_Lan`), when it runs, then it exits with code 2 and calls neither VBoxManage, ssh nor scp.
- AC-56 (US-1..US-8): Given the script files, when one is opened, then it contains usage, arguments, exit-code table (including 99), `set -euo pipefail` and comments with sections and numbered steps.

## 6. Out of scope
- HTTPS, certificates, ports other than 80, multiple sites or virtual hosts per web server, dynamic content (PHP, proxies).
- Editing web server configuration files; changing the type of an existing web server.
- Building the Apache, Nginx and DNS base disks (the team prepares them by hand once, as in 001 and 004).
- More than one DNS server, more than one zone, secondary servers, DNSSEC.
- Record types other than A and the zone NS (PTR, CNAME, MX, TXT, AAAA); custom record names or TTLs.
- Renaming records when a hostname changes.
- Choosing the DNS server's IP from the UI; DHCP for the DNS server.
- Setting the resolver of the host or of LAN machines outside the app.
- Periodic IP polling; IP changes while a VM keeps running.
- IPv6.

## 7. Clarifications
- 2026-10-31. Q: The constitution mission names four modules; how are web servers and DNS added? A: Constitution amended to 2.1.0: the mission lists six modules; no principle changed.
- 2026-10-31. Q: How are the features split into modules? A: Two modules, `web` ("Servidores web") and `dns` ("DNS"), each with its home card and screen.
- 2026-10-31. Q: Which base disks back the new VMs? A: Three prebuilt disks: `APP_APACHE_BASE_DISK`, `APP_NGINX_BASE_DISK` and `APP_DNS_BASE_DISK`, with the same root password file as 001.
- 2026-10-31. Q: Which domain and record name does each VM get? A: `<vm name>.<APP_DNS_DOMAIN>`, default domain `stratus.lan`; the VM name is unique and equals the hostname at creation.
- 2026-10-31. Q: How many DNS servers can exist? A: Exactly one; a second creation is rejected with "Ya existe un servidor DNS".
- 2026-10-31. Q: A web server assigned to a load balancer has an internal IP; what does its record point to? A: Its internal IP, the one 001 shows; LAN clients reach the site through the load balancer's own record.
- 2026-10-31. Q: When are records refreshed after DHCP changes? A: Like the 004 refresh: after every successful 001 start of a VM with a record, after the DNS server starts (all records), and with "Actualizar registros"; a failed update keeps the operation successful with a warning and a failed DNS history row.
- 2026-10-31. Q: Should the DNS server use DHCP? A: No, a static IP from `APP_DNS_IP` (CIDR) and `APP_DNS_GATEWAY`, chosen by the team outside the router's DHCP pool.
- 2026-10-31. Q: Do VMs use the DNS server as resolver? A: Optionally, per VM, with a toggle on the DNS screen.
- 2026-10-31. Q: How do VMs using it as resolver reach internet names? A: bind9 forwards names outside the zone to `APP_DNS_FORWARDERS`, only for the LAN subnet and `10.10.0.0/16`; the VM keeps its DHCP nameservers after the DNS server.
- 2026-10-31. Q: How is the web page installed and changed? A: Like 003: one `.tar.gz` up to 200 MB, same invalid-archive rules, must hold `index.html` at its root, copied with scp, replaces the whole `/var/www/html` after confirmation, previous content restored on failure.
- 2026-10-31. Q: Which service actions exist? A: Iniciar, Detener, Reiniciar and Estado for apache2 or nginx on each web server and for bind9 on the DNS server.
- 2026-10-31. Q: What happens to records on create and delete? A: Creating a web server adds its record when a DNS server exists (warning if it fails); deleting any VM removes its record; deleting the DNS server drops all records.
- 2026-10-31. Q: Deleting the DNS server while VMs use it as resolver? A: Rejected with "Quite primero el resolvedor DNS de las VMs: <vm list>" until the user unsets each resolver.
- 2026-10-31. Q: Adding a record for a powered-off VM? A: That VM is rejected with the 001 IP message; the other selected VMs are still added and the result lists each one.
- 2026-10-31. Q: Record TTL, and do 004 assign and remove update records? A: TTL 60 s; assign and remove update the VM's record (bridged to internal IP and back), with the same warning on failure.
- 2026-10-31. Q: Can the DNS server be a load balancer server? A: No; it is rejected with the 004 message, because it must stay on the LAN at its static IP.
- 2026-10-31. Q: What if the DNS server is created after web servers exist? A: Its creation registers every existing web server that reports an IPv4 and names the skipped ones.
- 2026-11-01. Q: "Actualizar registros" while the DNS server is off? A: Fails with the REQ-25 message, changes nothing, and adds a failed DNS history row.
- 2026-11-01. Q: How is "La IP del servidor DNS ya está en uso" detected? A: One ARP probe on `APP_BRIDGE_ADAPTER` and one ICMP echo, 1-second timeout each; any reply means in use.
- 2026-11-01. Q: Deleting the DNS server while a powered-off VM still has the resolver set? A: Still rejected; the user starts that VM and unsets the resolver, or deletes the VM.

# Prepare system

## 1. Metadata
- Status: Approved
- Updated: 2026-10-22
- Constitution version: 2.0.0
- Required by: none

## 2. Constitution relationship
- Applies:
  - P1: the backend only validates input, invokes one script per operation and reports the result.
  - P2: each operation has its own script file with `set -euo pipefail`, explicit arguments and a documented exit-code table.
  - P3: every input is validated against an allowlist in the UI, in the backend and in the script, and passed as discrete arguments; the /etc/issue message allowlist is printable UTF-8 plus line breaks, without backslash or other control characters.
  - P4: Guest Additions is used only during VM creation, before key access is configured, as root with the base image password (`APP_GUEST_PASSWORD_FILE`), to bootstrap key access (REQ-30); from the end of creation every operation uses SSH with the backend key only against the VM's bridged IP, and SSH password login is disabled in the same bootstrap.
  - P5: the user's private key lives only in memory, is downloadable once, is never logged or stored, and the UI shows a usage message; the backend's own key is a separate pair that is never offered for download nor logged.
  - P6: every operation ends with a success or failure message; the history keeps command, exit code, stderr and timestamp.
  - P7: home with module cards, one screen for the prepare module, forms that confirm the result.
  - P8: each script file is runnable on its own and has bats tests that run it without the web app; a before/after demonstration on the real host covers what stubs cannot (AC-26, AC-38, AC-40, AC-44).
  - P9: no new abstraction beyond what the operations need; shadcn/ui approved by the user.
- Conflicts: none

## 3. User stories
| ID | Priority | Actor | Story |
|---|---|---|---|
| US-1 | P1 | Student admin | As a student admin, I need to create, list, start, stop and delete standardized Debian 13 VMs from a shared base disk, see their IP address and pick the one I work on, so every operation targets a known VM that I can also reach by SSH |
| US-2 | P1 | Student admin | As a student admin, I need to install or remove an application on the selected VM, so I can prepare it without opening a terminal |
| US-3 | P1 | Student admin | As a student admin, I need to generate the root SSH key and download the private key once, so I can connect to the VM's IP with key-only root access |
| US-4 | P2 | Student admin | As a student admin, I need to compose /etc/issue from dynamic fields and a custom message with a preview, so the login screen shows the information I want |
| US-5 | P1 | Student admin | As a student admin, I need to set the VM hostname and have /etc/hosts updated, so the system resolves its own name |
| US-6 | P2 | Student admin | As a student admin, I need to see the history of operations of the module, so I can diagnose failures |
| US-7 | P1 | Student admin | As a student admin, I need a home screen with cards for all four modules, so I can navigate now and future modules have a place |

## 4. Requirements
- REQ-1 (US-1): The user can create a VM giving name (`^[a-z0-9][a-z0-9-]{0,31}$`, unique among registered VMs; a duplicate is rejected with "Ya existe una VM con ese nombre"), memory in MB (512-8192) and CPUs (1-4). The VM's disk is a differencing disk on top of the multi-attach base disk at `APP_BASE_DISK` (Debian 13 with Guest Additions and openssh-server already installed, prebuilt by the team), so every VM starts from the same system and its capacity is the base disk's. Its network adapter is bridged to the host interface `APP_BRIDGE_ADAPTER` and gets its IPv4 address by DHCP from the LAN; no port forwarding is used. The operation leaves the VM running and reachable by SSH at its IP after the bootstrap of REQ-30.
- REQ-2 (US-1): The user can list registered VMs with their state (running or powered off) and their IPv4 address (shown as "-" when the VM is off or reports no address), and select one as target for every other operation.
- REQ-3 (US-1): The user can start and stop the selected VM, and delete it (unregister and remove its differencing disk) after confirming. Stop sends an ACPI shutdown and forces a poweroff if the VM is still running after 60 seconds. Deleting a running VM forces a poweroff first, then unregisters it and removes its differencing disk. The base disk is never removed nor modified by any operation.
- REQ-4 (US-2): The user can install or remove an application by picking it from a catalog (openssh-server, haproxy, nginx, apache2, curl, htop, git, ufw) or typing a package name matching `^[a-z0-9][a-z0-9+.-]{1,62}$`.
- REQ-5 (US-2): The catalog is defined in one place, so adding an entry requires changing a single list.
- REQ-24 (US-2): Every operation on an existing VM (all except create, list, start, stop and delete) runs over SSH as root with the backend key against `<VM IPv4>:22`; installing openssh-server is a plain package install, because key access is already configured at creation.
- REQ-25 (US-2): Removing openssh-server is always rejected with "No se puede eliminar openssh-server: el backend lo usa para conectarse", and installing ufw also allows 22/tcp.
- REQ-6 (US-3): The user can generate an ed25519 key pair without passphrase for root; the public key replaces any previous user key generated by the app in root's authorized_keys, the backend's public key is kept (added if missing), and SSH password login stays disabled (`PasswordAuthentication no`, `PermitRootLogin prohibit-password`).
- REQ-7 (US-3): Regenerating the key when one already exists (`user_key_generated` set for the VM) requires confirmation and warns that the old key stops working.
- REQ-8 (US-3): The private key is kept only in backend memory and can be downloaded once; it is discarded after the first download or after 10 minutes, whichever comes first.
- REQ-9 (US-3): After generation, the UI shows a message on correct use of the private key: keep it private, set permissions to 600, do not share it or commit it, and how to connect with it (`ssh -i id_ed25519_<vm> root@<VM IPv4>`).
- REQ-10 (US-4): The user can compose /etc/issue by checking any of these fields: hostname (`\n`), date (`\d`), time (`\t`), IPv4 (`\4`), OS name (`\s`), kernel release (`\r`), architecture (`\m`), tty (`\l`), logged-in users (`\U`), plus a custom message of at most 500 characters made of printable UTF-8 characters (accents and ñ allowed) and line breaks, without backslash or any other control character.
- REQ-11 (US-4): The UI shows a live preview of the resulting /etc/issue before applying it.
- REQ-26 (US-4): The /etc/issue order is fixed: the custom message first, then each checked field in the list order of REQ-10 (hostname, date, time, IPv4, OS name, kernel release, architecture, tty, logged-in users), one per line with these labels: `Host: \n`, `Fecha: \d`, `Hora: \t`, `IPv4: \4`, `Sistema: \s`, `Kernel: \r`, `Arquitectura: \m`, `Terminal: \l`, `Usuarios: \U`.
- REQ-12 (US-5): The user can set the hostname of the selected VM; it must match `^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`.
- REQ-13 (US-5): Setting the hostname replaces the `127.0.1.1` line of /etc/hosts with the new hostname, or adds it if missing, keeping every other line.
- REQ-14 (US-1..US-5): Each operation is executed by its own script file; the request waits for the script to finish, with a 15-minute timeout.
- REQ-15 (US-1..US-5): Only one operation runs per VM at a time; a second request on the same VM while one runs is rejected with the message "Operación en curso en esta VM".
- REQ-16 (US-1..US-5): Every operation ends with a success or failure message in the UI; failure messages are understandable and do not show raw stack traces.
- REQ-17 (US-6): Every operation is recorded with operation name, VM, command and arguments, exit code, stderr, result and timestamp; the private key is never recorded.
- REQ-18 (US-6): The module screen shows the last 50 recorded operations of the module; each row can expand to show its stderr.
- REQ-19 (US-7): The home screen shows four module cards: Preparar sistema (enabled), Gestión de usuarios, Servicios systemd and Backends HAProxy (disabled, labeled "Próximamente").
- REQ-20 (US-1..US-7): The UI is built with shadcn/ui components and all its text is in Spanish.
- REQ-21 (US-1..US-5): Each script file starts with a header documenting usage, arguments and exit-code table, followed by `set -euo pipefail`. The exit-code table keeps the reserved code 99 ("Operación no implementada") for a placeholder body, so a script not yet written shows a failure in the UI and the history records it. Each script also contains comments describing its intended behavior, organized in sections with numbered steps.
- REQ-22 (US-1): The Guest Additions bootstrap authenticates as root with the base image password read from the host file `APP_GUEST_PASSWORD_FILE` (mode 600); the password is never passed in argv, written to the history or logs, or shown in the UI, and the backend never reads it.
- REQ-23 (US-3): The backend owns its own ed25519 key pair at `APP_SSH_KEY_PATH`; it is never offered for download nor logged, and every SSH operation uses it against `<VM IPv4>:22`. Regenerating the user's key does not remove the backend's key. If the pair does not exist at `APP_SSH_KEY_PATH` when the backend starts, the backend runs a script under `scripts/prepare/` that creates it with permissions 600.
- REQ-27 (US-3): The backend stores the flag `user_key_generated` per VM in SQLite, set when generating the key succeeds and cleared when the VM is deleted.
- REQ-29 (US-2..US-5): An SSH operation on a VM that reports no IPv4 address (off, or Guest Additions not reporting yet) fails with "La VM no tiene dirección IP; verifique que esté encendida" and runs no remote command.
- REQ-30 (US-1): Creating a VM, after the first boot, uses Guest Additions as root (REQ-22) to: add the backend's public key to root's authorized_keys, set `PasswordAuthentication no` and `PermitRootLogin prohibit-password`, regenerate `/etc/machine-id` and the SSH host keys (so clones do not share DHCP client IDs or host keys), and set the hostname to the VM name with its `127.0.1.1` line in /etc/hosts. The creation succeeds only when the VM reports an IPv4 address and the backend key opens an SSH session on it.
- REQ-31 (US-1): Creation fails without registering any VM when the base disk at `APP_BASE_DISK` does not exist ("Disco base no encontrado") or the interface `APP_BRIDGE_ADAPTER` is not a host bridge interface ("Interfaz de puente no encontrada"), or a VM with that name is already registered in VirtualBox outside the app ("VM ya registrada en VirtualBox"). When any later step fails, the VM and its differencing disk are removed and the base disk is left intact.
- REQ-32 (US-1): Only one VM creation runs at a time across all VMs, because every creation uses the shared base disk; a create request while another creation runs is rejected with "Ya hay una creación de VM en curso" and registers nothing. Operations on existing VMs are not blocked by it.

## 5. Acceptance criteria
- AC-1 (US-1): Given valid VM parameters, when the user submits the create form, then the backend invokes the create script with name, memory and CPUs as separate arguments and the UI shows the result.
- AC-2 (US-1): Given a VM name with uppercase letters, starting with a hyphen (`-vm`) or longer than 32 characters, or memory or CPUs out of range, when the user submits, then the form shows a validation error and no script is invoked.
- AC-3 (US-1): Given registered VMs, when the user opens the prepare screen, then a selector lists each VM with its state and IPv4 address.
- AC-4 (US-1): Given a selected VM, when the user clicks delete, then a confirmation dialog appears and the delete script is invoked only after confirming.
- AC-5 (US-1): Given no VM is selected, when the user opens any VM operation form, then its submit button is disabled with a hint to select a VM.
- AC-6 (US-2): Given a catalog entry or a valid typed package, when the user clicks Instalar or Eliminar, then the matching script is invoked with the package name as one argument and the UI shows the result.
- AC-7 (US-2): Given a package name like `nginx; rm -rf /` or an empty field, when the user submits, then the UI and backend reject it and no script is invoked.
- AC-8 (US-2): Given the script exits non-zero, when the operation finishes, then the UI shows a failure message and the history stores exit code and stderr.
- AC-9 (US-3): Given the key is generated, when the operation finishes, then the UI shows a download button and the private-key usage message with the VM's IPv4 address.
- AC-10 (US-3): Given the key was downloaded once, when the user tries to download again, then the UI shows "La llave ya no está disponible, genere una nueva".
- AC-11 (US-3): Given the key was generated more than 10 minutes ago and not downloaded, when the user tries to download, then the same "no longer available" message is shown.
- AC-12 (US-3): Given a key already exists for the VM, when the user clicks generate, then a confirmation warns that the old key stops working before the script runs.
- AC-13 (US-3): Given any key operation, when the history and logs are inspected, then no private key content appears.
- AC-14 (US-4): Given checked fields and a message, when the user edits them, then the preview updates immediately showing the message first and then each checked field on its own line with its label, in the order of REQ-26.
- AC-15 (US-4): Given a message longer than 500 characters, when the user submits, then the form shows a validation error and no script is invoked.
- AC-16 (US-4): Given no field checked and an empty message, when the user submits, then the form shows a validation error.
- AC-17 (US-5): Given hostname `web-01`, when the user submits, then the hostname script is invoked with `web-01` as one argument and the UI shows the result.
- AC-18 (US-5): Given hostname `Web_01`, `-web` or a 64-character name, when the user submits, then the form shows a validation error and no script is invoked.
- AC-19 (US-1..US-5): Given an operation running on a VM, when another operation is requested on the same VM, then it is rejected with "Operación en curso en esta VM".
- AC-20 (US-1..US-5): Given a script that runs longer than 15 minutes, when the timeout is reached, then the process is stopped, the UI shows a timeout failure and the history records it.
- AC-21 (US-1..US-5): Given a running operation, when the UI waits, then it shows a loading indicator and disables the form buttons.
- AC-22 (US-6): Given more than 50 recorded operations, when the user opens the module screen, then only the 50 most recent are listed, newest first.
- AC-23 (US-7): Given the home screen, when it loads, then four module cards appear and only Preparar sistema navigates; the other three show "Próximamente".
- AC-24 (US-1..US-5): Given the script files, when one is opened, then it contains usage, arguments, exit-code table (including 99), `set -euo pipefail` and comments with sections and numbered steps describing the intended behavior.
- AC-25 (US-4): Given a message containing `\` or a control character other than a line break, when the user submits, then the UI and backend reject it and no script is invoked; a message such as `Configuración de producción` with a line break is accepted.
- AC-26 (US-1): Given a VM created, when its configuration is inspected, then its disk is a differencing child of the multi-attach base disk at `APP_BASE_DISK` and its first adapter is bridged to `APP_BRIDGE_ADAPTER`, with no NAT port forward.
- AC-27 (US-3): Given the user's key is regenerated, when the operation finishes, then root's authorized_keys holds the new user key and the backend key, the old user key is gone, and the backend can still run SSH operations.
- AC-28 (US-1..US-5): Given any operation, when the history and logs are inspected, then neither the base image root password nor the backend private key appears.
- AC-29 (US-1): Given a registered VM named `vm1`, when the user creates another VM named `vm1`, then the UI shows "Ya existe una VM con ese nombre" and no script is invoked.
- AC-30 (US-1): Given a running VM that ignores the ACPI shutdown, when the user clicks stop, then the VM is forced off after 60 seconds and the UI shows the result.
- AC-31 (US-1): Given a running VM, when the user confirms delete, then the VM is powered off, unregistered and its differencing disk removed, and the UI shows the result.
- AC-32 (US-2): Given a VM just created, when the user installs a package, then the operation runs over SSH with the backend key against the VM's IPv4 address.
- AC-33 (US-2): Given any VM, when the user requests removing openssh-server, then the UI shows "No se puede eliminar openssh-server: el backend lo usa para conectarse" and no script is invoked.
- AC-34 (US-2): Given a VM, when the user installs ufw, then 22/tcp is allowed and the backend can still run SSH operations.
- AC-35 (US-3): Given a VM just created, when the user generates the key, then one SSH operation adds the user key beside the backend key and sets `user_key_generated`.
- AC-36 (US-3): Given no key pair at `APP_SSH_KEY_PATH`, when the backend starts, then the key-creation script runs and the pair exists with permissions 600.
- AC-37 (US-1..US-5): Given a script whose body is still the placeholder, when its operation runs, then the UI shows a failure, and the history stores exit code 99 and stderr "Operación no implementada".
- AC-38 (US-1): Given two VMs created from the same base disk and one of them deleted, when the base disk and the other VM are inspected, then the base disk still exists unchanged and the other VM still starts and answers SSH.
- AC-39 (US-1): Given a VM with `user_key_generated` set, when it is deleted, then the flag is cleared.
- AC-40 (US-1): Given a VM created, when it is inspected over SSH, then root's authorized_keys holds the backend key, `PasswordAuthentication no` and `PermitRootLogin prohibit-password` are set, the hostname and the `127.0.1.1` line are the VM name, and its `/etc/machine-id` and SSH host keys differ from those of another VM created from the same base disk.
- AC-41 (US-1): Given `APP_BASE_DISK` points to a missing file or `APP_BRIDGE_ADAPTER` names a missing interface, when the user creates a VM, then the UI shows "Disco base no encontrado" or "Interfaz de puente no encontrada" and no VM is registered in VirtualBox; given a VM `vm1` registered in VirtualBox but not in the app, when the user creates `vm1`, then the UI shows "VM ya registrada en VirtualBox" and the existing VM is not modified.
- AC-42 (US-1): Given a running VM with IPv4 `192.168.1.50` and a powered-off VM, when the user opens the prepare screen, then the selector shows `192.168.1.50` for the first and `-` for the second.
- AC-43 (US-2..US-5): Given a powered-off VM, when the user installs a package, sets the hostname, sets /etc/issue or generates the key, then the UI shows "La VM no tiene dirección IP; verifique que esté encendida".
- AC-44 (US-1): Given a creation whose bootstrap fails (for example, the base image root password is wrong), when the operation finishes, then the UI shows a failure, the VM and its differencing disk no longer exist, and the base disk is unchanged.
- AC-45 (US-1..US-5): Given the base image root password file, when the history, logs, VBoxManage argv and script output are inspected, then the password does not appear.
- AC-46 (US-1): Given a creation in progress, when the user creates another VM with a different name, then the UI shows "Ya hay una creación de VM en curso", no script is invoked, and an operation on an existing VM still runs.
- AC-47 (US-1): Given a powered-off VM selected, when the user clicks Iniciar, then the start script is invoked with the VM name, the VM ends running and the UI shows the result; given the VM is already running, the operation succeeds without changing it; given a VM missing from VirtualBox, the UI shows a failure.
- AC-48 (US-1..US-5): Given a script run directly with a missing argument or a value outside its allowlist (for example VM name `VM1`), when it runs, then it exits with code 2 and calls neither VBoxManage nor ssh.

## 6. Out of scope
- Building the base disk (the team prepares it by hand once: Debian 13, Guest Additions, openssh-server, root password).
- Choosing the bridge interface per VM, static IPs and IPv6.
- Changing the disk size of a VM.
- Migrating data from the previous schema (the old `stratus.db` is deleted).
- User management, systemd service management and HAProxy backend modules (only their home cards).
- Authentication and multiple concurrent users of the web console.
- Key passphrases and key types other than ed25519.
- IPv6 or FQDN hostnames.
- Async job queues and progress streaming.

## 7. Clarifications
- 2026-10-21. Q: Simulated scripts vs P8? A: Not a conflict; temporary decision. Script files are created with header and placeholder body, the backend invokes them for real, and the team writes the bodies manually. Script tests may fail until then.
- 2026-10-21. Q: Which VM is targeted? A: The app creates VMs through a script using `VBoxManage createvm`; the user selects the target VM from the list.
- 2026-10-21. Q: How is the application chosen? A: Catalog (openssh-server, haproxy, nginx, apache2, curl, htop, git, ufw), easy to extend, plus validated free text.
- 2026-10-21. Q: Private key lifecycle? A: One-time download from memory; discarded after download or 10 minutes.
- 2026-10-21. Q: Script file content? A: Header with usage, arguments and exit-code table, `set -euo pipefail`, placeholder body.
- 2026-10-21. Q: /etc/issue editor? A: Escape checkboxes, message up to 500 characters, live preview.
- 2026-10-21. Q: Hostname rules? A: Single label, `^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`; `127.0.1.1` line replaced or added.
- 2026-10-21. Q: SSH key details? A: ed25519, no passphrase; regenerating replaces the old key after confirmation; password login disabled.
- 2026-10-21. Q: Long or concurrent operations? A: Synchronous with 15-minute timeout; one operation per VM at a time.
- 2026-10-21. Q: History in UI? A: Table per module with the last 50 operations; stderr expandable.
- 2026-10-21. Q: VM actions? A: Create, list with state, start, stop, delete with confirmation.
- 2026-10-21. Q: Create VM inputs and result? A: Name, memory, CPUs, disk; unattended Debian 13 install from `APP_DEBIAN_ISO` with Guest Additions; VM left running. Superseded on 2026-10-22: name, memory and CPUs on a differencing disk over the multi-attach base disk.
- 2026-10-21. Q: Where does the backend run, given VBoxManage needs the host? A: Decided in the plan.
- 2026-10-21. Q: shadcn/ui in AGENTS.md? A: Added to the frontend stack line.
- 2026-10-21. Q: How does the backend authenticate over SSH once the user's private key is discarded (P4 vs P5)? A: The backend owns a separate ed25519 pair (`APP_SSH_KEY_PATH`), never downloaded, added to root's authorized_keys beside the user's key; regenerating replaces only the user's key.
- 2026-10-21. Q: How does the backend reach a VM over SSH? A: NAT with a port forward added by the create script. Superseded on 2026-10-22: bridged adapter, SSH to the VM's IPv4 on port 22.
- 2026-10-21. Q: Which guest account do Guest Additions operations use? A: A fixed account from `APP_GUEST_USER` and `APP_GUEST_PASSWORD`. Superseded on 2026-10-22: root with the base image password in `APP_GUEST_PASSWORD_FILE`, only during creation.
- 2026-10-21. Q: Which characters are allowed in the /etc/issue custom message (P3 allowlist)? A: Printable UTF-8 (accents and ñ allowed) and line breaks; no backslash and no other control characters.
- 2026-10-21. Q: P4 switches to SSH once SSH is installed, the spec switched when key access was enabled; how is it resolved? A: Installing openssh-server through the app also sets up key access. Superseded on 2026-10-22: openssh-server is in the base image and key access is set up at creation.
- 2026-10-21. Q: Duplicate and invalid VM names? A: A duplicate is rejected with "Ya existe una VM con ese nombre"; the regex becomes `^[a-z0-9][a-z0-9-]{0,31}$`.
- 2026-10-21. Q: How do stop and delete behave? A: Stop is an ACPI shutdown with forced poweroff after 60 s; delete on a running VM forces poweroff, then unregisters and deletes the disk, after confirmation.
- 2026-10-21. Q: Protect the backend's SSH access? A: Removing openssh-server is rejected with "No se puede eliminar openssh-server: el backend lo usa para conectarse"; installing ufw also allows 22/tcp. Since 2026-10-22 the block applies to every VM.
- 2026-10-21. Q: 'Generar llave' on a VM without openssh-server? A: Install it first in the same operation. Superseded on 2026-10-22: every VM has openssh-server and key access from creation.
- 2026-10-21. Q: `APP_SSH_KEY_PATH` missing at startup? A: The backend runs a script under `scripts/prepare/` that creates the ed25519 pair with permissions 600.
- 2026-10-21. Q: How does the app know per VM whether SSH is enabled and a user key exists? A: SQLite flags `ssh_enabled` and `user_key_generated`. Superseded on 2026-10-22: only `user_key_generated`; SSH is always enabled after creation.
- 2026-10-21. Q: Placeholder script body? A: Print "Operación no implementada" to stderr and exit with reserved code 99 listed in the exit-code table; the UI shows a failure and the history records it. Each script also contains comments describing its intended behavior, organized in sections and numbered steps.
- 2026-10-21. Q: /etc/issue order? A: Fixed: custom message first, then checked fields in list order (hostname, date, time, IPv4, OS, kernel, arch, tty, users), one per line with a Spanish label, for example `Host: \n`.
- 2026-10-21. Q: Port reuse? A: A new VM gets the lowest free port starting at 2201. Superseded on 2026-10-22: no port forwarding.
- 2026-10-21. Q: Spanish labels for the /etc/issue fields? A: Short labels: `Host: \n`, `Fecha: \d`, `Hora: \t`, `IPv4: \4`, `Sistema: \s`, `Kernel: \r`, `Arquitectura: \m`, `Terminal: \l`, `Usuarios: \U`.
- 2026-10-22. Q: One `APP_GUEST_PASSWORD` for every VM? A: A per-VM password in `<APP_GUEST_SECRETS_DIR>/<vm_name>.pass`. Superseded the same day: VMs are clones of a base disk, so the only guest secret is the base image root password in `APP_GUEST_PASSWORD_FILE`; `APP_GUEST_SECRETS_DIR` is removed.
- 2026-10-22. Q: Why change the architecture? A: Real Debian installs are slow, NAT port forwarding makes user access complex, and VMs were not standardized. VMs are now clones of one multi-attach base disk on a bridged network.
- 2026-10-22. Q: Where does the base disk come from? A: Prebuilt by hand by the team (Debian 13, Guest Additions, openssh-server, root password); its path is `APP_BASE_DISK`. The create script marks it multi-attach if needed. `APP_DEBIAN_ISO` is removed.
- 2026-10-22. Q: How does the backend get access to a new VM? A: Guest Additions bootstrap at creation as root with `APP_GUEST_PASSWORD_FILE`: backend key, password SSH login disabled, new machine-id and SSH host keys, hostname = VM name. Afterwards only SSH.
- 2026-10-22. Q: How do VMs get their IP? A: DHCP from the LAN; read from Guest Additions; shown in the UI; SSH scripts resolve it at run time because it may change between boots.
- 2026-10-22. Q: Which host interface do VMs bridge to? A: One interface for every VM, `APP_BRIDGE_ADAPTER`.
- 2026-10-22. Q: Disk size field? A: Removed; every VM has the base disk's capacity.
- 2026-10-22. Q: Existing `stratus.db`? A: Breaking change: the schema changes and the old file is deleted; no migration code.
- 2026-10-22. Q: Are script bodies still placeholders written by the team (2026-10-21 'Simulated scripts vs P8')? A: Superseded: every script body is implemented test-first by the tasks; code 99 stays reserved in the exit-code tables.
- 2026-10-22. Q: Why is there no REQ-28? A: REQ-28 (per-VM guest password) was removed with the base disk change; its ID is not reused. The only guest secret is `APP_GUEST_PASSWORD_FILE` (REQ-22).
- 2026-10-22. Q: The GA bootstrap runs after openssh-server is already in the base image; does it break P4? A: Constitution amended to 2.0.0: P4 switches to SSH when key access is configured, which happens at the end of creation.
- 2026-10-22. Q: Can two VMs be created at the same time? A: No. One creation at a time across all VMs (REQ-32), because creations change and attach the shared base disk; other operations keep the per-VM rule of REQ-15.

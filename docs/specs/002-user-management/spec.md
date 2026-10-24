# User management

## 1. Metadata
- Status: Approved
- Updated: 2026-10-22
- Constitution version: 2.0.0
- Required by: none

## 2. Constitution relationship
- Applies:
  - P1: the backend only validates input, invokes one script per operation (list, create, delete) and reports the result.
  - P2: each operation has its own script file with `set -euo pipefail`, explicit arguments and a documented exit-code table; the password reaches the script as the path of a secret file given as an argument (REQ-9).
  - P3: username, password and checkboxes are validated against an allowlist in the UI, in the backend and in the script, and passed as discrete arguments.
  - P4: every operation runs over SSH as root with the backend key (001 REQ-24); a user created with a key has the password locked, so password login is disabled for that user; a user created with a password logs in only at the console or with su, because SSH password login stays disabled for every user (001 REQ-30).
  - P5: the user's private key lives only in memory, is downloadable once, is never logged or stored, and the UI shows a usage message (same conditions as 001 US-3).
  - P6: every operation ends with a success or failure message; the history keeps command, exit code, stderr and timestamp of every create, delete and failed list, without passwords or keys (successful lists are not recorded, REQ-19).
  - P7: the home card "Gestión de usuarios" opens one screen for the module; forms confirm the result.
  - P8: each script runs on its own with bats tests; a before/after demonstration on a real VM covers what stubs cannot (AC-21, AC-22, AC-23).
  - P9: no new abstraction beyond the three operations; reuses the VM selector, history and key download of 001.
- Conflicts: none. P2 vs password in argv resolved in the spec: the password is passed as a secret-file path (2026-10-22 clarification).
## 3. User stories
| ID | Priority | Actor | Story |
|---|---|---|---|
| US-1 | P1 | Student admin | As a student admin, I need to see the regular users of the selected VM with their sudo and key status, so I know what exists before creating or deleting |
| US-2 | P1 | Student admin | As a student admin, I need to create a user with optional passwordless sudo and optional key-only SSH access with a one-time private key download, so I can give people controlled access to the VM |
| US-3 | P1 | Student admin | As a student admin, I need to delete a user, optionally removing every file it owns, so I can revoke access and clean the system |
| US-4 | P2 | Student admin | As a student admin, I need to see the history of operations of the module, so I can diagnose failures |

## 4. Requirements
- REQ-1 (US-1): The module screen uses the same VM selector as 001 (REQ-2) and lists the regular users (UID 1000-59999) of the selected VM with username, UID, sudo (Sí/No) and key (Sí/No), read from the VM at each load and after each operation.
- REQ-2 (US-1): Sudo is "Sí" when the user can run sudo by any means (sudo group or sudoers rule); key is "Sí" when the user's `~/.ssh/authorized_keys` holds at least one key.
- REQ-3 (US-2): The create form has a username field, a "Privilegios de sudo" checkbox and a "Generar llaves SSH" checkbox, both unchecked by default.
- REQ-4 (US-2): The username must match `^[a-z][a-z0-9_-]{0,31}$`; a name that already exists on the VM (any UID, for example `root` or `www-data`) is rejected with "El usuario ya existe en la VM"; a name used by an existing group with no user of that name is rejected with "Ya existe un grupo con ese nombre en la VM"; in both cases nothing changes on the VM.
- REQ-5 (US-2): The new user gets the home directory `/home/<username>`, the shell `/bin/bash` and a primary group with its own name.
- REQ-6 (US-2): With "Privilegios de sudo" checked, the user can run any command with sudo without a password prompt; unchecked, the user has no sudo rights.
- REQ-7 (US-2): With "Generar llaves SSH" unchecked, the form shows a password and a confirmation field; both must match and hold 8 to 72 printable ASCII characters without leading or trailing spaces; the user logs in with that password at the VM console and with `su - <username>`; SSH password login stays disabled for every user (P4, 001 REQ-30), so SSH with that password is refused.
- REQ-8 (US-2): With "Generar llaves SSH" checked, the form shows no password fields; an ed25519 key pair without passphrase is generated, the public key is installed in the user's `~/.ssh/authorized_keys` (owned by the user, permissions 600, `~/.ssh` 700), and the account password is locked, so no password login (SSH, console or su) works for that user.
- REQ-9 (US-2): The password reaches the create script as the path of a temporary secret file (permissions 600) given as an argument, and the file is deleted when the operation ends, success or failure; the password never appears in argv, the history, logs, script output or the UI after submission.
- REQ-10 (US-2): The user's private key follows 001 US-3: kept only in backend memory, downloadable once, discarded after the first download, after 10 minutes, or as soon as the delete of its user succeeds, whichever comes first, never recorded; the file is named `id_ed25519_<vm>_<username>`. A download attempt after the key was discarded shows "La llave ya no está disponible; elimine y vuelva a crear el usuario para obtener una nueva".
- REQ-11 (US-2): After a creation with keys, the UI shows a download button and a message on correct use of the private key: keep it private, set permissions to 600, do not share it or commit it, and how to connect (`ssh -i id_ed25519_<vm>_<username> <username>@<VM IPv4>`).
- REQ-12 (US-2): A creation that fails after the account was added removes the account, its home and its sudo rule, so no partial user remains. If that cleanup also fails, the script exits with its own exit code (distinct from the creation failure), the UI shows a specific message naming what may remain (for example "No se pudo crear el usuario ana y la limpieza falló; revise la VM") and the history stderr records both errors.
- REQ-13 (US-3): Each listed user has a delete button that opens a confirmation dialog naming the user, with an "Eliminar todos sus archivos" checkbox unchecked by default; the script runs only after confirming.
- REQ-14 (US-3): Only users with UID 1000-59999 can be deleted; a request for `root`, a system user or a missing user is rejected with "Solo se pueden eliminar usuarios regulares existentes".
- REQ-15 (US-3): Deleting a user terminates all its processes first, then removes the account, its primary group (when no other user uses it) and its sudo rule.
- REQ-16 (US-3): With "Eliminar todos sus archivos" checked, deletion also removes `/home/<username>` entirely, `/var/mail/<username>` and every file owned by the user on local filesystems, skipping `/proc`, `/sys`, `/dev` and `/run`; a directory owned by the user outside `/home/<username>` is removed only if it is empty after that, so files of other users stay; a directory kept this way is given to `root:root` with its permissions unchanged, so no file or directory keeps the deleted UID; unchecked, every file stays on disk.
- REQ-17 (US-1..US-3): Every operation runs over SSH as root with the backend key against the VM's IPv4 (001 REQ-24, REQ-29), is executed by its own script file with a 15-minute timeout (001 REQ-14) and follows the one-operation-per-VM rule (001 REQ-15).
- REQ-18 (US-1..US-3): Every operation ends with a success or failure message in the UI; failure messages are understandable and do not show raw stack traces. A successful list shows the toast "Lista de usuarios actualizada" on every load, including the reload after each create or delete; a failed list shows the usual failure message.
- REQ-19 (US-4): Every create and delete operation, and every failed list, is recorded with operation name, VM, command and arguments, exit code, stderr, result and timestamp; a successful list adds no history row; passwords and private keys are never recorded. Listing still follows the one-operation-per-VM rule (REQ-17).
- REQ-20 (US-4): The module screen shows the last 50 recorded operations of the module; each row can expand to show its stderr.
- REQ-21 (US-1..US-4): The home card "Gestión de usuarios" is enabled and navigates to the module screen (supersedes the "Próximamente" state of 001 REQ-19 for this card only).
- REQ-22 (US-1..US-3): Each script file starts with a header documenting usage, arguments and exit-code table (including reserved code 99 "Operación no implementada"), followed by `set -euo pipefail`, with comments organized in sections and numbered steps.
- REQ-23 (US-1..US-4): The UI is built with shadcn/ui components and all its text is in Spanish.

## 5. Acceptance criteria
- AC-1 (US-1): Given a VM with users `ana` (UID 1001, sudo, key) and `luis` (UID 1002, no sudo, no key), when the user opens the module screen with that VM selected, then the table shows `ana | 1001 | Sí | Sí` and `luis | 1002 | No | No`, and no row for `root` or `www-data`.
- AC-2 (US-1): Given no VM selected, when the user opens the module screen, then the list is empty and the create button is disabled with a hint to select a VM.
- AC-3 (US-1..US-3): Given a powered-off VM, when the user lists, creates or deletes users, then the UI shows "La VM no tiene dirección IP; verifique que esté encendida".
- AC-4 (US-2): Given username `ana`, sudo checked and keys checked, when the user submits, then the create script is invoked with the username and both options as separate arguments and the UI shows the result, a download button and the usage message with the VM's IPv4.
- AC-5 (US-2): Given usernames `Ana`, `1ana`, `ana;rm`, an empty field or a 33-character name, when the user submits, then the UI and backend reject it and no script is invoked.
- AC-6 (US-2): Given user `ana` exists on the VM, when the user creates `ana` again, then the UI shows "El usuario ya existe en la VM" and the existing user is unchanged.
- AC-7 (US-2): Given keys unchecked and a password of 7 characters, 73 characters, a non-ASCII character, a leading space or a confirmation that differs, when the user submits, then the form shows a validation error and no script is invoked.
- AC-8 (US-2): Given keys checked, when the form is shown, then no password fields are visible and none are sent.
- AC-9 (US-2): Given a user created with sudo checked, when it runs `sudo -n true` over SSH, then the command succeeds without a password prompt; given a user created with sudo unchecked, the same command fails.
- AC-10 (US-2): Given a user created with keys, when someone connects with the downloaded key, then the SSH session opens; when someone tries `su - <username>` or SSH with any password, then it fails because the password is locked.
- AC-11 (US-2): Given a user created with password `Secreta123`, when it logs in on the VM console or runs `su - <username>` with that password, then the login succeeds; when someone runs `ssh <username>@<VM IPv4>` with that password, then the login is refused.
- AC-12 (US-2): Given a creation with password `Secreta123`, when the history, logs, script output and process arguments are inspected, then `Secreta123` does not appear and the temporary secret file no longer exists.
- AC-13 (US-2): Given the key was downloaded once, or generated more than 10 minutes ago, when the user tries to download it, then the UI shows "La llave ya no está disponible; elimine y vuelva a crear el usuario para obtener una nueva".
- AC-14 (US-2): Given any key operation, when the history and logs are inspected, then no private key content appears.
- AC-15 (US-2): Given a creation that fails after the account was added (for example, installing the public key fails), when the operation finishes, then the UI shows a failure and the user, its home and its sudo rule do not exist on the VM.
- AC-16 (US-3): Given user `luis` listed, when the user clicks delete, then a confirmation dialog names `luis` with "Eliminar todos sus archivos" unchecked, and the delete script is invoked only after confirming.
- AC-17 (US-3): Given a request to delete `root`, `www-data` or a missing user (UI or direct API call), when it is processed, then it is rejected with "Solo se pueden eliminar usuarios regulares existentes" and the account is unchanged.
- AC-18 (US-3): Given user `luis` with a running process, when the user confirms delete, then the process is terminated, the account is removed and the UI shows the result.
- AC-19 (US-3): Given `luis` owns `/home/luis/a.txt` and `/tmp/luis.log` and delete runs with "Eliminar todos sus archivos" unchecked, when it finishes, then both files still exist.
- AC-20 (US-3): Given the same files plus `/srv/datos/x` owned by `luis`, when delete runs with "Eliminar todos sus archivos" checked, then `/home/luis`, `/var/mail/luis`, `/tmp/luis.log` and `/srv/datos/x` no longer exist and files of other users in `/tmp` and `/srv/datos` remain.
- AC-21 (US-2): Given a real VM, when a user is created with sudo and keys, then before/after `getent passwd`, `sudo -l -U <username>`, `passwd -S <username>` (locked) and the authorized_keys content show the change.
- AC-22 (US-3): Given a real VM, when a user with sudo is deleted with files, then before/after `getent passwd`, `sudo -l -U <username>` and `find / -xdev -user <uid>` show nothing remains.
- AC-23 (US-2): Given a user created with sudo and then deleted, when `/etc/sudoers` and `/etc/sudoers.d/` are inspected, then no rule for that user remains and `visudo -c` passes.
- AC-24 (US-1..US-3): Given an operation running on a VM, when another operation (of any module) is requested on the same VM, then it is rejected with "Operación en curso en esta VM".
- AC-25 (US-1..US-3): Given a script run directly with a missing argument or a value outside its allowlist (for example username `Ana` or a missing secret file), when it runs, then it exits with code 2 and makes no change.
- AC-26 (US-4): Given more than 50 recorded operations of the module, when the user opens the module screen, then only the 50 most recent are listed, newest first, each with expandable stderr.
- AC-27 (US-1..US-4): Given the home screen, when it loads, then the "Gestión de usuarios" card navigates to the module screen and the systemd and HAProxy cards still show "Próximamente".
- AC-28 (US-1..US-3): Given the script files, when one is opened, then it contains usage, arguments, exit-code table (including 99), `set -euo pipefail` and comments with sections and numbered steps.
- AC-29 (US-2): Given group `ana` exists on the VM and no user `ana` exists, when the user creates `ana`, then the UI shows "Ya existe un grupo con ese nombre en la VM", no user is created and the group is unchanged.
- AC-30 (US-1, US-4): Given the module screen is opened 20 times and every list succeeds, when the history is inspected, then no list row was added; given a list that fails (for example SSH timeout), then the history has one row with its exit code and stderr.
- AC-31 (US-3): Given `luis` owns directory `/srv/proyecto` holding `/srv/proyecto/l.txt` owned by `luis` and `/srv/proyecto/ana.txt` owned by `ana`, plus an empty directory `/srv/vacio` owned by `luis`, when delete runs with "Eliminar todos sus archivos" checked, then `/srv/proyecto/l.txt` and `/srv/vacio` no longer exist, and `/srv/proyecto` (now owned by `root:root`, permissions unchanged) and `/srv/proyecto/ana.txt` remain.
- AC-32 (US-2, US-3): Given user `ana` was created with keys and the key was not downloaded yet, when `ana` is deleted successfully and the user then tries to download the key, then the UI shows "La llave ya no está disponible; elimine y vuelva a crear el usuario para obtener una nueva".
- AC-33 (US-2): Given a creation that fails after the account was added and whose cleanup also fails, when the operation finishes, then the script exits with the cleanup-failure code, the UI shows "No se pudo crear el usuario <username> y la limpieza falló; revise la VM" naming what may remain, and the history stderr contains both errors.
- AC-34 (US-1): Given a selected running VM, when the user list loads successfully (on screen load or after a create or delete), then the toast "Lista de usuarios actualizada" appears; when the list fails, then the failure message appears instead.

## 6. Out of scope
- Editing existing users: toggling sudo, changing passwords, generating or regenerating keys, renaming.
- Creating a user with both a password and a key.
- SSH password login for any user, including users created with a password.
- Reusing an existing group as the new user's primary group.
- Group management, quotas, shells other than `/bin/bash`, custom home paths, UID selection.
- Deleting system users (UID below 1000 or above 59999) and root.
- Key passphrases and key types other than ed25519.
- Removing files on network or non-local filesystems.

## 7. Clarifications
- 2026-10-22. Q: Which root key conditions does "US-2" in the assignment refer to? A: 001 US-3: ed25519 without passphrase, kept in memory, one-time download, discarded after 10 minutes, usage message.
- 2026-10-22. Q: How does the new user authenticate, and what does disabling login and password mean? A: Without keys, a password set in the form. With keys, no password is set and the account password is locked, so no password login works (SSH is already password-free globally since 001 REQ-30).
- 2026-10-22. Q: How do sudo privileges work? A: Sudo without a password prompt, since key-only users have a locked password.
- 2026-10-22. Q: What does "delete all files" remove? A: Home, mail spool and every file owned by the user on local filesystems (skipping /proc, /sys, /dev, /run). Unchecked, every file stays.
- 2026-10-22. Q: Which users can be deleted, and with running processes? A: Only UID 1000-59999; the user's processes are terminated before deleting.
- 2026-10-22. Q: Username rules? A: `^[a-z][a-z0-9_-]{0,31}$`; an existing name on the VM is rejected with "El usuario ya existe en la VM".
- 2026-10-22. Q: Password rules? A: 8-72 printable ASCII characters, no leading or trailing spaces, with a matching confirmation field.
- 2026-10-22. Q: Can existing users be edited? A: No, only create and delete; out of scope.
- 2026-10-22. Q: What does the user list show? A: Regular users with username, UID, sudo and key status, read from the VM.
- 2026-10-22. Q: P2 requires inputs as explicit arguments, but a password in argv leaks to the process list and history; how is it resolved? A: In the spec: the backend writes the password to a temporary secret file (600) and passes its path as an argument, deleting it after the run, like `APP_GUEST_PASSWORD_FILE` in 001.
- 2026-10-22. Q: Where can a user created with a password log in? A: Only at the VM console and with `su - <username>`; SSH stays key-only for everyone (P4, 001 REQ-30), so `ssh ana@<IP>` with the password is refused.
- 2026-10-22. Q: How is listing users treated in the history and the per-VM lock? A: Listing keeps the per-VM lock, but only failed lists are recorded in the history; successful lists add no history rows.
- 2026-10-22. Q: With "Eliminar todos sus archivos", what happens to a directory the user owns outside /home/<username> that holds other users' files? A: Only what the deleted user owns is removed; such a directory is removed only if empty afterwards, so other users' files stay. /home/<username> is deleted entirely.
- 2026-10-22. Q: What if a group with the username already exists on the VM without such a user? A: Creation is rejected with "Ya existe un grupo con ese nombre en la VM" and nothing changes.
- 2026-10-22. Q: Who owns a directory of the deleted user that is kept because it holds other users' files? A: It is given to `root:root` with permissions unchanged, so no orphan UID remains and AC-22's find stays empty.
- 2026-10-22. Q: What happens to a pending private key when its user is deleted before the download? A: It is discarded from backend memory as soon as the delete succeeds; a later download shows "La llave ya no está disponible, genere una nueva".
- 2026-10-22. Q: What if the cleanup of a failed creation also fails? A: The script uses a separate exit code, the UI shows a specific message naming what may remain (for example "No se pudo crear el usuario ana y la limpieza falló; revise la VM") and the history stderr records both errors.
- 2026-10-22. Q: What does a successful list show? A: A toast "Lista de usuarios actualizada" on every load, including the reload after each create or delete; a failed list shows the usual failure message.
- 2026-10-22. Q: Regenerating keys is out of scope here, yet the 001 message says "genere una nueva"; what should this module show for an unavailable key? A: Its own message: "La llave ya no está disponible; elimine y vuelva a crear el usuario para obtener una nueva" (AC-13, AC-32).

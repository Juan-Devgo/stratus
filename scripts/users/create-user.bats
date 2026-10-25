#!/usr/bin/env bats
# Pruebas de create-user.sh con stubs de VBoxManage, ssh y ssh-keygen: no tocan VirtualBox ni VMs.

load test_helper

# "run --separate-stderr" necesita bats 1.5 o superior.
bats_require_minimum_version 1.5.0

SCRIPT="$USERS_DIR/create-user.sh"

# Contenido de la llave privada que escribe el ssh-keygen falso del helper.
FAKE_KEY=$'-----BEGIN OPENSSH PRIVATE KEY-----\nFAKEKEYDATA\n-----END OPENSSH PRIVATE KEY-----'

setup() {
  setup_users_stubs vm1 192.168.1.50
  CALLS="$BATS_TEST_TMPDIR/calls.log"
  REMOTE="$BATS_TEST_TMPDIR/remote.log"
  export APP_SSH_KEY_PATH="$BATS_TEST_TMPDIR/stratus_backend"
  : >"$APP_SSH_KEY_PATH"
}

@test "AC-28 create-user.sh tiene cabecera, códigos de salida, modo estricto y secciones" {
  assert_script_structure "$SCRIPT"
}

@test "AC-25 create-user sale con 2 ante argumentos inválidos y no llama a VBoxManage ni ssh" {
  setup_password_file "Secreta123"
  local long_name
  long_name="$(printf 'a%.0s' {1..33})"
  local bad_file="$BATS_TEST_TMPDIR/bad-password"

  # Cantidad de argumentos: faltan, sobran, o el archivo de contraseña no corresponde al modo.
  run "$SCRIPT" </dev/null
  [ "$status" -eq 2 ]
  run "$SCRIPT" vm1 ana yes </dev/null
  [ "$status" -eq 2 ]
  run "$SCRIPT" vm1 ana no no </dev/null
  [ "$status" -eq 2 ]
  run "$SCRIPT" vm1 ana no no "$PASSWORD_FILE" extra </dev/null
  [ "$status" -eq 2 ]
  # Valores fuera de la lista permitida.
  run "$SCRIPT" VM_1 ana no no "$PASSWORD_FILE" </dev/null
  [ "$status" -eq 2 ]
  for name in Ana 1ana 'ana;rm' "$long_name"; do
    run "$SCRIPT" vm1 "$name" no no "$PASSWORD_FILE" </dev/null
    [ "$status" -eq 2 ]
  done
  run "$SCRIPT" vm1 ana si no "$PASSWORD_FILE" </dev/null
  [ "$status" -eq 2 ]
  run "$SCRIPT" vm1 ana no si "$PASSWORD_FILE" </dev/null
  [ "$status" -eq 2 ]
  # Archivo de contraseña inexistente o con una contraseña fuera de la regla.
  run "$SCRIPT" vm1 ana no no "$BATS_TEST_TMPDIR/no-existe" </dev/null
  [ "$status" -eq 2 ]
  for password in "Corta12" "$(printf 'x%.0s' {1..73})" "Contraseñ1a" " Secreta123" "Secreta123 "; do
    printf '%s' "$password" >"$bad_file"
    run --separate-stderr "$SCRIPT" vm1 ana no no "$bad_file" </dev/null
    [ "$status" -eq 2 ]
    # El mensaje de error no repite la contraseña.
    [[ "$stderr" != *"$password"* ]]
  done
  # Sin la llave del backend tampoco se ejecuta nada.
  APP_SSH_KEY_PATH="" run "$SCRIPT" vm1 ana no no "$PASSWORD_FILE" </dev/null
  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

@test "AC-3 create-user sale con 7 si la VM no tiene IP y no ejecuta ssh" {
  setup_users_stubs vm1 -
  setup_password_file "Secreta123"

  run --separate-stderr "$SCRIPT" vm1 ana no no "$PASSWORD_FILE" </dev/null

  [ "$status" -eq 7 ]
  [[ "$stderr" == *"La VM no tiene dirección IP; verifique que esté encendida"* ]]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

@test "AC-8 create-user con llaves rechaza password_file" {
  setup_password_file "Secreta123"

  run "$SCRIPT" vm1 ana no yes "$PASSWORD_FILE" </dev/null

  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

@test "AC-6 create-user sale con 6 si el usuario ya existe y no crea nada" {
  setup_password_file "Secreta123"

  LOOKUP=user run --separate-stderr "$SCRIPT" vm1 ana no no "$PASSWORD_FILE" </dev/null

  [ "$status" -eq 6 ]
  [[ "$stderr" == *"El usuario ya existe en la VM"* ]]
  ! grep -qF -- "useradd" "$CALLS" || return 1
}

@test "AC-29 create-user sale con 8 si existe un grupo con ese nombre y no crea nada" {
  setup_password_file "Secreta123"

  LOOKUP=group run --separate-stderr "$SCRIPT" vm1 ana no no "$PASSWORD_FILE" </dev/null

  [ "$status" -eq 8 ]
  [[ "$stderr" == *"Ya existe un grupo con ese nombre en la VM"* ]]
  ! grep -qE 'useradd|groupadd' "$CALLS" || return 1
}

@test "AC-4 create-user con sudo y llaves crea la cuenta y entrega solo la llave privada" {
  run --separate-stderr "$SCRIPT" vm1 ana yes yes </dev/null

  [ "$status" -eq 0 ]
  [ "$output" = "$FAKE_KEY" ]
  grep -qF -- "useradd -m -s /bin/bash -U ana" "$CALLS"
  grep -qF -- "authorized_keys" "$CALLS"
  grep -qF -- "passwd -l ana" "$CALLS"
  grep -qF -- "sudoers.d" "$CALLS"
  # El par se genera en el host con el comentario stratus-<vm>-<usuario>.
  grep -qE '^ssh-keygen -q -t ed25519 -N +-C stratus-vm1-ana -f ' "$CALLS"
  # El directorio temporal con la llave privada ya no existe.
  keyfile="$(grep -E '^ssh-keygen ' "$CALLS" | sed 's/.* -f //')"
  [ -n "$keyfile" ]
  [ ! -e "$keyfile" ]
  [ ! -e "$(dirname "$keyfile")" ]
}

@test "create-user no mezcla la salida de visudo ni de passwd con la llave privada" {
  run --separate-stderr "$SCRIPT" vm1 ana yes yes </dev/null

  [ "$status" -eq 0 ]
  [ "$output" = "$FAKE_KEY" ]
  [[ "$output" != *"parsed OK"* ]]
  [[ "$output" != *"contraseña cambiada"* ]]
}

@test "AC-9 create-user con sudo escribe la regla NOPASSWD validada; sin sudo no hay paso sudoers" {
  run --separate-stderr "$SCRIPT" vm1 ana yes yes </dev/null
  [ "$status" -eq 0 ]
  grep -F -- "sudoers.d" "$REMOTE" | grep -qF -- "NOPASSWD: ALL"
  grep -F -- "sudoers.d" "$REMOTE" | grep -qF -- "chmod 440"
  grep -F -- "sudoers.d" "$REMOTE" | grep -qF -- "visudo -cf"
  # La regla se escribe con un nombre temporal con punto, que sudo ignora, y se renombra.
  grep -F -- "sudoers.d" "$REMOTE" | grep -qF -- "/etc/sudoers.d/.stratus-"
  grep -F -- "sudoers.d" "$REMOTE" | grep -qF -- "mv "

  : >"$CALLS"
  : >"$REMOTE"
  run --separate-stderr "$SCRIPT" vm1 luis no yes </dev/null
  [ "$status" -eq 0 ]
  ! grep -qF -- "sudoers" "$CALLS" || return 1
  ! grep -qF -- "NOPASSWD" "$CALLS" || return 1
}

@test "AC-9 create-user instala sudo solo si falta, antes de useradd" {
  run --separate-stderr "$SCRIPT" vm1 ana yes yes </dev/null
  [ "$status" -eq 0 ]
  grep -qF -- "command -v sudo >/dev/null || DEBIAN_FRONTEND=noninteractive apt-get install -y sudo" "$REMOTE"
  install_line="$(grep -nF -- "apt-get install -y sudo" "$REMOTE" | head -n1 | cut -d: -f1)"
  useradd_line="$(grep -nF -- "useradd" "$REMOTE" | head -n1 | cut -d: -f1)"
  [ "$install_line" -lt "$useradd_line" ]

  : >"$CALLS"
  : >"$REMOTE"
  run --separate-stderr "$SCRIPT" vm1 luis no yes </dev/null
  [ "$status" -eq 0 ]
  ! grep -qF -- "apt-get" "$CALLS" || return 1
}

@test "AC-10 create-user con llaves instala authorized_keys y bloquea la contraseña" {
  run --separate-stderr "$SCRIPT" vm1 ana no yes </dev/null

  [ "$status" -eq 0 ]
  grep -qF -- "install -d -m 700 -o ana -g ana /home/ana/.ssh" "$CALLS"
  grep -F -- "authorized_keys" "$REMOTE" | grep -qF -- "ssh-ed25519 AAAAFAKEPUBLICKEY"
  grep -F -- "authorized_keys" "$REMOTE" | grep -qF -- "chmod 600"
  grep -F -- "authorized_keys" "$REMOTE" | grep -qF -- 'chown "$1:$1"'
  grep -qF -- "passwd -l ana" "$CALLS"
  # El bloqueo va al final: después de instalar la llave.
  keys_line="$(grep -nF -- "authorized_keys" "$REMOTE" | head -n1 | cut -d: -f1)"
  lock_line="$(grep -nF -- "passwd -l ana" "$REMOTE" | head -n1 | cut -d: -f1)"
  [ "$keys_line" -lt "$lock_line" ]
  ! grep -qF -- "chpasswd" "$CALLS" || return 1
}

@test "AC-11 create-user con contraseña usa chpasswd y no toca sshd ni bloquea la cuenta" {
  setup_password_file "Secreta123"

  run --separate-stderr "$SCRIPT" vm1 ana no no "$PASSWORD_FILE" </dev/null

  [ "$status" -eq 0 ]
  [ "$output" = "Usuario ana creado en vm1" ]
  grep -qF -- "useradd -m -s /bin/bash -U ana" "$CALLS"
  grep -qE '(^| )chpasswd( |$)' "$CALLS"
  ! grep -qF -- "passwd -l" "$CALLS" || return 1
  ! grep -qF -- "sshd_config" "$CALLS" || return 1
  ! grep -qF -- "authorized_keys" "$CALLS" || return 1
  ! grep -qE '^ssh-keygen ' "$CALLS" || return 1
}

@test "AC-12 create-user envía la contraseña por stdin y nunca en argv ni en la salida" {
  setup_password_file "Secreta123"

  run --separate-stderr "$SCRIPT" vm1 ana no no "$PASSWORD_FILE" </dev/null

  [ "$status" -eq 0 ]
  grep -qxF -- "ana:Secreta123" "$BATS_TEST_TMPDIR/ssh-stdin.log"
  ! grep -qF -- "Secreta123" "$CALLS" || return 1
  ! grep -qF -- "Secreta123" "$REMOTE" || return 1
  [[ "$output" != *"Secreta123"* ]]
  [[ "$stderr" != *"Secreta123"* ]]
}

@test "AC-14 create-user no escribe la llave privada en stderr ni en las llamadas" {
  run --separate-stderr "$SCRIPT" vm1 ana yes yes </dev/null

  [ "$status" -eq 0 ]
  [[ "$stderr" != *"PRIVATE KEY"* ]]
  [[ "$stderr" != *"FAKEKEYDATA"* ]]
  ! grep -qF -- "PRIVATE KEY" "$CALLS" || return 1
  ! grep -qF -- "FAKEKEYDATA" "$CALLS" || return 1
  ! grep -qF -- "FAKEKEYDATA" "$REMOTE" || return 1
  ! grep -qF -- "FAKEKEYDATA" "$BATS_TEST_TMPDIR/ssh-stdin.log" || return 1
}

@test "AC-15 create-user limpia la cuenta, el home y la regla sudo si falla tras crearla" {
  FAIL_REMOTE="authorized_keys" run --separate-stderr "$SCRIPT" vm1 ana yes yes </dev/null

  [ "$status" -eq 5 ]
  # No se entrega ninguna llave si la creación falló.
  [ -z "$output" ]
  [[ "$stderr" != *"PRIVATE KEY"* ]]
  grep -qF -- "useradd -m -s /bin/bash -U ana" "$CALLS"
  grep -qF -- "userdel -r ana" "$CALLS"
  grep -qF -- "rm -f /etc/sudoers.d/stratus-ana /etc/sudoers.d/.stratus-ana" "$CALLS"
  grep -qF -- "groupdel" "$CALLS"
  # El último paso comprueba que no quede cuenta, home ni regla sudo.
  grep -F -- '[ ! -e "/home/$1" ]' "$REMOTE" | grep -qF -- '[ ! -e "/etc/sudoers.d/stratus-$1" ]'
  # La limpieza va después del fallo, y el directorio temporal de la llave también se borra.
  create_line="$(grep -nF -- "authorized_keys" "$REMOTE" | head -n1 | cut -d: -f1)"
  cleanup_line="$(grep -nF -- "userdel -r ana" "$REMOTE" | head -n1 | cut -d: -f1)"
  [ "$create_line" -lt "$cleanup_line" ]
  keyfile="$(grep -E '^ssh-keygen ' "$CALLS" | sed 's/.* -f //')"
  [ ! -e "$(dirname "$keyfile")" ]
}

@test "AC-15 create-user no limpia nada si falla antes de crear la cuenta" {
  FAIL_REMOTE="apt-get" run --separate-stderr "$SCRIPT" vm1 ana yes yes </dev/null

  [ "$status" -eq 5 ]
  ! grep -qF -- "useradd" "$CALLS" || return 1
  ! grep -qF -- "userdel" "$CALLS" || return 1
}

@test "AC-33 create-user sale con 9 si la limpieza también falla y deja ambos errores en stderr" {
  FAIL_REMOTE="authorized_keys" FAIL_CLEANUP="userdel" \
    run --separate-stderr "$SCRIPT" vm1 ana yes yes </dev/null

  [ "$status" -eq 9 ]
  # El error original se imprimió antes que el de la limpieza.
  [[ "$stderr" == *"Falló el paso sh"* ]]
  [ "$(tail -n1 <<<"$stderr")" = "Limpieza fallida; puede quedar en la VM: cuenta ana, /home/ana, /etc/sudoers.d/stratus-ana" ]
  # La limpieza intentó todos sus pasos aunque userdel falló.
  grep -qF -- "groupdel" "$CALLS"
  grep -qF -- "/etc/sudoers.d/.stratus-ana" "$CALLS"
}

@test "AC-37 create-user con --jump entra al servidor por el balanceador" {
  setup_password_file "Secreta123"
  stub_jump_topology lb1 running web-a running
  # jump_sync descarta la llave del backend: necesita su llave pública.
  printf 'ssh-ed25519 BACKENDBLOB stratus-backend\n' >"$APP_SSH_KEY_PATH.pub"

  run --separate-stderr "$SCRIPT" --jump lb1 web-a ana no no "$PASSWORD_FILE" </dev/null

  [ "$status" -eq 0 ]
  assert_routed_ssh lb1 web-a
}

@test "AC-37 create-user sale con 2 si --jump no trae un balanceador válido y no llama a nada" {
  setup_password_file "Secreta123"
  stub_jump_topology lb1 running web-a running
  # jump_sync descarta la llave del backend: necesita su llave pública.
  printf 'ssh-ed25519 BACKENDBLOB stratus-backend\n' >"$APP_SSH_KEY_PATH.pub"

  run "$SCRIPT" --jump </dev/null
  [ "$status" -eq 2 ]
  run "$SCRIPT" --jump Lb1 web-a ana no no "$PASSWORD_FILE" </dev/null
  [ "$status" -eq 2 ]
  run "$SCRIPT" --jump "" web-a ana no no "$PASSWORD_FILE" </dev/null
  [ "$status" -eq 2 ]
  run "$SCRIPT" --jump 'lb1;x' web-a ana no no "$PASSWORD_FILE" </dev/null
  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

@test "AC-29 create-user con --jump sale con 12 si el balanceador está apagado y no llama a ssh" {
  setup_password_file "Secreta123"
  stub_jump_topology lb1 poweroff web-a running

  run --separate-stderr "$SCRIPT" --jump lb1 web-a ana no no "$PASSWORD_FILE" </dev/null

  [ "$status" -eq 12 ]
  [[ "$stderr" == *"El balanceador lb1 está apagado; enciéndalo para llegar al servidor"* ]]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

# remote_line <texto fijo> [n]: número de la n-ésima línea de remote.log que lo contiene (vacío si no hay).
remote_line() {
  grep -nF -- "$1" "$REMOTE" | sed -n "${2:-1}p" | cut -d: -f1 || true
}

@test "AC-27 create-user con --jump y llave sincroniza jump después de instalar authorized_keys" {
  stub_jump_topology lb1 running web-a running
  # jump_sync descarta la llave del backend: necesita su llave pública.
  printf 'ssh-ed25519 BACKENDBLOB stratus-backend\n' >"$APP_SSH_KEY_PATH.pub"

  run --separate-stderr "$SCRIPT" --jump lb1 web-a ana no yes </dev/null

  [ "$status" -eq 0 ]
  [[ "$output" == *"-----BEGIN OPENSSH PRIVATE KEY-----"* ]]
  install_key="$(remote_line "/.ssh/authorized_keys")"
  jump_write="$(remote_line "/var/lib/jump/.ssh/authorized_keys")"
  [ -n "$install_key" ]
  [ -n "$jump_write" ]
  [ "$install_key" -lt "$jump_write" ]
  # La sincronización es lo último remoto: nada de la cuenta cambia después.
  [ "$(wc -l <"$REMOTE")" -eq "$jump_write" ]
  assert_server_calls_routed lb1 web-a
}

@test "AC-27 create-user con --jump y contraseña no toca jump (no hay llave que copiar)" {
  stub_jump_topology lb1 running web-a running
  # jump_sync descarta la llave del backend: necesita su llave pública.
  printf 'ssh-ed25519 BACKENDBLOB stratus-backend\n' >"$APP_SSH_KEY_PATH.pub"
  setup_password_file "Secreta123"

  run --separate-stderr "$SCRIPT" --jump lb1 web-a ana no no "$PASSWORD_FILE" </dev/null

  [ "$status" -eq 0 ]
  ! grep -qF -- "/var/lib/jump" "$REMOTE" || return 1
}

@test "AC-27 create-user sin --jump no toca jump" {
  run --separate-stderr "$SCRIPT" vm1 ana no yes </dev/null

  [ "$status" -eq 0 ]
  ! grep -qF -- "/var/lib/jump" "$REMOTE" || return 1
}

@test "AC-42 create-user deshace la cuenta y sale con 15 si falla la actualización de jump; stdout vacío" {
  stub_jump_topology lb1 running web-a running
  # jump_sync descarta la llave del backend: necesita su llave pública.
  printf 'ssh-ed25519 BACKENDBLOB stratus-backend\n' >"$APP_SSH_KEY_PATH.pub"

  FAIL_REMOTE='/var/lib/jump' run --separate-stderr "$SCRIPT" --jump lb1 web-a ana no yes </dev/null

  [ "$status" -eq 15 ]
  [ -z "$output" ]
  [[ "$stderr" == *"jump"* ]]
  [[ "$stderr" != *"PRIVATE KEY"* ]]
  jump_write="$(remote_line "/var/lib/jump/.ssh/authorized_keys")"
  cleanup="$(remote_line "userdel -r ana")"
  [ -n "$jump_write" ]
  [ -n "$cleanup" ]
  [ "$jump_write" -lt "$cleanup" ]
}

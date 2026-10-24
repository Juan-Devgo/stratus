#!/usr/bin/env bats
# Pruebas de delete-user.sh con stubs de VBoxManage y ssh: no tocan VirtualBox ni VMs.

load test_helper

# "run --separate-stderr" necesita bats 1.5 o superior.
bats_require_minimum_version 1.5.0

SCRIPT="$USERS_DIR/delete-user.sh"

setup() {
  setup_users_stubs vm1 192.168.1.50
  CALLS="$BATS_TEST_TMPDIR/calls.log"
  REMOTE="$BATS_TEST_TMPDIR/remote.log"
  export APP_SSH_KEY_PATH="$BATS_TEST_TMPDIR/stratus_backend"
  : >"$APP_SSH_KEY_PATH"
}

@test "AC-28 delete-user.sh tiene cabecera, códigos de salida, modo estricto y secciones" {
  assert_script_structure "$SCRIPT"
}

@test "AC-25 delete-user sale con 2 ante argumentos inválidos y no llama a VBoxManage ni ssh" {
  local long_name
  long_name="$(printf 'a%.0s' {1..33})"

  run "$SCRIPT" </dev/null
  [ "$status" -eq 2 ]
  run "$SCRIPT" vm1 luis </dev/null
  [ "$status" -eq 2 ]
  run "$SCRIPT" vm1 luis no extra </dev/null
  [ "$status" -eq 2 ]
  run "$SCRIPT" VM_1 luis no </dev/null
  [ "$status" -eq 2 ]
  for name in Luis 1luis 'luis;rm' "$long_name" ""; do
    run "$SCRIPT" vm1 "$name" no </dev/null
    [ "$status" -eq 2 ]
  done
  run "$SCRIPT" vm1 luis si </dev/null
  [ "$status" -eq 2 ]
  run "$SCRIPT" vm1 luis "" </dev/null
  [ "$status" -eq 2 ]
  # Sin la llave del backend tampoco se ejecuta nada.
  APP_SSH_KEY_PATH="" run "$SCRIPT" vm1 luis no </dev/null
  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

@test "AC-3 delete-user sale con 7 si la VM no tiene IP y no ejecuta ssh" {
  setup_users_stubs vm1 -

  run --separate-stderr "$SCRIPT" vm1 luis no </dev/null

  [ "$status" -eq 7 ]
  [[ "$stderr" == *"La VM no tiene dirección IP; verifique que esté encendida"* ]]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

@test "AC-17 delete-user rechaza root, www-data, usuarios inexistentes y UID fuera de 1000-59999" {
  for target in 0 33 none 999 60000 65534; do
    : >"$CALLS"
    : >"$REMOTE"
    TARGET_UID="$target" run --separate-stderr "$SCRIPT" vm1 luis no </dev/null

    [ "$status" -eq 6 ]
    [[ "$stderr" == *"Solo se pueden eliminar usuarios regulares existentes"* ]]
    ! grep -qE 'pkill|userdel|rm ' "$CALLS" || return 1
  done
}

# Número de la primera línea de remote.log que contiene el texto, o vacío si no hay.
remote_line() {
  grep -nF -- "$1" "$REMOTE" | head -n1 | cut -d: -f1
}

@test "AC-18 delete-user termina los procesos antes de eliminar la cuenta" {
  TARGET_UID=1002 run --separate-stderr "$SCRIPT" vm1 luis no </dev/null

  [ "$status" -eq 0 ]
  [ "$output" = "Usuario luis eliminado de vm1" ]
  pkill_line="$(remote_line "pkill -KILL -u")"
  userdel_line="$(remote_line "userdel luis")"
  visudo_line="$(remote_line "visudo -c")"
  [ -n "$pkill_line" ] && [ -n "$userdel_line" ] && [ -n "$visudo_line" ]
  [ "$pkill_line" -lt "$userdel_line" ]
  [ "$userdel_line" -lt "$visudo_line" ]
  # La espera de los procesos tiene un límite de 10 segundos.
  grep -F -- "pkill -KILL -u" "$REMOTE" | grep -qF -- "pgrep -u"
  grep -F -- "pkill -KILL -u" "$REMOTE" | grep -qF -- '[ "$i" -le 10 ]'
}

@test "AC-18 delete-user sale con 5 si los procesos no terminan y no elimina la cuenta" {
  TARGET_UID=1002 FAIL_REMOTE="pkill" run --separate-stderr "$SCRIPT" vm1 luis yes </dev/null

  [ "$status" -eq 5 ]
  [[ "$stderr" == *"No se pudieron terminar los procesos de luis"* ]]
  ! grep -qE 'userdel|rm ' "$CALLS" || return 1
}

@test "AC-19 delete-user sin archivos no borra nada de disco" {
  TARGET_UID=1002 run --separate-stderr "$SCRIPT" vm1 luis no </dev/null

  [ "$status" -eq 0 ]
  ! grep -qE 'rm -rf|find ' "$CALLS" || return 1
  [ "$output" = "Usuario luis eliminado de vm1" ]
}

@test "AC-23 delete-user quita la regla sudo y verifica con visudo -c" {
  TARGET_UID=1002 run --separate-stderr "$SCRIPT" vm1 luis no </dev/null

  [ "$status" -eq 0 ]
  grep -qF -- "rm -f /etc/sudoers.d/stratus-luis" "$CALLS"
  rule_line="$(remote_line "rm -f /etc/sudoers.d/stratus-luis")"
  userdel_line="$(remote_line "userdel luis")"
  visudo_line="$(remote_line "visudo -c")"
  [ "$rule_line" -lt "$userdel_line" ]
  [ "$userdel_line" -lt "$visudo_line" ]
}

@test "AC-23 delete-user sale con 5 si userdel o visudo fallan" {
  TARGET_UID=1002 FAIL_REMOTE="^userdel" run --separate-stderr "$SCRIPT" vm1 luis no </dev/null
  [ "$status" -eq 5 ]
  ! grep -qF -- "visudo" "$CALLS" || return 1

  TARGET_UID=1002 FAIL_REMOTE="^visudo" run --separate-stderr "$SCRIPT" vm1 luis no </dev/null
  [ "$status" -eq 5 ]
  [ -z "$output" ]
}

@test "AC-20 delete-user con archivos borra home, correo y los archivos del UID" {
  TARGET_UID=1002 run --separate-stderr "$SCRIPT" vm1 luis yes </dev/null

  [ "$status" -eq 0 ]
  [ "$output" = "Usuario luis eliminado de vm1 junto con todos sus archivos" ]
  grep -qF -- "rm -rf -- /home/luis /var/mail/luis" "$CALLS"
  # Los archivos del UID se buscan en todo el árbol, saltando /proc, /sys, /dev, /run y los
  # sistemas de archivos de red.
  files_cmd="$(grep -F -- "-uid 1002 ! -type d" "$REMOTE")"
  [ -n "$files_cmd" ]
  for part in "find /" "-path /proc" "-path /sys" "-path /dev" "-path /run" "-fstype nfs" "-fstype nfs4" \
    "-fstype cifs" "-fstype smb3" "-fstype fuse.sshfs" "-prune" "rm -f --"; do
    grep -qF -- "$part" <<<"$files_cmd"
  done
  # Orden: procesos, home y correo, archivos, regla sudo y cuenta.
  pkill_line="$(remote_line "pkill -KILL -u")"
  home_line="$(remote_line "rm -rf -- /home/luis")"
  files_line="$(remote_line "-uid 1002 ! -type d")"
  rule_line="$(remote_line "rm -f /etc/sudoers.d/stratus-luis")"
  [ "$pkill_line" -lt "$home_line" ]
  [ "$home_line" -lt "$files_line" ]
  [ "$files_line" -lt "$rule_line" ]
}

@test "AC-20 delete-user sale con 5 si el borrado de archivos falla y no elimina la cuenta" {
  TARGET_UID=1002 FAIL_REMOTE="rm -rf" run --separate-stderr "$SCRIPT" vm1 luis yes </dev/null

  [ "$status" -eq 5 ]
  ! grep -qE 'userdel|visudo' "$CALLS" || return 1
}

@test "AC-31 delete-user borra directorios vacíos y entrega los demás a root:root" {
  TARGET_UID=1002 run --separate-stderr "$SCRIPT" vm1 luis yes </dev/null

  [ "$status" -eq 0 ]
  dirs_cmd="$(grep -F -- 'sort -rz' "$REMOTE")"
  [ -n "$dirs_cmd" ]
  # El UID llega como argumento del programa y los directorios se recorren del más profundo al más alto.
  for part in '-uid "$1" -type d' "-print0" 'rmdir -- "$d"' 'chown root:root -- "$d"' "-path /proc" "-fstype nfs"; do
    grep -qF -- "$part" <<<"$dirs_cmd"
  done
  [[ "$dirs_cmd" == *" bash 1002" ]]
  # Los directorios se tratan después de los archivos y antes de quitar la cuenta.
  files_line="$(remote_line "-uid 1002 ! -type d")"
  dirs_line="$(remote_line "sort -rz")"
  userdel_line="$(remote_line "userdel luis")"
  [ "$files_line" -lt "$dirs_line" ]
  [ "$dirs_line" -lt "$userdel_line" ]
}

@test "AC-31 delete-user sin archivos no busca directorios" {
  TARGET_UID=1002 run --separate-stderr "$SCRIPT" vm1 luis no </dev/null

  [ "$status" -eq 0 ]
  ! grep -qE 'sort -rz|rmdir|chown' "$CALLS" || return 1
}

@test "AC-37 delete-user con --jump entra al servidor por el balanceador" {
  stub_jump_topology lb1 running web-a running
  # jump_sync descarta la llave del backend: necesita su llave pública.
  printf 'ssh-ed25519 BACKENDBLOB stratus-backend\n' >"$APP_SSH_KEY_PATH.pub"

  run --separate-stderr "$SCRIPT" --jump lb1 web-a luis no </dev/null

  [ "$status" -eq 0 ]
  assert_server_calls_routed lb1 web-a
}

@test "AC-37 delete-user sale con 2 si --jump no trae un balanceador válido y no llama a nada" {
  stub_jump_topology lb1 running web-a running
  # jump_sync descarta la llave del backend: necesita su llave pública.
  printf 'ssh-ed25519 BACKENDBLOB stratus-backend\n' >"$APP_SSH_KEY_PATH.pub"

  run "$SCRIPT" --jump </dev/null
  [ "$status" -eq 2 ]
  run "$SCRIPT" --jump Lb1 web-a luis no </dev/null
  [ "$status" -eq 2 ]
  run "$SCRIPT" --jump "" web-a luis no </dev/null
  [ "$status" -eq 2 ]
  run "$SCRIPT" --jump 'lb1;x' web-a luis no </dev/null
  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

@test "AC-29 delete-user con --jump sale con 12 si el balanceador está apagado y no llama a ssh" {
  stub_jump_topology lb1 poweroff web-a running

  run --separate-stderr "$SCRIPT" --jump lb1 web-a luis no </dev/null

  [ "$status" -eq 12 ]
  [[ "$stderr" == *"El balanceador lb1 está apagado; enciéndalo para llegar al servidor"* ]]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

# remote_line <texto fijo> [n]: número de la n-ésima línea de remote.log que lo contiene (vacío si no hay).
remote_line() {
  grep -nF -- "$1" "$REMOTE" | sed -n "${2:-1}p" | cut -d: -f1 || true
}

@test "AC-27 delete-user con --jump sincroniza jump sin la llave del usuario antes de tocar la cuenta" {
  stub_jump_topology lb1 running web-a running
  # jump_sync descarta la llave del backend: necesita su llave pública.
  printf 'ssh-ed25519 BACKENDBLOB stratus-backend\n' >"$APP_SSH_KEY_PATH.pub"

  run --separate-stderr "$SCRIPT" --jump lb1 web-a luis no </dev/null

  [ "$status" -eq 0 ]
  id_check="$(remote_line "id -u")"
  jump_write="$(remote_line "/var/lib/jump/.ssh/authorized_keys")"
  pkill_line="$(remote_line "pkill")"
  userdel_line="$(remote_line "userdel luis")"
  [ -n "$id_check" ]
  [ -n "$jump_write" ]
  [ -n "$pkill_line" ]
  [ "$id_check" -lt "$jump_write" ]
  [ "$jump_write" -lt "$pkill_line" ]
  [ "$pkill_line" -lt "$userdel_line" ]
  # Una sola sincronización: salió bien y no hace falta repetirla.
  [ "$(grep -cF -- "/var/lib/jump/.ssh/authorized_keys" "$REMOTE")" -eq 1 ]
  assert_server_calls_routed lb1 web-a
}

@test "AC-42 delete-user sale con 15 si falla la actualización de jump, sin pkill ni userdel" {
  stub_jump_topology lb1 running web-a running
  # jump_sync descarta la llave del backend: necesita su llave pública.
  printf 'ssh-ed25519 BACKENDBLOB stratus-backend\n' >"$APP_SSH_KEY_PATH.pub"

  FAIL_REMOTE='/var/lib/jump' run --separate-stderr "$SCRIPT" --jump lb1 web-a luis no </dev/null

  [ "$status" -eq 15 ]
  [[ "$stderr" == *"jump"* ]]
  ! grep -qF -- "pkill" "$REMOTE" || return 1
  ! grep -qF -- "userdel" "$REMOTE" || return 1
}

@test "AC-42 delete-user vuelve a sincronizar jump con la llave del usuario si un paso posterior falla" {
  stub_jump_topology lb1 running web-a running
  # jump_sync descarta la llave del backend: necesita su llave pública.
  printf 'ssh-ed25519 BACKENDBLOB stratus-backend\n' >"$APP_SSH_KEY_PATH.pub"

  FAIL_REMOTE='userdel luis' run --separate-stderr "$SCRIPT" --jump lb1 web-a luis no </dev/null

  [ "$status" -eq 5 ]
  [ "$(grep -cF -- "/var/lib/jump/.ssh/authorized_keys" "$REMOTE")" -eq 2 ]
  failed_step="$(remote_line "userdel luis")"
  resync="$(remote_line "/var/lib/jump/.ssh/authorized_keys" 2)"
  [ -n "$failed_step" ]
  [ -n "$resync" ]
  [ "$failed_step" -lt "$resync" ]
}

@test "AC-27 delete-user sin --jump no toca jump" {
  run --separate-stderr "$SCRIPT" vm1 luis no </dev/null

  [ "$status" -eq 0 ]
  ! grep -qF -- "/var/lib/jump" "$REMOTE" || return 1
}

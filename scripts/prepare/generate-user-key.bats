#!/usr/bin/env bats
# Pruebas de generate-user-key.sh con stubs de VBoxManage, ssh, ssh-keygen y sleep: no tocan
# VirtualBox ni VMs.

load test_helper

# "run --separate-stderr" necesita bats 1.5 o superior.
bats_require_minimum_version 1.5.0

SCRIPT="$PREPARE_DIR/generate-user-key.sh"

setup() {
  setup_stubs
  CALLS="$BATS_TEST_TMPDIR/calls.log"
  # Par de llaves falso del backend: el script solo lee la llave pública "<ruta>.pub".
  BACKEND_PUB_BODY="AAAAC3NzaC1lZDI1NTE5AAAAIPruebaDeLlaveDelBackend"
  export APP_SSH_KEY_PATH="$BATS_TEST_TMPDIR/stratus_backend"
  printf '%s\n' "-----BEGIN OPENSSH PRIVATE KEY-----" "falsa" "-----END OPENSSH PRIVATE KEY-----" >"$APP_SSH_KEY_PATH"
  printf 'ssh-ed25519 %s stratus-backend\n' "$BACKEND_PUB_BODY" >"$APP_SSH_KEY_PATH.pub"
  # vm1 está encendida y reporta 192.168.1.50.
  stub_vm_ip vm1 192.168.1.50

  # Llave de usuario que "genera" el stub de ssh-keygen.
  USER_PUB_BODY="AAAAC3NzaC1lZDI1NTE5AAAAIPruebaDeLlaveDelUsuario"
  USER_PRIV_BODY="b3BlbnNzaC1rZXktdjEAAAAAPruebaPrivadaDelUsuario"

  # El stub escribe el par en la ruta de "-f" y registra el directorio usado, para comprobar
  # que el script lo borra al terminar.
  stub_command ssh-keygen 'key_file=""
prev=""
for arg in "$@"; do
  if [[ "$prev" == "-f" ]]; then key_file="$arg"; fi
  prev="$arg"
done
if [[ -n "$key_file" ]]; then
  printf "%s\n" "$(dirname "$key_file")" >>"'"$BATS_TEST_TMPDIR"'/keydirs.log"
  printf "%s\n" "-----BEGIN OPENSSH PRIVATE KEY-----" "'"$USER_PRIV_BODY"'" "-----END OPENSSH PRIVATE KEY-----" >"$key_file"
  printf "ssh-ed25519 %s stratus-user\n" "'"$USER_PUB_BODY"'" >"$key_file.pub"
fi
exit 0'
}

@test "AC-24 generate-user-key.sh tiene cabecera, códigos de salida, modo estricto y secciones" {
  assert_script_structure "$SCRIPT"
}

@test "AC-9 generate-user-key imprime solo la llave privada" {
  run --separate-stderr "$SCRIPT" vm1

  [ "$status" -eq 0 ]
  [ "${lines[0]}" = "-----BEGIN OPENSSH PRIVATE KEY-----" ]
  [ "${lines[1]}" = "$USER_PRIV_BODY" ]
  [ "${lines[2]}" = "-----END OPENSSH PRIVATE KEY-----" ]
  [ "${#lines[@]}" -eq 3 ]
  # Se generó con ed25519, sin frase de paso y con el comentario del usuario.
  keygen_call="$(grep -E '^ssh-keygen ' "$CALLS")"
  grep -qF -- "-t ed25519" <<<"$keygen_call"
  grep -qF -- "-C stratus-user" <<<"$keygen_call"
}

@test "AC-13 generate-user-key no deja la llave en stderr ni en disco" {
  run --separate-stderr "$SCRIPT" vm1

  [ "$status" -eq 0 ]
  ! grep -qF -- "PRIVATE KEY" <<<"$stderr" || return 1
  ! grep -qF -- "$USER_PRIV_BODY" <<<"$stderr" || return 1
  # La llave privada nunca viaja a la VM.
  ! grep -qF -- "$USER_PRIV_BODY" "$CALLS" || return 1
  # El directorio temporal usado por ssh-keygen ya no existe.
  [ -s "$BATS_TEST_TMPDIR/keydirs.log" ]
  while read -r dir; do
    [ ! -e "$dir" ]
  done <"$BATS_TEST_TMPDIR/keydirs.log"
}

@test "AC-27 regenerar reemplaza la llave del usuario y conserva la del backend" {
  run --separate-stderr "$SCRIPT" vm1

  [ "$status" -eq 0 ]
  ssh_calls="$(grep -E '^ssh ' "$CALLS")"
  # Se quitan las líneas anteriores del usuario en authorized_keys.
  grep -F -- "/root/.ssh/authorized_keys" <<<"$ssh_calls" | grep -qF -- "stratus-user"
  # Se agrega la nueva llave pública del usuario.
  grep -qF -- "$USER_PUB_BODY" <<<"$ssh_calls"
  # La llave del backend se conserva (se agrega si falta).
  grep -qF -- "$BACKEND_PUB_BODY" <<<"$ssh_calls"
  # El acceso con contraseña queda deshabilitado.
  grep -qF -- "PasswordAuthentication no" <<<"$ssh_calls"
  # Por SSH no se usa Guest Additions.
  ! grep -qE '^VBoxManage guestcontrol ' "$CALLS" || return 1
}

@test "AC-35 generate-user-key usa solo SSH contra la IP y agrega la llave del usuario junto a la del backend" {
  run --separate-stderr "$SCRIPT" vm1

  [ "$status" -eq 0 ]
  ssh_calls="$(grep -E '^ssh ' "$CALLS")"
  # Toda llamada remota es una llamada ssh con la llave del backend a root@IP.
  [ "$(grep -cE '^ssh ' "$CALLS")" -eq "$(grep -cF -- "-i $APP_SSH_KEY_PATH" <<<"$ssh_calls")" ]
  [ "$(grep -cE '^ssh ' "$CALLS")" -eq "$(grep -cF -- "root@192.168.1.50" <<<"$ssh_calls")" ]
  [ "$(grep -cE '^ssh ' "$CALLS")" -eq "$(grep -cF -- "HostKeyAlias=vm1" <<<"$ssh_calls")" ]
  ! grep -qF -- "guestcontrol" "$CALLS" || return 1
  ! grep -qF -- "apt-get" "$CALLS" || return 1
  # La llave del usuario se agrega y la del backend se conserva o se agrega.
  grep -qF -- "$USER_PUB_BODY" <<<"$ssh_calls"
  grep -qF -- "$BACKEND_PUB_BODY" <<<"$ssh_calls"
}

@test "AC-43 generate-user-key sale con 7 si la VM no tiene IP, sin ssh ni ssh-keygen" {
  setup_stubs
  stub_vm_ip vm1 -

  run --separate-stderr "$SCRIPT" vm1

  [ "$status" -eq 7 ]
  [ -z "$output" ]
  [[ "$stderr" == *"La VM no tiene dirección IP; verifique que esté encendida"* ]]
  ! grep -qE '^ssh(-keygen)? ' "$CALLS" || return 1
}

@test "AC-48 generate-user-key rechaza argumentos inválidos sin llamar a VBoxManage ni ssh" {
  run "$SCRIPT"
  [ "$status" -eq 2 ]
  run "$SCRIPT" VM1
  [ "$status" -eq 2 ]
  run "$SCRIPT" vm1 extra
  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

@test "AC-8 generate-user-key sale con 3 si la VM no existe" {
  setup_stubs
  stub_command VBoxManage '[[ "$1" == showvminfo ]] && exit 1'
  run "$SCRIPT" vm1
  [ "$status" -eq 3 ]
}

@test "AC-37 generate-user-key con --jump entra al servidor por el balanceador" {
  stub_jump_topology lb1 running web-a running

  run --separate-stderr "$SCRIPT" --jump lb1 web-a

  [ "$status" -eq 0 ]
  assert_server_calls_routed lb1 web-a
}

@test "AC-37 generate-user-key sale con 2 si --jump no trae un balanceador válido y no llama a nada" {
  stub_jump_topology lb1 running web-a running

  run "$SCRIPT" --jump
  [ "$status" -eq 2 ]
  run "$SCRIPT" --jump Lb1 web-a
  [ "$status" -eq 2 ]
  run "$SCRIPT" --jump "" web-a
  [ "$status" -eq 2 ]
  run "$SCRIPT" --jump 'lb1;x' web-a
  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

@test "AC-29 generate-user-key con --jump sale con 12 si el balanceador está apagado y no llama a ssh" {
  stub_jump_topology lb1 poweroff web-a running

  run --separate-stderr "$SCRIPT" --jump lb1 web-a

  [ "$status" -eq 12 ]
  [[ "$stderr" == *"El balanceador lb1 está apagado; enciéndalo para llegar al servidor"* ]]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

# line_of <texto fijo>: número de la primera línea de calls.log que lo contiene (vacío si no hay).
line_of() {
  grep -nF -- "$1" "$CALLS" | head -1 | cut -d: -f1 || true
}

@test "AC-27 generate-user-key con --jump respalda authorized_keys y sincroniza jump después de reiniciar ssh" {
  stub_jump_topology lb1 running web-a running

  run --separate-stderr "$SCRIPT" --jump lb1 web-a

  [ "$status" -eq 0 ]
  [[ "$output" == *"-----BEGIN OPENSSH PRIVATE KEY-----"* ]]
  backup="$(line_of "cp -p /root/.ssh/authorized_keys /root/.ssh/authorized_keys.stratus-backup")"
  restart="$(line_of "systemctl restart ssh")"
  jump_write="$(line_of "root@192.168.1.60 -- ")"
  [ -n "$backup" ]
  [ -n "$restart" ]
  [ -n "$jump_write" ]
  # Orden: copia de seguridad, cambios de llaves, reinicio de ssh y por último jump.
  [ "$backup" -lt "$(line_of "sed -i")" ]
  [ "$restart" -lt "$jump_write" ]
  grep -qF -- "/var/lib/jump/.ssh/authorized_keys" <<<"$(sed -n "${jump_write}p" "$CALLS")"
  # La copia de seguridad se borra al terminar bien.
  grep -qF -- "rm -f /root/.ssh/authorized_keys.stratus-backup" "$CALLS"
  assert_server_calls_routed lb1 web-a
}

@test "AC-42 generate-user-key restaura authorized_keys y sale con 15 si falla la escritura en jump; stdout vacío" {
  stub_jump_topology lb1 running web-a running
  # La escritura en el balanceador (la única llamada que nombra /var/lib/jump) falla.
  stub_command ssh 'if [[ "${@: -1}" == *"/var/lib/jump"* ]]; then echo "fallo en jump" >&2; exit 1; fi'

  run --separate-stderr "$SCRIPT" --jump lb1 web-a

  [ "$status" -eq 15 ]
  [ -z "$output" ]
  [[ "$stderr" == *"No se pudo actualizar la cuenta jump"* ]]
  [[ "$stderr" != *"PRIVATE KEY"* ]]
  jump_write="$(line_of "/var/lib/jump/.ssh/authorized_keys")"
  restore="$(line_of "mv /root/.ssh/authorized_keys.stratus-backup /root/.ssh/authorized_keys")"
  [ -n "$jump_write" ]
  [ -n "$restore" ]
  [ "$jump_write" -lt "$restore" ]
}

@test "AC-42 generate-user-key sin --jump no hace copia de seguridad ni toca jump" {
  run "$SCRIPT" vm1

  [ "$status" -eq 0 ]
  ! grep -qF -- "stratus-backup" "$CALLS" || return 1
  ! grep -qF -- "/var/lib/jump" "$CALLS" || return 1
}

#!/usr/bin/env bats
# Pruebas de jump-keys.sh (jump_sync y jump_drop) con los stubs de VBoxManage y ssh. Las pruebas de
# escritura ejecutan el programa del balanceador aquí mismo (JUMP_LOCAL) sobre un archivo de prueba.

load ../test_helper

bats_require_minimum_version 1.5.0

setup() {
  setup_haproxy_stubs
  CALLS="$BATS_TEST_TMPDIR/calls.log"
  REMOTE="$BATS_TEST_TMPDIR/remote.log"
  export APP_SSH_KEY_PATH="$BATS_TEST_TMPDIR/stratus_backend"
  : >"$APP_SSH_KEY_PATH"
  printf 'ssh-ed25519 BACKENDBLOB stratus-backend\n' >"$APP_SSH_KEY_PATH.pub"
  # lb1 (192.168.1.60) con el servidor web-a (10.10.1.11) detrás.
  stub_jump_topology lb1 running web-a running
  # authorized_keys de jump en el balanceador (se ejecuta en local con JUMP_LOCAL=yes).
  JUMP_FILE="$BATS_TEST_TMPDIR/jump/.ssh/authorized_keys"
  mkdir -p "$(dirname "$JUMP_FILE")"
  {
    echo "restrict,port-forwarding ssh-ed25519 BACKENDBLOB stratus-backend"
    echo "restrict,port-forwarding ssh-ed25519 OLDBLOB stratus-jump:web-a"
    echo "restrict,port-forwarding ssh-ed25519 PREFIXBLOB stratus-jump:web-ab"
    echo "restrict,port-forwarding ssh-ed25519 BBLOB stratus-jump:web-b"
  } >"$JUMP_FILE"
  export JUMP_LOCAL=yes
  # shellcheck source=../../prepare/lib/vm-exec.sh
  source "$HX/../prepare/lib/vm-exec.sh"
  # shellcheck source=jump-keys.sh
  source "$HX/lib/jump-keys.sh"
  # Llaves del servidor: la del backend, la de root repetida (dos comentarios), la de ana y una línea
  # de comentario que no es una llave.
  export SERVER_KEYS="root ssh-ed25519 BACKENDBLOB stratus-backend
root ssh-ed25519 ROOTBLOB root@laptop
root ssh-ed25519 ROOTBLOB root@otro-equipo
ana ssh-ed25519 ANABLOB ana-key
ana # comentario
luis ssh-rsa LUISBLOB"
}

# lb_write_stdin: la entrada estándar de la última llamada a ssh (la escritura en el balanceador).
lb_write_stdin() {
  cat "$BATS_TEST_TMPDIR/stdin/$(cat "$BATS_TEST_TMPDIR/counter")"
}

@test "AC-26 jump_sync copia las llaves sin la del backend y sin repetir, etiquetadas con la VM" {
  jump_sync lb1 web-a

  [ "$(lb_write_stdin)" = "restrict,port-forwarding ssh-ed25519 ROOTBLOB stratus-jump:web-a
restrict,port-forwarding ssh-ed25519 ANABLOB stratus-jump:web-a
restrict,port-forwarding ssh-rsa LUISBLOB stratus-jump:web-a" ]
}

@test "AC-26 jump_sync lee las llaves del servidor por el salto y escribe directo en el balanceador" {
  jump_sync lb1 web-a

  ssh_lines="$(grep -E '^ssh ' "$CALLS")"
  [ "$(wc -l <<<"$ssh_lines")" -eq 2 ]
  read_line="$(sed -n 1p <<<"$ssh_lines")"
  write_line="$(sed -n 2p <<<"$ssh_lines")"
  [[ "$read_line" == *"ProxyCommand="*"jump@192.168.1.60"* ]]
  [[ "$read_line" == *"-o HostKeyAlias=web-a root@10.10.1.11 -- "* ]]
  [[ "$write_line" != *ProxyCommand* ]]
  [[ "$write_line" == *"root@192.168.1.60 -- "* ]]
}

@test "AC-26 jump_sync conserva las líneas de otras VMs y la del backend, y reemplaza las de la VM" {
  jump_sync lb1 web-a

  [ "$(grep -c 'stratus-backend$' "$JUMP_FILE")" -eq 1 ]
  grep -qF -- "restrict,port-forwarding ssh-ed25519 BACKENDBLOB stratus-backend" "$JUMP_FILE"
  # Otra VM cuyo nombre empieza igual (web-ab) y web-b no se tocan.
  grep -qF -- "PREFIXBLOB stratus-jump:web-ab" "$JUMP_FILE"
  grep -qF -- "BBLOB stratus-jump:web-b" "$JUMP_FILE"
  # La llave vieja de web-a sale y las nuevas entran, una sola vez.
  ! grep -qF -- "OLDBLOB" "$JUMP_FILE" || return 1
  [ "$(grep -c 'stratus-jump:web-a$' "$JUMP_FILE")" -eq 3 ]
  [ "$(grep -c 'ROOTBLOB' "$JUMP_FILE")" -eq 1 ]
  [ "$(wc -l <"$JUMP_FILE")" -eq 6 ]
}

@test "AC-26 jump_sync es idempotente" {
  jump_sync lb1 web-a
  first="$(cat "$JUMP_FILE")"
  jump_sync lb1 web-a

  [ "$(cat "$JUMP_FILE")" = "$first" ]
}

@test "AC-26 jump_sync omite al usuario excluido" {
  jump_sync lb1 web-a ana

  grep -qF -- "ROOTBLOB" "$JUMP_FILE"
  ! grep -qF -- "ANABLOB" "$JUMP_FILE" || return 1
  grep -qF -- "LUISBLOB" "$JUMP_FILE"
}

@test "AC-26 jump_sync no deja llaves de un usuario excluido que root también tenga" {
  # La misma llave en root y en ana: solo se excluye la copia de ana; la de root sigue valiendo.
  export SERVER_KEYS="root ssh-ed25519 SHAREDBLOB root
ana ssh-ed25519 SHAREDBLOB ana"

  jump_sync lb1 web-a ana

  [ "$(grep -c 'SHAREDBLOB' "$JUMP_FILE")" -eq 1 ]
}

@test "AC-26 jump_sync sin llaves en el servidor deja solo las de otras VMs" {
  export SERVER_KEYS="root ssh-ed25519 BACKENDBLOB stratus-backend"

  jump_sync lb1 web-a

  ! grep -q -- "stratus-jump:web-a$" "$JUMP_FILE" || return 1
  [ "$(wc -l <"$JUMP_FILE")" -eq 3 ]
}

@test "AC-26 jump_drop envía un conjunto vacío y conserva stratus-backend y las otras VMs" {
  jump_drop lb1 web-a

  [ -z "$(lb_write_stdin)" ]
  ! grep -q -- "stratus-jump:web-a$" "$JUMP_FILE" || return 1
  grep -qF -- "stratus-backend" "$JUMP_FILE"
  grep -qF -- "stratus-jump:web-ab" "$JUMP_FILE"
  grep -qF -- "stratus-jump:web-b" "$JUMP_FILE"
  [ "$(wc -l <"$JUMP_FILE")" -eq 3 ]
  # No lee nada del servidor: puede estar apagado o ya sin red interna.
  [ "$(grep -cE '^ssh ' "$CALLS")" -eq 1 ]
}

@test "AC-26 jump_drop funciona aunque el archivo de jump aún no exista" {
  rm -f "$JUMP_FILE"

  jump_drop lb1 web-a

  [ -z "$(lb_write_stdin)" ]
}

@test "AC-26 jump_sync devuelve 15 si la escritura en el balanceador falla y no cambia el archivo" {
  before="$(cat "$JUMP_FILE")"

  FAIL_REMOTE='/var/lib/jump' run --separate-stderr jump_sync lb1 web-a

  [ "$status" -eq 15 ]
  [[ "$stderr" == *"No se pudo actualizar la cuenta jump de lb1"* ]]
  [ "$(cat "$JUMP_FILE")" = "$before" ]
}

@test "AC-26 jump_sync devuelve 15 si no puede leer las llaves del servidor" {
  FAIL_REMOTE='getent passwd' run --separate-stderr jump_sync lb1 web-a

  [ "$status" -eq 15 ]
  # Sin llaves leídas no escribe nada: el archivo de jump queda igual.
  [ "$(wc -l <"$JUMP_FILE")" -eq 4 ]
  [ "$(grep -cE '^ssh ' "$CALLS")" -eq 1 ]
}

@test "AC-26 jump_drop devuelve 15 si la escritura falla" {
  FAIL_REMOTE='/var/lib/jump' run jump_drop lb1 web-a

  [ "$status" -eq 15 ]
}

@test "AC-26 jump_sync y jump_drop exigen balanceador, VM y nombres válidos" {
  run jump_sync lb1
  [ "$status" -eq 2 ]
  run jump_sync lb1 web-a ana extra
  [ "$status" -eq 2 ]
  run jump_drop lb1
  [ "$status" -eq 2 ]
  run jump_sync 'Lb1' web-a
  [ "$status" -eq 2 ]
  run jump_sync lb1 'web;a'
  [ "$status" -eq 2 ]
  run jump_drop lb1 'web-a x'
  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

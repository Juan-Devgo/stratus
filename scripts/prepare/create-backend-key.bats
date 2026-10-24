#!/usr/bin/env bats
# Pruebas de create-backend-key.sh: comprueban la estructura del script (AC-24) y que crea el par
# ed25519 con permisos 600 (AC-36). Usan el ssh-keygen real en un directorio temporal: el script
# solo trabaja en el host, sin VM.

load test_helper

SCRIPT="$PREPARE_DIR/create-backend-key.sh"

setup() {
  KEY_PATH="$BATS_TEST_TMPDIR/llaves/stratus_backend"
}

@test "AC-24 create-backend-key.sh tiene cabecera, códigos de salida, modo estricto y secciones" {
  assert_script_structure "$SCRIPT"
}

@test "AC-36 create-backend-key crea el par ed25519 con permisos 600" {
  run "$SCRIPT" "$KEY_PATH"

  [ "$status" -eq 0 ]
  [ -f "$KEY_PATH" ]
  [ -f "$KEY_PATH.pub" ]
  [ "$(stat -c '%a' "$KEY_PATH")" = "600" ]
  # Es una llave ed25519 con el comentario del backend.
  grep -qE '^ssh-ed25519 [A-Za-z0-9+/=]+ stratus-backend$' "$KEY_PATH.pub"
  # La llave privada nunca se imprime.
  ! grep -qF -- "PRIVATE KEY" <<<"$output" || return 1
}

@test "AC-48 create-backend-key rechaza argumentos inválidos sin llamar a VBoxManage ni ssh" {
  # Con stubs, para comprobar que no se llama a ssh-keygen ni a nada más.
  setup_stubs
  CALLS="$BATS_TEST_TMPDIR/calls.log"

  run "$SCRIPT"
  [ "$status" -eq 2 ]
  run "$SCRIPT" "$KEY_PATH" extra
  [ "$status" -eq 2 ]
  run "$SCRIPT" "llaves/relativa"
  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
  [ ! -e "$KEY_PATH" ]
}

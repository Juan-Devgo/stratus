#!/usr/bin/env bats
# Pruebas de list-users.sh con stubs de VBoxManage y ssh: no tocan VirtualBox ni VMs.

load test_helper

# "run --separate-stderr" necesita bats 1.5 o superior.
bats_require_minimum_version 1.5.0

SCRIPT="$USERS_DIR/list-users.sh"

setup() {
  setup_users_stubs vm1 192.168.1.50
  CALLS="$BATS_TEST_TMPDIR/calls.log"
  export APP_SSH_KEY_PATH="$BATS_TEST_TMPDIR/stratus_backend"
  : >"$APP_SSH_KEY_PATH"
}

@test "AC-28 list-users.sh tiene cabecera, códigos de salida, modo estricto y secciones" {
  assert_script_structure "$SCRIPT"
}

@test "AC-25 list-users sale con 2 ante argumentos inválidos y no llama a VBoxManage ni ssh" {
  run "$SCRIPT" </dev/null
  [ "$status" -eq 2 ]
  run "$SCRIPT" vm1 extra </dev/null
  [ "$status" -eq 2 ]
  run "$SCRIPT" VM_1 </dev/null
  [ "$status" -eq 2 ]
  run "$SCRIPT" "-vm" </dev/null
  [ "$status" -eq 2 ]
  # Sin la llave del backend tampoco se ejecuta nada.
  APP_SSH_KEY_PATH="" run "$SCRIPT" vm1 </dev/null
  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

@test "AC-3 list-users sale con 7 si la VM no tiene IP y no ejecuta ssh" {
  setup_users_stubs vm1 -

  run --separate-stderr "$SCRIPT" vm1 </dev/null

  [ "$status" -eq 7 ]
  [[ "$stderr" == *"La VM no tiene dirección IP; verifique que esté encendida"* ]]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

@test "AC-1 list-users imprime solo usuarios regulares con sudo y llave" {
  run_remote_locally

  run --separate-stderr "$SCRIPT" vm1 </dev/null

  [ "$status" -eq 0 ]
  [ "$output" = $'ana 1001 yes yes\nluis 1002 no no' ]
}

@test "AC-37 list-users con --jump entra al servidor por el balanceador" {
  stub_jump_topology lb1 running web-a running

  run --separate-stderr "$SCRIPT" --jump lb1 web-a </dev/null

  [ "$status" -eq 0 ]
  assert_routed_ssh lb1 web-a
}

@test "AC-37 list-users sale con 2 si --jump no trae un balanceador válido y no llama a nada" {
  stub_jump_topology lb1 running web-a running

  run "$SCRIPT" --jump </dev/null
  [ "$status" -eq 2 ]
  run "$SCRIPT" --jump Lb1 web-a </dev/null
  [ "$status" -eq 2 ]
  run "$SCRIPT" --jump "" web-a </dev/null
  [ "$status" -eq 2 ]
  run "$SCRIPT" --jump 'lb1;x' web-a </dev/null
  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

@test "AC-29 list-users con --jump sale con 12 si el balanceador está apagado y no llama a ssh" {
  stub_jump_topology lb1 poweroff web-a running

  run --separate-stderr "$SCRIPT" --jump lb1 web-a </dev/null

  [ "$status" -eq 12 ]
  [[ "$stderr" == *"El balanceador lb1 está apagado; enciéndalo para llegar al servidor"* ]]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

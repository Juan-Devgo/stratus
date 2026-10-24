#!/usr/bin/env bats
# Pruebas de status-service.sh con stubs de VBoxManage y ssh: no tocan VirtualBox ni VMs.

load test_helper

# "run --separate-stderr" necesita bats 1.5 o superior.
bats_require_minimum_version 1.5.0

SCRIPT="$SERVICES_DIR/status-service.sh"

setup() {
  setup_services_stubs vm1 192.168.1.50
  CALLS="$BATS_TEST_TMPDIR/calls.log"
  REMOTE="$BATS_TEST_TMPDIR/remote.log"
  export APP_SSH_KEY_PATH="$BATS_TEST_TMPDIR/stratus_backend"
  : >"$APP_SSH_KEY_PATH"
}

@test "AC-36 status-service.sh tiene cabecera, códigos de salida, modo estricto y secciones" {
  assert_script_structure "$SCRIPT"
}

@test "AC-30 status-service sale con 2 ante argumentos inválidos y no llama a VBoxManage ni ssh" {
  run "$SCRIPT" </dev/null
  [ "$status" -eq 2 ]
  run "$SCRIPT" vm1 </dev/null
  [ "$status" -eq 2 ]
  run "$SCRIPT" vm1 api extra </dev/null
  [ "$status" -eq 2 ]
  run "$SCRIPT" VM_1 api </dev/null
  [ "$status" -eq 2 ]
  for name in Demian 1app 'app;rm' app_x '' "$(printf 'a%.0s' {1..33})"; do
    run "$SCRIPT" vm1 "$name" </dev/null
    [ "$status" -eq 2 ]
  done
  # Sin la llave del backend tampoco se ejecuta nada.
  APP_SSH_KEY_PATH="" run "$SCRIPT" vm1 api </dev/null
  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

@test "AC-17 status-service sale con 7 si la VM no tiene IP y no ejecuta ssh" {
  setup_services_stubs vm1 -

  run --separate-stderr "$SCRIPT" vm1 api </dev/null

  [ "$status" -eq 7 ]
  [[ "$stderr" == *"La VM no tiene dirección IP; verifique que esté encendida"* ]]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

@test "status-service sale con 3 si VBoxManage no conoce la VM y no ejecuta ssh" {
  printf '#!/usr/bin/env bash\necho "VBoxManage $*" >>"%s"\nexit 1\n' "$CALLS" >"$BATS_TEST_TMPDIR/bin/VBoxManage"

  run --separate-stderr "$SCRIPT" vm1 api </dev/null

  [ "$status" -eq 3 ]
  [[ "$stderr" == *"VM no encontrada en VirtualBox"* ]]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

@test "AC-20 status-service rechaza una unidad sin la marca con 6 y no lee nada" {
  MANAGED=no run --separate-stderr "$SCRIPT" vm1 ssh </dev/null

  [ "$status" -eq 6 ]
  [ "$stderr" = "Solo se pueden gestionar servicios creados por Stratus" ]
  # Solo se hizo la comprobación de la marca.
  [ "$(wc -l <"$REMOTE")" -eq 1 ]
  grep -q 'grep -qxF' "$REMOTE"
}

@test "AC-21 status-service acepta servicios inactivos o fallidos: imprime la salida y sale con 0" {
  STATUS_RC=3 run --separate-stderr "$SCRIPT" vm1 api </dev/null

  [ "$status" -eq 0 ]
  [[ "$output" == *"failed"* ]]
  [[ "$output" == *"api"* ]]
  # El comando remoto es el status de systemd sin paginador y con líneas completas.
  grep -q 'systemctl status --no-pager --full "\$1"' "$REMOTE"
}

@test "AC-21 status-service con un servicio activo sale con 0" {
  STATUS_RC=0 run "$SCRIPT" vm1 api </dev/null

  [ "$status" -eq 0 ]
  [ -n "$output" ]
}

@test "AC-21 status-service sale con 5 si systemctl status falla con otro código" {
  STATUS_RC=1 run --separate-stderr "$SCRIPT" vm1 api </dev/null

  [ "$status" -eq 5 ]
  [ "$stderr" = "Falló el paso sh en la VM (código 5)" ]
}

@test "AC-37 status-service con --jump entra al servidor por el balanceador" {
  stub_jump_topology lb1 running web-a running

  run --separate-stderr "$SCRIPT" --jump lb1 web-a api </dev/null

  [ "$status" -eq 0 ]
  assert_routed_ssh lb1 web-a
}

@test "AC-37 status-service sale con 2 si --jump no trae un balanceador válido y no llama a nada" {
  stub_jump_topology lb1 running web-a running

  run "$SCRIPT" --jump </dev/null
  [ "$status" -eq 2 ]
  run "$SCRIPT" --jump Lb1 web-a api </dev/null
  [ "$status" -eq 2 ]
  run "$SCRIPT" --jump "" web-a api </dev/null
  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

@test "AC-29 status-service con --jump sale con 12 si el balanceador está apagado y no llama a ssh" {
  stub_jump_topology lb1 poweroff web-a running

  run --separate-stderr "$SCRIPT" --jump lb1 web-a api </dev/null

  [ "$status" -eq 12 ]
  [[ "$stderr" == *"El balanceador lb1 está apagado; enciéndalo para llegar al servidor"* ]]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

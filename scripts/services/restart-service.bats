#!/usr/bin/env bats
# Pruebas de restart-service.sh con stubs de VBoxManage y ssh: no tocan VirtualBox ni VMs.

load test_helper

# "run --separate-stderr" necesita bats 1.5 o superior.
bats_require_minimum_version 1.5.0

SCRIPT="$SERVICES_DIR/restart-service.sh"

setup() {
  setup_services_stubs vm1 192.168.1.50
  CALLS="$BATS_TEST_TMPDIR/calls.log"
  REMOTE="$BATS_TEST_TMPDIR/remote.log"
  export APP_SSH_KEY_PATH="$BATS_TEST_TMPDIR/stratus_backend"
  : >"$APP_SSH_KEY_PATH"
}

@test "AC-36 restart-service.sh tiene cabecera, códigos de salida, modo estricto y secciones" {
  assert_script_structure "$SCRIPT"
}

@test "AC-30 restart-service sale con 2 ante argumentos inválidos y no llama a VBoxManage ni ssh" {
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

@test "AC-17 restart-service sale con 7 si la VM no tiene IP y no ejecuta ssh" {
  setup_services_stubs vm1 -

  run --separate-stderr "$SCRIPT" vm1 api </dev/null

  [ "$status" -eq 7 ]
  [[ "$stderr" == *"La VM no tiene dirección IP; verifique que esté encendida"* ]]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

@test "restart-service sale con 3 si VBoxManage no conoce la VM y no ejecuta ssh" {
  printf '#!/usr/bin/env bash\necho "VBoxManage $*" >>"%s"\nexit 1\n' "$CALLS" >"$BATS_TEST_TMPDIR/bin/VBoxManage"

  run --separate-stderr "$SCRIPT" vm1 api </dev/null

  [ "$status" -eq 3 ]
  [[ "$stderr" == *"VM no encontrada en VirtualBox"* ]]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

@test "AC-20 restart-service rechaza una unidad sin la marca con 6 y no cambia nada" {
  MANAGED=no run --separate-stderr "$SCRIPT" vm1 ssh </dev/null

  [ "$status" -eq 6 ]
  [ "$stderr" = "Solo se pueden gestionar servicios creados por Stratus" ]
  # Solo se hizo la comprobación de la marca.
  [ "$(wc -l <"$REMOTE")" -eq 1 ]
  grep -q 'grep -qxF' "$REMOTE"
  ! grep -q 'systemctl restart' "$REMOTE" || return 1
}

@test "AC-18 restart-service ejecuta systemctl restart después de comprobar la marca" {
  run --separate-stderr "$SCRIPT" vm1 api </dev/null

  [ "$status" -eq 0 ]
  sed -n 1p "$REMOTE" | grep -q 'grep -qxF'
  [ "$(sed -n 2p "$REMOTE")" = "systemctl restart api" ]
  [ "$output" = $'Servicio api reiniciado en vm1\nactive_state=active' ]
}

@test "AC-19 restart-service sobre un servicio activo termina bien y el estado sigue active" {
  ACTIVE=active run "$SCRIPT" vm1 api </dev/null

  [ "$status" -eq 0 ]
  [[ "$output" == *"active_state=active" ]]
}

@test "AC-42 restart-service termina bien con la app fallando: espera 3 s y muestra el estado" {
  local state
  for state in failed "activating (auto-restart)"; do
    : >"$CALLS"
    ACTIVE="$state" run "$SCRIPT" vm1 api </dev/null

    [ "$status" -eq 0 ]
    [ "$output" = "Servicio api reiniciado en vm1"$'\n'"active_state=$state" ]
    # "sleep 3" va después de systemctl restart y antes de leer el estado.
    local action_line sleep_line show_line
    action_line="$(grep -n 'restart.*api' "$CALLS" | tail -n 1 | cut -d: -f1)"
    sleep_line="$(grep -n '^sleep 3$' "$CALLS" | cut -d: -f1)"
    show_line="$(grep -n 'systemctl.*show' "$CALLS" | tail -n 1 | cut -d: -f1)"
    [ "$action_line" -lt "$sleep_line" ] && [ "$sleep_line" -lt "$show_line" ]
  done
}

@test "restart-service imprime active_state=desconocido si no puede leer el estado" {
  FAIL_REMOTE='systemctl show' run "$SCRIPT" vm1 api </dev/null

  [ "$status" -eq 0 ]
  [ "$output" = $'Servicio api reiniciado en vm1\nactive_state=desconocido' ]
}

@test "restart-service sale con 5 si systemctl restart falla" {
  FAIL_REMOTE='systemctl restart' run --separate-stderr "$SCRIPT" vm1 api </dev/null

  [ "$status" -eq 5 ]
  [ "$stderr" = "Falló el paso systemctl en la VM (código 5)" ]
}

@test "AC-37 restart-service con --jump entra al servidor por el balanceador" {
  stub_jump_topology lb1 running web-a running

  run --separate-stderr "$SCRIPT" --jump lb1 web-a api </dev/null

  [ "$status" -eq 0 ]
  assert_routed_ssh lb1 web-a
}

@test "AC-37 restart-service sale con 2 si --jump no trae un balanceador válido y no llama a nada" {
  stub_jump_topology lb1 running web-a running

  run "$SCRIPT" --jump </dev/null
  [ "$status" -eq 2 ]
  run "$SCRIPT" --jump Lb1 web-a api </dev/null
  [ "$status" -eq 2 ]
  run "$SCRIPT" --jump "" web-a api </dev/null
  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

@test "AC-29 restart-service con --jump sale con 12 si el balanceador está apagado y no llama a ssh" {
  stub_jump_topology lb1 poweroff web-a running

  run --separate-stderr "$SCRIPT" --jump lb1 web-a api </dev/null

  [ "$status" -eq 12 ]
  [[ "$stderr" == *"El balanceador lb1 está apagado; enciéndalo para llegar al servidor"* ]]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

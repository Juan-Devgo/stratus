#!/usr/bin/env bats
# Pruebas de delete-service.sh con stubs de VBoxManage y ssh: no tocan VirtualBox ni VMs.

load test_helper

# "run --separate-stderr" necesita bats 1.5 o superior.
bats_require_minimum_version 1.5.0

SCRIPT="$SERVICES_DIR/delete-service.sh"

setup() {
  setup_services_stubs vm1 192.168.1.50
  CALLS="$BATS_TEST_TMPDIR/calls.log"
  REMOTE="$BATS_TEST_TMPDIR/remote.log"
  export APP_SSH_KEY_PATH="$BATS_TEST_TMPDIR/stratus_backend"
  : >"$APP_SSH_KEY_PATH"
}

@test "AC-36 delete-service.sh tiene cabecera, códigos de salida, modo estricto y secciones" {
  assert_script_structure "$SCRIPT"
}

@test "AC-30 delete-service sale con 2 ante argumentos inválidos y no llama a VBoxManage ni ssh" {
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
  APP_SSH_KEY_PATH="" run "$SCRIPT" vm1 api </dev/null
  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

@test "AC-17 delete-service sale con 7 si la VM no tiene IP y no ejecuta ssh" {
  setup_services_stubs vm1 -

  run --separate-stderr "$SCRIPT" vm1 api </dev/null

  [ "$status" -eq 7 ]
  [[ "$stderr" == *"La VM no tiene dirección IP; verifique que esté encendida"* ]]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

@test "delete-service sale con 3 si VBoxManage no conoce la VM y no ejecuta ssh" {
  printf '#!/usr/bin/env bash\necho "VBoxManage $*" >>"%s"\nexit 1\n' "$CALLS" >"$BATS_TEST_TMPDIR/bin/VBoxManage"

  run --separate-stderr "$SCRIPT" vm1 api </dev/null

  [ "$status" -eq 3 ]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

@test "AC-20 delete-service rechaza una unidad sin la marca con 6 y no borra nada" {
  MANAGED=no run --separate-stderr "$SCRIPT" vm1 ssh </dev/null

  [ "$status" -eq 6 ]
  [ "$stderr" = "Solo se pueden gestionar servicios creados por Stratus" ]
  [ "$(wc -l <"$REMOTE")" -eq 1 ]
  grep -q 'grep -qxF' "$REMOTE"
}

@test "AC-28 delete-service quita todo en orden y la unidad al final" {
  run --separate-stderr "$SCRIPT" vm1 api </dev/null

  [ "$status" -eq 0 ]
  assert_in_order \
    'grep -qxF' \
    '^systemctl disable --now api$' \
    '^rm -f /etc/api.env$' \
    '^rm -rf /opt/api$' \
    'userdel "[$]1" sh api$' \
    'groupdel "[$]1" sh api$' \
    '^rm -f /etc/systemd/system/api.service$' \
    'systemctl daemon-reload && .*reset-failed .* sh api$'
  [ "$output" = "Servicio api eliminado de vm1" ]
}

@test "AC-41 los pasos de usuario y grupo toleran que ya no existan" {
  run "$SCRIPT" vm1 api </dev/null

  [ "$status" -eq 0 ]
  grep -qF '! getent passwd "$1" >/dev/null || userdel "$1"' "$REMOTE"
  grep -qF '! getent group "$1" >/dev/null || groupdel "$1"' "$REMOTE"
  # rm -f y rm -rf no fallan si ya no están, y reset-failed nunca hace fallar el borrado.
  grep -qF 'reset-failed "$1.service" 2>/dev/null || true' "$REMOTE"
}

@test "AC-41 delete-service falla con el nombre del servicio y no quita la unidad si falla userdel" {
  FAIL_REMOTE='userdel' run --separate-stderr "$SCRIPT" vm1 api </dev/null

  [ "$status" -eq 5 ]
  [ "$stderr" = "No se pudo eliminar el servicio api: falló el paso 5" ]
  # Se detuvo antes de la unidad: sigue instalada y se puede borrar otra vez.
  assert_in_order '^systemctl disable --now api$' '^rm -f /etc/api.env$' '^rm -rf /opt/api$' 'userdel'
  ! grep -qE 'groupdel|rm -f /etc/systemd/system/api.service|daemon-reload' "$REMOTE" || return 1
}

@test "AC-41 delete-service se puede repetir y termina bien tras un fallo" {
  FAIL_REMOTE='userdel' run "$SCRIPT" vm1 api </dev/null
  [ "$status" -eq 5 ]

  : >"$REMOTE"
  run --separate-stderr "$SCRIPT" vm1 api </dev/null

  [ "$status" -eq 0 ]
  [ "$output" = "Servicio api eliminado de vm1" ]
  assert_in_order 'userdel' 'groupdel' '^rm -f /etc/systemd/system/api.service$' 'daemon-reload'
}

@test "AC-41 cada paso que falla nombra su número" {
  local step pattern
  for step in "2|^systemctl disable" "3|^rm -f /etc/api.env" "4|^rm -rf /opt/api" "5|userdel" "6|groupdel" \
    "7|^rm -f /etc/systemd/system/api.service" "8|daemon-reload"; do
    step_n="${step%%|*}"
    pattern="${step#*|}"
    rm -f "$BATS_TEST_TMPDIR/failed"
    FAIL_REMOTE="$pattern" run --separate-stderr "$SCRIPT" vm1 api </dev/null
    [ "$status" -eq 5 ]
    [ "$stderr" = "No se pudo eliminar el servicio api: falló el paso $step_n" ]
  done
}

@test "AC-37 delete-service con --jump entra al servidor por el balanceador" {
  stub_jump_topology lb1 running web-a running

  run --separate-stderr "$SCRIPT" --jump lb1 web-a api </dev/null

  [ "$status" -eq 0 ]
  assert_routed_ssh lb1 web-a
}

@test "AC-37 delete-service sale con 2 si --jump no trae un balanceador válido y no llama a nada" {
  stub_jump_topology lb1 running web-a running

  run "$SCRIPT" --jump </dev/null
  [ "$status" -eq 2 ]
  run "$SCRIPT" --jump Lb1 web-a api </dev/null
  [ "$status" -eq 2 ]
  run "$SCRIPT" --jump "" web-a api </dev/null
  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

@test "AC-29 delete-service con --jump sale con 12 si el balanceador está apagado y no llama a ssh" {
  stub_jump_topology lb1 poweroff web-a running

  run --separate-stderr "$SCRIPT" --jump lb1 web-a api </dev/null

  [ "$status" -eq 12 ]
  [[ "$stderr" == *"El balanceador lb1 está apagado; enciéndalo para llegar al servidor"* ]]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

#!/usr/bin/env bats
# Pruebas de restart-web.sh (005) con stubs de VBoxManage, ssh y sleep: no tocan VMs.

load test_helper

bats_require_minimum_version 1.5.0

setup() {
  setup_web_stubs
  CALLS="$BATS_TEST_TMPDIR/calls.log"
  REMOTE="$BATS_TEST_TMPDIR/remote.log"
  export APP_SSH_KEY_PATH="$BATS_TEST_TMPDIR/stratus_backend"
  : >"$APP_SSH_KEY_PATH"
  SCRIPT="$WX/restart-web.sh"
  stub_vm_ip web-a 192.168.1.40
}

@test "AC-56 restart-web.sh tiene la estructura exigida" {
  assert_script_structure "$WEB_DIR/restart-web.sh"
}

@test "AC-18 restart-web ejecuta systemctl restart con la unidad del tipo" {
  run --separate-stderr "$SCRIPT" web-a nginx

  [ "$status" -eq 0 ]
  grep -qx 'systemctl restart nginx' "$REMOTE"
  [ "${lines[0]}" = "Servicio nginx reiniciado en web-a" ]
  [ "${lines[-1]}" = "active_state=active" ]

  run --separate-stderr "$SCRIPT" web-a apache2
  [ "$status" -eq 0 ]
  grep -qx 'systemctl restart apache2' "$REMOTE"
}

@test "AC-21 restart-web termina bien aunque el servicio no quede activo e informa el estado" {
  export ACTIVE_STATE=failed

  run --separate-stderr "$SCRIPT" web-a nginx

  [ "$status" -eq 0 ]
  [ "${lines[-1]}" = "active_state=failed" ]
}

@test "AC-18 restart-web sale con 5 si systemctl falla" {
  export FAIL_REMOTE='^systemctl restart'

  run --separate-stderr "$SCRIPT" web-a nginx

  [ "$status" -eq 5 ]
}

@test "AC-18 restart-web con --jump entra por el balanceador" {
  setup_web_stubs
  stub_jump_topology lb1 running web-a running

  run --separate-stderr "$SCRIPT" --jump lb1 web-a nginx

  [ "$status" -eq 0 ]
  assert_routed_ssh lb1 web-a
}

@test "AC-18 restart-web sale con 12 si el balanceador está apagado" {
  setup_web_stubs
  stub_jump_topology lb1 poweroff web-a running

  run --separate-stderr "$SCRIPT" --jump lb1 web-a nginx

  [ "$status" -eq 12 ]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

@test "AC-55 restart-web sale con 3 si la VM no existe y con 7 sin IP" {
  stub_command VBoxManage 'exit 1'
  run --separate-stderr "$SCRIPT" web-b nginx
  [ "$status" -eq 3 ]

  setup_web_stubs
  stub_vm_ip web-a -
  run --separate-stderr "$SCRIPT" web-a nginx
  [ "$status" -eq 7 ]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

@test "AC-55 restart-web sale con 2 ante argumentos inválidos sin llamar a nada" {
  for args in "web-a sshd" "web-a" "Web_A nginx" "web-a nginx extra" "--jump Lb1 web-a nginx"; do
    read -ra argv <<<"$args"
    run "$SCRIPT" "${argv[@]}"
    [ "$status" -eq 2 ]
  done
  run env -u APP_SSH_KEY_PATH "$SCRIPT" web-a nginx
  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

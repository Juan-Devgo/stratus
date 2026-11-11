#!/usr/bin/env bats
# Pruebas de status-web.sh (005) con stubs de VBoxManage y ssh: no tocan VMs.

load test_helper

bats_require_minimum_version 1.5.0

setup() {
  setup_web_stubs
  CALLS="$BATS_TEST_TMPDIR/calls.log"
  REMOTE="$BATS_TEST_TMPDIR/remote.log"
  export APP_SSH_KEY_PATH="$BATS_TEST_TMPDIR/stratus_backend"
  : >"$APP_SSH_KEY_PATH"
  SCRIPT="$WX/status-web.sh"
  stub_vm_ip web-a 192.168.1.40
}

@test "AC-56 status-web.sh tiene la estructura exigida" {
  assert_script_structure "$WEB_DIR/status-web.sh"
}

@test "AC-20 status-web imprime systemctl status y acepta un servicio fallido" {
  export STATUS_OUT="nginx.service - A high performance web server
     Active: failed (Result: exit-code)"

  run --separate-stderr "$SCRIPT" web-a nginx

  [ "$status" -eq 0 ]
  [[ "$output" == *"Active: failed"* ]]
  grep -q 'systemctl status --no-pager --full "$1"; rc=$?; \[ "$rc" -eq 0 \] || \[ "$rc" -eq 3 \] sh nginx$' "$REMOTE"
}

@test "AC-20 status-web con --jump entra por el balanceador" {
  setup_web_stubs
  stub_jump_topology lb1 running web-a running

  run --separate-stderr "$SCRIPT" --jump lb1 web-a apache2

  [ "$status" -eq 0 ]
  assert_routed_ssh lb1 web-a
}

@test "AC-55 status-web sale con 5 si systemctl status falla con otro código" {
  export FAIL_REMOTE='systemctl status'

  run --separate-stderr "$SCRIPT" web-a nginx

  [ "$status" -eq 5 ]
}

@test "AC-55 status-web sale con 3 si la VM no existe" {
  stub_command VBoxManage 'exit 1'
  run --separate-stderr "$SCRIPT" web-b nginx
  [ "$status" -eq 3 ]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

@test "AC-55 status-web sale con 2 ante argumentos inválidos sin llamar a nada" {
  for args in "web-a sshd" "web-a" "Web_A nginx" "web-a nginx extra"; do
    read -ra argv <<<"$args"
    run "$SCRIPT" "${argv[@]}"
    [ "$status" -eq 2 ]
  done
  [ ! -s "$CALLS" ]
}

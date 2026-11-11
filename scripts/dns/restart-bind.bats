#!/usr/bin/env bats
# Pruebas de restart-bind.sh (005) con stubs de VBoxManage, ssh y sleep: no tocan VMs.

load test_helper

bats_require_minimum_version 1.5.0

setup() {
  setup_dns_stubs
  CALLS="$BATS_TEST_TMPDIR/calls.log"
  REMOTE="$BATS_TEST_TMPDIR/remote.log"
  export APP_SSH_KEY_PATH="$BATS_TEST_TMPDIR/stratus_backend"
  : >"$APP_SSH_KEY_PATH"
  SCRIPT="$DX/restart-bind.sh"
  stub_vm_ip dns1 192.168.1.53
}

@test "AC-56 restart-bind.sh tiene la estructura exigida" {
  assert_script_structure "$DNS_DIR/restart-bind.sh"
}

@test "AC-19 restart-bind ejecuta systemctl restart named" {
  run --separate-stderr "$SCRIPT" dns1

  [ "$status" -eq 0 ]
  grep -qx 'systemctl restart named' "$REMOTE"
  [ "${lines[0]}" = "Servicio named reiniciado en dns1" ]
  [ "${lines[-1]}" = "active_state=active" ]
}

@test "AC-19 restart-bind sale con 5 si systemctl falla" {
  export FAIL_REMOTE='^systemctl restart'

  run --separate-stderr "$SCRIPT" dns1

  [ "$status" -eq 5 ]
}

@test "AC-55 restart-bind sale con 7 si el servidor DNS no tiene IP" {
  setup_dns_stubs
  stub_vm_ip dns1 -

  run --separate-stderr "$SCRIPT" dns1

  [ "$status" -eq 7 ]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

@test "AC-55 restart-bind sale con 2 ante argumentos inválidos sin llamar a nada" {
  run "$SCRIPT"
  [ "$status" -eq 2 ]
  run "$SCRIPT" DNS_1
  [ "$status" -eq 2 ]
  run "$SCRIPT" dns1 named
  [ "$status" -eq 2 ]
  run "$SCRIPT" --jump lb1 dns1
  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

#!/usr/bin/env bats
# Pruebas de remove-record.sh (005) con stubs de VBoxManage y ssh: no tocan VMs.

load test_helper

bats_require_minimum_version 1.5.0

setup() {
  setup_dns_stubs
  CALLS="$BATS_TEST_TMPDIR/calls.log"
  REMOTE="$BATS_TEST_TMPDIR/remote.log"
  export APP_SSH_KEY_PATH="$BATS_TEST_TMPDIR/stratus_backend"
  : >"$APP_SSH_KEY_PATH"
  SCRIPT="$DX/remove-record.sh"
  stub_vm_ip dns1 192.168.1.53
}

# zone_upload: entrada estándar de la llamada que escribe la zona nueva.
zone_upload() {
  local n
  n="$(grep -n -m1 'db.stratus.lan.new$' "$REMOTE" | cut -d: -f1)"
  cat "$BATS_TEST_TMPDIR/stdin/$n"
}

@test "AC-56 remove-record.sh tiene la estructura exigida" {
  assert_script_structure "$DNS_DIR/remove-record.sh"
}

@test "AC-33 remove-record aplica el conjunto restante sin la línea de la VM" {
  run --separate-stderr "$SCRIPT" dns1 stratus.lan 192.168.1.53 vm1 lb1,192.168.1.41

  [ "$status" -eq 0 ]
  [ "${lines[0]}" = "Registro de vm1 quitado" ]
  zone="$(zone_upload)"
  [[ "$zone" == *"lb1 IN A 192.168.1.41"* ]]
  [[ "$zone" != *"vm1 IN A"* ]]
  grep -qx 'rndc reload stratus.lan' "$REMOTE"
}

@test "AC-33 remove-record sin registros restantes deja solo el del servidor DNS" {
  run --separate-stderr "$SCRIPT" dns1 stratus.lan 192.168.1.53 vm1

  [ "$status" -eq 0 ]
  zone="$(zone_upload)"
  [ "$(grep -c ' IN A ' <<<"$zone")" -eq 1 ]
  [[ "$zone" == *"dns1 IN A 192.168.1.53"* ]]
}

@test "AC-35 remove-record sale con 7 si el servidor DNS está apagado, sin ssh" {
  setup_dns_stubs
  stub_vm_ip dns1 -

  run --separate-stderr "$SCRIPT" dns1 stratus.lan 192.168.1.53 vm1

  [ "$status" -eq 7 ]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

@test "AC-55 remove-record sale con 2 ante argumentos inválidos sin llamar a nada" {
  run "$SCRIPT" dns1 stratus.lan 192.168.1.53
  [ "$status" -eq 2 ]
  run "$SCRIPT" dns1 stratus.lan 192.168.1.53 VM_1
  [ "$status" -eq 2 ]
  run "$SCRIPT" dns1 stratus.lan 192.168.1.53 vm1 vm1,192.168.1.40
  [ "$status" -eq 2 ]
  run "$SCRIPT" dns1 stratus.lan 192.168.1.53 vm1 lb1
  [ "$status" -eq 2 ]
  run "$SCRIPT" dns1 lab 192.168.1 vm1
  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

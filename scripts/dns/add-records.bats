#!/usr/bin/env bats
# Pruebas de add-records.sh (005) con stubs de VBoxManage y ssh: no tocan VMs.

load test_helper

bats_require_minimum_version 1.5.0

setup() {
  setup_dns_stubs
  CALLS="$BATS_TEST_TMPDIR/calls.log"
  REMOTE="$BATS_TEST_TMPDIR/remote.log"
  export APP_SSH_KEY_PATH="$BATS_TEST_TMPDIR/stratus_backend"
  : >"$APP_SSH_KEY_PATH"
  SCRIPT="$DX/add-records.sh"
  stub_vm_ip dns1 192.168.1.53
}

# zone_upload: entrada estándar de la llamada que escribe la zona nueva.
zone_upload() {
  local n
  n="$(grep -n -m1 'db.stratus.lan.new$' "$REMOTE" | cut -d: -f1)"
  cat "$BATS_TEST_TMPDIR/stdin/$n"
}

@test "AC-56 add-records.sh tiene la estructura exigida" {
  assert_script_structure "$DNS_DIR/add-records.sh"
}

@test "AC-30 add-records aplica el conjunto completo, lo comprueba y recarga la zona" {
  run --separate-stderr "$SCRIPT" dns1 stratus.lan 192.168.1.53 vm1,192.168.1.40 lb1,192.168.1.41

  [ "$status" -eq 0 ]
  [ "${lines[0]}" = "Registros aplicados" ]
  [ "${lines[-1]}" = "reloaded=yes" ]
  zone="$(zone_upload)"
  [[ "$zone" == *"dns1 IN A 192.168.1.53"* ]]
  [[ "$zone" == *$'lb1 IN A 192.168.1.41\nvm1 IN A 192.168.1.40'* ]]
  grep -qx 'named-checkzone stratus.lan /var/lib/bind/db.stratus.lan.new' "$REMOTE"
  grep -qx 'rndc reload stratus.lan' "$REMOTE"
}

@test "AC-41 add-records sale con 10 si la zona no pasa named-checkzone" {
  export ZONE_CHECK=fail

  run --separate-stderr "$SCRIPT" dns1 stratus.lan 192.168.1.53 vm1,192.168.1.40

  [ "$status" -eq 10 ]
  ! grep -q 'rndc' "$REMOTE" || return 1
}

@test "AC-35 add-records sale con 7 si el servidor DNS está apagado, sin ssh" {
  setup_dns_stubs
  stub_vm_ip dns1 -

  run --separate-stderr "$SCRIPT" dns1 stratus.lan 192.168.1.53 vm1,192.168.1.40

  [ "$status" -eq 7 ]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

@test "AC-55 add-records sale con 2 ante argumentos inválidos sin llamar a nada" {
  run "$SCRIPT" dns1 stratus.lan 192.168.1.53
  [ "$status" -eq 2 ]
  run "$SCRIPT" DNS_1 stratus.lan 192.168.1.53 vm1,192.168.1.40
  [ "$status" -eq 2 ]
  run "$SCRIPT" dns1 Stratus_Lan 192.168.1.53 vm1,192.168.1.40
  [ "$status" -eq 2 ]
  run "$SCRIPT" dns1 stratus.lan 192.168.1.999 vm1,192.168.1.40
  [ "$status" -eq 2 ]
  run "$SCRIPT" dns1 stratus.lan 192.168.1.53 vm1
  [ "$status" -eq 2 ]
  run "$SCRIPT" dns1 stratus.lan 192.168.1.53 dns1,192.168.1.40
  [ "$status" -eq 2 ]
  run "$SCRIPT" dns1 stratus.lan 192.168.1.53 vm1,192.168.1.40 vm1,192.168.1.41
  [ "$status" -eq 2 ]
  run env -u APP_SSH_KEY_PATH "$SCRIPT" dns1 stratus.lan 192.168.1.53 vm1,192.168.1.40
  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

#!/usr/bin/env bats
# Pruebas de show-zone.sh (005, enmienda del 2026-11-11) con stubs de VBoxManage y ssh: no tocan VMs.

load test_helper

bats_require_minimum_version 1.5.0

# Salida canónica de "named-checkzone -D -o -", con las líneas de carga que algunas versiones
# imprimen en stdout.
DUMP="zone stratus.lan/IN: loaded serial 1731300000
stratus.lan.					60 IN SOA	dns1.stratus.lan. hostmaster.stratus.lan. 1731300000 3600 600 86400 60
stratus.lan.					60 IN NS	dns1.stratus.lan.
dns1.stratus.lan.				60 IN A	192.168.1.53
web-a.stratus.lan.				60 IN A	192.168.1.40
OK"

setup() {
  setup_dns_stubs
  CALLS="$BATS_TEST_TMPDIR/calls.log"
  REMOTE="$BATS_TEST_TMPDIR/remote.log"
  export APP_SSH_KEY_PATH="$BATS_TEST_TMPDIR/stratus_backend"
  : >"$APP_SSH_KEY_PATH"
  SCRIPT="$DX/show-zone.sh"
  stub_vm_ip dns1 192.168.1.53
}

@test "AC-56 show-zone.sh tiene la estructura exigida" {
  assert_script_structure "$DNS_DIR/show-zone.sh"
}

@test "AC-66 show-zone imprime cada registro como tipo, nombre, TTL y valor" {
  export ZONE_DUMP="$DUMP"

  run --separate-stderr "$SCRIPT" dns1 stratus.lan

  [ "$status" -eq 0 ]
  expected="$(printf '%s\t%s\t%s\t%s\n' \
    SOA stratus.lan 60 "dns1.stratus.lan. hostmaster.stratus.lan. 1731300000 3600 600 86400 60" \
    NS stratus.lan 60 "dns1.stratus.lan." \
    A dns1.stratus.lan 60 192.168.1.53 \
    A web-a.stratus.lan 60 192.168.1.40)"
  [ "$output" == "$expected" ]
  grep -qx 'named-checkzone -D -o - stratus.lan /var/lib/bind/db.stratus.lan' "$REMOTE"
}

@test "AC-68 show-zone sale con 7 si el servidor DNS no tiene IP" {
  # Los stubs se agregan al final: se rehacen para que dns1 responda sin IP.
  setup_dns_stubs
  stub_vm_ip dns1 -

  run --separate-stderr "$SCRIPT" dns1 stratus.lan

  [ "$status" -eq 7 ]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

@test "AC-68 show-zone sale con 5 si named-checkzone falla" {
  export ZONE_CHECK=fail

  run --separate-stderr "$SCRIPT" dns1 stratus.lan

  [ "$status" -eq 5 ]
  [[ "$stderr" == *"bad owner name"* ]]
  [ -z "$output" ]
}

@test "show-zone sale con 3 si la VM no existe" {
  setup_dns_stubs
  stub_command VBoxManage 'exit 1'
  run --separate-stderr "$SCRIPT" dns9 stratus.lan

  [ "$status" -eq 3 ]
}

@test "AC-69 show-zone sale con 2 ante argumentos inválidos sin llamar a nada" {
  run "$SCRIPT"
  [ "$status" -eq 2 ]
  run "$SCRIPT" dns1
  [ "$status" -eq 2 ]
  run "$SCRIPT" DNS_1 stratus.lan
  [ "$status" -eq 2 ]
  run "$SCRIPT" dns1 Stratus_Lan
  [ "$status" -eq 2 ]
  run "$SCRIPT" dns1 stratus.lan extra
  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
  unset APP_SSH_KEY_PATH
  run "$SCRIPT" dns1 stratus.lan
  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

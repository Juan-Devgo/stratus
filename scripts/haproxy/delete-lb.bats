#!/usr/bin/env bats
# Pruebas de delete-lb.sh con stubs de VBoxManage y de los scripts de ../prepare: no tocan VirtualBox.

load test_helper

# "run --separate-stderr" necesita bats 1.5 o superior.
bats_require_minimum_version 1.5.0

setup() {
  setup_haproxy_stubs
  SCRIPT="$HX/delete-lb.sh"
  CALLS="$BATS_TEST_TMPDIR/calls.log"
}

@test "AC-38 delete-lb.sh tiene cabecera, códigos de salida, modo estricto y secciones" {
  assert_script_structure "$HAPROXY_DIR/delete-lb.sh"
}

@test "AC-37 delete-lb sale con 2 ante argumentos inválidos y no llama a nada" {
  run "$SCRIPT"
  [ "$status" -eq 2 ]
  run "$SCRIPT" lb1 extra
  [ "$status" -eq 2 ]
  for name in Lb1 -lb 'lb;x' '' "$(printf 'a%.0s' {1..33})"; do
    run "$SCRIPT" "$name"
    [ "$status" -eq 2 ]
  done
  [ ! -s "$CALLS" ]
}

@test "AC-10 delete-lb borra la VM y después el servidor DHCP de su red interna" {
  run --separate-stderr "$SCRIPT" lb1

  [ "$status" -eq 0 ]
  [ "$output" = "Balanceador lb1 eliminado junto con su red interna" ]
  vm="$(grep -nF -- "delete-vm.sh lb1" "$CALLS" | cut -d: -f1)"
  net="$(grep -nF -- "dhcpserver remove --network=stratus-lb1" "$CALLS" | cut -d: -f1)"
  [ -n "$vm" ]
  [ -n "$net" ]
  [ "$vm" -lt "$net" ]
}

@test "AC-10 delete-lb tolera que el servidor DHCP ya no exista" {
  stub_command VBoxManage 'if [[ "$1 ${2:-}" == "dhcpserver remove" ]]; then echo "VBoxManage: error: DHCP server not found" >&2; exit 1; fi'

  run --separate-stderr "$SCRIPT" lb1

  [ "$status" -eq 0 ]
  [ "$output" = "Balanceador lb1 eliminado junto con su red interna" ]
}

@test "AC-10 delete-lb sale con 4 y no toca la red si delete-vm falla" {
  RC_DELETE_VM=4 run --separate-stderr "$SCRIPT" lb1

  [ "$status" -eq 4 ]
  ! grep -qF -- "dhcpserver remove" "$CALLS" || return 1
}

#!/usr/bin/env bats
# Pruebas de start-vm.sh con stubs de VBoxManage: comprueban la estructura del script (AC-24), que
# enciende la VM en modo headless solo si hace falta (AC-38) y sus códigos de error (AC-8).
# No tocan VirtualBox.

load test_helper

# "run --separate-stderr" necesita bats 1.5 o superior.
bats_require_minimum_version 1.5.0

SCRIPT="$PREPARE_DIR/start-vm.sh"

setup() {
  setup_stubs
  CALLS="$BATS_TEST_TMPDIR/calls.log"
}

@test "AC-24 start-vm.sh tiene cabecera, códigos de salida, modo estricto y secciones" {
  assert_script_structure "$SCRIPT"
}

@test "AC-47 start-vm enciende la VM en modo headless" {
  stub_command VBoxManage '[[ "$1" == showvminfo ]] && printf "VMState=\"poweroff\"\n"
exit 0'

  run --separate-stderr "$SCRIPT" vm1

  [ "$status" -eq 0 ]
  [ "$output" = "VM vm1 encendida" ]
  grep -qF -- "startvm vm1 --type headless" "$CALLS"
}

@test "AC-47 start-vm no hace nada si la VM ya está encendida" {
  stub_command VBoxManage '[[ "$1" == showvminfo ]] && printf "VMState=\"running\"\n"
exit 0'

  run --separate-stderr "$SCRIPT" vm1

  [ "$status" -eq 0 ]
  [ "$output" = "La VM vm1 ya estaba encendida" ]
  ! grep -qF -- "startvm" "$CALLS" || return 1
}

@test "AC-47 start-vm sale con 3 si la VM no existe" {
  stub_command VBoxManage '[[ "$1" == showvminfo ]] && exit 1
exit 0'

  run "$SCRIPT" vm1

  [ "$status" -eq 3 ]
  ! grep -qF -- "startvm" "$CALLS" || return 1
}

@test "AC-48 start-vm rechaza argumentos inválidos sin llamar a VBoxManage ni ssh" {
  run "$SCRIPT"
  [ "$status" -eq 2 ]
  run "$SCRIPT" vm1 vm2
  [ "$status" -eq 2 ]
  run "$SCRIPT" VM1
  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

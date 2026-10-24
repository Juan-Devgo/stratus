#!/usr/bin/env bats
# Pruebas de stop-vm.sh con stubs de VBoxManage y sleep: comprueban la estructura del script (AC-24)
# y el apagado forzado tras 60 segundos de espera simulada (AC-30). No tocan VirtualBox.

load test_helper

SCRIPT="$PREPARE_DIR/stop-vm.sh"

setup() {
  setup_stubs
  CALLS="$BATS_TEST_TMPDIR/calls.log"
}

@test "AC-24 stop-vm.sh tiene cabecera, códigos de salida, modo estricto y secciones" {
  assert_script_structure "$SCRIPT"
}

@test "AC-30 stop-vm fuerza el apagado tras 60 segundos si la VM ignora ACPI" {
  # La VM nunca se apaga: todas las consultas de estado responden "running".
  stub_command VBoxManage 'case "$1 ${2:-}" in
  showvminfo*) printf "VMState=\"running\"\n" ;;
  "list runningvms") printf "\"vm1\" {11111111-1111-1111-1111-111111111111}\n" ;;
esac
exit 0'

  run "$SCRIPT" vm1

  [ "$status" -eq 0 ]
  acpi_line="$(grep -nF -- "controlvm vm1 acpipowerbutton" "$CALLS" | head -n1 | cut -d: -f1)"
  poweroff_line="$(grep -nF -- "controlvm vm1 poweroff" "$CALLS" | head -n1 | cut -d: -f1)"
  [ -n "$acpi_line" ]
  [ -n "$poweroff_line" ]
  [ "$acpi_line" -lt "$poweroff_line" ]
  # Las esperas simuladas (stub de sleep) suman exactamente 60 segundos.
  waited="$(awk '$1 == "sleep" { total += $2 } END { print total + 0 }' "$CALLS")"
  [ "$waited" -eq 60 ]
}

@test "AC-48 stop-vm rechaza argumentos inválidos sin llamar a VBoxManage ni ssh" {
  run "$SCRIPT"
  [ "$status" -eq 2 ]
  run "$SCRIPT" vm1 vm2
  [ "$status" -eq 2 ]
  run "$SCRIPT" VM1
  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

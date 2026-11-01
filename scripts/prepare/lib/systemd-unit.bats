#!/usr/bin/env bats
# Pruebas de lib/systemd-unit.sh (unit_action y unit_status, 005) con stubs de VBoxManage, ssh y
# sleep: no tocan VMs. La biblioteca se carga con "source" después de vm-exec.sh.

load ../test_helper

bats_require_minimum_version 1.5.0

setup() {
  setup_stubs
  CALLS="$BATS_TEST_TMPDIR/calls.log"
  export APP_SSH_KEY_PATH="$BATS_TEST_TMPDIR/stratus_backend"
  : >"$APP_SSH_KEY_PATH"
  stub_vm_ip web-a 192.168.1.40
  # shellcheck source=vm-exec.sh
  source "$PREPARE_DIR/lib/vm-exec.sh"
  # shellcheck source=systemd-unit.sh
  source "$PREPARE_DIR/lib/systemd-unit.sh"
}

@test "005 AC-56 systemd-unit.sh tiene la estructura exigida" {
  assert_script_structure "$PREPARE_DIR/lib/systemd-unit.sh"
}

@test "005 AC-18 unit_action start ejecuta systemctl start, espera 3 s e imprime active_state" {
  stub_command ssh 'case "$*" in *"systemctl show"*) echo active ;; esac'

  run --separate-stderr unit_action web-a nginx start

  [ "$status" -eq 0 ]
  grep -qE '^ssh .*-- systemctl start nginx $' "$CALLS"
  grep -qx 'sleep 3' "$CALLS"
  [ "${lines[0]}" = "Servicio nginx iniciado en web-a" ]
  [ "${lines[1]}" = "active_state=active" ]
  # El estado se lee después de la espera.
  [ "$(grep -nx 'sleep 3' "$CALLS" | cut -d: -f1)" -lt "$(grep -n 'systemctl show' "$CALLS" | cut -d: -f1)" ]
}

@test "005 AC-18 unit_action restart informa el estado aunque no sea active" {
  stub_command ssh 'case "$*" in *"systemctl show"*) echo failed ;; esac'

  run --separate-stderr unit_action web-a apache2 restart

  [ "$status" -eq 0 ]
  grep -qE '^ssh .*-- systemctl restart apache2 $' "$CALLS"
  [ "${lines[0]}" = "Servicio apache2 reiniciado en web-a" ]
  [ "${lines[1]}" = "active_state=failed" ]
}

@test "005 AC-18 unit_action stop no espera ni lee el estado" {
  run --separate-stderr unit_action web-a named stop

  [ "$status" -eq 0 ]
  grep -qE '^ssh .*-- systemctl stop named $' "$CALLS"
  ! grep -q '^sleep' "$CALLS" || return 1
  ! grep -q 'systemctl show' "$CALLS" || return 1
  [ "$output" = "Servicio named detenido en web-a" ]
}

@test "005 AC-18 unit_action devuelve 5 si systemctl falla y 7 sin IP" {
  stub_command ssh 'exit 1'
  run --separate-stderr unit_action web-a nginx start
  [ "$status" -eq 5 ]
  [[ "$stderr" == *"No se pudo ejecutar systemctl start nginx en web-a"* ]]

  setup_stubs
  stub_vm_ip web-a -
  run --separate-stderr unit_action web-a nginx start
  [ "$status" -eq 7 ]
}

@test "005 AC-18 unit_action rechaza una acción fuera de la lista" {
  run --separate-stderr unit_action web-a nginx enable
  [ "$status" -eq 2 ]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

@test "005 AC-20 unit_status imprime la salida y devuelve 0 aunque systemctl status salga con 3" {
  # El programa remoto convierte el 3 (inactivo o fallido) en éxito; el stub imprime la salida.
  stub_command ssh 'echo "× nginx.service - A high performance web server"; echo "     Active: failed (Result: exit-code)"'

  run --separate-stderr unit_status web-a nginx

  [ "$status" -eq 0 ]
  [[ "$output" == *"Active: failed"* ]]
  ssh_line="$(grep -E '^ssh ' "$CALLS")"
  # vm_exec cita el programa con printf %q: los espacios llegan escapados.
  [[ "$ssh_line" == *'systemctl\ status\ --no-pager\ --full'* ]]
  [[ "$ssh_line" == *'-eq\ 3'* ]]
  [[ "$ssh_line" == *" sh nginx " ]]
}

@test "005 AC-20 unit_status devuelve 5 si systemctl status falla con otro código" {
  stub_command ssh 'exit 4'
  run --separate-stderr unit_status web-a nginx
  [ "$status" -eq 5 ]
}

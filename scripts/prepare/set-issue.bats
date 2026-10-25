#!/usr/bin/env bats
# Pruebas de set-issue.sh con stubs de VBoxManage, ssh y sleep: no tocan VirtualBox ni VMs.

load test_helper

# "run --separate-stderr" necesita bats 1.5 o superior.
bats_require_minimum_version 1.5.0

SCRIPT="$PREPARE_DIR/set-issue.sh"

setup() {
  setup_stubs
  CALLS="$BATS_TEST_TMPDIR/calls.log"
  export APP_SSH_KEY_PATH="$BATS_TEST_TMPDIR/stratus_backend"
  : >"$APP_SSH_KEY_PATH"
  # vm1 está encendida y reporta 192.168.1.50.
  stub_vm_ip vm1 192.168.1.50
  # El stub de ssh guarda el comando remoto (último argumento) de cada llamada; ssh recibe el
  # comando ya citado con "printf '%q '", así que la prueba lo vuelve a separar con eval.
  stub_command ssh 'printf "%s\n" "${@: -1}" >>"'"$BATS_TEST_TMPDIR"'/remote.log"'
}

# remote_arg <n>: n-ésimo argumento (desde 0) del comando remoto que escribe /etc/issue.
remote_arg() {
  local line args
  line="$(grep -F -- "/etc/issue" "$BATS_TEST_TMPDIR/remote.log" | head -n1)"
  eval "args=( $line )"
  printf '%s' "${args[$1]}"
}

@test "AC-24 set-issue.sh tiene cabecera, códigos de salida, modo estricto y secciones" {
  assert_script_structure "$SCRIPT"
}

@test "AC-14 set-issue escribe el mensaje y luego los campos con su etiqueta, por SSH a la IP" {
  run "$SCRIPT" vm1 Hola hostname users

  [ "$status" -eq 0 ]
  [ "$output" = "/etc/issue de vm1 actualizado" ]
  ssh_line="$(grep -E '^ssh ' "$CALLS" | grep -F -- "/etc/issue")"
  [[ "$ssh_line" == *"root@192.168.1.50"* ]]
  [[ "$ssh_line" == *"HostKeyAlias=vm1"* ]]
  # El contenido viaja como un solo argumento: el mensaje, "Host: \n" y "Usuarios: \U" con los
  # escapes literales para que agetty los expanda.
  [ "$(remote_arg 4)" = $'Hola\nHost: \\n\nUsuarios: \\U' ]
}

@test "AC-14 set-issue escribe /etc/issue.net con los escapes expandidos y configura el Banner de sshd" {
  run "$SCRIPT" vm1 Hola hostname

  [ "$status" -eq 0 ]
  # El contenido sigue siendo el argumento 4; el guion remoto (argumento 2) escribe /etc/issue tal
  # cual, expande los escapes para /etc/issue.net (sshd no lo hace) y activa "Banner /etc/issue.net".
  [ "$(remote_arg 4)" = $'Hola\nHost: \\n' ]
  remote_script="$(remote_arg 2)"
  [[ "$remote_script" == *"/etc/issue.net"* ]]
  [[ "$remote_script" == *"Banner /etc/issue.net"* ]]
  [[ "$remote_script" == *"sshd -t"* ]]
}

@test "AC-14 set-issue sin mensaje escribe solo los campos en el orden recibido" {
  run "$SCRIPT" vm1 "" hostname date

  [ "$status" -eq 0 ]
  [ "$(remote_arg 4)" = $'Host: \\n\nFecha: \\d' ]
}

@test "AC-25 set-issue rechaza un mensaje con barra invertida" {
  run "$SCRIPT" vm1 'a\b'

  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

@test "AC-25 set-issue rechaza un mensaje con tabulación" {
  run "$SCRIPT" vm1 "$(printf 'a\tb')"

  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

@test "AC-25 set-issue acepta tildes, ñ y saltos de línea" {
  run "$SCRIPT" vm1 "$(printf 'Configuración de producción\nlínea 2 con ñ')"

  [ "$status" -eq 0 ]
  [ "$(remote_arg 4)" = $'Configuración de producción\nlínea 2 con ñ' ]
}

@test "AC-15 set-issue rechaza un mensaje de más de 500 caracteres, un campo desconocido y un pedido vacío" {
  run "$SCRIPT" vm1 "$(printf 'a%.0s' $(seq 1 501))"
  [ "$status" -eq 2 ]
  run "$SCRIPT" vm1 "ok" campo-raro
  [ "$status" -eq 2 ]
  run "$SCRIPT" vm1 ""
  [ "$status" -eq 2 ]
  run "$SCRIPT" vm1
  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

@test "AC-43 set-issue sale con 7 si la VM no tiene IP y no ejecuta ssh" {
  setup_stubs
  stub_vm_ip vm1 -

  run --separate-stderr "$SCRIPT" vm1 Hola hostname

  [ "$status" -eq 7 ]
  [[ "$stderr" == *"La VM no tiene dirección IP; verifique que esté encendida"* ]]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

@test "AC-48 set-issue rechaza argumentos inválidos sin llamar a VBoxManage ni ssh" {
  run "$SCRIPT"
  [ "$status" -eq 2 ]
  run "$SCRIPT" vm1
  [ "$status" -eq 2 ]
  run "$SCRIPT" VM1 Hola hostname
  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

@test "AC-37 set-issue con --jump entra al servidor por el balanceador" {
  stub_jump_topology lb1 running web-a running

  run --separate-stderr "$SCRIPT" --jump lb1 web-a Hola hostname users

  [ "$status" -eq 0 ]
  assert_routed_ssh lb1 web-a
}

@test "AC-37 set-issue sale con 2 si --jump no trae un balanceador válido y no llama a nada" {
  stub_jump_topology lb1 running web-a running

  run "$SCRIPT" --jump
  [ "$status" -eq 2 ]
  run "$SCRIPT" --jump Lb1 web-a Hola hostname users
  [ "$status" -eq 2 ]
  run "$SCRIPT" --jump "" web-a Hola hostname users
  [ "$status" -eq 2 ]
  run "$SCRIPT" --jump 'lb1;x' web-a Hola hostname users
  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

@test "AC-29 set-issue con --jump sale con 12 si el balanceador está apagado y no llama a ssh" {
  stub_jump_topology lb1 poweroff web-a running

  run --separate-stderr "$SCRIPT" --jump lb1 web-a Hola hostname users

  [ "$status" -eq 12 ]
  [[ "$stderr" == *"El balanceador lb1 está apagado; enciéndalo para llegar al servidor"* ]]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

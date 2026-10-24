#!/usr/bin/env bats
# Pruebas de lb-status.sh con stubs de VBoxManage y ssh: no tocan VirtualBox ni VMs.

load test_helper

# "run --separate-stderr" necesita bats 1.5 o superior.
bats_require_minimum_version 1.5.0

setup() {
  setup_haproxy_stubs
  SCRIPT="$HX/lb-status.sh"
  CALLS="$BATS_TEST_TMPDIR/calls.log"
  REMOTE="$BATS_TEST_TMPDIR/remote.log"
  export APP_SSH_KEY_PATH="$BATS_TEST_TMPDIR/stratus_backend"
  : >"$APP_SSH_KEY_PATH"
  stub_vm_nics lb1 running 080027CCCCCC 192.168.1.60 080027DDDDDD -
}

# csv_row <proxy> <servidor> <estado>: una fila de "show stat" con el estado en la columna 18.
csv_row() {
  printf '%s,%s,0,0,0,0,,0,0,0,,0,,0,0,0,0,%s,\n' "$1" "$2" "$3"
}

# csv_header: la primera línea de "show stat" (HAProxy la empieza con "# pxname,svname,...").
csv_header() {
  echo "# pxname,svname,qcur,qmax,scur,smax,slim,stot,bin,bout,dreq,dresp,ereq,econ,eresp,wretr,wredis,status,"
}

@test "AC-38 lb-status.sh tiene cabecera, códigos de salida, modo estricto y secciones" {
  assert_script_structure "$HAPROXY_DIR/lb-status.sh"
}

@test "AC-37 lb-status sale con 2 ante argumentos inválidos y no llama a nada" {
  long_name="$(printf 'a%.0s' {1..33})"
  run "$SCRIPT"
  [ "$status" -eq 2 ]
  run "$SCRIPT" lb1 extra
  [ "$status" -eq 2 ]
  for name in Lb1 -lb 'lb;x' '' "$long_name"; do
    run "$SCRIPT" "$name"
    [ "$status" -eq 2 ]
  done
  APP_SSH_KEY_PATH="" run "$SCRIPT" lb1
  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

@test "AC-25 lb-status imprime el estado de cada servidor sin las filas FRONTEND ni BACKEND" {
  STAT_CSV="$(
    csv_header
    csv_row stratus_front FRONTEND OPEN
    csv_row stratus_back server1 UP
    csv_row stratus_back server2 DOWN
    csv_row stratus_back BACKEND UP
  )" run --separate-stderr "$SCRIPT" lb1

  [ "$status" -eq 0 ]
  [ "$output" = $'server1 UP\nserver2 DOWN' ]
  # El programa remoto abre el socket de estadísticas y pide "show stat".
  grep -qF -- "/dev/tcp/127.0.0.1/9999" "$REMOTE"
  grep -qF -- "show stat" "$REMOTE"
}

@test "AC-25 lb-status deja solo la primera palabra del estado (UP 1/3, MAINT, NOLB)" {
  STAT_CSV="$(
    csv_header
    csv_row stratus_back server1 "UP 1/3"
    csv_row stratus_back server2 "MAINT"
    csv_row stratus_back server3 "NOLB"
    csv_row stratus_back server4 "DOWN 2/3"
  )" run --separate-stderr "$SCRIPT" lb1

  [ "$status" -eq 0 ]
  [ "$output" = $'server1 UP\nserver2 MAINT\nserver3 NOLB\nserver4 DOWN' ]
}

@test "AC-25 lb-status ignora los proxies que no son stratus_back y responde vacío sin servidores" {
  STAT_CSV="$(
    csv_header
    csv_row stratus_front FRONTEND OPEN
    csv_row otro_back server9 UP
    csv_row stratus_back BACKEND UP
  )" run --separate-stderr "$SCRIPT" lb1

  [ "$status" -eq 0 ]
  [ -z "$output" ]
}

@test "AC-25 lb-status lee el estado por el nombre de la columna, no por su posición" {
  STAT_CSV=$'# pxname,svname,status,qcur,\nstratus_back,server1,UP,0,\nstratus_back,BACKEND,UP,0,' \
    run --separate-stderr "$SCRIPT" lb1

  [ "$status" -eq 0 ]
  [ "$output" = "server1 UP" ]
}

@test "AC-25 lb-status sale con 5 si no puede leer el socket de HAProxy" {
  FAIL_REMOTE='/dev/tcp' run --separate-stderr "$SCRIPT" lb1

  [ "$status" -eq 5 ]
  [[ "$stderr" == *"No se pudo leer el estado de HAProxy"* ]]
  [ -z "$output" ]
}

@test "AC-25 lb-status sale con 5 si la respuesta no tiene la columna status" {
  STAT_CSV=$'# pxname,svname,qcur,\nstratus_back,server1,0,' run --separate-stderr "$SCRIPT" lb1

  [ "$status" -eq 5 ]
  [ -z "$output" ]
}

@test "AC-40 lb-status sale con 12 si el balanceador está apagado y no llama a ssh" {
  stub_vm_nics lb2 poweroff 080027EEEEEE 192.168.1.61 080027FFFFFF -

  run --separate-stderr "$SCRIPT" lb2

  [ "$status" -eq 12 ]
  [[ "$stderr" == *"El balanceador lb2 está apagado; enciéndalo para llegar al servidor"* ]]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

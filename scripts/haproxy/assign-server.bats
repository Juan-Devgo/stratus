#!/usr/bin/env bats
# Pruebas de assign-server.sh con stubs de VBoxManage, ssh y de los scripts de ../prepare: no tocan
# VirtualBox ni VMs. El script se ejecuta desde la copia "$HX" (ver test_helper.bash).

load test_helper

# "run --separate-stderr" necesita bats 1.5 o superior.
bats_require_minimum_version 1.5.0

setup() {
  setup_haproxy_stubs
  SCRIPT="$HX/assign-server.sh"
  CALLS="$BATS_TEST_TMPDIR/calls.log"
  REMOTE="$BATS_TEST_TMPDIR/remote.log"
  STDIN_LOG="$BATS_TEST_TMPDIR/ssh-stdin.log"
  export APP_SSH_KEY_PATH="$BATS_TEST_TMPDIR/stratus_backend"
  : >"$APP_SSH_KEY_PATH"
  printf 'ssh-ed25519 BACKENDBLOB stratus-backend\n' >"$APP_SSH_KEY_PATH.pub"
  # lb1 está encendido con su IPv4 en puente 192.168.1.60.
  stub_vm_nics lb1 running 080027CCCCCC 192.168.1.60 080027DDDDDD -
  # web-a no tiene IP interna en el primer sondeo y luego recibe 10.10.1.11.
  stub_ip_sequence web-a - 10.10.1.11
  # Llaves públicas que tiene el servidor.
  export SERVER_KEYS="root ssh-ed25519 ROOTBLOB root@laptop"
  # Argumentos: lb subnet vm server_name port frontend_port bridge
  ARGS=(lb1 1 web-a server1 80 80 eth0)
}

# line_of <texto fijo> [n]: número de la n-ésima línea de calls.log que lo contiene (vacío si no hay).
line_of() {
  grep -nF -- "$1" "$CALLS" | sed -n "${2:-1}p" | cut -d: -f1 || true
}

# assert_order <texto A> <texto B>...: cada texto aparece en calls.log después del anterior.
assert_order() {
  local prev=0 text n
  for text in "$@"; do
    n="$(line_of "$text")"
    [ -n "$n" ] || { echo "no aparece: $text" >&2; return 1; }
    [ "$n" -gt "$prev" ] || { echo "'$text' ($n) no va después de la línea $prev" >&2; return 1; }
    prev="$n"
  done
}

@test "AC-38 assign-server.sh tiene cabecera, códigos de salida, modo estricto y secciones" {
  assert_script_structure "$HAPROXY_DIR/assign-server.sh"
}

@test "AC-37 assign-server sale con 2 ante argumentos inválidos y no llama a nada" {
  specs11=()
  for i in {1..11}; do specs11+=("server$i,vm$i,80,10.10.1.$((10 + i)),on"); done
  specs10=("${specs11[@]:0:10}")
  long_name="$(printf 'a%.0s' {1..33})"
  bad=(
    ""
    "lb1 1 web-a server1 80 80"
    "Lb1 1 web-a server1 80 80 eth0"
    "$long_name 1 web-a server1 80 80 eth0"
    "lb1 0 web-a server1 80 80 eth0"
    "lb1 255 web-a server1 80 80 eth0"
    "lb1 1 lb1 server1 80 80 eth0"
    "lb1 1 Web-A server1 80 80 eth0"
    "lb1 1 web-a server0 80 80 eth0"
    "lb1 1 web-a server11 80 80 eth0"
    "lb1 1 web-a server1 0 80 eth0"
    "lb1 1 web-a server1 65536 80 eth0"
    "lb1 1 web-a server1 80a 80 eth0"
    "lb1 1 web-a server1 80 22 eth0"
    "lb1 1 web-a server1 80 0 eth0"
    "lb1 1 web-a server1 80 80 eth0;x"
    "lb1 1 web-a server1 80 80 eth0 server2,web-b,80,10.10.1.12"
    "lb1 1 web-a server1 80 80 eth0 server2,web-b,80,10.10.2.12,on"
    "lb1 1 web-a server1 80 80 eth0 server2,web-b,80,10.10.1.12,on;x"
    "lb1 1 web-a server1 80 80 eth0 server1,web-b,80,10.10.1.12,on"
    "lb1 1 web-a server1 80 80 eth0 server2,web-a,80,10.10.1.12,on"
    "lb1 1 web-a server1 80 80 eth0 server2,web-b,80,10.10.1.12,on server2,web-c,80,10.10.1.13,on"
  )
  for args in "${bad[@]}"; do
    # shellcheck disable=SC2086 # las palabras se separan a propósito; ninguna lleva espacios
    run "$SCRIPT" $args
    [ "$status" -eq 2 ]
  done
  # Once especificaciones, y diez más la nueva serían once servidores.
  run "$SCRIPT" "${ARGS[@]}" "${specs11[@]}"
  [ "$status" -eq 2 ]
  run "$SCRIPT" lb1 1 web-z server1 80 80 eth0 "${specs10[@]}"
  [ "$status" -eq 2 ]
  # Sin la llave del backend tampoco se ejecuta nada.
  APP_SSH_KEY_PATH="" run "$SCRIPT" "${ARGS[@]}"
  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

@test "AC-13 assign-server detiene la VM, la pasa a la red interna, espera la IP, copia las llaves y agrega la línea" {
  run --separate-stderr "$SCRIPT" "${ARGS[@]}"

  [ "$status" -eq 0 ]
  [ "$output" = $'Servidor server1 (web-a) asignado a lb1\nip=10.10.1.11' ]
  # Orden: stop, adaptadores, arranque, llaves de jump, revisión y recarga de HAProxy.
  assert_order \
    "stop-vm.sh web-a" \
    "modifyvm web-a --nic1 intnet --intnet1 stratus-lb1 --nic2 nat" \
    "startvm web-a" \
    "/var/lib/jump/.ssh/authorized_keys" \
    'haproxy -c -f' \
    'reload haproxy'
  # Las llaves del servidor (sin la del backend) quedan en jump, etiquetadas con la VM.
  grep -qxF -- "restrict,port-forwarding ssh-ed25519 ROOTBLOB stratus-jump:web-a" "$STDIN_LOG"
  # La línea del backend lleva la IP aprendida y el puerto.
  grep -qxF -- "  server server1 10.10.1.11:80 check" "$STDIN_LOG"
  grep -qxF -- "  bind :80" "$STDIN_LOG"
}

@test "AC-13 assign-server borra las propiedades de IP antes de arrancar para no leer la IP anterior" {
  run "$SCRIPT" "${ARGS[@]}"

  [ "$status" -eq 0 ]
  assert_order \
    "modifyvm web-a --nic1 intnet" \
    "guestproperty delete web-a /VirtualBox/GuestInfo/Net/0/V4/IP" \
    "startvm web-a"
  grep -qF -- "guestproperty delete web-a /VirtualBox/GuestInfo/Net/1/V4/IP" "$CALLS"
}

@test "AC-13 assign-server conserva las líneas de los servidores existentes y agrega la nueva al final" {
  run "$SCRIPT" lb1 1 web-a server2 8080 8081 eth0 server1,web-b,80,10.10.1.12,on

  [ "$status" -eq 0 ]
  server_lines="$(grep -E '^\s*server ' "$STDIN_LOG")"
  [ "$server_lines" = $'  server server1 10.10.1.12:80 check\n  server server2 10.10.1.11:8080 check' ]
  grep -qxF -- "  bind :8081" "$STDIN_LOG"
}

@test "AC-20 assign-server agrega la NIC NAT sin reenvío de puertos" {
  run "$SCRIPT" "${ARGS[@]}"

  [ "$status" -eq 0 ]
  grep -qF -- "--nic2 nat" "$CALLS"
  ! grep -qF -- "natpf" "$CALLS" || return 1
}

@test "AC-48 assign-server deja encendida una VM que estaba apagada, sin detenerla ni apagarla después" {
  set_vm_state web-a poweroff

  run "$SCRIPT" "${ARGS[@]}"

  [ "$status" -eq 0 ]
  ! grep -qF -- "stop-vm.sh" "$CALLS" || return 1
  grep -qF -- "startvm web-a" "$CALLS"
  ! grep -qF -- "controlvm web-a poweroff" "$CALLS" || return 1
  [ "$(cat "$BATS_TEST_TMPDIR/state-web-a")" = running ]
}

@test "AC-13 assign-server sale con 3 si una de las VMs no existe en VirtualBox y no cambia nada" {
  stub_command VBoxManage 'if [[ "$1 ${2:-}" == "showvminfo web-q" ]]; then exit 1; fi'

  run --separate-stderr "$SCRIPT" lb1 1 web-q server1 80 80 eth0

  [ "$status" -eq 3 ]
  [[ "$stderr" == *"VM no encontrada en VirtualBox"* ]]
  ! grep -qE 'modifyvm|stop-vm.sh|startvm' "$CALLS" || return 1
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

@test "AC-13 assign-server sale con 12 si el balanceador está apagado y no cambia nada" {
  stub_vm_nics lb2 poweroff 080027EEEEEE 192.168.1.61 080027FFFFFF -

  run --separate-stderr "$SCRIPT" lb2 1 web-a server1 80 80 eth0

  [ "$status" -eq 12 ]
  [[ "$stderr" == *"El balanceador lb2 está apagado; enciéndalo para llegar al servidor"* ]]
  ! grep -qE 'modifyvm|stop-vm.sh|startvm|controlvm' "$CALLS" || return 1
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

@test "AC-17 assign-server revierte si no llega la IP interna: sale con 14 tras 60 esperas y no reinicia una VM que estaba apagada" {
  set_vm_state web-b poweroff
  stub_ip_sequence web-b -
  run --separate-stderr "$SCRIPT" lb1 1 web-b server1 80 80 eth0

  [ "$status" -eq 14 ]
  [[ "$stderr" == *"120 segundos"* ]]
  [ "$(grep -c '^sleep 2$' "$CALLS")" -eq 60 ]
  # Restauración: jump_drop, apagado, adaptadores originales y ningún arranque posterior.
  assert_order "startvm web-b" "/var/lib/jump/.ssh/authorized_keys" "controlvm web-b poweroff" \
    "modifyvm web-b --nic1 bridged --bridgeadapter1 eth0 --nic2 none"
  restore="$(line_of "modifyvm web-b --nic1 bridged")"
  [ "$(line_of "startvm web-b" 2)" = "" ]
  [ "$restore" -gt "$(line_of "startvm web-b")" ]
  # No se subió ningún haproxy.cfg.
  ! grep -qF -- "haproxy.cfg.new" "$REMOTE" || return 1
  [ -z "$output" ]
}

@test "AC-17 assign-server revierte y vuelve a encender la VM si estaba encendida" {
  stub_ip_sequence web-b -

  run --separate-stderr "$SCRIPT" lb1 1 web-b server1 80 80 eth0

  [ "$status" -eq 14 ]
  assert_order "stop-vm.sh web-b" "modifyvm web-b --nic1 intnet" "startvm web-b" "controlvm web-b poweroff" \
    "modifyvm web-b --nic1 bridged --bridgeadapter1 eth0 --nic2 none"
  restore="$(line_of "modifyvm web-b --nic1 bridged")"
  [ "$(line_of "startvm web-b" 2)" -gt "$restore" ]
  [ "$(cat "$BATS_TEST_TMPDIR/state-web-b")" = running ]
}

@test "AC-24 assign-server sale con 10 y revierte la VM si HAProxy rechaza la configuración" {
  HAPROXY_CHECK=fail run --separate-stderr "$SCRIPT" "${ARGS[@]}"

  [ "$status" -eq 10 ]
  grep -qF -- "modifyvm web-a --nic1 bridged --bridgeadapter1 eth0 --nic2 none" "$CALLS"
  ! grep -qF -- "haproxy.cfg.bak" "$REMOTE" || return 1
  [ -z "$output" ]
}

@test "AC-42 assign-server sale con 15 y revierte si no puede actualizar jump" {
  FAIL_REMOTE='/var/lib/jump' run --separate-stderr "$SCRIPT" "${ARGS[@]}"

  [ "$status" -eq 15 ]
  grep -qF -- "modifyvm web-a --nic1 bridged --bridgeadapter1 eth0 --nic2 none" "$CALLS"
  ! grep -qF -- "haproxy.cfg.new" "$REMOTE" || return 1
}

@test "AC-17 assign-server sale con 9 si falla el paso y también la restauración" {
  stub_ip_sequence web-b -
  stub_command VBoxManage 'if [[ "$1" == modifyvm && "$*" == *"--nic2 none"* ]]; then echo "error de VBoxManage" >&2; exit 1; fi'

  run --separate-stderr "$SCRIPT" lb1 1 web-b server1 80 80 eth0

  [ "$status" -eq 9 ]
  [[ "$stderr" == *"restauración"* ]]
}

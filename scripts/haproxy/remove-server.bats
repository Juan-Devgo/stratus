#!/usr/bin/env bats
# Pruebas de remove-server.sh con stubs de VBoxManage, ssh y de los scripts de ../prepare: no tocan
# VirtualBox ni VMs. El script se ejecuta desde la copia "$HX" (ver test_helper.bash).

load test_helper

# "run --separate-stderr" necesita bats 1.5 o superior.
bats_require_minimum_version 1.5.0

setup() {
  setup_haproxy_stubs
  SCRIPT="$HX/remove-server.sh"
  CALLS="$BATS_TEST_TMPDIR/calls.log"
  REMOTE="$BATS_TEST_TMPDIR/remote.log"
  STDIN_LOG="$BATS_TEST_TMPDIR/ssh-stdin.log"
  export APP_SSH_KEY_PATH="$BATS_TEST_TMPDIR/stratus_backend"
  : >"$APP_SSH_KEY_PATH"
  stub_vm_nics lb1 running 080027CCCCCC 192.168.1.60 080027DDDDDD -
  stub_vm_power web-a running
  # Argumentos: lb vm frontend_port bridge [specs de los servidores que quedan]
  ARGS=(lb1 web-a 80 eth0 server2,web-b,80,10.10.1.12,on)
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

@test "AC-38 remove-server.sh tiene cabecera, códigos de salida, modo estricto y secciones" {
  assert_script_structure "$HAPROXY_DIR/remove-server.sh"
}

@test "AC-37 remove-server sale con 2 ante argumentos inválidos y no llama a nada" {
  long_name="$(printf 'a%.0s' {1..33})"
  bad=(
    ""
    "lb1 web-a 80"
    "Lb1 web-a 80 eth0"
    "$long_name web-a 80 eth0"
    "lb1 WEB 80 eth0"
    "lb1 lb1 80 eth0"
    "lb1 web-a 22 eth0"
    "lb1 web-a 0 eth0"
    "lb1 web-a 65536 eth0"
    "lb1 web-a 80 eth0;x"
    "lb1 web-a 80 eth0 server2,web-b,80"
    "lb1 web-a 80 eth0 server2,web-b,80,10.10.1.12,maybe"
    "lb1 web-a 80 eth0 server1,web-a,80,10.10.1.11,on"
    "lb1 web-a 80 eth0 server2,web-b,80,10.10.1.12,on server2,web-c,80,10.10.1.13,on"
  )
  for args in "${bad[@]}"; do
    # shellcheck disable=SC2086 # las palabras se separan a propósito; ninguna lleva espacios
    run "$SCRIPT" $args
    [ "$status" -eq 2 ]
  done
  [ ! -s "$CALLS" ]
}

@test "AC-18 remove-server quita la línea, las llaves y devuelve la VM al puente, reiniciándola" {
  run --separate-stderr "$SCRIPT" "${ARGS[@]}"

  [ "$status" -eq 0 ]
  [ "$output" = "Servidor web-a quitado de lb1" ]
  # Orden: cfg revisada y recargada, llaves de jump, detener, adaptadores, arrancar.
  assert_order \
    'haproxy -c -f' \
    'reload haproxy' \
    "/var/lib/jump/.ssh/authorized_keys" \
    "stop-vm.sh web-a" \
    "modifyvm web-a --nic2 none --nic1 bridged --bridgeadapter1 eth0" \
    "startvm web-a"
  # El haproxy.cfg nuevo solo trae al servidor que queda.
  grep -qxF -- "  server server2 10.10.1.12:80 check" "$STDIN_LOG"
  [ "$(grep -cE '^\s*server ' "$STDIN_LOG")" -eq 1 ]
  # jump_drop no lee nada del servidor: solo escribe en el balanceador.
  ! grep -qF -- "getent passwd" "$REMOTE" || return 1
  [ "$(cat "$BATS_TEST_TMPDIR/state-web-a")" = running ]
}

@test "AC-18 remove-server con el último servidor deja el backend vacío" {
  run "$SCRIPT" lb1 web-a 8080 eth0

  [ "$status" -eq 0 ]
  grep -qxF -- "  bind :8080" "$STDIN_LOG"
  ! grep -qE '^\s*server ' "$STDIN_LOG" || return 1
}

@test "AC-18 remove-server no enciende una VM que estaba apagada ni la detiene" {
  set_vm_state web-a poweroff

  run "$SCRIPT" "${ARGS[@]}"

  [ "$status" -eq 0 ]
  ! grep -qF -- "stop-vm.sh" "$CALLS" || return 1
  grep -qF -- "modifyvm web-a --nic2 none --nic1 bridged --bridgeadapter1 eth0" "$CALLS"
  ! grep -qF -- "startvm" "$CALLS" || return 1
}

@test "AC-40 remove-server con el balanceador apagado sale con 12 y no cambia nada" {
  stub_vm_nics lb2 poweroff 080027EEEEEE 192.168.1.61 080027FFFFFF -

  run --separate-stderr "$SCRIPT" lb2 web-a 80 eth0

  [ "$status" -eq 12 ]
  [[ "$stderr" == *"El balanceador lb2 está apagado; enciéndalo para llegar al servidor"* ]]
  ! grep -qE 'modifyvm|stop-vm.sh|startvm|controlvm' "$CALLS" || return 1
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

@test "AC-18 remove-server sale con 3 si la VM no existe en VirtualBox y no cambia nada" {
  stub_command VBoxManage 'if [[ "$1 ${2:-}" == "showvminfo web-q" ]]; then exit 1; fi'

  run --separate-stderr "$SCRIPT" lb1 web-q 80 eth0

  [ "$status" -eq 3 ]
  ! grep -qE 'modifyvm|stop-vm.sh|startvm' "$CALLS" || return 1
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

@test "AC-18 remove-server se puede reintentar: con la línea, las llaves y los adaptadores ya quitados termina bien" {
  # La línea ya no está: el archivo subido es igual al actual, no hay recarga.
  CMP=same run --separate-stderr "$SCRIPT" "${ARGS[@]}"

  [ "$status" -eq 0 ]
  [ "$output" = "Servidor web-a quitado de lb1" ]
  ! grep -qF -- "reload haproxy" "$REMOTE" || return 1
  # Las llaves se vuelven a quitar (no hay nada) y los adaptadores se vuelven a fijar.
  grep -qF -- "/var/lib/jump/.ssh/authorized_keys" "$CALLS"
  grep -qF -- "modifyvm web-a --nic2 none --nic1 bridged --bridgeadapter1 eth0" "$CALLS"
}

@test "AC-24 remove-server sale con 10 y no toca la VM si HAProxy rechaza la configuración" {
  HAPROXY_CHECK=fail run --separate-stderr "$SCRIPT" "${ARGS[@]}"

  [ "$status" -eq 10 ]
  ! grep -qE 'modifyvm|stop-vm.sh|startvm' "$CALLS" || return 1
  ! grep -qF -- "/var/lib/jump/.ssh/authorized_keys" "$CALLS" || return 1
}

@test "AC-18 remove-server sale con 15 si no puede quitar las llaves de jump y no toca la VM" {
  FAIL_REMOTE='/var/lib/jump' run --separate-stderr "$SCRIPT" "${ARGS[@]}"

  [ "$status" -eq 15 ]
  ! grep -qE 'modifyvm|stop-vm.sh|startvm' "$CALLS" || return 1
}

@test "AC-18 remove-server sale con 4 si VBoxManage no puede devolver el adaptador al puente" {
  stub_command VBoxManage 'if [[ "$1" == modifyvm ]]; then echo "error de VBoxManage" >&2; exit 1; fi'

  run --separate-stderr "$SCRIPT" "${ARGS[@]}"

  [ "$status" -eq 4 ]
}

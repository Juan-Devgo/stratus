#!/usr/bin/env bats
# Pruebas de refresh-lb.sh con stubs de VBoxManage y ssh: no tocan VirtualBox ni VMs.

load test_helper

# "run --separate-stderr" necesita bats 1.5 o superior.
bats_require_minimum_version 1.5.0

setup() {
  setup_haproxy_stubs
  SCRIPT="$HX/refresh-lb.sh"
  CALLS="$BATS_TEST_TMPDIR/calls.log"
  REMOTE="$BATS_TEST_TMPDIR/remote.log"
  STDIN_LOG="$BATS_TEST_TMPDIR/ssh-stdin.log"
  export APP_SSH_KEY_PATH="$BATS_TEST_TMPDIR/stratus_backend"
  : >"$APP_SSH_KEY_PATH"
  stub_vm_nics lb1 running 080027CCCCCC 192.168.1.60 080027DDDDDD -
}

@test "AC-38 refresh-lb.sh tiene cabecera, códigos de salida, modo estricto y secciones" {
  assert_script_structure "$HAPROXY_DIR/refresh-lb.sh"
}

@test "AC-37 refresh-lb sale con 2 ante argumentos inválidos y no llama a nada" {
  specs11=()
  for i in {1..11}; do specs11+=("server$i,vm$i,80,10.10.1.$((10 + i)),on"); done
  long_name="$(printf 'a%.0s' {1..33})"
  bad=(
    ""
    "lb1 1"
    "Lb1 1 80"
    "$long_name 1 80"
    "lb1 0 80"
    "lb1 255 80"
    "lb1 1 22"
    "lb1 1 0"
    "lb1 1 65536"
    "lb1 1 80 server1,web-a,80"
    "lb1 1 80 server1,web-a,80,10.10.2.11,on"
    "lb1 1 80 server1,web-a,80,10.10.1.11,maybe"
    "lb1 1 80 server1,web-a,80,10.10.1.11,on server1,web-b,80,10.10.1.12,on"
  )
  for args in "${bad[@]}"; do
    # shellcheck disable=SC2086 # las palabras se separan a propósito; ninguna lleva espacios
    run "$SCRIPT" $args
    [ "$status" -eq 2 ]
  done
  run "$SCRIPT" lb1 1 80 "${specs11[@]}"
  [ "$status" -eq 2 ]
  APP_SSH_KEY_PATH="" run "$SCRIPT" lb1 1 80
  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

@test "AC-21 refresh-lb reescribe la IP nueva del servidor, recarga y la informa" {
  stub_ip_sequence web-a 10.10.1.15

  CMP=diff run --separate-stderr "$SCRIPT" lb1 1 80 server1,web-a,80,10.10.1.11,on

  [ "$status" -eq 0 ]
  [ "$output" = $'server1 10.10.1.15 on\nreloaded=yes' ]
  grep -qxF -- "  server server1 10.10.1.15:80 check" "$STDIN_LOG"
  ! grep -qF -- "10.10.1.11" "$STDIN_LOG" || return 1
  grep -qF -- "systemctl reload haproxy" "$REMOTE"
}

@test "AC-22 refresh-lb no recarga si nada cambió" {
  stub_ip_sequence web-a 10.10.1.11

  CMP=same run --separate-stderr "$SCRIPT" lb1 1 80 server1,web-a,80,10.10.1.11,on

  [ "$status" -eq 0 ]
  [ "$output" = $'server1 10.10.1.11 on\nreloaded=no' ]
  ! grep -qF -- "systemctl reload" "$REMOTE" || return 1
  ! grep -qF -- "haproxy -c" "$REMOTE" || return 1
}

@test "AC-23 un servidor apagado queda disabled con su última IP y no se sondea" {
  stub_ip_sequence web-a 10.10.1.11
  stub_ip_sequence web-b 10.10.1.99
  set_vm_state web-b poweroff

  run --separate-stderr "$SCRIPT" lb1 1 80 server1,web-a,80,10.10.1.11,on server2,web-b,80,10.10.1.12,on

  [ "$status" -eq 0 ]
  [ "$output" = $'server1 10.10.1.11 on\nserver2 10.10.1.12 off\nreloaded=yes' ]
  grep -qxF -- "  server server1 10.10.1.11:80 check" "$STDIN_LOG"
  grep -qxF -- "  server server2 10.10.1.12:80 check disabled" "$STDIN_LOG"
  ! grep -qF -- "guestproperty get web-b" "$CALLS" || return 1
  ! grep -qF -- "10.10.1.99" "$STDIN_LOG" || return 1
}

@test "AC-23 un servidor que estaba disabled vuelve a on cuando está encendido con su IP" {
  stub_ip_sequence web-b 10.10.1.12

  run --separate-stderr "$SCRIPT" lb1 1 80 server2,web-b,8080,10.10.1.12,off

  [ "$status" -eq 0 ]
  [ "$output" = $'server2 10.10.1.12 on\nreloaded=yes' ]
  grep -qxF -- "  server server2 10.10.1.12:8080 check" "$STDIN_LOG"
}

@test "AC-23 un servidor encendido que no recibe IPv4 en el rango queda disabled con su última IP" {
  stub_ip_sequence web-a 192.168.1.5 10.10.2.11

  run --separate-stderr "$SCRIPT" lb1 1 80 server1,web-a,80,10.10.1.11,on

  [ "$status" -eq 0 ]
  [ "$output" = $'server1 10.10.1.11 off\nreloaded=yes' ]
  grep -qxF -- "  server server1 10.10.1.11:80 check disabled" "$STDIN_LOG"
}

@test "AC-39 refresh-lb sondea los servidores en paralelo con un solo límite de 120 s" {
  specs=()
  for i in {1..10}; do
    specs+=("server$i,vm$i,80,10.10.1.$((10 + i)),on")
    if ((i <= 3)); then
      stub_ip_sequence "vm$i" -
    else
      stub_ip_sequence "vm$i" "10.10.1.$((100 + i))"
    fi
  done

  run --separate-stderr "$SCRIPT" lb1 1 80 "${specs[@]}"

  [ "$status" -eq 0 ]
  # Los tres que nunca informan agotan sus 60 sondeos de 2 s; los siete restantes responden al primero.
  [ "$(grep -c '^sleep 2$' "$CALLS")" -eq 180 ]
  for i in 1 2 3; do
    [ "$(grep -c "guestproperty get vm$i /VirtualBox/GuestInfo/Net/1/V4/IP" "$CALLS")" -eq 60 ]
  done
  for i in 4 5 6 7 8 9 10; do
    [ "$(grep -c "guestproperty get vm$i /VirtualBox/GuestInfo/Net/1/V4/IP" "$CALLS")" -eq 1 ]
  done
  # Los tres quedan disabled con su última IP; los otros siete, con la nueva.
  for i in 1 2 3; do
    grep -qxF -- "server$i 10.10.1.$((10 + i)) off" <<<"$output"
    grep -qxF -- "  server server$i 10.10.1.$((10 + i)):80 check disabled" "$STDIN_LOG"
  done
  for i in 4 5 6 7 8 9 10; do
    grep -qxF -- "server$i 10.10.1.$((100 + i)) on" <<<"$output"
  done
  [ "$(tail -n 1 <<<"$output")" = "reloaded=yes" ]
}

@test "refresh-lb sin servidores deja el backend vacío y solo informa reloaded" {
  CMP=same run --separate-stderr "$SCRIPT" lb1 1 8080

  [ "$status" -eq 0 ]
  [ "$output" = "reloaded=no" ]
  grep -qxF -- "  bind :8080" "$STDIN_LOG"
  ! grep -qE '^\s*server ' "$STDIN_LOG" || return 1
}

@test "AC-24 refresh-lb sale con 10 si HAProxy rechaza la configuración y no imprime nada en stdout" {
  stub_ip_sequence web-a 10.10.1.15

  HAPROXY_CHECK=fail run --separate-stderr "$SCRIPT" lb1 1 80 server1,web-a,80,10.10.1.11,on

  [ "$status" -eq 10 ]
  [ -z "$output" ]
  ! grep -qF -- "systemctl reload" "$REMOTE" || return 1
}

@test "AC-40 refresh-lb sale con 12 si el balanceador está apagado y no llama a ssh" {
  stub_vm_nics lb2 poweroff 080027EEEEEE 192.168.1.61 080027FFFFFF -

  run --separate-stderr "$SCRIPT" lb2 1 80

  [ "$status" -eq 12 ]
  [[ "$stderr" == *"El balanceador lb2 está apagado; enciéndalo para llegar al servidor"* ]]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

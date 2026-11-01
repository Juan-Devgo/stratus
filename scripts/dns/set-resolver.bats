#!/usr/bin/env bats
# Pruebas de set-resolver.sh (005) con stubs de VBoxManage y ssh; el programa remoto se ejecuta
# además sobre una raíz falsa (run_resolver_program) para comprobar lo que escribe.

load test_helper

bats_require_minimum_version 1.5.0

setup() {
  setup_dns_stubs
  CALLS="$BATS_TEST_TMPDIR/calls.log"
  REMOTE="$BATS_TEST_TMPDIR/remote.log"
  export APP_SSH_KEY_PATH="$BATS_TEST_TMPDIR/stratus_backend"
  : >"$APP_SSH_KEY_PATH"
  SCRIPT="$DX/set-resolver.sh"
}

# dhcp_state: resolv.conf y dhclient.conf como los deja DHCP, antes de usar el servidor DNS.
dhcp_state() {
  mkdir -p "$BATS_TEST_TMPDIR/root/etc/dhcp"
  printf 'domain lan\nnameserver 192.168.1.1\n' >"$BATS_TEST_TMPDIR/root/etc/resolv.conf"
  printf 'request subnet-mask, routers;\n' >"$BATS_TEST_TMPDIR/root/etc/dhcp/dhclient.conf"
}

@test "AC-56 set-resolver.sh tiene la estructura exigida" {
  assert_script_structure "$DNS_DIR/set-resolver.sh"
}

@test "AC-42 set-resolver con dhclient antepone el servidor DNS y lo deja en dhclient.conf" {
  stub_vm_ip vm1 192.168.1.40

  run --separate-stderr "$SCRIPT" vm1 192.168.1.53

  [ "$status" -eq 0 ]
  [ "$output" = "Resolvedor DNS 192.168.1.53 configurado en vm1" ]
  grep -q 'stratus-resolver set 192.168.1.53$' "$REMOTE"
  prog="$(resolver_program_file)"
  dhcp_state
  run_resolver_program "$prog" dhclient set 192.168.1.53
  # Una segunda vez no cambia nada.
  run_resolver_program "$prog" dhclient set 192.168.1.53

  [ "$(cat "$RROOT/etc/dhcp/dhclient.conf")" = $'request subnet-mask, routers;\n# stratus-resolver\nprepend domain-name-servers 192.168.1.53;' ]
  [ "$(cat "$RROOT/etc/resolv.conf")" = $'nameserver 192.168.1.53\ndomain lan\nnameserver 192.168.1.1' ]
}

@test "AC-42 set-resolver con dhcpcd instala un hook que vuelve a anteponer el servidor DNS" {
  stub_vm_ip vm1 192.168.1.40
  run --separate-stderr "$SCRIPT" vm1 192.168.1.53
  [ "$status" -eq 0 ]
  prog="$(resolver_program_file)"
  dhcp_state

  run_resolver_program "$prog" dhcpcd set 192.168.1.53

  hook="$RROOT/usr/lib/dhcpcd/dhcpcd-hooks/99-stratus-resolver"
  [ -f "$hook" ]
  [ "$(head -n 1 "$RROOT/etc/resolv.conf")" = "nameserver 192.168.1.53" ]
  [ ! -s "$RROOT/etc/dhcp/dhclient.conf" ] || ! grep -q stratus-resolver "$RROOT/etc/dhcp/dhclient.conf"
  # Una renovación de DHCP reescribe resolv.conf; el hook vuelve a anteponerlo una sola vez.
  printf 'nameserver 192.168.1.1\n' >"$RROOT/etc/resolv.conf"
  PATH="$BATS_TEST_TMPDIR/rbin" sh "$hook"
  PATH="$BATS_TEST_TMPDIR/rbin" sh "$hook"
  [ "$(cat "$RROOT/etc/resolv.conf")" = $'nameserver 192.168.1.53\nnameserver 192.168.1.1' ]
}

@test "AC-42 set-resolver sin cliente DHCP compatible sale con 5" {
  stub_vm_ip vm1 192.168.1.40
  export DHCP_CLIENT=none

  run --separate-stderr "$SCRIPT" vm1 192.168.1.53

  [ "$status" -eq 5 ]
  [[ "$stderr" == *"No se encontró un cliente DHCP compatible"* ]]
  prog="$(resolver_program_file)"
  dhcp_state
  run -3 run_resolver_program "$prog" none set 192.168.1.53
  [ "$(cat "$BATS_TEST_TMPDIR/root/etc/resolv.conf")" = $'domain lan\nnameserver 192.168.1.1' ]
}

@test "AC-43 set-resolver con la VM apagada sale con 7 sin ssh" {
  stub_vm_ip vm1 -

  run --separate-stderr "$SCRIPT" vm1 192.168.1.53

  [ "$status" -eq 7 ]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

@test "AC-43 set-resolver con --jump entra por el balanceador" {
  stub_jump_topology lb1 running web-a running

  run --separate-stderr "$SCRIPT" --jump lb1 web-a 192.168.1.53

  [ "$status" -eq 0 ]
  assert_routed_ssh lb1 web-a
}

@test "AC-55 set-resolver sale con 2 ante argumentos inválidos sin llamar a nada" {
  for args in "vm1" "vm1 192.168.1.53 extra" "VM1 192.168.1.53" "vm1 192.168.1.256" "vm1 nada" \
    "--jump Lb1 vm1 192.168.1.53" "--jump"; do
    # shellcheck disable=SC2086 # se separa a propósito
    run "$SCRIPT" $args
    [ "$status" -eq 2 ]
  done
  run env -u APP_SSH_KEY_PATH "$SCRIPT" vm1 192.168.1.53
  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

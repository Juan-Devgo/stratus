#!/usr/bin/env bats
# Pruebas de unset-resolver.sh (005) con stubs de VBoxManage y ssh; el programa remoto se ejecuta
# además sobre una raíz falsa (run_resolver_program) para comprobar lo que deja.

load test_helper

bats_require_minimum_version 1.5.0

setup() {
  setup_dns_stubs
  CALLS="$BATS_TEST_TMPDIR/calls.log"
  REMOTE="$BATS_TEST_TMPDIR/remote.log"
  export APP_SSH_KEY_PATH="$BATS_TEST_TMPDIR/stratus_backend"
  : >"$APP_SSH_KEY_PATH"
  SCRIPT="$DX/unset-resolver.sh"
}

# resolver_state: lo que deja set-resolver con dhclient y con dhcpcd a la vez (peor caso).
resolver_state() {
  local root="$BATS_TEST_TMPDIR/root"
  mkdir -p "$root/etc/dhcp" "$root/usr/lib/dhcpcd/dhcpcd-hooks"
  printf 'nameserver 192.168.1.53\ndomain lan\nnameserver 192.168.1.1\n' >"$root/etc/resolv.conf"
  printf 'request subnet-mask, routers;\n# stratus-resolver\nprepend domain-name-servers 192.168.1.53;\nsend host-name "vm1";\n' \
    >"$root/etc/dhcp/dhclient.conf"
  printf '# stratus-resolver\n' >"$root/usr/lib/dhcpcd/dhcpcd-hooks/99-stratus-resolver"
}

@test "AC-56 unset-resolver.sh tiene la estructura exigida" {
  assert_script_structure "$DNS_DIR/unset-resolver.sh"
}

@test "AC-42 unset-resolver quita el bloque, el hook y las líneas del servidor DNS; repetirlo no falla" {
  stub_vm_ip vm1 192.168.1.40

  run --separate-stderr "$SCRIPT" vm1 192.168.1.53

  [ "$status" -eq 0 ]
  [ "$output" = "Resolvedor DNS 192.168.1.53 quitado de vm1" ]
  grep -q 'stratus-resolver unset 192.168.1.53$' "$REMOTE"
  prog="$(resolver_program_file)"
  resolver_state
  run_resolver_program "$prog" dhclient unset 192.168.1.53
  [ "$(cat "$RROOT/etc/dhcp/dhclient.conf")" = $'request subnet-mask, routers;\nsend host-name "vm1";' ]
  [ "$(cat "$RROOT/etc/resolv.conf")" = $'domain lan\nnameserver 192.168.1.1' ]
  [ ! -e "$RROOT/usr/lib/dhcpcd/dhcpcd-hooks/99-stratus-resolver" ]

  run_resolver_program "$prog" dhclient unset 192.168.1.53
  [ "$(cat "$RROOT/etc/resolv.conf")" = $'domain lan\nnameserver 192.168.1.1' ]
}

@test "AC-42 unset-resolver sin nada que quitar termina bien" {
  stub_vm_ip vm1 192.168.1.40
  run --separate-stderr "$SCRIPT" vm1 192.168.1.53
  [ "$status" -eq 0 ]
  prog="$(resolver_program_file)"

  run_resolver_program "$prog" none unset 192.168.1.53

  [ ! -e "$RROOT/etc/resolv.conf" ]
}

@test "AC-43 unset-resolver con la VM apagada sale con 7 sin ssh" {
  stub_vm_ip vm1 -

  run --separate-stderr "$SCRIPT" vm1 192.168.1.53

  [ "$status" -eq 7 ]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

@test "AC-43 unset-resolver con --jump entra por el balanceador" {
  stub_jump_topology lb1 running web-a running

  run --separate-stderr "$SCRIPT" --jump lb1 web-a 192.168.1.53

  [ "$status" -eq 0 ]
  assert_routed_ssh lb1 web-a
}

@test "AC-55 unset-resolver sale con 2 ante argumentos inválidos sin llamar a nada" {
  for args in "vm1" "vm1 192.168.1.53 extra" "VM1 192.168.1.53" "vm1 192.168.1.256" \
    "--jump Lb1 vm1 192.168.1.53" "--jump"; do
    # shellcheck disable=SC2086 # se separa a propósito
    run "$SCRIPT" $args
    [ "$status" -eq 2 ]
  done
  run env -u APP_SSH_KEY_PATH "$SCRIPT" vm1 192.168.1.53
  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

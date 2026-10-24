#!/usr/bin/env bats
# Pruebas de list-vms.sh con stubs de VBoxManage: no tocan VirtualBox.

bats_require_minimum_version 1.5.0

load test_helper

SCRIPT="$PREPARE_DIR/list-vms.sh"

# setup: VirtualBox tiene vm1 encendida y vm2 apagada. La propiedad de IP de vm1 se controla con
# VM1_PROP (por defecto "Value: 192.168.1.50"; "FAIL" hace que guestproperty falle). Con VM1_NICS=two,
# vm1 es un servidor de balanceador: dos NICs, y Guest Additions publica el adaptador 2 (NAT) en
# /Net/0 y el adaptador 1 (red interna, 10.10.1.11) en /Net/1.
setup() {
  setup_stubs
  CALLS="$BATS_TEST_TMPDIR/calls.log"
  unset VM1_PROP FAIL_LIST VM1_NICS
  stub_command VBoxManage 'case "$1 ${2:-}" in
  "list vms")
    [[ -z "${FAIL_LIST:-}" ]] || exit 1
    printf "\"vm1\" {11111111-1111-1111-1111-111111111111}\n\"vm2\" {22222222-2222-2222-2222-222222222222}\n" ;;
  "list runningvms") printf "\"vm1\" {11111111-1111-1111-1111-111111111111}\n" ;;
  "showvminfo vm1")
    printf "VMState=\"running\"\nmacaddress1=\"080027AAAAAA\"\n"
    if [[ "${VM1_NICS:-}" == two ]]; then printf "nic2=\"nat\"\nmacaddress2=\"080027BBBBBB\"\n"; else printf "nic2=\"none\"\n"; fi ;;
  "guestproperty get")
    if [[ "${VM1_NICS:-}" == two && "${3:-}" == vm1 ]]; then
      case "${4:-}" in
        /VirtualBox/GuestInfo/Net/0/MAC) echo "Value: 080027BBBBBB" ;;
        /VirtualBox/GuestInfo/Net/0/V4/IP) echo "Value: 10.0.3.15" ;;
        /VirtualBox/GuestInfo/Net/1/MAC) echo "Value: 080027AAAAAA" ;;
        /VirtualBox/GuestInfo/Net/1/V4/IP) echo "Value: 10.10.1.11" ;;
        *) echo "No value set!" ;;
      esac
    elif [[ "${3:-}" == vm1 && "${4:-}" == /VirtualBox/GuestInfo/Net/0/V4/IP ]]; then
      [[ "${VM1_PROP:-}" != FAIL ]] || exit 1
      echo "${VM1_PROP:-Value: 192.168.1.50}"
    else
      echo "No value set!"
    fi ;;
esac
exit 0'
}

@test "AC-24 list-vms.sh tiene cabecera, códigos de salida, modo estricto y secciones" {
  assert_script_structure "$SCRIPT"
}

@test "AC-3 list-vms imprime nombre, estado e IP de cada VM" {
  run --separate-stderr "$SCRIPT"

  [ "$status" -eq 0 ]
  [ "$output" = $'vm1 running 192.168.1.50\nvm2 poweroff -' ]
}

@test "AC-3 list-vms no consulta la IP de una VM apagada" {
  run "$SCRIPT"

  [ "$status" -eq 0 ]
  ! grep -qF -- "guestproperty get vm2" "$CALLS" || return 1
}

@test "AC-42 list-vms imprime - para una VM encendida sin IP" {
  VM1_PROP="No value set!" run --separate-stderr "$SCRIPT"

  [ "$status" -eq 0 ]
  [ "$output" = $'vm1 running -\nvm2 poweroff -' ]
}

@test "AC-42 list-vms imprime - si la propiedad no es una IPv4 o la consulta falla" {
  VM1_PROP="Value: fe80::1" run --separate-stderr "$SCRIPT"
  [ "$status" -eq 0 ]
  [ "$output" = $'vm1 running -\nvm2 poweroff -' ]

  VM1_PROP="FAIL" run --separate-stderr "$SCRIPT"
  [ "$status" -eq 0 ]
  [ "$output" = $'vm1 running -\nvm2 poweroff -' ]
}

@test "AC-3 list-vms sale con 4 si VBoxManage falla" {
  FAIL_LIST=1 run "$SCRIPT"

  [ "$status" -eq 4 ]
}

@test "AC-48 list-vms rechaza argumentos sin llamar a VBoxManage" {
  run "$SCRIPT" extra

  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

@test "AC-9 list-vms imprime la IPv4 del adaptador 1 de una VM con dos NICs" {
  VM1_NICS=two run --separate-stderr "$SCRIPT"

  [ "$status" -eq 0 ]
  [ "$output" = $'vm1 running 10.10.1.11\nvm2 poweroff -' ]
}

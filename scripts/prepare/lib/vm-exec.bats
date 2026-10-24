#!/usr/bin/env bats
# Pruebas de lib/vm-exec.sh (vm_ip y vm_exec) con stubs de VBoxManage y ssh: no tocan VirtualBox
# ni VMs. La biblioteca se carga con "source", como hacen los scripts de invitado.

load ../test_helper

# "run --separate-stderr" necesita bats 1.5 o superior.
bats_require_minimum_version 1.5.0

setup() {
  setup_stubs
  CALLS="$BATS_TEST_TMPDIR/calls.log"
  export APP_SSH_KEY_PATH="$BATS_TEST_TMPDIR/stratus_backend"
  : >"$APP_SSH_KEY_PATH"
  # shellcheck source=vm-exec.sh
  source "$PREPARE_DIR/lib/vm-exec.sh"
}

@test "AC-14 vm_exec cita cada argumento con printf %q (espacios y ;)" {
  stub_vm_ip vm1 192.168.1.50

  run vm_exec vm1 echo 'a b; rm -rf /'

  [ "$status" -eq 0 ]
  ssh_line="$(grep -E '^ssh ' "$CALLS")"
  # ssh recibe un solo argumento con cada palabra citada: el shell remoto no separa el espacio ni el ";".
  [[ "$ssh_line" == *'echo a\ b\;\ rm\ -rf\ /'* ]]
  [[ "$ssh_line" == *"root@192.168.1.50 -- "* ]]
}

@test "AC-8 vm_exec devuelve 4 si ssh termina con 255 y 5 si el comando remoto falla" {
  stub_vm_ip vm1 192.168.1.50

  stub_command ssh 'exit 255'
  run --separate-stderr vm_exec vm1 true
  [ "$status" -eq 4 ]

  setup_stubs
  stub_vm_ip vm1 192.168.1.50
  stub_command ssh 'exit 1'
  run vm_exec vm1 true
  [ "$status" -eq 5 ]
}

@test "AC-48 vm_exec devuelve 2 sin APP_SSH_KEY_PATH o sin comando y no llama a VBoxManage ni ssh" {
  unset APP_SSH_KEY_PATH
  run vm_exec vm1 true
  [ "$status" -eq 2 ]

  export APP_SSH_KEY_PATH="$BATS_TEST_TMPDIR/stratus_backend"
  run vm_exec vm1
  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

@test "AC-43 vm_ip devuelve 7 si la VM está apagada aunque quede una IP antigua" {
  # showvminfo informa poweroff, pero la propiedad de Guest Additions aún guarda la última IP.
  stub_command VBoxManage 'if [[ "$1" == showvminfo ]]; then printf "VMState=\"poweroff\"\n"; exit 0; fi
if [[ "$1 ${2:-}" == "guestproperty get" ]]; then echo "Value: 192.168.1.50"; exit 0; fi'

  run --separate-stderr vm_ip vm1

  [ "$status" -eq 7 ]
  [ -z "$output" ]
  [[ "$stderr" == *"La VM no tiene dirección IP; verifique que esté encendida"* ]]

  # Y con la VM encendida imprime la IP.
  setup_stubs
  stub_vm_ip vm1 192.168.1.50
  run --separate-stderr vm_ip vm1
  [ "$status" -eq 0 ]
  [ "$output" = "192.168.1.50" ]
}

@test "AC-43 vm_exec devuelve 7 sin llamar a ssh si la VM no tiene IP" {
  stub_vm_ip vm1 -

  run vm_exec vm1 true

  [ "$status" -eq 7 ]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

# --- 004: IP por adaptador y salto por el balanceador (REQ-13, REQ-19) ---

@test "AC-47 vm_nic_ip con una sola NIC lee /Net/0 como siempre" {
  stub_vm_nics vm1 running 080027AAAAAA 192.168.1.50

  run --separate-stderr vm_nic_ip vm1 1

  [ "$status" -eq 0 ]
  [ "$output" = "192.168.1.50" ]
  grep -qF -- "guestproperty get vm1 /VirtualBox/GuestInfo/Net/0/V4/IP" "$CALLS"
}

@test "AC-47 vm_nic_ip elige la IP por MAC cuando hay dos NICs (Net/0 es la NIC NAT)" {
  # Net/0 = adaptador 2 (NAT, 10.0.3.15); Net/1 = adaptador 1 (la red interna).
  stub_vm_nics web-a running 080027AAAAAA 10.10.1.11 080027BBBBBB 10.0.3.15

  run --separate-stderr vm_nic_ip web-a 1
  [ "$status" -eq 0 ]
  [ "$output" = "10.10.1.11" ]

  run --separate-stderr vm_nic_ip web-a 2
  [ "$status" -eq 0 ]
  [ "$output" = "10.0.3.15" ]
}

@test "AC-47 vm_nic_ip devuelve 7 si la VM está apagada o el adaptador no tiene IPv4" {
  stub_vm_nics web-a poweroff 080027AAAAAA 10.10.1.11 080027BBBBBB 10.0.3.15
  run --separate-stderr vm_nic_ip web-a 1
  [ "$status" -eq 7 ]
  [ -z "$output" ]
  [[ "$stderr" == *"La VM no tiene dirección IP; verifique que esté encendida"* ]]

  setup_stubs
  stub_vm_nics web-a running 080027AAAAAA - 080027BBBBBB 10.0.3.15
  run --separate-stderr vm_nic_ip web-a 1
  [ "$status" -eq 7 ]
}

@test "AC-47 vm_nic_ip rechaza argumentos inválidos" {
  run vm_nic_ip vm1
  [ "$status" -eq 2 ]
  run vm_nic_ip vm1 0
  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

@test "AC-47 vm_ip de una VM sin ruta usa el adaptador 1 aunque tenga dos NICs" {
  stub_vm_nics web-a running 080027AAAAAA 10.10.1.11 080027BBBBBB 10.0.3.15

  run --separate-stderr vm_ip web-a

  [ "$status" -eq 0 ]
  [ "$output" = "10.10.1.11" ]
}

@test "AC-29 vm_ip y vm_exec de una VM con ruta salen con 12 si el balanceador está apagado y no llaman a ssh" {
  stub_vm_nics lb1 poweroff 080027CCCCCC 192.168.1.60 080027DDDDDD -
  stub_vm_nics web-a running 080027AAAAAA 10.10.1.11 080027BBBBBB 10.0.3.15
  vm_route web-a lb1

  run --separate-stderr vm_ip web-a
  [ "$status" -eq 12 ]
  [ -z "$output" ]
  [[ "$stderr" == *"El balanceador lb1 está apagado; enciéndalo para llegar al servidor"* ]]

  run --separate-stderr vm_exec web-a true
  [ "$status" -eq 12 ]
  [[ "$stderr" == *"El balanceador lb1 está apagado; enciéndalo para llegar al servidor"* ]]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

@test "AC-29 un balanceador encendido sin IPv4 en puente también cuenta como apagado" {
  stub_vm_nics lb1 running 080027CCCCCC - 080027DDDDDD -
  stub_vm_nics web-a running 080027AAAAAA 10.10.1.11 080027BBBBBB 10.0.3.15
  vm_route web-a lb1

  run --separate-stderr vm_ip web-a

  [ "$status" -eq 12 ]
  [[ "$stderr" == *"El balanceador lb1 está apagado"* ]]
}

@test "AC-47 vm_ip con ruta: balanceador encendido y servidor apagado da 7" {
  stub_vm_nics lb1 running 080027CCCCCC 192.168.1.60 080027DDDDDD -
  stub_vm_nics web-a poweroff 080027AAAAAA 10.10.1.11 080027BBBBBB 10.0.3.15
  vm_route web-a lb1

  run --separate-stderr vm_exec web-a true

  [ "$status" -eq 7 ]
  [[ "$stderr" == *"La VM no tiene dirección IP; verifique que esté encendida"* ]]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

@test "AC-27 vm_exec con ruta salta por jump@IP del balanceador con la llave del backend" {
  stub_vm_nics lb1 running 080027CCCCCC 192.168.1.60 080027DDDDDD -
  stub_vm_nics web-a running 080027AAAAAA 10.10.1.11 080027BBBBBB 10.0.3.15
  vm_route web-a lb1

  run vm_exec web-a systemctl status nginx

  [ "$status" -eq 0 ]
  ssh_line="$(grep -E '^ssh ' "$CALLS")"
  [ "$(grep -cE '^ssh ' "$CALLS")" -eq 1 ]
  proxy="ProxyCommand=ssh -i $APP_SSH_KEY_PATH -o BatchMode=yes -o ConnectTimeout=10 -o StrictHostKeyChecking=accept-new -o UserKnownHostsFile=$APP_SSH_KEY_PATH.known_hosts -o HostKeyAlias=lb1 -W %h:%p jump@192.168.1.60"
  [[ "$ssh_line" == *"$proxy"* ]]
  # El destino final es el servidor por su IP interna, con su propio alias de llave de host.
  [[ "$ssh_line" == *"-o HostKeyAlias=web-a root@10.10.1.11 -- "* ]]
  [[ "$ssh_line" == *"systemctl status nginx"* ]]
}

@test "AC-29 el balanceador mismo y las VMs sin ruta se alcanzan directo, sin ProxyCommand" {
  stub_vm_nics lb1 running 080027CCCCCC 192.168.1.60 080027DDDDDD -
  stub_vm_nics web-a running 080027AAAAAA 10.10.1.11 080027BBBBBB 10.0.3.15
  vm_route web-a lb1

  run vm_exec lb1 true

  [ "$status" -eq 0 ]
  ssh_line="$(grep -E '^ssh ' "$CALLS")"
  [[ "$ssh_line" == *"root@192.168.1.60"* ]]
  [[ "$ssh_line" != *ProxyCommand* ]]
}

@test "AC-29 vm_route exige dos argumentos y un balanceador distinto de la VM" {
  run vm_route web-a
  [ "$status" -eq 2 ]
  run vm_route web-a web-a
  [ "$status" -eq 2 ]
}

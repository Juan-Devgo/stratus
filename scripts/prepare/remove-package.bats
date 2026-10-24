#!/usr/bin/env bats
# Pruebas de remove-package.sh con stubs de VBoxManage, ssh y sleep: no tocan VirtualBox ni VMs.

load test_helper

# "run --separate-stderr" necesita bats 1.5 o superior.
bats_require_minimum_version 1.5.0

SCRIPT="$PREPARE_DIR/remove-package.sh"

setup() {
  setup_stubs
  CALLS="$BATS_TEST_TMPDIR/calls.log"
  export APP_SSH_KEY_PATH="$BATS_TEST_TMPDIR/stratus_backend"
  : >"$APP_SSH_KEY_PATH"
  stub_vm_ip vm1 192.168.1.50
}

@test "AC-24 remove-package.sh tiene cabecera, códigos de salida, modo estricto y secciones" {
  assert_script_structure "$SCRIPT"
}

@test "AC-6 remove-package purga el paquete por SSH en la IP de la VM" {
  run "$SCRIPT" vm1 nginx

  [ "$status" -eq 0 ]
  [ "$output" = "Paquete nginx eliminado de vm1" ]
  ssh_line="$(grep -E '^ssh ' "$CALLS" | grep -F -- "apt-get purge -y nginx")"
  [[ "$ssh_line" == *"root@192.168.1.50"* ]]
  [[ "$ssh_line" == *"HostKeyAlias=vm1"* ]]
}

@test "AC-7 remove-package rechaza un nombre de paquete con inyección" {
  run "$SCRIPT" vm1 "nginx; rm -rf /"

  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

@test "AC-7 remove-package rechaza un nombre de paquete vacío" {
  run "$SCRIPT" vm1 ""

  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

@test "AC-33 remove-package rechaza openssh-server" {
  run --separate-stderr "$SCRIPT" vm1 openssh-server

  [ "$status" -eq 2 ]
  [[ "$stderr" == *"No se puede eliminar openssh-server: el backend lo usa para conectarse"* ]]
  [ ! -s "$CALLS" ]
}

@test "AC-43 remove-package sale con 7 si la VM no tiene IP y no ejecuta ssh" {
  setup_stubs
  stub_vm_ip vm1 -

  run --separate-stderr "$SCRIPT" vm1 nginx

  [ "$status" -eq 7 ]
  [[ "$stderr" == *"La VM no tiene dirección IP; verifique que esté encendida"* ]]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

@test "AC-8 remove-package sale con 3 si la VM no existe y con 5 si apt-get falla" {
  setup_stubs
  stub_command VBoxManage '[[ "$1" == showvminfo ]] && exit 1'
  run "$SCRIPT" vm1 nginx
  [ "$status" -eq 3 ]

  setup_stubs
  stub_vm_ip vm1 192.168.1.50
  stub_command ssh 'exit 100'
  run "$SCRIPT" vm1 nginx
  [ "$status" -eq 5 ]
}

@test "AC-48 remove-package rechaza argumentos inválidos sin llamar a VBoxManage ni ssh" {
  run "$SCRIPT"
  [ "$status" -eq 2 ]
  run "$SCRIPT" vm1
  [ "$status" -eq 2 ]
  run "$SCRIPT" vm1 nginx extra
  [ "$status" -eq 2 ]
  run "$SCRIPT" VM1 nginx
  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

@test "AC-37 remove-package con --jump entra al servidor por el balanceador" {
  stub_jump_topology lb1 running web-a running

  run --separate-stderr "$SCRIPT" --jump lb1 web-a nginx

  [ "$status" -eq 0 ]
  assert_routed_ssh lb1 web-a
}

@test "AC-37 remove-package sale con 2 si --jump no trae un balanceador válido y no llama a nada" {
  stub_jump_topology lb1 running web-a running

  run "$SCRIPT" --jump
  [ "$status" -eq 2 ]
  run "$SCRIPT" --jump Lb1 web-a nginx
  [ "$status" -eq 2 ]
  run "$SCRIPT" --jump "" web-a nginx
  [ "$status" -eq 2 ]
  run "$SCRIPT" --jump 'lb1;x' web-a nginx
  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

@test "AC-29 remove-package con --jump sale con 12 si el balanceador está apagado y no llama a ssh" {
  stub_jump_topology lb1 poweroff web-a running

  run --separate-stderr "$SCRIPT" --jump lb1 web-a nginx

  [ "$status" -eq 12 ]
  [[ "$stderr" == *"El balanceador lb1 está apagado; enciéndalo para llegar al servidor"* ]]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

@test "AC-49 remove-package con --frontend rechaza eliminar haproxy sin llamar a nada" {
  run --separate-stderr "$SCRIPT" --frontend 80 vm1 haproxy

  [ "$status" -eq 2 ]
  [[ "$stderr" == *"No se puede eliminar haproxy: el balanceador lo usa"* ]]
  [ ! -s "$CALLS" ]
}

@test "AC-49 remove-package sin --frontend sí elimina haproxy y con --frontend elimina otros paquetes" {
  run "$SCRIPT" vm1 haproxy
  [ "$status" -eq 0 ]
  grep -qF -- "apt-get purge" "$CALLS"

  : >"$CALLS"
  run "$SCRIPT" --frontend 80 vm1 nginx
  [ "$status" -eq 0 ]
  grep -qF -- "apt-get purge -y nginx" "$CALLS"
}

@test "AC-49 remove-package rechaza un --frontend inválido o el 22 sin llamar a nada" {
  for port in 22 0 65536 080 "" "80;x"; do
    run "$SCRIPT" --frontend "$port" vm1 nginx
    [ "$status" -eq 2 ]
  done
  [ ! -s "$CALLS" ]
}

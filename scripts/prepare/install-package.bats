#!/usr/bin/env bats
# Pruebas de install-package.sh con stubs de VBoxManage, ssh y sleep: no tocan VirtualBox ni VMs.

load test_helper

# "run --separate-stderr" necesita bats 1.5 o superior.
bats_require_minimum_version 1.5.0

SCRIPT="$PREPARE_DIR/install-package.sh"

setup() {
  setup_stubs
  CALLS="$BATS_TEST_TMPDIR/calls.log"
  # Llave falsa del backend: el script solo comprueba que el archivo exista.
  export APP_SSH_KEY_PATH="$BATS_TEST_TMPDIR/stratus_backend"
  : >"$APP_SSH_KEY_PATH"
  # vm1 está encendida y reporta 192.168.1.50.
  stub_vm_ip vm1 192.168.1.50
}

@test "AC-24 install-package.sh tiene cabecera, códigos de salida, modo estricto y secciones" {
  assert_script_structure "$SCRIPT"
}

@test "AC-6 install-package instala el paquete por SSH en la IP de la VM" {
  run "$SCRIPT" vm1 nginx

  [ "$status" -eq 0 ]
  [ "$output" = "Paquete nginx instalado en vm1" ]
  ssh_line="$(grep -E '^ssh ' "$CALLS" | grep -F -- "apt-get install -y nginx")"
  [[ "$ssh_line" == *"root@192.168.1.50"* ]]
  [[ "$ssh_line" == *"HostKeyAlias=vm1"* ]]
}

@test "AC-7 install-package rechaza un nombre de paquete con inyección" {
  run "$SCRIPT" vm1 "nginx; rm -rf /"

  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

@test "AC-7 install-package rechaza un nombre de paquete vacío" {
  run "$SCRIPT" vm1 ""

  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

@test "AC-48 install-package rechaza una cantidad de argumentos incorrecta o una VM inválida" {
  run "$SCRIPT" vm1
  [ "$status" -eq 2 ]
  run "$SCRIPT" VM1 nginx
  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

@test "AC-32 instalar openssh-server es una instalación normal por SSH, sin Guest Additions" {
  run "$SCRIPT" vm1 openssh-server

  [ "$status" -eq 0 ]
  ssh_calls="$(grep -E '^ssh ' "$CALLS")"
  grep -qF -- "apt-get install -y openssh-server" <<<"$ssh_calls"
  # Todas las llamadas remotas usan la llave del backend contra la IP; no hay Guest Additions.
  [ "$(grep -cE '^ssh ' "$CALLS")" -eq "$(grep -cF -- "-i $APP_SSH_KEY_PATH" <<<"$ssh_calls")" ]
  [ "$(grep -cE '^ssh ' "$CALLS")" -eq "$(grep -cF -- "root@192.168.1.50" <<<"$ssh_calls")" ]
  ! grep -qF -- "guestcontrol" "$CALLS" || return 1
  ! grep -qF -- "authorized_keys" "$CALLS" || return 1
}

@test "AC-34 instalar ufw permite el puerto 22/tcp" {
  run "$SCRIPT" vm1 ufw

  [ "$status" -eq 0 ]
  ssh_calls="$(grep -E '^ssh ' "$CALLS")"
  grep -qF -- "apt-get install -y ufw" <<<"$ssh_calls"
  grep -qF -- "ufw allow 22/tcp" <<<"$ssh_calls"
}

@test "AC-34 install-package no toca ufw al instalar otro paquete" {
  run "$SCRIPT" vm1 nginx

  [ "$status" -eq 0 ]
  ! grep -qF -- "ufw" "$CALLS" || return 1
}

@test "AC-43 install-package sale con 7 si la VM no tiene IP y no ejecuta ssh" {
  setup_stubs
  stub_vm_ip vm1 -

  run --separate-stderr "$SCRIPT" vm1 nginx

  [ "$status" -eq 7 ]
  [[ "$stderr" == *"La VM no tiene dirección IP; verifique que esté encendida"* ]]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

@test "AC-8 install-package sale con 3 si la VM no existe" {
  setup_stubs
  stub_command VBoxManage '[[ "$1" == showvminfo ]] && exit 1'

  run "$SCRIPT" vm1 nginx

  [ "$status" -eq 3 ]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

@test "AC-8 install-package sale con 5 si apt-get falla y con 4 si no hay conexión SSH" {
  stub_command ssh 'exit 100'
  run "$SCRIPT" vm1 nginx
  [ "$status" -eq 5 ]

  # Stubs nuevos: el ssh anterior ya termina con 100 antes de llegar a un "exit 255" agregado.
  setup_stubs
  stub_vm_ip vm1 192.168.1.50
  stub_command ssh 'exit 255'
  run "$SCRIPT" vm1 nginx
  [ "$status" -eq 4 ]
}

@test "AC-37 install-package con --jump entra al servidor por el balanceador" {
  stub_jump_topology lb1 running web-a running

  run --separate-stderr "$SCRIPT" --jump lb1 web-a nginx

  [ "$status" -eq 0 ]
  assert_routed_ssh lb1 web-a
}

@test "AC-37 install-package sale con 2 si --jump no trae un balanceador válido y no llama a nada" {
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

@test "AC-29 install-package con --jump sale con 12 si el balanceador está apagado y no llama a ssh" {
  stub_jump_topology lb1 poweroff web-a running

  run --separate-stderr "$SCRIPT" --jump lb1 web-a nginx

  [ "$status" -eq 12 ]
  [[ "$stderr" == *"El balanceador lb1 está apagado; enciéndalo para llegar al servidor"* ]]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

@test "AC-49 install-package con --frontend y ufw permite el 22 y el puerto del frontend" {
  run "$SCRIPT" --frontend 80 vm1 ufw

  [ "$status" -eq 0 ]
  [ "$output" = "Paquete ufw instalado en vm1" ]
  ssh_lines="$(grep -E '^ssh ' "$CALLS")"
  [[ "$ssh_lines" == *"ufw allow 22/tcp"* ]]
  [[ "$ssh_lines" == *"ufw allow 80/tcp"* ]]
  # El 22 va antes de cualquier regla nueva, para no perder el acceso por SSH.
  [ "$(grep -nF -- "ufw allow 22/tcp" "$CALLS" | cut -d: -f1)" -lt "$(grep -nF -- "ufw allow 80/tcp" "$CALLS" | cut -d: -f1)" ]
}

@test "AC-49 install-package con --frontend y otro paquete no toca ufw" {
  run "$SCRIPT" --frontend 80 vm1 nginx

  [ "$status" -eq 0 ]
  ! grep -qF -- "ufw" "$CALLS" || return 1
}

@test "AC-49 install-package de ufw sin --frontend solo permite el 22" {
  run "$SCRIPT" vm1 ufw

  [ "$status" -eq 0 ]
  grep -qF -- "ufw allow 22/tcp" "$CALLS"
  ! grep -qF -- "/tcp" <(grep -v "ufw allow 22/tcp" "$CALLS") || return 1
}

@test "AC-49 install-package rechaza un --frontend inválido o el 22 sin llamar a nada" {
  for port in 22 0 65536 080 8o80 "" "80;x"; do
    run "$SCRIPT" --frontend "$port" vm1 ufw
    [ "$status" -eq 2 ]
  done
  run "$SCRIPT" --frontend
  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

@test "AC-49 install-package acepta --jump y --frontend juntos, en ese orden" {
  stub_jump_topology lb1 running web-a running

  run "$SCRIPT" --jump lb1 --frontend 8080 web-a ufw

  [ "$status" -eq 0 ]
  grep -qF -- "ufw allow 8080/tcp" "$CALLS"
  assert_routed_ssh lb1 web-a
}

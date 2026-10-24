#!/usr/bin/env bats
# Pruebas de set-hostname.sh con stubs de VBoxManage, ssh y sleep: no tocan VirtualBox ni VMs.

load test_helper

# "run --separate-stderr" necesita bats 1.5 o superior.
bats_require_minimum_version 1.5.0

SCRIPT="$PREPARE_DIR/set-hostname.sh"

setup() {
  setup_stubs
  CALLS="$BATS_TEST_TMPDIR/calls.log"
  export APP_SSH_KEY_PATH="$BATS_TEST_TMPDIR/stratus_backend"
  : >"$APP_SSH_KEY_PATH"
  # vm1 está encendida y reporta 192.168.1.50.
  stub_vm_ip vm1 192.168.1.50
}

@test "AC-24 set-hostname.sh tiene cabecera, códigos de salida, modo estricto y secciones" {
  assert_script_structure "$SCRIPT"
}

@test "AC-17 set-hostname cambia el hostname y la línea 127.0.1.1 de /etc/hosts" {
  run "$SCRIPT" vm1 web-01

  [ "$status" -eq 0 ]
  [ "$output" = "Hostname de vm1 cambiado a web-01" ]
  ssh_calls="$(grep -E '^ssh ' "$CALLS")"
  # Todas las llamadas van por SSH a la IP de la VM.
  [ "$(grep -cE '^ssh ' "$CALLS")" -eq "$(grep -cF -- "root@192.168.1.50" <<<"$ssh_calls")" ]
  grep -qF -- "HostKeyAlias=vm1" <<<"$ssh_calls"
  grep -qF -- "hostnamectl set-hostname web-01" <<<"$ssh_calls"
  # Algún comando remoto reemplaza o agrega la línea 127.0.1.1 con el nuevo nombre en /etc/hosts.
  grep -F -- "/etc/hosts" <<<"$ssh_calls" | grep -F -- "127.0.1.1" | grep -qF -- "web-01"
  # El hostname se aplica antes de tocar /etc/hosts.
  hostname_line="$(grep -nF -- "hostnamectl set-hostname" "$CALLS" | head -n1 | cut -d: -f1)"
  hosts_line="$(grep -nF -- "/etc/hosts" "$CALLS" | head -n1 | cut -d: -f1)"
  [ "$hostname_line" -le "$hosts_line" ]
}

@test "AC-18 set-hostname rechaza Web_01" {
  run "$SCRIPT" vm1 Web_01

  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

@test "AC-18 set-hostname rechaza un nombre que empieza con guion" {
  run "$SCRIPT" vm1 -web

  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

@test "AC-18 set-hostname rechaza un nombre de 64 caracteres" {
  run "$SCRIPT" vm1 "$(printf 'a%.0s' {1..64})"

  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

@test "AC-43 set-hostname sale con 7 si la VM no tiene IP y no ejecuta ssh" {
  setup_stubs
  stub_vm_ip vm1 -

  run --separate-stderr "$SCRIPT" vm1 web-01

  [ "$status" -eq 7 ]
  [[ "$stderr" == *"La VM no tiene dirección IP; verifique que esté encendida"* ]]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

@test "AC-48 set-hostname rechaza una cantidad de argumentos incorrecta" {
  run "$SCRIPT" vm1
  [ "$status" -eq 2 ]
  run "$SCRIPT" vm1 web-01 extra
  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

@test "AC-37 set-hostname con --jump entra al servidor por el balanceador" {
  stub_jump_topology lb1 running web-a running

  run --separate-stderr "$SCRIPT" --jump lb1 web-a web-01

  [ "$status" -eq 0 ]
  assert_routed_ssh lb1 web-a
}

@test "AC-37 set-hostname sale con 2 si --jump no trae un balanceador válido y no llama a nada" {
  stub_jump_topology lb1 running web-a running

  run "$SCRIPT" --jump
  [ "$status" -eq 2 ]
  run "$SCRIPT" --jump Lb1 web-a web-01
  [ "$status" -eq 2 ]
  run "$SCRIPT" --jump "" web-a web-01
  [ "$status" -eq 2 ]
  run "$SCRIPT" --jump 'lb1;x' web-a web-01
  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

@test "AC-29 set-hostname con --jump sale con 12 si el balanceador está apagado y no llama a ssh" {
  stub_jump_topology lb1 poweroff web-a running

  run --separate-stderr "$SCRIPT" --jump lb1 web-a web-01

  [ "$status" -eq 12 ]
  [[ "$stderr" == *"El balanceador lb1 está apagado; enciéndalo para llegar al servidor"* ]]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

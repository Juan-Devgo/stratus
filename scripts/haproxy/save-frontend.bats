#!/usr/bin/env bats
# Pruebas de save-frontend.sh con stubs de VBoxManage y ssh: no tocan VirtualBox ni VMs.

load test_helper

# "run --separate-stderr" necesita bats 1.5 o superior.
bats_require_minimum_version 1.5.0

setup() {
  setup_haproxy_stubs
  SCRIPT="$HX/save-frontend.sh"
  CALLS="$BATS_TEST_TMPDIR/calls.log"
  REMOTE="$BATS_TEST_TMPDIR/remote.log"
  STDIN_LOG="$BATS_TEST_TMPDIR/ssh-stdin.log"
  export APP_SSH_KEY_PATH="$BATS_TEST_TMPDIR/stratus_backend"
  : >"$APP_SSH_KEY_PATH"
  stub_vm_nics lb1 running 080027CCCCCC 192.168.1.60 080027DDDDDD -
}

@test "AC-38 save-frontend.sh tiene cabecera, códigos de salida, modo estricto y secciones" {
  assert_script_structure "$HAPROXY_DIR/save-frontend.sh"
}

@test "AC-37 save-frontend sale con 2 ante argumentos inválidos y no llama a nada" {
  specs11=()
  for i in {1..11}; do specs11+=("server$i,vm$i,80,10.10.1.$((10 + i)),on"); done
  long_name="$(printf 'a%.0s' {1..33})"
  bad=(
    ""
    "lb1"
    "Lb1 8080"
    "$long_name 8080"
    "lb1 22"
    "lb1 0"
    "lb1 65536"
    "lb1 80a"
    "lb1 8080 server1,web-a,80"
    "lb1 8080 server1,web-a,80,10.10.1.11,maybe"
    "lb1 8080 server1,web-a,0,10.10.1.11,on"
    "lb1 8080 server1,web-a,80,10.10.1.11,on server1,web-b,80,10.10.1.12,on"
  )
  for args in "${bad[@]}"; do
    # shellcheck disable=SC2086 # las palabras se separan a propósito; ninguna lleva espacios
    run "$SCRIPT" $args
    [ "$status" -eq 2 ]
  done
  run "$SCRIPT" lb1 8080 "${specs11[@]}"
  [ "$status" -eq 2 ]
  APP_SSH_KEY_PATH="" run "$SCRIPT" lb1 8080
  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

@test "AC-11 save-frontend escribe el nuevo puerto con los servidores guardados, lo revisa y recarga" {
  run --separate-stderr "$SCRIPT" lb1 8080 server1,web-a,80,10.10.1.11,on server2,web-b,8000,10.10.1.12,off

  [ "$status" -eq 0 ]
  [ "$output" = "Frontend de lb1 en el puerto 8080" ]
  grep -qxF -- "  bind :8080" "$STDIN_LOG"
  ! grep -qxF -- "  bind :80" "$STDIN_LOG" || return 1
  server_lines="$(grep -E '^\s*server ' "$STDIN_LOG")"
  [ "$server_lines" = $'  server server1 10.10.1.11:80 check\n  server server2 10.10.1.12:8000 check disabled' ]
  haproxy_check="$(grep -nF -- "haproxy -c -f /etc/haproxy/haproxy.cfg.new" "$REMOTE" | cut -d: -f1)"
  reload="$(grep -nF -- "systemctl reload haproxy" "$REMOTE" | cut -d: -f1)"
  [ -n "$haproxy_check" ]
  [ -n "$reload" ]
  [ "$haproxy_check" -lt "$reload" ]
  # Se verifica que HAProxy escuche en el puerto nuevo.
  grep -qF -- "ss -Hltnp sport = :8080" "$REMOTE"
}

@test "AC-12 save-frontend sin servidores deja el backend vacío" {
  run "$SCRIPT" lb1 80

  [ "$status" -eq 0 ]
  grep -qxF -- "  bind :80" "$STDIN_LOG"
  ! grep -qE '^\s*server ' "$STDIN_LOG" || return 1
}

@test "AC-49 save-frontend permite el puerto en ufw si ufw está instalado" {
  UFW=yes run "$SCRIPT" lb1 8080

  [ "$status" -eq 0 ]
  grep -qF -- "ufw allow 8080/tcp" "$REMOTE"
  # La regla va antes de cambiar el haproxy.cfg: el puerto nuevo ya está permitido al recargar.
  [ "$(grep -nF -- "ufw allow 8080/tcp" "$REMOTE" | cut -d: -f1)" -lt "$(grep -nF -- "systemctl reload haproxy" "$REMOTE" | cut -d: -f1)" ]
}

@test "AC-49 save-frontend no llama a ufw si no está instalado" {
  UFW=no run "$SCRIPT" lb1 8080

  [ "$status" -eq 0 ]
  ! grep -qF -- "ufw allow" "$REMOTE" || return 1
  grep -qF -- "command -v ufw" "$REMOTE"
}

@test "AC-45 save-frontend sale con 11 si HAProxy no puede enlazar el puerto y restaura la configuración" {
  SS_OUT='LISTEN 0 511 0.0.0.0:8080 0.0.0.0:* users:(("nginx",pid=7,fd=6))' \
    run --separate-stderr "$SCRIPT" lb1 8080

  [ "$status" -eq 11 ]
  [[ "$stderr" == *"No se pudo recargar HAProxy; se mantuvo la configuración anterior"* ]]
  grep -qF -- "mv /etc/haproxy/haproxy.cfg.bak /etc/haproxy/haproxy.cfg" "$REMOTE"
  [ -z "$output" ]
}

@test "AC-24 save-frontend sale con 10 si HAProxy rechaza la configuración" {
  HAPROXY_CHECK=fail run --separate-stderr "$SCRIPT" lb1 8080

  [ "$status" -eq 10 ]
  ! grep -qF -- "systemctl reload" "$REMOTE" || return 1
  [ -z "$output" ]
}

@test "AC-40 save-frontend sale con 12 si el balanceador está apagado y no llama a ssh" {
  stub_vm_nics lb2 poweroff 080027EEEEEE 192.168.1.61 080027FFFFFF -

  run --separate-stderr "$SCRIPT" lb2 8080

  [ "$status" -eq 12 ]
  [[ "$stderr" == *"El balanceador lb2 está apagado; enciéndalo para llegar al servidor"* ]]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

@test "AC-49 save-frontend sale con 5 si ufw falla y no cambia haproxy.cfg" {
  UFW=yes FAIL_REMOTE='ufw allow' run --separate-stderr "$SCRIPT" lb1 8080

  [ "$status" -eq 5 ]
  ! grep -qF -- "haproxy.cfg.new" "$REMOTE" || return 1
}

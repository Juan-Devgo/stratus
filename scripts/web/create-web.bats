#!/usr/bin/env bats
# Pruebas de create-web.sh (005) con stubs de VBoxManage, ssh, sleep y de los scripts de
# prepare (create-vm.sh y delete-vm.sh): no tocan VirtualBox ni VMs.

load test_helper

bats_require_minimum_version 1.5.0

setup() {
  setup_web_stubs
  setup_guest_password
  CALLS="$BATS_TEST_TMPDIR/calls.log"
  REMOTE="$BATS_TEST_TMPDIR/remote.log"
  export APP_SSH_KEY_PATH="$BATS_TEST_TMPDIR/stratus_backend"
  : >"$APP_SSH_KEY_PATH"
  DISK="$BATS_TEST_TMPDIR/nginx.vdi"
  : >"$DISK"
  SCRIPT="$WX/create-web.sh"
  stub_vm_ip web-a 192.168.1.40
}

# stdin_of <ERE>: entrada estándar de la primera llamada a ssh cuyo comando coincide.
stdin_of() {
  local n
  n="$(grep -nE -m1 -- "$1" "$REMOTE" | cut -d: -f1)"
  cat "$BATS_TEST_TMPDIR/stdin/$n"
}

@test "AC-56 create-web.sh tiene la estructura exigida" {
  assert_script_structure "$WEB_DIR/create-web.sh"
}

@test "AC-1 create-web crea la VM con el disco de Nginx, escribe el sitio y habilita nginx" {
  run --separate-stderr "$SCRIPT" web-a 1024 1 nginx "$DISK" eth0 stratus.lan

  [ "$status" -eq 0 ]
  grep -qxF "create-vm.sh web-a 1024 1 $DISK eth0" "$CALLS"
  [[ "$(stdin_of 'sites-available/stratus$')" == *"server_name web-a.stratus.lan;"* ]]
  grep -qx 'ln -sf /etc/nginx/sites-available/stratus /etc/nginx/sites-enabled/stratus' "$REMOTE"
  grep -qx 'rm -f /etc/nginx/sites-enabled/default' "$REMOTE"
  grep -qx 'nginx -t' "$REMOTE"
  grep -qx 'systemctl enable nginx' "$REMOTE"
  grep -qx 'systemctl reload-or-restart nginx' "$REMOTE"
  grep -qx 'systemctl is-active nginx' "$REMOTE"
  [ "${lines[0]}" = "Servidor web web-a (Nginx) creado; IP 192.168.1.40" ]
  [ "${lines[-1]}" = "ip=192.168.1.40" ]
  ! grep -q '^delete-vm.sh' "$CALLS" || return 1
}

@test "AC-4 create-web de Apache reemplaza 000-default.conf y revisa con apache2ctl" {
  run --separate-stderr "$SCRIPT" web-a 1024 1 apache "$DISK" eth0 stratus.lan

  [ "$status" -eq 0 ]
  site="$(stdin_of 'sites-available/000-default.conf$')"
  [[ "$site" == *"ServerName web-a.stratus.lan"* ]]
  [[ "$site" == *"DocumentRoot /var/www/html"* ]]
  grep -qx 'apache2ctl configtest' "$REMOTE"
  grep -qx 'systemctl enable apache2' "$REMOTE"
  grep -qx 'systemctl is-active apache2' "$REMOTE"
  ! grep -q 'nginx' "$REMOTE" || return 1
  [ "${lines[0]}" = "Servidor web web-a (Apache) creado; IP 192.168.1.40" ]
}

@test "AC-4 create-web limpia con delete-vm.sh si la configuración del sitio es inválida" {
  export CONFIGTEST=fail

  run --separate-stderr "$SCRIPT" web-a 1024 1 apache "$DISK" eth0 stratus.lan

  [ "$status" -eq 5 ]
  [[ "$stderr" == *"error de sintaxis"* ]]
  grep -qx 'delete-vm.sh web-a' "$CALLS"
  ! grep -q 'systemctl enable' "$REMOTE" || return 1
}

@test "AC-4 create-web limpia y sale con 5 si el servicio no queda activo" {
  export ACTIVE_STATE=failed

  run --separate-stderr "$SCRIPT" web-a 1024 1 nginx "$DISK" eth0 stratus.lan

  [ "$status" -eq 5 ]
  grep -qx 'delete-vm.sh web-a' "$CALLS"
}

@test "AC-4 create-web sale con 9 si también falla la limpieza" {
  export CONFIGTEST=fail RC_DELETE_VM=1

  run --separate-stderr "$SCRIPT" web-a 1024 1 nginx "$DISK" eth0 stratus.lan

  [ "$status" -eq 9 ]
}

@test "AC-3 create-web sale con 6 sin el disco base de Apache y no llama a create-vm" {
  run --separate-stderr "$SCRIPT" web-a 1024 1 apache "$BATS_TEST_TMPDIR/no-existe.vdi" eth0 stratus.lan

  [ "$status" -eq 6 ]
  [ "$stderr" = "Disco base de Apache no encontrado" ]
  ! grep -q 'create-vm.sh' "$CALLS" || return 1
  ! grep -q '^delete-vm.sh' "$CALLS" || return 1

  run --separate-stderr "$SCRIPT" web-a 1024 1 nginx "$BATS_TEST_TMPDIR/no-existe.vdi" eth0 stratus.lan
  [ "$status" -eq 6 ]
  [ "$stderr" = "Disco base de Nginx no encontrado" ]
}

@test "AC-3 create-web sale con 6 si la VM ya está registrada y no borra la ajena" {
  stub_command VBoxManage '[[ "$1 ${2:-}" == "list vms" ]] && echo "\"web-a\" {11111111-2222-3333-4444-555555555555}"'

  run --separate-stderr "$SCRIPT" web-a 1024 1 nginx "$DISK" eth0 stratus.lan

  [ "$status" -eq 6 ]
  ! grep -q 'create-vm.sh' "$CALLS" || return 1
  ! grep -q '^delete-vm.sh' "$CALLS" || return 1
}

@test "AC-1 create-web conserva el código de create-vm.sh y limpia" {
  export RC_CREATE_VM=5

  run --separate-stderr "$SCRIPT" web-a 1024 1 nginx "$DISK" eth0 stratus.lan

  [ "$status" -eq 5 ]
  grep -qx 'delete-vm.sh web-a' "$CALLS"
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

@test "AC-55 create-web sale con 2 ante argumentos inválidos sin llamar a nada" {
  local -a bad
  for args in \
    "Web1 1024 1 nginx $DISK eth0 stratus.lan" \
    "web-a 256 1 nginx $DISK eth0 stratus.lan" \
    "web-a 1024 5 nginx $DISK eth0 stratus.lan" \
    "web-a 1024 1 iis $DISK eth0 stratus.lan" \
    "web-a 1024 1 nginx disco.vdi eth0 stratus.lan" \
    "web-a 1024 1 nginx $DISK et;h0 stratus.lan" \
    "web-a 1024 1 nginx $DISK eth0 Stratus_Lan" \
    "web-a 1024 1 nginx $DISK eth0"; do
    read -ra bad <<<"$args"
    run "$SCRIPT" "${bad[@]}"
    [ "$status" -eq 2 ]
  done
  [ ! -s "$CALLS" ]
}

@test "AC-55 create-web sale con 2 sin APP_GUEST_PASSWORD_FILE o sin APP_SSH_KEY_PATH" {
  run env -u APP_GUEST_PASSWORD_FILE "$SCRIPT" web-a 1024 1 nginx "$DISK" eth0 stratus.lan
  [ "$status" -eq 2 ]
  run env -u APP_SSH_KEY_PATH "$SCRIPT" web-a 1024 1 nginx "$DISK" eth0 stratus.lan
  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

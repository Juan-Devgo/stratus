#!/usr/bin/env bats
# Pruebas de create-lb.sh con stubs de VBoxManage, ssh y de los scripts de ../prepare: no tocan
# VirtualBox ni VMs. El script se ejecuta desde la copia "$HX" (ver test_helper.bash).

load test_helper

# "run --separate-stderr" necesita bats 1.5 o superior.
bats_require_minimum_version 1.5.0

setup() {
  setup_haproxy_stubs
  SCRIPT="$HX/create-lb.sh"
  CALLS="$BATS_TEST_TMPDIR/calls.log"
  REMOTE="$BATS_TEST_TMPDIR/remote.log"
  STDIN_LOG="$BATS_TEST_TMPDIR/ssh-stdin.log"
  BASE="$BATS_TEST_TMPDIR/haproxy-base.vdi"
  printf 'BASE-DISK-CONTENT' >"$BASE"
  setup_guest_password
  export APP_SSH_KEY_PATH="$BATS_TEST_TMPDIR/stratus_backend"
  : >"$APP_SSH_KEY_PATH"
  printf 'ssh-ed25519 AAAAC3BACKENDKEYBLOB stratus-backend\n' >"$APP_SSH_KEY_PATH.pub"
  # lb1 queda encendido con su IPv4 en puente 192.168.1.60 y la red interna en el adaptador 2.
  stub_vm_nics lb1 running 080027CCCCCC 192.168.1.60 080027DDDDDD -
}

# line_of <texto fijo> [n]: número de la n-ésima línea de calls.log que lo contiene (vacío si no hay).
line_of() {
  grep -nF -- "$1" "$CALLS" | sed -n "${2:-1}p" | cut -d: -f1 || true
}

@test "AC-38 create-lb.sh tiene cabecera, códigos de salida, modo estricto y secciones" {
  assert_script_structure "$HAPROXY_DIR/create-lb.sh"
}

@test "AC-37 create-lb sale con 2 ante argumentos inválidos y no llama a VBoxManage, ssh ni create-vm" {
  local long_name
  long_name="$(printf 'a%.0s' {1..33})"
  for args in \
    "" \
    "lb1 1024 1 $BASE eth0" \
    "lb1 1024 1 $BASE eth0 - extra" \
    "Lb1 1024 1 $BASE eth0 -" \
    "$long_name 1024 1 $BASE eth0 -" \
    "lb1 511 1 $BASE eth0 -" \
    "lb1 8193 1 $BASE eth0 -" \
    "lb1 1024 0 $BASE eth0 -" \
    "lb1 1024 5 $BASE eth0 -" \
    "lb1 1024 1 base.vdi eth0 -" \
    "lb1 1024 1 $BASE eth0;x -" \
    "lb1 1024 1 $BASE eth0 0" \
    "lb1 1024 1 $BASE eth0 255" \
    "lb1 1024 1 $BASE eth0 1,,3" \
    "lb1 1024 1 $BASE eth0 a"; do
    # shellcheck disable=SC2086 # las palabras se separan a propósito; ninguna lleva espacios
    run "$SCRIPT" $args
    [ "$status" -eq 2 ]
  done
  # Sin la contraseña de la imagen o sin la llave pública del backend tampoco se ejecuta nada.
  APP_GUEST_PASSWORD_FILE="$BATS_TEST_TMPDIR/no-existe" run "$SCRIPT" lb1 1024 1 "$BASE" eth0 -
  [ "$status" -eq 2 ]
  rm -f "$APP_SSH_KEY_PATH.pub"
  run "$SCRIPT" lb1 1024 1 "$BASE" eth0 -
  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

@test "AC-1 create-lb crea la red con su DHCP y luego llama a create-vm con la red interna" {
  run --separate-stderr "$SCRIPT" lb1 1024 1 "$BASE" eth0 -

  [ "$status" -eq 0 ]
  dhcp="$(line_of "dhcpserver add --network=stratus-lb1")"
  vm="$(line_of "create-vm.sh lb1 1024 1 $BASE eth0 stratus-lb1")"
  [ -n "$dhcp" ]
  [ -n "$vm" ]
  [ "$dhcp" -lt "$vm" ]
  [ "$(sed -n 1p <<<"$output")" = "Balanceador lb1 creado y encendido; IP 192.168.1.60; subred 10.10.1.0/24" ]
  [ "$(tail -n 1 <<<"$output")" = "subnet=1" ]
  [ "$(wc -l <<<"$output")" -eq 2 ]
  # La contraseña de la imagen base nunca llega a argv ni a la salida.
  ! grep -qF -- "$GUEST_PASSWORD" "$CALLS" || return 1
}

@test "AC-4 create-lb configura el servidor DHCP de la subred 10.10.N.0/24 con el rango 10 a 200" {
  run "$SCRIPT" lb1 1024 1 "$BASE" eth0 -

  [ "$status" -eq 0 ]
  grep -qF -- "dhcpserver add --network=stratus-lb1 --server-ip=10.10.1.2 --netmask=255.255.255.0 --lower-ip=10.10.1.10 --upper-ip=10.10.1.200 --enable" "$CALLS"
}

@test "AC-3 create-lb sale con 6 sin el disco base de HAProxy y no crea ni red ni VM" {
  run --separate-stderr "$SCRIPT" lb1 1024 1 "$BATS_TEST_TMPDIR/no-existe.vdi" eth0 -

  [ "$status" -eq 6 ]
  [[ "$stderr" == *"Disco base de HAProxy no encontrado"* ]]
  ! grep -qF -- "dhcpserver" "$CALLS" || return 1
  ! grep -qF -- "create-vm.sh" "$CALLS" || return 1
  ! grep -qF -- "delete-vm.sh" "$CALLS" || return 1
}

@test "AC-3 create-lb sale con 6 si ya hay una VM registrada con ese nombre y no la borra" {
  stub_command VBoxManage 'if [[ "$1 ${2:-}" == "list vms" ]]; then printf "\"lb1\" {11111111-1111-1111-1111-111111111111}\n"; exit 0; fi'

  run --separate-stderr "$SCRIPT" lb1 1024 1 "$BASE" eth0 -

  [ "$status" -eq 6 ]
  [[ "$stderr" == *"VM ya registrada en VirtualBox"* ]]
  ! grep -qF -- "dhcpserver" "$CALLS" || return 1
  ! grep -qF -- "delete-vm.sh" "$CALLS" || return 1
}

@test "AC-5 create-lb elige la subred libre más baja que no esté reservada" {
  run --separate-stderr "$SCRIPT" lb1 1024 1 "$BASE" eth0 1,3

  [ "$status" -eq 0 ]
  [[ "$output" == *"subred 10.10.2.0/24"* ]]
  [ "$(tail -n 1 <<<"$output")" = "subnet=2" ]
  grep -qF -- "dhcpserver add --network=stratus-lb1 --server-ip=10.10.2.2 --netmask=255.255.255.0 --lower-ip=10.10.2.10 --upper-ip=10.10.2.200 --enable" "$CALLS"
  grep -qF -- "address 10.10.2.1/24" "$STDIN_LOG"
}

@test "AC-50 create-lb evita las subredes de servidores DHCP de VirtualBox que la app no conoce" {
  DHCPSERVERS=$'NetworkName:    HostInterfaceNetworking-vboxnet0\nDhcpd IP:       10.10.1.2\nLowerIPAddress: 10.10.1.10\nUpperIPAddress: 10.10.1.200\nNetworkMask:    255.255.255.0\nEnabled:        Yes\n' \
    run --separate-stderr "$SCRIPT" lb1 1024 1 "$BASE" eth0 -

  [ "$status" -eq 0 ]
  [ "$(tail -n 1 <<<"$output")" = "subnet=2" ]
}

@test "AC-50 create-lb sale con 13 si stratus-<lb> ya tiene un servidor DHCP y no crea nada" {
  DHCPSERVERS=$'NetworkName:    stratus-lb1\nDhcpd IP:       10.10.7.2\nEnabled:        Yes\n' \
    run --separate-stderr "$SCRIPT" lb1 1024 1 "$BASE" eth0 -

  [ "$status" -eq 13 ]
  [[ "$stderr" == *"Red interna ya registrada en VirtualBox"* ]]
  ! grep -qF -- "dhcpserver add" "$CALLS" || return 1
  ! grep -qF -- "create-vm.sh" "$CALLS" || return 1
  ! grep -qF -- "dhcpserver remove" "$CALLS" || return 1
}

@test "AC-50 create-lb sale con 8 si no queda ninguna subred libre" {
  reserved="$(seq -s, 1 254)"

  run --separate-stderr "$SCRIPT" lb1 1024 1 "$BASE" eth0 "$reserved"

  [ "$status" -eq 8 ]
  [[ "$stderr" == *"No hay subredes internas libres"* ]]
  ! grep -qF -- "dhcpserver add" "$CALLS" || return 1
  ! grep -qF -- "create-vm.sh" "$CALLS" || return 1
}

@test "AC-6 create-lb configura la IP fija, jump, sshd y HAProxy por SSH" {
  run "$SCRIPT" lb1 1024 1 "$BASE" eth0 -

  [ "$status" -eq 0 ]
  # IP fija 10.10.1.1/24 en la interfaz del adaptador 2, buscada por su MAC.
  grep -qF -- "/sys/class/net" "$REMOTE"
  grep -qF -- "08:00:27:dd:dd:dd" "$REMOTE"
  grep -qF -- "auto enp0s8" "$STDIN_LOG"
  grep -qF -- "address 10.10.1.1/24" "$STDIN_LOG"
  grep -qF -- "/etc/network/interfaces.d/stratus-internal" "$REMOTE"
  grep -qE -- "ifup" "$REMOTE"
  # Cuenta jump de sistema, sin contraseña ni shell.
  grep -qF -- "useradd --system --create-home --home-dir /var/lib/jump --shell /usr/sbin/nologin jump" "$REMOTE"
  # Su authorized_keys lleva solo la llave del backend con los límites.
  grep -qxF -- "restrict,port-forwarding ssh-ed25519 AAAAC3BACKENDKEYBLOB stratus-backend" "$STDIN_LOG"
  # sshd revisado, HAProxy revisado y habilitado.
  grep -qF -- "sshd -t" "$REMOTE"
  grep -qF -- "haproxy -c -f /etc/haproxy/haproxy.cfg.new" "$REMOTE"
  grep -qF -- "systemctl enable --now haproxy" "$REMOTE"
  # El haproxy.cfg inicial: puerto 80 y ningún servidor.
  grep -qF -- "bind :80" "$STDIN_LOG"
  ! grep -qE '^\s*server ' "$STDIN_LOG" || return 1
  # Orden: IP fija, jump, sshd, haproxy.
  ifup_l="$(grep -nF -- "ifup" "$REMOTE" | head -1 | cut -d: -f1)"
  useradd_l="$(grep -nF -- "useradd" "$REMOTE" | head -1 | cut -d: -f1)"
  sshd_l="$(grep -nF -- "sshd -t" "$REMOTE" | head -1 | cut -d: -f1)"
  haproxy_l="$(grep -nF -- "haproxy -c" "$REMOTE" | head -1 | cut -d: -f1)"
  [ "$ifup_l" -lt "$useradd_l" ]
  [ "$useradd_l" -lt "$sshd_l" ]
  [ "$sshd_l" -lt "$haproxy_l" ]
}

@test "AC-28 create-lb restringe jump en sshd: solo reenvío local al puerto 22 de 10.10.N.10-200, sin shell" {
  run "$SCRIPT" lb1 1024 1 "$BASE" eth0 -

  [ "$status" -eq 0 ]
  grep -qxF -- "Match User jump" "$STDIN_LOG"
  grep -qxF -- "  AllowTcpForwarding local" "$STDIN_LOG"
  grep -qxF -- "  PermitTTY no" "$STDIN_LOG"
  grep -qxF -- "  X11Forwarding no" "$STDIN_LOG"
  grep -qxF -- "  AllowAgentForwarding no" "$STDIN_LOG"
  grep -qxF -- "  ForceCommand /usr/sbin/nologin" "$STDIN_LOG"
  permit="$(grep -E '^  PermitOpen ' "$STDIN_LOG")"
  read -ra items <<<"${permit#  PermitOpen }"
  [ "${#items[@]}" -eq 191 ]
  [ "${items[0]}" = "10.10.1.10:22" ]
  [ "${items[190]}" = "10.10.1.200:22" ]
  [ "$(printf '%s\n' "${items[@]}" | sort -u | wc -l)" -eq 191 ]
  # Cada elemento es 10.10.1.<10..200>:22
  ! printf '%s\n' "${items[@]}" | grep -vE '^10\.10\.1\.([0-9]{2}|1[0-9]{2}|200):22$' | grep -q . || return 1
}

@test "AC-44 create-lb crea jump como cuenta de sistema" {
  run "$SCRIPT" lb1 1024 1 "$BASE" eth0 -

  [ "$status" -eq 0 ]
  useradd_line="$(grep -F "useradd" "$REMOTE")"
  [[ "$useradd_line" == *"useradd --system"* ]]
  [[ "$useradd_line" == *"--shell /usr/sbin/nologin"* ]]
  # Nunca se le pone contraseña.
  ! grep -qE 'chpasswd|passwd jump|usermod -p' "$REMOTE" || return 1
}

@test "AC-7 create-lb limpia si falla el paso remoto: borra la VM y la red, sale con 5 y no toca el disco base" {
  FAIL_REMOTE='useradd' run --separate-stderr "$SCRIPT" lb1 1024 1 "$BASE" eth0 -

  [ "$status" -eq 5 ]
  [ -z "$output" ]
  vm="$(line_of "delete-vm.sh lb1")"
  net="$(line_of "dhcpserver remove --network=stratus-lb1")"
  [ -n "$vm" ]
  [ -n "$net" ]
  [ "$vm" -lt "$net" ]
  ! grep -qE 'modifymedium|closemedium' "$CALLS" || return 1
  [ "$(cat "$BASE")" = "BASE-DISK-CONTENT" ]
}

@test "AC-7 create-lb limpia si falla create-vm y conserva su código de salida" {
  RC_CREATE_VM=5 run --separate-stderr "$SCRIPT" lb1 1024 1 "$BASE" eth0 -

  [ "$status" -eq 5 ]
  grep -qF -- "delete-vm.sh lb1" "$CALLS"
  grep -qF -- "dhcpserver remove --network=stratus-lb1" "$CALLS"
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

@test "AC-7 create-lb limpia si falla la revisión de HAProxy" {
  HAPROXY_CHECK=fail run --separate-stderr "$SCRIPT" lb1 1024 1 "$BASE" eth0 -

  [ "$status" -eq 5 ]
  grep -qF -- "delete-vm.sh lb1" "$CALLS"
  grep -qF -- "dhcpserver remove --network=stratus-lb1" "$CALLS"
}

@test "AC-7 create-lb sale con 9 si falla el paso y también la limpieza" {
  FAIL_REMOTE='useradd' RC_DELETE_VM=4 run --separate-stderr "$SCRIPT" lb1 1024 1 "$BASE" eth0 -

  [ "$status" -eq 9 ]
  [[ "$stderr" == *"limpieza"* ]]
}

@test "AC-7 create-lb no deja nada si falla el DHCP: sale con 4 sin llamar a create-vm" {
  stub_command VBoxManage 'if [[ "$1 ${2:-}" == "dhcpserver add" ]]; then echo "error de dhcp" >&2; exit 1; fi'

  run --separate-stderr "$SCRIPT" lb1 1024 1 "$BASE" eth0 -

  [ "$status" -eq 4 ]
  ! grep -qF -- "create-vm.sh" "$CALLS" || return 1
}

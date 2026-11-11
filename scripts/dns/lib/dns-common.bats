#!/usr/bin/env bats
# Pruebas de lib/dns-common.sh (005): validadores, ip_in_use, render_options, render_zone,
# apply_zone y wait_ips, con stubs de VBoxManage, ssh, arping, ping y sleep.

load ../test_helper

bats_require_minimum_version 1.5.0

setup() {
  setup_dns_stubs
  CALLS="$BATS_TEST_TMPDIR/calls.log"
  REMOTE="$BATS_TEST_TMPDIR/remote.log"
  export APP_SSH_KEY_PATH="$BATS_TEST_TMPDIR/stratus_backend"
  : >"$APP_SSH_KEY_PATH"
  # shellcheck source=../../prepare/lib/vm-exec.sh
  source "$DX/../prepare/lib/vm-exec.sh"
  # shellcheck source=dns-common.sh
  source "$DX/lib/dns-common.sh"
}

@test "AC-56 dns-common.sh tiene la estructura exigida" {
  assert_script_structure "$DNS_DIR/lib/dns-common.sh"
}

# --- Validadores (REQ-15, REQ-22) ---

@test "AC-24 validate_cidr acepta IPv4 con prefijo 8-30 y rechaza el resto" {
  local ok bad
  for ok in 192.168.1.53/24 10.0.0.53/8 192.168.1.53/30; do validate_cidr "$ok"; done
  for bad in "" 192.168.1.53 192.168.1.53/31 10.0.0.53/7 192.168.1.256/24 192.168.1/24 \
    192.168.1.0/24 192.168.1.255/24 fd00::53/64 192.168.1.53/024; do
    run --separate-stderr validate_cidr "$bad"
    [ "$status" -eq 1 ]
  done
}

@test "AC-24 validate_gateway exige una IPv4 de la subred distinta de la red, la difusión y la IP" {
  validate_gateway 192.168.1.53/24 192.168.1.1
  validate_gateway 192.168.1.53/30 192.168.1.54
  local bad
  for bad in 10.0.0.1 192.168.1.0 192.168.1.255 192.168.1.53 "" 192.168.1; do
    run validate_gateway 192.168.1.53/24 "$bad"
    [ "$status" -eq 1 ]
  done
}

@test "AC-24 validate_domain rechaza Stratus_Lan y 5 etiquetas" {
  validate_domain stratus.lan
  run validate_domain Stratus_Lan
  [ "$status" -eq 1 ]
  run validate_domain a.b.c.d.lan
  [ "$status" -eq 1 ]
}

@test "AC-24 validate_forwarders acepta 1 a 3 IPv4 separadas por comas" {
  validate_forwarders 1.1.1.1
  validate_forwarders 1.1.1.1,8.8.8.8,9.9.9.9
  local bad
  for bad in "" 1.1.1 1.1.1.1,8.8.8.8,9.9.9.9,1.0.0.1 "1.1.1.1, 8.8.8.8" 1.1.1.1, 300.1.1.1; do
    run validate_forwarders "$bad"
    [ "$status" -eq 1 ]
  done
}

@test "AC-24 validate_ipv4 acepta solo IPv4" {
  validate_ipv4 192.168.1.53
  run validate_ipv4 192.168.1.53/24
  [ "$status" -eq 1 ]
}

@test "REQ-22 validate_records: <vm>,<ipv4>, nombres únicos y sin el servidor DNS" {
  validate_records dns1
  validate_records dns1 web-a,192.168.1.40 lb1,192.168.1.60
  run --separate-stderr validate_records dns1 web-a
  [ "$status" -eq 1 ]
  run validate_records dns1 Web-A,192.168.1.40
  [ "$status" -eq 1 ]
  run validate_records dns1 web-a,192.168.1
  [ "$status" -eq 1 ]
  run validate_records dns1 web-a,192.168.1.40 web-a,192.168.1.41
  [ "$status" -eq 1 ]
  run validate_records dns1 dns1,192.168.1.53
  [ "$status" -eq 1 ]
  local -a many=()
  for i in $(seq 1 201); do many+=("vm$i,10.0.0.1"); done
  run validate_records dns1 "${many[@]}"
  [ "$status" -eq 1 ]
}

@test "AC-59 ip_in_subnet y validate_lan_records" {
  ip_in_subnet 192.168.1.53/24 192.168.1.40
  run ip_in_subnet 192.168.1.53/24 10.0.2.15
  [ "$status" -eq 1 ]
  [ -z "$output" ]
  run ip_in_subnet 192.168.1.53/24 169.254.3.7
  [ "$status" -eq 1 ]
  ip_in_subnet 10.0.0.53/24 10.0.0.40
  # Un prefijo que no es /24 usa la máscara completa
  ip_in_subnet 192.168.1.53/22 192.168.2.9
  run ip_in_subnet 192.168.1.53/22 192.168.4.9
  [ "$status" -eq 1 ]

  validate_lan_records 192.168.1.53/24
  validate_lan_records 192.168.1.53/24 web-a,192.168.1.40 vm1,192.168.1.41
  validate_lan_records 10.0.0.53/24 vm1,10.0.0.40
  run --separate-stderr validate_lan_records 192.168.1.53/24 web-a,192.168.1.40 vm1,10.0.2.15
  [ "$status" -eq 1 ]
  [[ "$stderr" == *"Registro inválido: la dirección no está en la LAN"* ]]
  run --separate-stderr validate_lan_records 192.168.1.53/24 vm1,169.254.3.7
  [ "$status" -eq 1 ]
  [[ "$stderr" == *"Registro inválido: la dirección no está en la LAN"* ]]
}

@test "REQ-27 validate_wait_vms acepta - o nombres separados por comas" {
  validate_wait_vms -
  validate_wait_vms vm1
  validate_wait_vms vm1,web-a
  local bad
  for bad in "" vm1, ,vm1 "vm1 web-a" VM1 vm1,,web-a; do
    run validate_wait_vms "$bad"
    [ "$status" -eq 1 ]
  done
}

# --- ip_in_use (REQ-15, AC-25) ---

@test "AC-25 ip_in_use da 13 si responde ARP aunque ICMP esté bloqueado" {
  ARPING=reply PING=none run --separate-stderr ip_in_use enp3s0 192.168.1.53
  [ "$status" -eq 13 ]
  [ "$stderr" = "La IP del servidor DNS ya está en uso" ]
  grep -qx 'arping -c 1 -w 1 -I enp3s0 192.168.1.53' "$CALLS"
}

@test "AC-25 ip_in_use da 13 si solo responde ICMP" {
  ARPING=none PING=reply run --separate-stderr ip_in_use enp3s0 192.168.1.53
  [ "$status" -eq 13 ]
  grep -qx 'ping -c 1 -W 1 192.168.1.53' "$CALLS"
}

@test "AC-25 ip_in_use da 0 si nadie responde" {
  ARPING=none PING=none run --separate-stderr ip_in_use enp3s0 192.168.1.53
  [ "$status" -eq 0 ]
  grep -q '^arping ' "$CALLS"
  grep -q '^ping ' "$CALLS"
}

@test "AC-25 ip_in_use da 4 si arping no está instalado" {
  hide_arping
  run --separate-stderr ip_in_use enp3s0 192.168.1.53
  [ "$status" -eq 4 ]
  [ "$stderr" = "arping no está instalado en el host" ]
  ! grep -q '^ping ' "$CALLS" || return 1
}

# --- render_options y render_zone (REQ-16, REQ-22, AC-26, AC-32) ---

@test "AC-26 render_options: ACL con la subred de la LAN y 10.10.0.0/16, reenviadores y forward only" {
  run render_options 192.168.1.53/24 1.1.1.1,8.8.8.8

  [ "$status" -eq 0 ]
  [[ "$output" == *"acl stratus_clients { 192.168.1.0/24; 10.10.0.0/16; localhost; };"* ]]
  [[ "$output" == *"allow-query { stratus_clients; };"* ]]
  [[ "$output" == *"allow-recursion { stratus_clients; };"* ]]
  [[ "$output" == *"forwarders { 1.1.1.1; 8.8.8.8; };"* ]]
  [[ "$output" == *"forward only;"* ]]
  [[ "$output" == *"listen-on-v6 { none; };"* ]]
}

@test "AC-26 render_options calcula la red de prefijos que no son /24" {
  run render_options 10.20.30.53/12 9.9.9.9
  [[ "$output" == *"acl stratus_clients { 10.16.0.0/12; 10.10.0.0/16; localhost; };"* ]]
  [[ "$output" == *"forwarders { 9.9.9.9; };"* ]]
}

@test "AC-32 render_zone: TTL 60, serial, NS, A propio y registros ordenados" {
  run render_zone dns1 stratus.lan 192.168.1.53 web-b,192.168.1.41 vm1,192.168.1.50 lb1,192.168.1.60

  [ "$status" -eq 0 ]
  [ "${lines[0]}" = '$TTL 60' ]
  [ "${lines[1]}" = "@ IN SOA dns1.stratus.lan. hostmaster.stratus.lan. (" ]
  [[ "${lines[2]}" =~ ^\ \ [0-9]+\ \;\ serial$ ]]
  [ "${lines[3]}" = "  3600 600 86400 60 )" ]
  [ "${lines[4]}" = "@ IN NS dns1.stratus.lan." ]
  [ "${lines[5]}" = "dns1 IN A 192.168.1.53" ]
  [ "${lines[6]}" = "lb1 IN A 192.168.1.60" ]
  [ "${lines[7]}" = "vm1 IN A 192.168.1.50" ]
  [ "${lines[8]}" = "web-b IN A 192.168.1.41" ]
  [ "${#lines[@]}" -eq 9 ]
}

@test "AC-32 la zona generada pasa named-checkzone real (si está instalado)" {
  command -v named-checkzone >/dev/null || skip "named-checkzone no está instalado"
  render_zone dns1 stratus.lan 192.168.1.53 web-a,192.168.1.40 >"$BATS_TEST_TMPDIR/db.stratus.lan"
  run /usr/bin/named-checkzone stratus.lan "$BATS_TEST_TMPDIR/db.stratus.lan"
  [ "$status" -eq 0 ]
}

# --- apply_zone (REQ-28, AC-39, AC-41) ---

@test "AC-41 apply_zone: la zona inválida sale con 10, la salida va a stderr y no se recarga" {
  stub_vm_ip dns1 192.168.1.53
  export ZONE_CHECK=fail

  run --separate-stderr apply_zone dns1 stratus.lan <<<"zona"

  [ "$status" -eq 10 ]
  [[ "$stderr" == *"bad owner name"* ]]
  [[ "$stderr" == *"Zona DNS inválida; se mantuvo la anterior"* ]]
  grep -qx 'named-checkzone stratus.lan /var/lib/bind/db.stratus.lan.new' "$REMOTE"
  grep -qx 'rm -f /var/lib/bind/db.stratus.lan.new' "$REMOTE"
  ! grep -q 'mv ' "$REMOTE" || return 1
  ! grep -q 'rndc' "$REMOTE" || return 1
}

@test "REQ-28 apply_zone sube la zona, guarda .bak, recarga y borra el .bak" {
  stub_vm_ip dns1 192.168.1.53

  run --separate-stderr apply_zone dns1 stratus.lan <<<"zona nueva"

  [ "$status" -eq 0 ]
  [ "$output" = "reloaded=yes" ]
  # La zona llega por stdin a la llamada que escribe el .new.
  upload="$(grep -n 'db.stratus.lan.new' "$REMOTE" | head -n 1 | cut -d: -f1)"
  [ "$(cat "$BATS_TEST_TMPDIR/stdin/$upload")" = "zona nueva" ]
  grep -q 'cp -p' "$REMOTE"
  grep -q 'chown bind:bind' "$REMOTE"
  grep -qx 'rndc reload stratus.lan' "$REMOTE"
  grep -qx 'rm -f /var/lib/bind/db.stratus.lan.bak' "$REMOTE"
}

@test "AC-41 apply_zone: si la recarga falla restaura el .bak, recarga otra vez y sale con 11" {
  stub_vm_ip dns1 192.168.1.53
  export RNDC=fail

  run --separate-stderr apply_zone dns1 stratus.lan <<<"zona"

  [ "$status" -eq 11 ]
  [[ "$stderr" == *"No se pudo recargar bind9; se restauró la zona anterior"* ]]
  grep -qx 'mv /var/lib/bind/db.stratus.lan.bak /var/lib/bind/db.stratus.lan' "$REMOTE"
  [ "$(grep -cx 'rndc reload stratus.lan' "$REMOTE")" -eq 2 ]
}

@test "AC-41 apply_zone sale con 9 si también falla la restauración" {
  stub_vm_ip dns1 192.168.1.53
  export RNDC=failall

  run --separate-stderr apply_zone dns1 stratus.lan <<<"zona"

  [ "$status" -eq 9 ]
}

@test "AC-39 apply_zone --if-changed no sube ni recarga si solo cambia el serial" {
  stub_vm_ip dns1 192.168.1.53
  new="$(render_zone dns1 stratus.lan 192.168.1.53 vm1,192.168.1.50)"
  CURRENT_ZONE="$(sed 's/^  [0-9]* ; serial$/  12345 ; serial/' <<<"$new")"
  export CURRENT_ZONE

  run --separate-stderr apply_zone dns1 stratus.lan --if-changed <<<"$new"

  [ "$status" -eq 0 ]
  [ "$output" = "reloaded=no" ]
  ! grep -q 'db.stratus.lan.new' "$REMOTE" || return 1
  ! grep -q 'rndc' "$REMOTE" || return 1
}

@test "AC-39 apply_zone --if-changed aplica si cambió un registro o no hay zona" {
  stub_vm_ip dns1 192.168.1.53
  export CURRENT_ZONE="$(render_zone dns1 stratus.lan 192.168.1.53 vm1,192.168.1.50)"
  new="$(render_zone dns1 stratus.lan 192.168.1.53 vm1,192.168.1.51)"

  run --separate-stderr apply_zone dns1 stratus.lan --if-changed <<<"$new"
  [ "$status" -eq 0 ]
  [ "$output" = "reloaded=yes" ]

  unset CURRENT_ZONE
  run --separate-stderr apply_zone dns1 stratus.lan --if-changed <<<"$new"
  [ "$status" -eq 0 ]
  [ "$output" = "reloaded=yes" ]
}

@test "AC-35 apply_zone devuelve 7 sin llamar a ssh si el servidor DNS está apagado" {
  stub_vm_ip dns1 -

  run --separate-stderr apply_zone dns1 stratus.lan <<<"zona"

  [ "$status" -eq 7 ]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

# --- wait_ips (REQ-27, AC-40) ---

@test "AC-40 wait_ips espera a todas las VMs en paralelo con 60 sondeos en total por VM" {
  stub_ip_sequence vm1 - - 192.168.1.51
  stub_ip_sequence vm2 -

  run --separate-stderr wait_ips 192.168.1.53/24 vm1 vm2

  [ "$status" -eq 0 ]
  [ "${lines[0]}" = "vm1 192.168.1.51" ]
  [ "${lines[1]}" = "vm2 -" ]
  [ "$(cat "$BATS_TEST_TMPDIR/seq-vm1.count")" -eq 3 ]
  [ "$(cat "$BATS_TEST_TMPDIR/seq-vm2.count")" -eq 60 ]
  # Cada VM deja de esperar al tener IP: 2 esperas de vm1 y a lo sumo 60 de vm2.
  [ "$(grep -cx 'sleep 2' "$CALLS")" -le 62 ]
}

@test "AC-40 wait_ips sin VMs no imprime nada" {
  run wait_ips 192.168.1.53/24
  [ "$status" -eq 0 ]
  [ -z "$output" ]
}

@test "AC-59 wait_ips ignora una dirección fuera de la LAN" {
  stub_ip_sequence vm1 10.0.2.15 192.168.1.45
  stub_ip_sequence vm2 10.0.2.15

  run --separate-stderr wait_ips 192.168.1.53/24 vm1 vm2

  [ "$status" -eq 0 ]
  [ "${lines[0]}" = "vm1 192.168.1.45" ]
  [ "${lines[1]}" = "vm2 -" ]
  [ "$(cat "$BATS_TEST_TMPDIR/seq-vm1.count")" -eq 2 ]
  [ "$(cat "$BATS_TEST_TMPDIR/seq-vm2.count")" -eq 60 ]
}

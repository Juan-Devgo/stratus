#!/usr/bin/env bats
# Pruebas de create-dns.sh (005) con stubs de VBoxManage, ssh, sleep, arping, ping y de los scripts
# de prepare (create-vm.sh, stop-vm.sh y delete-vm.sh): no tocan VirtualBox, la red ni VMs.

load test_helper

bats_require_minimum_version 1.5.0

setup() {
  setup_dns_stubs
  setup_guest_password
  CALLS="$BATS_TEST_TMPDIR/calls.log"
  REMOTE="$BATS_TEST_TMPDIR/remote.log"
  export APP_SSH_KEY_PATH="$BATS_TEST_TMPDIR/stratus_backend"
  : >"$APP_SSH_KEY_PATH"
  DISK="$BATS_TEST_TMPDIR/dns.vdi"
  : >"$DISK"
  SCRIPT="$DX/create-dns.sh"
  ARGS=(dns1 1024 1 "$DISK" eth0 192.168.1.53/24 192.168.1.1 stratus.lan 1.1.1.1,8.8.8.8)
  stub_dns_vm dns1 192.168.1.40 - 192.168.1.53
}

# stdin_of <ERE>: entrada estándar de la primera llamada a ssh cuyo comando coincide.
stdin_of() {
  local n
  n="$(grep -nE -m1 -- "$1" "$REMOTE" | cut -d: -f1)"
  cat "$BATS_TEST_TMPDIR/stdin/$n"
}

# line_in <archivo> <ERE>: número de la primera línea que coincide.
line_in() {
  grep -nE -m1 -- "$2" "$1" | cut -d: -f1
}

@test "AC-56 create-dns.sh tiene la estructura exigida" {
  assert_script_structure "$DNS_DIR/create-dns.sh"
}

@test "AC-22 create-dns crea la VM, fija la IP estática, configura bind9 y reinicia" {
  run --separate-stderr "$SCRIPT" "${ARGS[@]}"

  [ "$status" -eq 0 ]
  [ "$output" = "Servidor DNS dns1 creado en 192.168.1.53; zona stratus.lan" ]
  grep -qxF "create-vm.sh dns1 1024 1 $DISK eth0" "$CALLS"
  grep -q '08:00:27:ee:ee:ee' "$REMOTE"

  interfaces="$(stdin_of 'cat >"\$1" sh /etc/network/interfaces$')"
  [[ "$interfaces" == *"source /etc/network/interfaces.d/*"* ]]
  [[ "$interfaces" == *"iface lo inet loopback"* ]]
  static="$(stdin_of 'interfaces.d/stratus-static$')"
  [[ "$static" == *"auto enp0s3"* ]]
  [[ "$static" == *"iface enp0s3 inet static"* ]]
  [[ "$static" == *"address 192.168.1.53/24"* ]]
  [[ "$static" == *"gateway 192.168.1.1"* ]]
  [ "$(stdin_of 'resolv.conf$')" = "nameserver 127.0.0.1" ]

  options="$(stdin_of 'named.conf.options$')"
  [[ "$options" == *"acl stratus_clients { 192.168.1.0/24; 10.10.0.0/16; localhost; };"* ]]
  [[ "$options" == *"forwarders { 1.1.1.1; 8.8.8.8; };"* ]]
  local_conf="$(stdin_of 'named.conf.local$')"
  [[ "$local_conf" == *'zone "stratus.lan" {'* ]]
  [[ "$local_conf" == *"type master;"* ]]
  [[ "$local_conf" == *'file "/var/lib/bind/db.stratus.lan";'* ]]
  zone="$(stdin_of 'db.stratus.lan$')"
  [[ "$zone" == *"@ IN NS dns1.stratus.lan."* ]]
  [[ "$zone" == *"dns1 IN A 192.168.1.53"* ]]
  grep -qx 'chown bind:bind /var/lib/bind/db.stratus.lan' "$REMOTE"
  grep -qx 'named-checkconf' "$REMOTE"
  grep -qx 'named-checkzone stratus.lan /var/lib/bind/db.stratus.lan' "$REMOTE"

  # Orden: crear, habilitar named, apagar, borrar la IP vieja, encender y esperar la IP estática.
  create="$(line_in "$CALLS" '^create-vm.sh')"
  stop="$(line_in "$CALLS" '^stop-vm.sh dns1$')"
  delprop="$(line_in "$CALLS" '^VBoxManage guestproperty delete dns1 /VirtualBox/GuestInfo/Net/0/V4/IP$')"
  start="$(line_in "$CALLS" '^VBoxManage startvm dns1 --type headless$')"
  [ -n "$create" ] && [ -n "$stop" ] && [ -n "$delprop" ] && [ -n "$start" ]
  ((create < stop && stop < delprop && delprop < start))
  enable="$(line_in "$REMOTE" '^systemctl enable named$')"
  checkzone="$(line_in "$REMOTE" '^named-checkzone')"
  [ -n "$enable" ] && ((checkzone < enable))
  grep -q 'dig @192.168.1.53 dns1.stratus.lan A +short' "$REMOTE"
  ! grep -q '^delete-vm.sh' "$CALLS" || return 1
}

@test "AC-27 create-dns agrega los registros iniciales a la zona" {
  run --separate-stderr "$SCRIPT" "${ARGS[@]}" web-b,192.168.1.41 web-a,192.168.1.40

  [ "$status" -eq 0 ]
  zone="$(stdin_of 'db.stratus.lan$')"
  [[ "$zone" == *$'web-a IN A 192.168.1.40\nweb-b IN A 192.168.1.41'* ]]
}

@test "AC-22 create-dns sale con 5 y borra la VM si la IP estática nunca aparece" {
  stub_dns_vm dns1 192.168.1.40 192.168.1.40

  run --separate-stderr "$SCRIPT" "${ARGS[@]}"

  [ "$status" -eq 5 ]
  [ "$(grep -c '^sleep' "$CALLS")" -ge 90 ]
  grep -qx 'delete-vm.sh dns1' "$CALLS"
}

@test "AC-22 create-dns sale con 5 si dig no devuelve la IP del servidor" {
  export DIG_OUT="192.168.1.99"

  run --separate-stderr "$SCRIPT" "${ARGS[@]}"

  [ "$status" -eq 5 ]
  grep -qx 'delete-vm.sh dns1' "$CALLS"
}

@test "AC-22 create-dns limpia si la zona inicial no pasa named-checkzone" {
  export ZONE_CHECK=fail

  run --separate-stderr "$SCRIPT" "${ARGS[@]}"

  [ "$status" -eq 5 ]
  grep -qx 'delete-vm.sh dns1' "$CALLS"
  ! grep -q '^VBoxManage startvm' "$CALLS" || return 1
}

@test "AC-22 create-dns sale con 9 si también falla la limpieza" {
  export CONF_CHECK=fail RC_DELETE_VM=1

  run --separate-stderr "$SCRIPT" "${ARGS[@]}"

  [ "$status" -eq 9 ]
}

@test "AC-24 create-dns sale con 2 y Configuración de DNS incompleta sin llamar a nada" {
  local -a cases=(
    "dns1 1024 1 $DISK eth0 192.168.1.53 192.168.1.1 stratus.lan 1.1.1.1"
    "dns1 1024 1 $DISK eth0 192.168.1.53/31 192.168.1.1 stratus.lan 1.1.1.1"
    "dns1 1024 1 $DISK eth0 192.168.1.53/24 10.0.0.1 stratus.lan 1.1.1.1"
    "dns1 1024 1 $DISK eth0 192.168.1.53/24 192.168.1.1 Stratus_Lan 1.1.1.1"
    "dns1 1024 1 $DISK eth0 192.168.1.53/24 192.168.1.1 stratus.lan 1.1.1"
    "dns1 1024 1 $DISK eth0 192.168.1.53/24 192.168.1.1 stratus.lan 1.1.1.1,2.2.2.2,3.3.3.3,4.4.4.4"
  )
  local args
  local -a argv
  for args in "${cases[@]}"; do
    read -ra argv <<<"$args"
    run --separate-stderr "$SCRIPT" "${argv[@]}"
    [ "$status" -eq 2 ]
    [ "$stderr" = "Configuración de DNS incompleta" ]
  done
  [ ! -s "$CALLS" ]
}

@test "AC-25 create-dns sale con 13 si la IP responde a ARP o a ICMP, sin crear la VM" {
  export ARPING=reply
  run --separate-stderr "$SCRIPT" "${ARGS[@]}"
  [ "$status" -eq 13 ]
  [ "$stderr" = "La IP del servidor DNS ya está en uso" ]

  export ARPING=none PING=reply
  run --separate-stderr "$SCRIPT" "${ARGS[@]}"
  [ "$status" -eq 13 ]
  grep -qx 'arping -c 1 -w 1 -I eth0 192.168.1.53' "$CALLS"
  ! grep -q 'create-vm.sh' "$CALLS" || return 1
}

@test "AC-25 create-dns sale con 4 si arping no está instalado" {
  hide_arping

  run --separate-stderr "$SCRIPT" "${ARGS[@]}"

  [ "$status" -eq 4 ]
  [ "$stderr" = "arping no está instalado en el host" ]
  ! grep -q 'create-vm.sh' "$CALLS" || return 1
}

@test "REQ-13 create-dns sale con 6 sin el disco base de DNS y no llama a create-vm" {
  ARGS[3]="$BATS_TEST_TMPDIR/no-existe.vdi"

  run --separate-stderr "$SCRIPT" "${ARGS[@]}"

  [ "$status" -eq 6 ]
  [ "$stderr" = "Disco base de DNS no encontrado" ]
  ! grep -qE 'create-vm.sh|arping' "$CALLS" || return 1
}

@test "AC-55 create-dns sale con 2 ante otros argumentos inválidos sin llamar a nada" {
  run "$SCRIPT" DNS_1 "${ARGS[@]:1}"
  [ "$status" -eq 2 ]
  run "$SCRIPT" dns1 256 "${ARGS[@]:2}"
  [ "$status" -eq 2 ]
  run "$SCRIPT" "${ARGS[@]}" dns1,192.168.1.60
  [ "$status" -eq 2 ]
  run "$SCRIPT" "${ARGS[@]}" web-a,999.1.1.1
  [ "$status" -eq 2 ]
  run "$SCRIPT" "${ARGS[@]:0:8}"
  [ "$status" -eq 2 ]
  run env -u APP_GUEST_PASSWORD_FILE "$SCRIPT" "${ARGS[@]}"
  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

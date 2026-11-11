#!/usr/bin/env bats
# Pruebas de haproxy-common.sh: validadores, lb_reachable, render_cfg, apply_cfg y wait_ips con los
# stubs de VBoxManage y ssh; no tocan VirtualBox ni VMs.

load ../test_helper

bats_require_minimum_version 1.5.0

setup() {
  setup_haproxy_stubs
  CALLS="$BATS_TEST_TMPDIR/calls.log"
  REMOTE="$BATS_TEST_TMPDIR/remote.log"
  export APP_SSH_KEY_PATH="$BATS_TEST_TMPDIR/stratus_backend"
  : >"$APP_SSH_KEY_PATH"
  # lb1 está encendido con su IPv4 en puente 192.168.1.60.
  stub_vm_nics lb1 running 080027CCCCCC 192.168.1.60 080027DDDDDD -
  # shellcheck source=../../prepare/lib/vm-exec.sh
  source "$HX/../prepare/lib/vm-exec.sh"
  # shellcheck source=haproxy-common.sh
  source "$HX/lib/haproxy-common.sh"
}

# --- Validadores (AC-37, P3: la misma lista permitida que la UI y el backend) ---

# check_validator <función> <ok|bad> <valor>...: cada valor debe pasar (ok) o fallar con el motivo
# en stderr (bad).
check_validator() {
  local fn="$1" expect="$2" value
  shift 2
  for value in "$@"; do
    if [[ "$expect" == ok ]]; then
      "$fn" "$value" || { echo "$fn rechazó '$value'" >&2; return 1; }
    else
      local err rc=0
      err="$("$fn" "$value" 2>&1 >/dev/null)" || rc=$?
      ((rc == 1)) || { echo "$fn aceptó '$value'" >&2; return 1; }
      [[ -n "$err" ]] || { echo "$fn no explicó el rechazo de '$value'" >&2; return 1; }
    fi
  done
}

@test "AC-37 validate_lb_name y validate_vm_name aceptan nombres válidos y rechazan el resto" {
  check_validator validate_lb_name ok lb1 a 0a "$(printf 'a%.0s' {1..32})" web-a
  check_validator validate_lb_name bad Lb1 -lb lb_1 'lb;x' '' "$(printf 'a%.0s' {1..33})" $'lb1\n' 'a b'
  check_validator validate_vm_name ok web-a
  check_validator validate_vm_name bad WEB 'a;b' ''
}

@test "AC-37 validate_memory y validate_cpus aceptan los rangos de 001" {
  check_validator validate_memory ok 512 1024 8192
  check_validator validate_memory bad 511 8193 '' 1e3 ' 512' 5.5
  check_validator validate_cpus ok 1 4
  check_validator validate_cpus bad 0 5 '' a 01x
}

@test "AC-37 validate_base_disk exige una ruta absoluta y validate_bridge un nombre de interfaz" {
  check_validator validate_base_disk ok /vms/haproxy-base.vdi '/ruta con espacios/d.vdi'
  check_validator validate_base_disk bad base.vdi '' $'/ruta\ncon-salto'
  check_validator validate_bridge ok eth0 wlp0s20f3 en0.1 br:0 'en0: Wi-Fi' \
    'en3: Adaptador Ethernet (en3)' 'anpi1: USB Ethernet(?)'
  check_validator validate_bridge bad '' 'eth0;x' 'eth0/path' $'eth0\nmal' \
    "$(printf 'a%.0s' {1..65})"
}

@test "AC-37 validate_port acepta 1-65535 y validate_frontend_port rechaza además el 22" {
  check_validator validate_port ok 1 22 80 8080 65535
  check_validator validate_port bad 0 65536 080 80a '' -1 ' 80' 99999
  check_validator validate_frontend_port ok 1 21 23 80 8080 65535
  check_validator validate_frontend_port bad 22 0 65536 80a ''

  run --separate-stderr validate_frontend_port 22
  [ "$status" -eq 1 ]
  [[ "$stderr" == *"22"* ]]
}

@test "AC-37 validate_server_name acepta server1 a server10" {
  check_validator validate_server_name ok server1 server9 server10
  check_validator validate_server_name bad server0 server11 server01 Server1 server '' 'server1;x'
}

@test "AC-37 validate_subnet acepta 1-254" {
  check_validator validate_subnet ok 1 9 10 254
  check_validator validate_subnet bad 0 255 '' 01 1a 1000 -1
}

@test "AC-37 validate_reserved acepta - o una lista de subredes 1-254" {
  check_validator validate_reserved ok - 1 1,3 1,2,254 003
  check_validator validate_reserved bad '' 0 255 1,0 1,255 1, ,1 1,,3 a 1.3 '1,3 '
}

@test "AC-37 validate_spec revisa los 5 campos y la IP según la subred" {
  # Sin subred: cualquier IPv4.
  run validate_spec server1,web-a,80,10.10.1.11,on
  [ "$status" -eq 0 ]
  run validate_spec server10,web-a,65535,192.168.1.5,off
  [ "$status" -eq 0 ]
  # Con subred: 10.10.<N>.10 a 10.10.<N>.200.
  run validate_spec server1,web-a,80,10.10.1.10,on 1
  [ "$status" -eq 0 ]
  run validate_spec server1,web-a,80,10.10.1.200,on 1
  [ "$status" -eq 0 ]
  for spec in \
    server1,web-a,80,10.10.1.9,on \
    server1,web-a,80,10.10.1.201,on \
    server1,web-a,80,10.10.2.11,on \
    server1,web-a,80,10.10.1.11.5,on; do
    run validate_spec "$spec" 1
    [ "$status" -eq 1 ]
  done
  # Campos inválidos o con otra forma.
  for spec in \
    '' \
    server1,web-a,80,10.10.1.11 \
    server1,web-a,80,10.10.1.11,on,extra \
    server0,web-a,80,10.10.1.11,on \
    server1,WEB,80,10.10.1.11,on \
    server1,web-a,0,10.10.1.11,on \
    server1,web-a,65536,10.10.1.11,on \
    server1,web-a,80,10.10.1.256,on \
    server1,web-a,80,host,on \
    server1,web-a,80,10.10.1.11,yes \
    server1,web-a,80,10.10.1.11, \
    'server1,web-a,80,10.10.1.11,on;x'; do
    run --separate-stderr validate_spec "$spec"
    [ "$status" -eq 1 ]
    [ -n "$stderr" ]
  done
}

@test "AC-37 validate_specs acepta hasta 10 especificaciones con nombres únicos" {
  specs=()
  for i in {1..10}; do specs+=("server$i,vm$i,80,10.10.1.$((10 + i)),on"); done
  run validate_specs 1 "${specs[@]}"
  [ "$status" -eq 0 ]
  run validate_specs 1
  [ "$status" -eq 0 ]
  run validate_specs - "${specs[@]:0:2}"
  [ "$status" -eq 0 ]

  # Once especificaciones, un nombre repetido y una especificación inválida.
  run validate_specs 1 "${specs[@]}" server1,vm11,80,10.10.1.50,on
  [ "$status" -eq 1 ]
  run validate_specs 1 server1,web-a,80,10.10.1.11,on server1,web-b,80,10.10.1.12,on
  [ "$status" -eq 1 ]
  run validate_specs 1 server1,web-a,80,10.10.1.11,on server2,web-b,80,10.10.9.12,on
  [ "$status" -eq 1 ]
}

@test "validate_* nunca repiten un valor secreto o largo: el motivo es corto" {
  run --separate-stderr validate_lb_name "$(printf 'x%.0s' {1..300})"
  [ "$status" -eq 1 ]
  [ "${#stderr}" -lt 200 ]
}

# --- fail ---

@test "fail imprime el mensaje en stderr y sale con el código" {
  run --separate-stderr fail 7 "algo falló"
  [ "$status" -eq 7 ]
  [ "$stderr" = "algo falló" ]
  [ -z "$output" ]
}

# --- lb_reachable (REQ-19, AC-40) ---

@test "lb_reachable devuelve 0 con el balanceador encendido y con IPv4 en puente" {
  run lb_reachable lb1
  [ "$status" -eq 0 ]
}

@test "AC-40 lb_reachable devuelve 12 con el mensaje si el balanceador está apagado" {
  stub_vm_nics lb2 poweroff 080027EEEEEE 192.168.1.61 080027FFFFFF -

  run --separate-stderr lb_reachable lb2

  [ "$status" -eq 12 ]
  [ "$stderr" = "El balanceador lb2 está apagado; enciéndalo para llegar al servidor" ]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

# --- render_cfg (AC-12, AC-23) ---

@test "AC-12 render_cfg sin servidores tiene el frontend, roundrobin, el socket de stats y ninguna línea server" {
  run render_cfg 80

  [ "$status" -eq 0 ]
  [[ "$output" == *$'frontend stratus_front\n  bind :80\n  default_backend stratus_back'* ]]
  [[ "$output" == *$'backend stratus_back\n  balance roundrobin'* ]]
  [[ "$output" == *"stats socket ipv4@127.0.0.1:9999 level user"* ]]
  [[ "$output" == *"option httplog"* ]]
  ! grep -qE '^\s*server ' <<<"$output" || return 1
}

@test "AC-11 render_cfg usa el puerto del frontend" {
  run render_cfg 8080

  [[ "$output" == *"  bind :8080"* ]]
  [[ "$output" != *"bind :80"$'\n'* ]]
}

@test "AC-23 render_cfg escribe una línea server por especificación y deshabilita las apagadas" {
  run render_cfg 80 server1,web-a,80,10.10.1.11,on server2,web-b,8080,10.10.1.12,off

  [ "$status" -eq 0 ]
  server_lines="$(grep -E '^\s*server ' <<<"$output")"
  [ "$server_lines" = $'  server server1 10.10.1.11:80 check\n  server server2 10.10.1.12:8080 check disabled' ]
  # Las líneas server son lo último del backend.
  [[ "$output" == *$'  balance roundrobin\n  server server1'* ]]
}

# --- apply_cfg (AC-22, AC-24, AC-45) ---

CFG=$'global\n  daemon\nfrontend stratus_front\n  bind :80\n'

@test "apply_cfg sube el archivo, lo revisa, lo reemplaza, recarga y verifica el puerto" {
  run --separate-stderr apply_cfg lb1 80 <<<"$CFG"

  [ "$status" -eq 0 ]
  # El archivo nuevo llega por la entrada estándar de ssh.
  [ "$(cat "$BATS_TEST_TMPDIR/ssh-stdin.log")" = "${CFG%$'\n'}" ]
  steps="$(cut -c1-80 "$REMOTE")"
  expected="$(printf '%s\n' \
    'sh -c cat > "$1" sh /etc/haproxy/haproxy.cfg.new' \
    'haproxy -c -f /etc/haproxy/haproxy.cfg.new' \
    'cp -p /etc/haproxy/haproxy.cfg /etc/haproxy/haproxy.cfg.bak' \
    'mv /etc/haproxy/haproxy.cfg.new /etc/haproxy/haproxy.cfg' \
    'systemctl reload haproxy' \
    'ss -Hltnp sport = :80' \
    'rm -f /etc/haproxy/haproxy.cfg.bak')"
  [ "$steps" = "$expected" ]
}

@test "AC-24 apply_cfg conserva la configuración si falla la revisión: sale con 10, borra .new, no hace mv ni recarga" {
  HAPROXY_CHECK=fail run --separate-stderr apply_cfg lb1 80 <<<"$CFG"

  [ "$status" -eq 10 ]
  [[ "$stderr" == *"[ALERT] (1) : Error en la configuración de prueba"* ]]
  [[ "$stderr" == *"Configuración de HAProxy inválida; se mantuvo la anterior"* ]]
  grep -qF -- "rm -f /etc/haproxy/haproxy.cfg.new" "$REMOTE"
  ! grep -qF -- "mv " "$REMOTE" || return 1
  ! grep -qF -- "systemctl reload" "$REMOTE" || return 1
  ! grep -qF -- "haproxy.cfg.bak" "$REMOTE" || return 1
}

@test "AC-45 apply_cfg restaura la copia si HAProxy no enlaza el puerto: 5 sondeos, mv de .bak, segunda recarga y 11" {
  SS_OUT='LISTEN 0 511 0.0.0.0:8080 0.0.0.0:* users:(("nginx",pid=7,fd=6))' \
    run --separate-stderr apply_cfg lb1 8080 <<<"$CFG"

  [ "$status" -eq 11 ]
  [[ "$stderr" == *"No se pudo recargar HAProxy; se mantuvo la configuración anterior"* ]]
  [[ "$stderr" == *"nginx"* ]]
  [ "$(grep -c 'ss -Hltnp sport = :8080' "$REMOTE")" -eq 5 ]
  [ "$(grep -c '^sleep 1$' "$CALLS")" -eq 5 ]
  [ "$(grep -c 'systemctl reload haproxy' "$REMOTE")" -eq 2 ]
  # Orden: mv del nuevo, recarga, 5 sondeos, mv de .bak de vuelta, recarga.
  restore_line="$(grep -nF -- "mv /etc/haproxy/haproxy.cfg.bak /etc/haproxy/haproxy.cfg" "$REMOTE" | cut -d: -f1)"
  last_ss="$(grep -n 'ss -Hltnp' "$REMOTE" | tail -1 | cut -d: -f1)"
  second_reload="$(grep -n 'systemctl reload haproxy' "$REMOTE" | tail -1 | cut -d: -f1)"
  [ "$restore_line" -gt "$last_ss" ]
  [ "$second_reload" -gt "$restore_line" ]
}

@test "AC-45 apply_cfg restaura también si la recarga falla" {
  FAIL_REMOTE='systemctl reload' run --separate-stderr apply_cfg lb1 80 <<<"$CFG"

  [ "$status" -eq 11 ]
  grep -qF -- "mv /etc/haproxy/haproxy.cfg.bak /etc/haproxy/haproxy.cfg" "$REMOTE"
  [ "$(grep -c 'systemctl reload haproxy' "$REMOTE")" -eq 2 ]
  ! grep -qF -- "ss -Hltnp" "$REMOTE" || return 1
}

@test "apply_cfg sale con 9 si la restauración también falla" {
  FAIL_REMOTE='systemctl reload' FAIL_RESTORE='mv /etc/haproxy/haproxy.cfg.bak' \
    run --separate-stderr apply_cfg lb1 80 <<<"$CFG"

  [ "$status" -eq 9 ]
  [[ "$stderr" == *"restaur"* ]]
}

@test "AC-22 apply_cfg --if-changed no recarga si el archivo es igual al actual" {
  CMP=same apply_cfg lb1 80 --if-changed <<<"$CFG"

  [ "$APPLY_RELOADED" = no ]
  grep -qF -- "cmp -s /etc/haproxy/haproxy.cfg.new /etc/haproxy/haproxy.cfg" "$REMOTE"
  grep -qF -- "rm -f /etc/haproxy/haproxy.cfg.new" "$REMOTE"
  ! grep -qF -- "systemctl reload" "$REMOTE" || return 1
  ! grep -qF -- "haproxy -c" "$REMOTE" || return 1
  ! grep -qF -- "mv " "$REMOTE" || return 1
}

@test "AC-22 apply_cfg --if-changed recarga si el archivo cambió" {
  CMP=diff apply_cfg lb1 80 --if-changed <<<"$CFG"

  [ "$APPLY_RELOADED" = yes ]
  grep -qF -- "systemctl reload haproxy" "$REMOTE"
}

@test "apply_cfg sin --if-changed no compara y deja APPLY_RELOADED en yes" {
  apply_cfg lb1 80 <<<"$CFG"

  [ "$APPLY_RELOADED" = yes ]
  ! grep -qF -- "cmp -s" "$REMOTE" || return 1
}

@test "apply_cfg devuelve 7 si el balanceador no tiene IP (los scripts ya comprobaron lb_reachable) y no llama a ssh" {
  stub_vm_nics lb2 poweroff 080027EEEEEE 192.168.1.61 080027FFFFFF -

  run --separate-stderr apply_cfg lb2 80 <<<"$CFG"

  [ "$status" -eq 7 ]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

# --- wait_ips (REQ-10, REQ-14) ---

@test "wait_ips devuelve la IP en rango tras varios sondeos, esperando 2 s entre ellos" {
  stub_ip_sequence web-a - - 10.10.1.11

  run --separate-stderr wait_ips 1 web-a

  [ "$status" -eq 0 ]
  [ "$output" = "web-a 10.10.1.11" ]
  [ "$(grep -c '^sleep 2$' "$CALLS")" -eq 2 ]
}

@test "wait_ips ignora IPs fuera de 10.10.<N>.10-200 y se rinde tras 60 sondeos con -" {
  stub_ip_sequence web-a 192.168.1.5 10.10.2.11 10.10.1.9 10.10.1.201

  run --separate-stderr wait_ips 1 web-a

  [ "$status" -eq 0 ]
  [ "$output" = "web-a -" ]
  [ "$(grep -c '^sleep 2$' "$CALLS")" -eq 60 ]
  [ "$(grep -c 'Net/1/V4/IP' "$CALLS")" -eq 60 ]
}

@test "wait_ips espera varias VMs en paralelo con el mismo límite y conserva el orden" {
  stub_ip_sequence web-a 10.10.1.11
  stub_ip_sequence web-b -
  stub_ip_sequence web-c - 10.10.1.200

  run --separate-stderr wait_ips 1 web-a web-b web-c

  [ "$status" -eq 0 ]
  [ "$output" = $'web-a 10.10.1.11\nweb-b -\nweb-c 10.10.1.200' ]
  # web-b agota sus 60 sondeos; web-a no espera y web-c espera una vez.
  [ "$(grep -c 'guestproperty get web-b /VirtualBox/GuestInfo/Net/1/V4/IP' "$CALLS")" -eq 60 ]
  [ "$(grep -c 'guestproperty get web-a /VirtualBox/GuestInfo/Net/1/V4/IP' "$CALLS")" -eq 1 ]
  [ "$(grep -c 'guestproperty get web-c /VirtualBox/GuestInfo/Net/1/V4/IP' "$CALLS")" -eq 2 ]
}

#!/usr/bin/env bats
# Pruebas de refresh-records.sh (005) con stubs de VBoxManage, ssh y sleep: no tocan VMs.

load test_helper

bats_require_minimum_version 1.5.0

setup() {
  setup_dns_stubs
  CALLS="$BATS_TEST_TMPDIR/calls.log"
  REMOTE="$BATS_TEST_TMPDIR/remote.log"
  export APP_SSH_KEY_PATH="$BATS_TEST_TMPDIR/stratus_backend"
  : >"$APP_SSH_KEY_PATH"
  SCRIPT="$DX/refresh-records.sh"
}

# zone_upload: entrada estándar de la llamada que escribe la zona nueva.
zone_upload() {
  local n
  n="$(grep -n -m1 'db.stratus.lan.new$' "$REMOTE" | cut -d: -f1)"
  cat "$BATS_TEST_TMPDIR/stdin/$n"
}

# current_zone <registro...>: la zona que render_zone daría para esos registros, con otro serial.
current_zone() {
  (
    # shellcheck source=../prepare/lib/vm-exec.sh
    source "$DX/../prepare/lib/vm-exec.sh"
    # shellcheck source=lib/dns-common.sh
    source "$DX/lib/dns-common.sh"
    render_zone dns1 stratus.lan 192.168.1.53 "$@" | sed 's/^  [0-9]* ; serial$/  1 ; serial/'
  )
}

@test "AC-56 refresh-records.sh tiene la estructura exigida" {
  assert_script_structure "$DNS_DIR/refresh-records.sh"
}

@test "AC-36 refresh-records escribe la IP nueva de la VM esperada y recarga la zona" {
  stub_vm_ip dns1 192.168.1.53
  stub_ip_sequence vm1 - 192.168.1.45
  export CURRENT_ZONE
  CURRENT_ZONE="$(current_zone vm1,192.168.1.40)"

  run --separate-stderr "$SCRIPT" dns1 stratus.lan 192.168.1.53 vm1 vm1,192.168.1.40

  [ "$status" -eq 0 ]
  [ "${lines[0]}" = "vm1 192.168.1.45" ]
  [ "${lines[-1]}" = "reloaded=yes" ]
  [[ "$(zone_upload)" == *"vm1 IN A 192.168.1.45"* ]]
  grep -qx 'rndc reload stratus.lan' "$REMOTE"
}

@test "AC-39 refresh-records no sube ni recarga si nada cambió; una VM apagada conserva su IP sin sondeos" {
  stub_vm_ip dns1 192.168.1.53
  stub_ip_sequence vm1 192.168.1.40
  stub_vm_power vm2 poweroff
  export CURRENT_ZONE
  CURRENT_ZONE="$(current_zone vm1,192.168.1.40 vm2,192.168.1.42)"

  run --separate-stderr "$SCRIPT" dns1 stratus.lan 192.168.1.53 vm1,vm2 vm1,192.168.1.40 vm2,192.168.1.42

  [ "$status" -eq 0 ]
  [ "${lines[0]}" = "vm1 192.168.1.40" ]
  [ "${lines[1]}" = "vm2 192.168.1.42" ]
  [ "${lines[-1]}" = "reloaded=no" ]
  ! grep -q 'db.stratus.lan.new' "$REMOTE" || return 1
  ! grep -q 'rndc' "$REMOTE" || return 1
  ! grep -q '^VBoxManage guestproperty get vm2' "$CALLS" || return 1
}

@test "AC-39 refresh-records sin VMs que esperar no sondea ninguna" {
  stub_vm_ip dns1 192.168.1.53
  stub_ip_sequence vm1 192.168.1.45

  run --separate-stderr "$SCRIPT" dns1 stratus.lan 192.168.1.53 - vm1,192.168.1.40

  [ "$status" -eq 0 ]
  [ "${lines[0]}" = "vm1 192.168.1.40" ]
  [ "${lines[-1]}" = "reloaded=yes" ]
  [ ! -s "$BATS_TEST_TMPDIR/seq-vm1.count" ]
}

@test "AC-40 refresh-records espera a 10 VMs y al servidor DNS en una sola ventana compartida" {
  local wait="" records=() i
  stub_ip_sequence dns1 - 192.168.1.53
  for i in 0 1 2 3 4 5 6 7 8 9; do
    if ((i < 3)); then
      stub_ip_sequence "vm$i" -
    else
      stub_ip_sequence "vm$i" - "192.168.1.6$i"
    fi
    wait+="${wait:+,}vm$i"
    records+=("vm$i,192.168.1.4$i")
  done

  run --separate-stderr "$SCRIPT" dns1 stratus.lan 192.168.1.53 "$wait" "${records[@]}"

  [ "$status" -eq 0 ]
  for i in 0 1 2; do
    [ "${lines[$i]}" = "vm$i 192.168.1.4$i" ]
    [ "$(cat "$BATS_TEST_TMPDIR/seq-vm$i.count")" -eq 60 ]
  done
  for i in 3 4 5 6 7 8 9; do
    [ "${lines[$i]}" = "vm$i 192.168.1.6$i" ]
  done
  # El servidor DNS se sondea en la misma ventana: lo leyó wait_ips y luego vm_exec.
  [ "$(head -n 1 "$BATS_TEST_TMPDIR/seq-dns1.count")" -ge 2 ]
  # Una sola ventana: 3 VMs con 59 esperas cada una y una espera por cada VM que respondió.
  [ "$(grep -cx 'sleep 2' "$CALLS")" -le $((3 * 59 + 8)) ]
  [ "${lines[-1]}" = "reloaded=yes" ]
}

@test "AC-35 refresh-records sale con 7 si el servidor DNS está apagado, sin ssh ni sondeos" {
  stub_vm_ip dns1 -
  stub_ip_sequence vm1 192.168.1.45

  run --separate-stderr "$SCRIPT" dns1 stratus.lan 192.168.1.53 vm1 vm1,192.168.1.40

  [ "$status" -eq 7 ]
  ! grep -qE '^ssh ' "$CALLS" || return 1
  [ ! -s "$BATS_TEST_TMPDIR/seq-vm1.count" ]
}

@test "AC-55 refresh-records sale con 2 ante argumentos inválidos sin llamar a nada" {
  run "$SCRIPT" dns1 stratus.lan 192.168.1.53
  [ "$status" -eq 2 ]
  run "$SCRIPT" DNS_1 stratus.lan 192.168.1.53 - vm1,192.168.1.40
  [ "$status" -eq 2 ]
  run "$SCRIPT" dns1 Stratus_Lan 192.168.1.53 - vm1,192.168.1.40
  [ "$status" -eq 2 ]
  run "$SCRIPT" dns1 stratus.lan 192.168.1.999 - vm1,192.168.1.40
  [ "$status" -eq 2 ]
  run "$SCRIPT" dns1 stratus.lan 192.168.1.53 vm1,,vm2 vm1,192.168.1.40
  [ "$status" -eq 2 ]
  run "$SCRIPT" dns1 stratus.lan 192.168.1.53 VM1 vm1,192.168.1.40
  [ "$status" -eq 2 ]
  run "$SCRIPT" dns1 stratus.lan 192.168.1.53 - vm1
  [ "$status" -eq 2 ]
  run "$SCRIPT" dns1 stratus.lan 192.168.1.53 - dns1,192.168.1.40
  [ "$status" -eq 2 ]
  run env -u APP_SSH_KEY_PATH "$SCRIPT" dns1 stratus.lan 192.168.1.53 - vm1,192.168.1.40
  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

#!/usr/bin/env bats
# Pruebas de create-vm.sh. Usan stubs de VBoxManage, ssh, ssh-keygen y sleep: no tocan VirtualBox
# ni ningún disco real. El stub de VBoxManage lleva un estado mínimo en "$FAKE" (VM registrada,
# estado de energía, disco conectado) para que el script vea lo que vería con VirtualBox.

load test_helper

# "run --separate-stderr" necesita bats 1.5 o superior.
bats_require_minimum_version 1.5.0

SCRIPT="$PREPARE_DIR/create-vm.sh"

# Comportamiento del stub de VBoxManage. Se controla con variables de entorno de la prueba:
#   MEDIUM_TYPE      tipo que informa showmediuminfo del disco base (por defecto multiattach)
#   ORPHAN_MULTIATTACH omite la VM propietaria para simular un multiattach huérfano
#   BRIDGES          interfaz que lista "list bridgedifs" (por defecto eth0)
#   HOST_ARCHES       arquitecturas que informa "list systemproperties" (por defecto x86)
#   FAIL_STORAGEATTACH, FAIL_BOOTSTRAP, NO_IP, IGNORE_ACPI   provocan el fallo correspondiente
#   PROP_NEEDS_WAIT  una propiedad de invitado solo existe después de "guestproperty wait" (y se
#                    borra con "guestproperty delete"), como al arrancar una VM real
#   GA_READY_ON      número de la llamada a "guestcontrol ... --exe /bin/true" que tiene éxito (por defecto 1)
# Disco base: UUID base-uuid ("Parent UUID: base"); disco diferencial: diff-uuid (padre base-uuid).
VBOX_STUB_BODY="$(
  cat <<'STUB'
case "$1" in
  showmediuminfo)
    case "${3:-}" in
      diff-uuid) printf 'UUID:           diff-uuid\nParent UUID:    base-uuid\nType:           normal (differencing)\n' ;;
      *)
        printf 'UUID:           base-uuid\nParent UUID:    base\nType:           %s\n' "${MEDIUM_TYPE:-multiattach}"
        if [[ "${MEDIUM_TYPE:-multiattach}" == multiattach && -z "${ORPHAN_MULTIATTACH:-}" ]]; then
          printf 'In use by VMs:  vm0 (UUID: vm0-uuid)\n'
        fi ;;
    esac ;;
  list)
    case "${2:-}" in
      bridgedifs) printf 'Name:            %s\nGUID:            1234\n' "${BRIDGES:-eth0}" ;;
      systemproperties) printf 'Supported platform architectures: %s\n' "${HOST_ARCHES:-x86}" ;;
    esac ;;
  showvminfo)
    [[ -e "$FAKE/registered" ]] || exit 1
    if [[ "${3:-}" == --machinereadable ]]; then
      printf 'VMState="%s"\n' "$(cat "$FAKE/vmstate")"
      [[ ! -e "$FAKE/attached" ]] || printf '"SATA-ImageUUID-0-0"="diff-uuid"\n'
    fi ;;
  createvm) touch "$FAKE/registered"; echo poweroff >"$FAKE/vmstate" ;;
  storageattach)
    if [[ "$*" == *"--medium none"* ]]; then
      rm -f "$FAKE/attached"
    else
      [[ -z "${FAIL_STORAGEATTACH:-}" ]] || exit 1
      touch "$FAKE/attached"
    fi ;;
  startvm) echo running >"$FAKE/vmstate" ;;
  controlvm)
    case "${3:-}" in
      acpipowerbutton) [[ -n "${IGNORE_ACPI:-}" ]] || echo poweroff >"$FAKE/vmstate" ;;
      poweroff) echo poweroff >"$FAKE/vmstate" ;;
    esac ;;
  unregistervm) rm -f "$FAKE/registered" "$FAKE/attached" ;;
  guestproperty)
    marker="$FAKE/prop$(tr / _ <<<"${4:-}")"
    case "${2:-}" in
      wait) touch "$marker" ;;
      delete) rm -f "$marker" ;;
      get)
        if [[ -n "${PROP_NEEDS_WAIT:-}" && ! -e "$marker" ]]; then
          echo "No value set!"
        else
          case "${4:-}" in
            /VirtualBox/GuestAdd/Version) echo "Value: 7.2.16" ;;
            /VirtualBox/GuestInfo/Net/0/V4/IP)
              if [[ -n "${NO_IP:-}" ]]; then echo "No value set!"; else echo "Value: 192.168.1.50"; fi ;;
          esac
        fi ;;
    esac ;;
  guestcontrol)
    # Cuenta las pruebas de disponibilidad (--exe /bin/true); la número GA_READY_ON tiene éxito.
    if [[ "$*" == *"--exe /bin/true"* ]]; then
      n=$(($(cat "$FAKE/true.count" 2>/dev/null || echo 0) + 1))
      echo "$n" >"$FAKE/true.count"
      [[ "$n" -ge "${GA_READY_ON:-1}" ]] || exit 1
    fi
    # La llamada del bootstrap es la única que lleva systemd-machine-id-setup; se guarda su argv
    # (separado por NUL) para comprobar cada argumento.
    if [[ "$*" == *systemd-machine-id-setup* ]]; then
      printf '%s\0' "$@" >"$FAKE/bootstrap.args"
      [[ -z "${FAIL_BOOTSTRAP:-}" ]] || { echo "Falló el bootstrap" >&2; exit 1; }
    fi ;;
esac
exit 0
STUB
)"

setup() {
  setup_stubs
  CALLS="$BATS_TEST_TMPDIR/calls.log"
  export FAKE="$BATS_TEST_TMPDIR/fake-vbox"
  mkdir -p "$FAKE"
  stub_command VBoxManage "$VBOX_STUB_BODY"

  # Disco base falso: el script solo comprueba que exista y VBoxManage es un stub.
  BASE="$BATS_TEST_TMPDIR/base.vdi"
  printf 'BASE-DISK-CONTENT' >"$BASE"
  BASE_SUM="$(md5sum "$BASE")"

  # Par de llaves del backend: create-vm.sh solo lee la pública.
  export APP_SSH_KEY_PATH="$BATS_TEST_TMPDIR/ssh/backend_key"
  mkdir -p "$BATS_TEST_TMPDIR/ssh"
  PUBKEY="ssh-ed25519 AAAATESTKEYBODY stratus-backend"
  printf '%s\n' "$PUBKEY" >"$APP_SSH_KEY_PATH.pub"

  setup_guest_password
  unset MEDIUM_TYPE ORPHAN_MULTIATTACH BRIDGES HOST_ARCHES FAIL_STORAGEATTACH FAIL_BOOTSTRAP NO_IP IGNORE_ACPI PROP_NEEDS_WAIT GA_READY_ON
}

# line_of <texto fijo> [n]: número de la n-ésima línea de calls.log que lo contiene (vacío si no hay).
line_of() {
  grep -nF -- "$1" "$CALLS" | sed -n "${2:-1}p" | cut -d: -f1 || true
}

# assert_before <texto A> <texto B> [n de B]: A aparece en calls.log antes que B.
assert_before() {
  local a b
  a="$(line_of "$1")"
  b="$(line_of "$2" "${3:-1}")"
  [ -n "$a" ] || { echo "no aparece: $1" >&2; return 1; }
  [ -n "$b" ] || { echo "no aparece: $2" >&2; return 1; }
  [ "$a" -lt "$b" ] || { echo "'$1' ($a) no va antes que '$2' ($b)" >&2; return 1; }
}

# assert_cleanup: la limpieza apagó la VM, soltó y borró el diferencial y la desregistró, en orden,
# y el disco base no se cerró ni se modificó.
assert_cleanup() {
  assert_before "controlvm vm1 poweroff" "storageattach vm1 --storagectl SATA --port 0 --device 0 --medium none"
  assert_before "--medium none" "closemedium disk diff-uuid --delete"
  assert_before "closemedium disk diff-uuid --delete" "unregistervm vm1 --delete"
  [ "$(md5sum "$BASE")" = "$BASE_SUM" ]
  local touched
  touched="$(grep -E '^VBoxManage (closemedium|modifymedium) ' "$CALLS" | grep -cE "base-uuid|$BASE" || true)"
  [ "$touched" -eq 0 ]
}

@test "AC-24 create-vm.sh tiene cabecera, códigos de salida, modo estricto y secciones" {
  assert_script_structure "$SCRIPT"
}

@test "AC-1 create-vm pasa los argumentos a VBoxManage y responde con la IP" {
  run --separate-stderr "$SCRIPT" vm1 2048 2 "$BASE" eth0

  [ "$status" -eq 0 ]
  [ "$output" = "VM vm1 creada y encendida; IP 192.168.1.50" ]
  grep -qF -- "createvm --name vm1" "$CALLS"
  grep -F -- "modifyvm vm1" "$CALLS" | grep -qF -- "--memory 2048 --cpus 2"
  grep -qF -- "startvm vm1 --type headless" "$CALLS"
}

@test "AC-1 create-vm acepta el nombre de interfaz que VirtualBox informa en macOS" {
  BRIDGES='en0: Wi-Fi' run --separate-stderr "$SCRIPT" vm1 2048 2 "$BASE" 'en0: Wi-Fi'

  [ "$status" -eq 0 ]
  grep -qF -- '--nic1 bridged --bridgeadapter1 en0: Wi-Fi' "$CALLS"
}

@test "AC-26 create-vm usa Debian 13 x86-64 cuando el host admite x86" {
  run "$SCRIPT" vm1 2048 2 "$BASE" eth0

  [ "$status" -eq 0 ]
  grep -qF -- 'createvm --name vm1 --ostype Debian13_64 --register' "$CALLS"
}

@test "AC-26 create-vm usa Debian 13 ARM64 cuando el host solo admite ARMv8" {
  HOST_ARCHES=ARMv8 run "$SCRIPT" vm1 2048 2 "$BASE" eth0

  [ "$status" -eq 0 ]
  grep -qF -- 'createvm --name vm1 --ostype Debian13_arm64 --register' "$CALLS"
  grep -F -- 'modifyvm vm1' "$CALLS" | grep -qF -- '--graphicscontroller vmsvga --vram 16'
}

@test "AC-48 create-vm rechaza argumentos inválidos sin llamar a VBoxManage ni ssh" {
  local args
  for args in \
    "-vm 2048 2 $BASE eth0" \
    "VM1 2048 2 $BASE eth0" \
    "vm1 511 2 $BASE eth0" \
    "vm1 8193 2 $BASE eth0" \
    "vm1 2048 5 $BASE eth0" \
    "vm1 2048 0 $BASE eth0" \
    "vm1 2048 2 base.vdi eth0" \
    "vm1 2048 2 $BASE eth0;x" \
    "vm1 2048 2 $BASE eth0 extra" \
    "vm1 2048 2 $BASE"; do
    # shellcheck disable=SC2086 # las palabras se separan a propósito; ninguna lleva espacios
    run "$SCRIPT" $args
    [ "$status" -eq 2 ]
  done
  [ ! -s "$CALLS" ]
}

@test "AC-26 create-vm conecta el disco base multiattach en una NIC puente, sin NAT ni disco nuevo" {
  run "$SCRIPT" vm1 2048 2 "$BASE" eth0

  [ "$status" -eq 0 ]
  grep -qF -- "--nic1 bridged --bridgeadapter1 eth0" "$CALLS"
  grep -qF -- "storageattach vm1 --storagectl SATA --port 0 --device 0 --type hdd --mtype multiattach --medium $BASE" "$CALLS"
  ! grep -qF -- "natpf1" "$CALLS" || return 1
  ! grep -qF -- "createmedium" "$CALLS" || return 1
  # Con el disco ya multiattach no se vuelve a modificar.
  ! grep -qF -- "modifymedium" "$CALLS" || return 1
}

@test "AC-26 create-vm registra el disco normal con storageattach multiattach, sin modifymedium previo" {
  MEDIUM_TYPE=normal run "$SCRIPT" vm1 2048 2 "$BASE" eth0

  [ "$status" -eq 0 ]
  normal="$(line_of "storageattach vm1 --storagectl SATA --port 0 --device 0 --type hdd --mtype normal --medium $BASE")"
  detached="$(line_of "storageattach vm1 --storagectl SATA --port 0 --device 0 --medium none")"
  multi="$(line_of "storageattach vm1 --storagectl SATA --port 0 --device 0 --type hdd --mtype multiattach --medium $BASE")"
  [ -n "$normal" ]
  [ -n "$detached" ]
  [ -n "$multi" ]
  [ "$normal" -lt "$detached" ]
  [ "$detached" -lt "$multi" ]
  ! grep -qF -- "modifymedium" "$CALLS" || return 1
}

@test "AC-38 create-vm recupera un disco multiattach huérfano antes de registrarlo de nuevo" {
  ORPHAN_MULTIATTACH=1 run "$SCRIPT" vm1 2048 2 "$BASE" eth0

  [ "$status" -eq 0 ]
  reset="$(line_of "modifymedium disk $BASE --type normal")"
  normal="$(line_of "storageattach vm1 --storagectl SATA --port 0 --device 0 --type hdd --mtype normal --medium $BASE")"
  detached="$(line_of "storageattach vm1 --storagectl SATA --port 0 --device 0 --medium none")"
  multi="$(line_of "storageattach vm1 --storagectl SATA --port 0 --device 0 --type hdd --mtype multiattach --medium $BASE")"
  [ -n "$reset" ]
  [ -n "$normal" ]
  [ -n "$detached" ]
  [ -n "$multi" ]
  [ "$reset" -lt "$normal" ]
  [ "$normal" -lt "$detached" ]
  [ "$detached" -lt "$multi" ]
}

@test "AC-44 create-vm limpia si storageattach no puede registrar el disco multiattach" {
  FAIL_STORAGEATTACH=1 run --separate-stderr "$SCRIPT" vm1 2048 2 "$BASE" eth0

  [ "$status" -eq 4 ]
  [[ "$stderr" == *"No se pudo conectar el disco base"* ]]
  grep -qF -- "createvm --name vm1" "$CALLS"
  grep -qF -- "unregistervm vm1 --delete" "$CALLS"
  ! grep -qF -- "modifymedium" "$CALLS" || return 1
}

@test "AC-41 create-vm rechaza un disco base inexistente" {
  run --separate-stderr "$SCRIPT" vm1 2048 2 "$BATS_TEST_TMPDIR/no-existe.vdi" eth0

  [ "$status" -eq 6 ]
  [[ "$stderr" == *"Disco base no encontrado"* ]]
  ! grep -qF -- "createvm" "$CALLS" || return 1
}

@test "AC-41 create-vm rechaza una interfaz de puente inexistente" {
  BRIDGES="wlan0" run --separate-stderr "$SCRIPT" vm1 2048 2 "$BASE" eth0

  [ "$status" -eq 6 ]
  [[ "$stderr" == *"Interfaz de puente no encontrada"* ]]
  ! grep -qF -- "createvm" "$CALLS" || return 1
}

@test "AC-41 create-vm rechaza una VM ya registrada en VirtualBox sin tocarla" {
  touch "$FAKE/registered"

  run --separate-stderr "$SCRIPT" vm1 2048 2 "$BASE" eth0

  [ "$status" -eq 6 ]
  [[ "$stderr" == *"VM ya registrada"* ]]
  ! grep -qF -- "createvm" "$CALLS" || return 1
  ! grep -qF -- "unregistervm" "$CALLS" || return 1
}

@test "AC-45 create-vm exige el archivo de contraseña y nunca la expone" {
  # Sin la variable, o con un archivo inexistente: error de argumentos y ninguna llamada.
  unset APP_GUEST_PASSWORD_FILE
  run "$SCRIPT" vm1 2048 2 "$BASE" eth0
  [ "$status" -eq 2 ]
  export APP_GUEST_PASSWORD_FILE="$BATS_TEST_TMPDIR/no-existe"
  run "$SCRIPT" vm1 2048 2 "$BASE" eth0
  [ "$status" -eq 2 ]
  ! grep -q '^VBoxManage' "$CALLS" || return 1

  # Con éxito y con un bootstrap fallido, la contraseña no aparece en argv ni en la salida.
  setup_guest_password
  run --separate-stderr "$SCRIPT" vm1 2048 2 "$BASE" eth0
  [ "$status" -eq 0 ]
  [[ "$output$stderr" != *"$GUEST_PASSWORD"* ]]
  # Estado limpio de VirtualBox para la segunda creación.
  rm -rf "$FAKE" && mkdir -p "$FAKE"
  FAIL_BOOTSTRAP=1 run --separate-stderr "$SCRIPT" vm1 2048 2 "$BASE" eth0
  [ "$status" -eq 5 ]
  [[ "$output$stderr" != *"$GUEST_PASSWORD"* ]]
  ! grep -qF -- "$GUEST_PASSWORD" "$CALLS" || return 1
  grep -qF -- "--passwordfile $APP_GUEST_PASSWORD_FILE" "$CALLS"
}

@test "create-vm no repite el nombre del ejecutable como argumento tras -- (VirtualBox 7.2 ya lo agrega)" {
  run "$SCRIPT" vm1 2048 2 "$BASE" eth0

  [ "$status" -eq 0 ]
  local args=() i
  mapfile -d '' -t args <"$FAKE/bootstrap.args"
  for i in "${!args[@]}"; do
    if [ "${args[i]}" = "--" ]; then
      # Si se repitiera "sh", /bin/sh lo trataría como un archivo de script.
      [ "${args[i+1]}" = "-c" ]
      return 0
    fi
  done
  return 1
}

@test "AC-40 create-vm hace el bootstrap como root por Guest Additions con una sola llamada" {
  run "$SCRIPT" vm1 2048 2 "$BASE" eth0

  [ "$status" -eq 0 ]
  local args=()
  mapfile -d '' -t args <"$FAKE/bootstrap.args"
  local joined="${args[*]}"
  [[ "$joined" == *"guestcontrol vm1 run --username root --passwordfile $APP_GUEST_PASSWORD_FILE "* ]]
  # La llave pública del backend y el nombre de la VM van como argumentos separados al final.
  local n=${#args[@]}
  [ "${args[n-1]}" = "vm1" ]
  [ "${args[n-2]}" = "$PUBKEY" ]
  [ "${args[n-3]}" = "sh" ]
  # El script constante cubre los pasos de REQ-30 y no lleva datos interpolados.
  local script_text="${args[n-4]}"
  local needle
  for needle in /root/.ssh/authorized_keys "PasswordAuthentication no" \
    "PermitRootLogin prohibit-password" systemd-machine-id-setup /var/lib/dhcpcd/duid \
    ssh_host_ "ssh-keygen -A" "hostnamectl set-hostname" 127.0.1.1 "sshd -t"; do
    [[ "$script_text" == *"$needle"* ]] || { echo "falta en el bootstrap: $needle" >&2; return 1; }
  done
  [[ "$script_text" != *"AAAATESTKEYBODY"* && "$script_text" != *"vm1"* ]]
}

@test "AC-40 create-vm reinicia, olvida la llave del host y comprueba SSH en la IP" {
  run --separate-stderr "$SCRIPT" vm1 2048 2 "$BASE" eth0

  [ "$status" -eq 0 ]
  [ "$output" = "VM vm1 creada y encendida; IP 192.168.1.50" ]
  assert_before "systemd-machine-id-setup" "controlvm vm1 acpipowerbutton"
  assert_before "controlvm vm1 acpipowerbutton" "guestproperty delete vm1 /VirtualBox/GuestInfo/Net/0/V4/IP"
  assert_before "guestproperty delete vm1 /VirtualBox/GuestInfo/Net/0/V4/IP" "startvm vm1 --type headless" 2
  assert_before "startvm vm1 --type headless" "ssh-keygen -R vm1 -f $APP_SSH_KEY_PATH.known_hosts" 1
  assert_before "ssh-keygen -R vm1 -f $APP_SSH_KEY_PATH.known_hosts" "root@192.168.1.50"
  local ssh_line
  ssh_line="$(grep -E '^ssh ' "$CALLS" | head -n1)"
  [[ "$ssh_line" == *"-i $APP_SSH_KEY_PATH"* ]]
  [[ "$ssh_line" == *"HostKeyAlias=vm1"* ]]
  [[ "$ssh_line" == *"UserKnownHostsFile=$APP_SSH_KEY_PATH.known_hosts"* ]]
  [[ "$ssh_line" == *"root@192.168.1.50 true" ]]
  # El apagado ordenado bastó: no se forzó el poweroff.
  ! grep -qF -- "controlvm vm1 poweroff" "$CALLS" || return 1
}

@test "AC-40 create-vm fuerza el apagado si la VM ignora ACPI durante el reinicio" {
  IGNORE_ACPI=1 run "$SCRIPT" vm1 2048 2 "$BASE" eth0

  [ "$status" -eq 0 ]
  assert_before "controlvm vm1 acpipowerbutton" "controlvm vm1 poweroff"
  assert_before "controlvm vm1 poweroff" "guestproperty delete vm1 /VirtualBox/GuestInfo/Net/0/V4/IP"
}

@test "AC-44 create-vm limpia y sale con 5 si falla el bootstrap" {
  FAIL_BOOTSTRAP=1 run --separate-stderr "$SCRIPT" vm1 2048 2 "$BASE" eth0

  [ "$status" -eq 5 ]
  [ -z "$output" ]
  assert_cleanup
}

@test "AC-44 create-vm limpia y sale con 5 si la VM no obtiene IP tras reiniciar" {
  NO_IP=1 run --separate-stderr "$SCRIPT" vm1 2048 2 "$BASE" eth0

  [ "$status" -eq 5 ]
  [[ "$stderr" == *"no obtuvo una dirección IP"* ]]
  assert_cleanup
}

@test "AC-44 create-vm limpia y sale con 5 si la llave del backend no abre SSH" {
  stub_command ssh 'exit 255'

  run --separate-stderr "$SCRIPT" vm1 2048 2 "$BASE" eth0

  [ "$status" -eq 5 ]
  [[ "$stderr" == *"no abrió una sesión SSH en 192.168.1.50"* ]]
  assert_cleanup
}

@test "AC-44 create-vm acota sus esperas a 14 minutos" {
  # La preparación de GA y el SSH solo funcionan en su último intento, las propiedades de invitado
  # existen solo tras "guestproperty wait" y la VM ignora ACPI: es el peor caso que el script debe acotar.
  stub_command ssh 'n=$(($(cat "$FAKE/ssh.count" 2>/dev/null || echo 0) + 1))
echo "$n" >"$FAKE/ssh.count"
[[ "$n" -ge 8 ]] || exit 1'
  PROP_NEEDS_WAIT=1 GA_READY_ON=8 IGNORE_ACPI=1 run --separate-stderr "$SCRIPT" vm1 2048 2 "$BASE" eth0

  [ "$status" -eq 0 ]
  # Cada espera de propiedad tiene su límite de 180 s.
  [ "$(grep -cE 'guestproperty wait .* --timeout 180000$' "$CALLS")" -eq 2 ]
  [ "$(grep -c 'guestproperty wait' "$CALLS")" -eq 2 ]
  # Cada guestcontrol tiene su límite: 10 s por prueba de disponibilidad y 60 s el bootstrap.
  [ "$(grep -c '^VBoxManage guestcontrol vm1 run' "$CALLS")" -eq 9 ]
  [ "$(grep -c '^VBoxManage guestcontrol vm1 run' "$CALLS")" -eq "$(grep '^VBoxManage guestcontrol vm1 run' "$CALLS" | grep -c -- '--timeout')" ]
  [ "$(grep -E '^VBoxManage guestcontrol vm1 run' "$CALLS" | grep -c -- '--timeout 10000 .*--exe /bin/true')" -eq 8 ]
  [ "$(grep -E '^VBoxManage guestcontrol vm1 run' "$CALLS" | grep -c -- '--timeout 60000 ')" -eq 1 ]
  # Ocho intentos de SSH, cada uno con ConnectTimeout=10.
  [ "$(grep -cE '^ssh ' "$CALLS")" -eq 8 ]
  [ "$(grep -E '^ssh ' "$CALLS" | grep -c -- 'ConnectTimeout=10')" -eq 8 ]

  # Suma de todas las cotas: esperas de propiedades, guestcontrol, ConnectTimeout y sleeps simulados.
  local total
  total="$(
    {
      grep 'guestproperty wait' "$CALLS" | sed -n 's/.*--timeout \([0-9]*\)$/\1/p' | awk '{ t += $1 / 1000 } END { print t + 0 }'
      grep '^VBoxManage guestcontrol vm1 run' "$CALLS" | sed -n 's/.*--timeout \([0-9]*\) .*/\1/p' | awk '{ t += $1 / 1000 } END { print t + 0 }'
      grep -E '^ssh ' "$CALLS" | sed -n 's/.*ConnectTimeout=\([0-9]*\).*/\1/p' | awk '{ t += $1 } END { print t + 0 }'
      awk '$1 == "sleep" { t += $2 } END { print t + 0 }' "$CALLS"
    } | awk '{ t += $1 } END { print t }'
  )"
  echo "suma de cotas: $total s" >&2
  [ "$total" -le 730 ]
}

@test "AC-4 create-vm con intnet_name agrega la NIC 2 en la red interna y sin él no hay NIC 2" {
  run "$SCRIPT" vm1 2048 2 "$BASE" eth0 stratus-lb1

  [ "$status" -eq 0 ]
  modify_line="$(grep -F -- "modifyvm vm1" "$CALLS")"
  [[ "$modify_line" == *"--nic1 bridged --bridgeadapter1 eth0 --nic2 intnet --intnet2 stratus-lb1"* ]]

  # Otra creación desde cero: sin la VM registrada por la anterior.
  rm -rf "$FAKE"
  setup
  run "$SCRIPT" vm1 2048 2 "$BASE" eth0

  [ "$status" -eq 0 ]
  ! grep -qF -- "--nic2" "$CALLS" || return 1
  ! grep -qF -- "intnet" "$CALLS" || return 1
}

@test "AC-4 create-vm rechaza un intnet_name inválido sin llamar a VBoxManage" {
  for name in "stratus-x;y" "lb1" "stratus-" "Stratus-lb1" "stratus-$(printf 'a%.0s' {1..33})"; do
    run "$SCRIPT" vm1 2048 2 "$BASE" eth0 "$name"
    [ "$status" -eq 2 ]
  done
  run "$SCRIPT" vm1 2048 2 "$BASE" eth0 stratus-lb1 extra
  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

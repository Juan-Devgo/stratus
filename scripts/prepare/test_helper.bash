# Utilidades compartidas por las pruebas bats de scripts/prepare.
# Uso en cada archivo .bats:
#   load test_helper
#
# Funciones:
#   assert_script_structure <archivo>  verifica la estructura exigida por AC-24.
#   setup_stubs                        pone stubs de VBoxManage, ssh, ssh-keygen y sleep
#                                      al inicio del PATH; cada llamada se registra en
#                                      "$BATS_TEST_TMPDIR/calls.log".
#   stub_command <comando> <cuerpo>    agrega una salida o un código de salida a un stub.
#   setup_guest_password               exporta APP_GUEST_PASSWORD_FILE con la contraseña
#                                      conocida "$GUEST_PASSWORD" de la imagen base (REQ-22).
#   stub_vm_ip <vm> <ipv4|->           hace que el stub de VBoxManage informe la VM como
#                                      encendida con esa IP, o apagada sin IP si es "-".
#   stub_vm_nics <vm> <estado> <mac1> <ip1|-> [<mac2> <ip2|->]
#                                      simula una VM con una o dos NICs para vm_nic_ip (REQ-13).
#   stub_vm_nics <vm> <estado> <mac1> <ip1|-> [<mac2> <ip2|->]
#                                      simula una VM con una o dos NICs para vm_nic_ip (REQ-13).

# Directorio de los scripts del módulo (donde vive este helper).
PREPARE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Imprime un motivo de fallo en stderr y devuelve 1, para que bats marque la prueba fallida.
_structure_fail() {
  echo "Estructura inválida en $1: $2" >&2
  return 1
}

# Número de la primera línea que coincide con la regex extendida, o vacío si no hay.
_first_line() {
  grep -nE -m1 -- "$2" "$1" | cut -d: -f1
}

# assert_script_structure <archivo>
# Comprueba (AC-24, REQ-21):
#   1. Cabecera con "Uso:", "Argumentos:" y "Códigos de salida:".
#   2. La tabla de códigos de salida incluye el 99.
#   3. "set -euo pipefail" después de la cabecera.
#   4. Una sección "# == 1. ... ==" con al menos un paso "# 1.1 ...".
#   5. (Ya no se exige el placeholder: al implementar el script se reemplaza por el código.)
assert_script_structure() {
  local file="$1"
  [[ -f "$file" ]] || _structure_fail "$file" "el archivo no existe" || return 1

  # 1. Cabecera.
  local label
  for label in "Uso:" "Argumentos:" "Códigos de salida:"; do
    grep -qE "^#.*${label}" "$file" || _structure_fail "$file" "falta \"$label\" en la cabecera" || return 1
  done

  # 2. El código 99 aparece como línea de comentario después de "Códigos de salida:".
  local codes_line
  codes_line="$(_first_line "$file" '^#.*Códigos de salida:')"
  tail -n "+$codes_line" "$file" | grep -qE '^#[[:space:]]+99([^0-9]|$)' ||
    _structure_fail "$file" "la tabla de códigos de salida no incluye 99" || return 1

  # 3. Modo estricto después de la cabecera.
  local strict_line
  strict_line="$(_first_line "$file" '^set -euo pipefail$')"
  [[ -n "$strict_line" ]] || _structure_fail "$file" "falta \"set -euo pipefail\"" || return 1
  ((strict_line > codes_line)) ||
    _structure_fail "$file" "\"set -euo pipefail\" debe ir después de la cabecera" || return 1

  # 4. Secciones numeradas con pasos numerados.
  local section_line
  section_line="$(_first_line "$file" '^# == 1\. .+ ==$')"
  [[ -n "$section_line" ]] || _structure_fail "$file" "falta la sección \"# == 1. ... ==\"" || return 1
  grep -qE '^# 1\.1 ' "$file" || _structure_fail "$file" "falta el paso \"# 1.1 ...\"" || return 1
}

# setup_stubs
# Crea "$BATS_TEST_TMPDIR/bin" con stubs que solo registran su argv y terminan con 0.
# Cada llamada agrega una línea "<comando> <arg1> <arg2> ..." a "$BATS_TEST_TMPDIR/calls.log".
# Una prueba que necesite otra salida puede sobrescribir el stub en ese directorio.
setup_stubs() {
  local bin_dir="$BATS_TEST_TMPDIR/bin"
  mkdir -p "$bin_dir"
  : >"$BATS_TEST_TMPDIR/calls.log"

  local cmd
  for cmd in VBoxManage ssh ssh-keygen sleep; do
    # El log se fija al crear el stub para que funcione aunque el script cambie el entorno.
    cat >"$bin_dir/$cmd" <<EOF
#!/usr/bin/env bash
# Una sola escritura por llamada: varios procesos en paralelo (wait_ips) no mezclan sus líneas.
printf '%s\n' "$cmd\${*:+ \$*}" >>"$BATS_TEST_TMPDIR/calls.log"
EOF
    chmod +x "$bin_dir/$cmd"
  done

  export PATH="$bin_dir:$PATH"
}

# stub_command <comando> <cuerpo>
# Agrega <cuerpo> (código bash) al final de un stub ya creado por setup_stubs, para que
# además de registrar su argv imprima una salida o termine con otro código.
# Ejemplo: stub_command VBoxManage '[[ "$1" == showvminfo ]] && exit 1'
stub_command() {
  printf '%s\n' "$2" >>"$BATS_TEST_TMPDIR/bin/$1"
}

# setup_guest_password
# Simula el archivo de contraseña de la imagen base: exporta APP_GUEST_PASSWORD_FILE (archivo
# temporal de la prueba, permisos 600) y guarda el texto en GUEST_PASSWORD para que la prueba
# compruebe que nunca aparece en argv, en calls.log ni en la salida (REQ-22, AC-45).
setup_guest_password() {
  GUEST_PASSWORD="Pr0ebaClave+/xyz"
  export APP_GUEST_PASSWORD_FILE="$BATS_TEST_TMPDIR/guest-password"
  printf '%s' "$GUEST_PASSWORD" >"$APP_GUEST_PASSWORD_FILE"
  chmod 600 "$APP_GUEST_PASSWORD_FILE"
}

# stub_vm_ip <vm> <ipv4|->
# Agrega al stub de VBoxManage las respuestas que usa un script para conocer la IP de <vm>:
#   - con una IPv4: "showvminfo <vm> --machinereadable" informa VMState="running" y
#     "guestproperty get <vm> /VirtualBox/GuestInfo/Net/0/V4/IP" imprime "Value: <ipv4>".
#   - con "-": informa VMState="poweroff" y la propiedad imprime "No value set!".
# Las demás llamadas siguen su curso. Va antes de cualquier stub_command que termine con "exit".
stub_vm_ip() {
  local vm="$1" ip="$2" state="poweroff" value="No value set!"
  if [[ "$ip" != "-" ]]; then
    state="running"
    value="Value: $ip"
  fi
  stub_command VBoxManage "if [[ \"\$1\" == showvminfo && \"\${2:-}\" == \"$vm\" ]]; then
  printf 'VMState=\"$state\"\\n'
  exit 0
fi
if [[ \"\$1 \${2:-}\" == \"guestproperty get\" && \"\${3:-}\" == \"$vm\" && \"\${4:-}\" == /VirtualBox/GuestInfo/Net/0/V4/IP ]]; then
  echo '$value'
  exit 0
fi"
}

# stub_vm_nics <vm> <estado> <mac1> <ip1|-> [<mac2> <ip2|->]
# Agrega al stub de VBoxManage las respuestas de una VM con una o dos NICs, como las ve vm_nic_ip:
#   - <estado> es running o poweroff (VMState de "showvminfo --machinereadable").
#   - showvminfo informa macaddress1 (y macaddress2 con dos NICs); sin la segunda NIC informa
#     nic2="none", que es el caso de una sola NIC (se lee /Net/0 como siempre).
#   - Con una NIC, Guest Additions publica el adaptador 1 en /Net/0. Con dos, publica el adaptador
#     2 en /Net/0 y el adaptador 1 en /Net/1, como pasa en un servidor real (Net/0 es la NIC NAT);
#     así una prueba distingue "buscar por MAC" de "leer siempre /Net/0".
#   - Una IP "-" hace que la propiedad responda "No value set!". Las propiedades se publican aunque
#     la VM esté apagada (una IP antigua), para comprobar que se mira VMState primero.
stub_vm_nics() {
  local vm="$1" state="$2" mac1="$3" ip1="$4" mac2="${5:-}" ip2="${6:-}"
  local info="VMState=\"$state\"\nnic1=\"bridged\"\nmacaddress1=\"$mac1\"\n"
  local -a props=()
  if [[ -n "$mac2" ]]; then
    info+="nic2=\"nat\"\nmacaddress2=\"$mac2\"\n"
    props=("0 $mac2 $ip2" "1 $mac1 $ip1")
  else
    info+="nic2=\"none\"\n"
    props=("0 $mac1 $ip1")
  fi
  local body="if [[ \"\$1\" == showvminfo && \"\${2:-}\" == \"$vm\" ]]; then printf '$info'; exit 0; fi
if [[ \"\$1 \${2:-}\" == \"guestproperty get\" && \"\${3:-}\" == \"$vm\" ]]; then
  case \"\${4:-}\" in"
  local entry index mac ip value
  for entry in "${props[@]}"; do
    read -r index mac ip <<<"$entry"
    value="Value: $ip"
    [[ "$ip" != "-" ]] || value="No value set!"
    body+="
    /VirtualBox/GuestInfo/Net/$index/MAC) echo 'Value: $mac'; exit 0 ;;
    /VirtualBox/GuestInfo/Net/$index/V4/IP) echo '$value'; exit 0 ;;"
  done
  body+="
    *) echo 'No value set!'; exit 0 ;;
  esac
fi"
  stub_command VBoxManage "$body"
}

# stub_jump_topology <lb> <estado_lb> <vm> <estado_vm>
# Simula un servidor <vm> asignado al balanceador <lb> (004), para las pruebas de "--jump":
#   - el balanceador tiene su IPv4 en puente 192.168.1.60 (adaptador 1) y la red interna sin IP;
#   - el servidor tiene la IP interna 10.10.1.11 (adaptador 1) y la NIC NAT 10.0.3.15.
# Los estados son running o poweroff. Se llama una vez por prueba, después de setup_stubs.
stub_jump_topology() {
  stub_vm_nics "$1" "$2" 080027CCCCCC 192.168.1.60 080027DDDDDD -
  stub_vm_nics "$3" "$4" 080027AAAAAA 10.10.1.11 080027BBBBBB 10.0.3.15
}

# assert_routed_ssh <lb> <vm>
# Comprueba que hubo llamadas a ssh y que TODAS pasan por jump@192.168.1.60 (ProxyCommand con
# HostKeyAlias=<lb>) hacia root@10.10.1.11 con HostKeyAlias=<vm> (stub_jump_topology).
assert_routed_ssh() {
  local total routed
  total="$(grep -cE '^ssh ' "$BATS_TEST_TMPDIR/calls.log" || true)"
  routed="$(grep -E '^ssh ' "$BATS_TEST_TMPDIR/calls.log" |
    grep -F -- "-o HostKeyAlias=$1 -W %h:%p jump@192.168.1.60" |
    grep -cF -- "-o HostKeyAlias=$2 root@10.10.1.11 -- " || true)"
  [ "$total" -gt 0 ] || { echo "no hubo llamadas a ssh" >&2; return 1; }
  [ "$total" -eq "$routed" ] || { echo "$routed de $total llamadas a ssh pasan por jump" >&2; return 1; }
}

# assert_server_calls_routed <lb> <vm>
# Como assert_routed_ssh, pero para operaciones que además escriben en el balanceador (jump_sync):
# toda llamada a ssh hacia root@10.10.1.11 pasa por jump@192.168.1.60 con HostKeyAlias=<lb>, y debe
# haber al menos una. Las llamadas directas al balanceador (root@192.168.1.60) se permiten.
assert_server_calls_routed() {
  local server_calls routed
  server_calls="$(grep -E '^ssh ' "$BATS_TEST_TMPDIR/calls.log" | grep -cF -- "root@10.10.1.11 -- " || true)"
  routed="$(grep -E '^ssh ' "$BATS_TEST_TMPDIR/calls.log" | grep -F -- "root@10.10.1.11 -- " |
    grep -F -- "-o HostKeyAlias=$1 -W %h:%p jump@192.168.1.60" | grep -cF -- "-o HostKeyAlias=$2 root@10.10.1.11 -- " || true)"
  [ "$server_calls" -gt 0 ] || { echo "no hubo llamadas a ssh hacia el servidor" >&2; return 1; }
  [ "$server_calls" -eq "$routed" ] || { echo "$routed de $server_calls llamadas al servidor pasan por jump" >&2; return 1; }
}

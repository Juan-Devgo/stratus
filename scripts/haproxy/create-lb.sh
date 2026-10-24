#!/usr/bin/env bash
# create-lb.sh - Crea un balanceador HAProxy: una VM como las de 001 (create-vm.sh) con una segunda
# NIC en su propia red interna de VirtualBox "stratus-<lb>" con servidor DHCP, la deja con la IP fija
# 10.10.N.1/24 en esa red, crea la cuenta "jump" restringida a reenviar conexiones al puerto 22 de la
# red interna y le deja un haproxy.cfg inicial (puerto 80, sin servidores). Si un paso falla, borra la
# VM, su disco diferencial y la red interna; el disco base nunca se modifica.
#
# Uso:
#   create-lb.sh <lb> <memory_mb> <cpus> <base_disk> <bridge_adapter> <reserved>
#
# Argumentos:
#   lb              nombre del balanceador (y de su VM), ^[a-z0-9][a-z0-9-]{0,31}$
#   memory_mb       memoria en MB, entero entre 512 y 8192
#   cpus            número de CPUs, entero entre 1 y 4
#   base_disk       ruta absoluta del disco base de HAProxy (APP_HAPROXY_BASE_DISK en el backend)
#   bridge_adapter  interfaz del host para la red en puente (APP_BRIDGE_ADAPTER), ^[A-Za-z0-9._:-]{1,64}$
#   reserved        subredes N ya usadas por balanceadores de la app: "-" (ninguna) o "1,3,..."
#
# Variables de entorno:
#   APP_GUEST_PASSWORD_FILE  archivo del host con la contraseña de root de la imagen base (lo lee
#                            create-vm.sh; nunca va en argv)
#   APP_SSH_KEY_PATH         llave privada del backend; se exige <ruta>.pub
#
# Salida:
#   stdout  "Balanceador <lb> creado y encendido; IP <ipv4>; subred 10.10.<N>.0/24" y, en la
#           última línea, "subnet=<N>"
#   stderr  diagnósticos
#
# Códigos de salida:
#   0   éxito
#   2   argumentos inválidos o variables de entorno faltantes
#   4   fallo de VBoxManage (lista de servidores DHCP o creación del servidor DHCP)
#   5   falló un paso (la VM y la red interna ya se borraron); también el código de create-vm.sh
#   6   disco base de HAProxy no encontrado, interfaz de puente no encontrada o VM ya registrada
#   8   no hay subredes internas libres
#   9   falló un paso y también la limpieza: puede quedar la VM o la red interna
#   13  la red interna stratus-<lb> ya tiene un servidor DHCP en VirtualBox
#   99  operación no implementada
#
# Tiempo máximo: create-vm.sh puede tardar hasta 14 minutos y los pasos por SSH cerca de 1 más; si
# el backend lo detiene por tiempo recibe SIGTERM y la limpieza de la sección 4 se ejecuta igual.

set -euo pipefail

scripts_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=../prepare/lib/vm-exec.sh
source "$scripts_dir/prepare/lib/vm-exec.sh"
# shellcheck source=lib/haproxy-common.sh
source "$scripts_dir/haproxy/lib/haproxy-common.sh"
# shellcheck source=lib/jump-keys.sh
source "$scripts_dir/haproxy/lib/jump-keys.sh"

# == 1. Validar argumentos ==
# 1.1 Exigir exactamente 6 argumentos; si no, imprimir el uso en stderr y salir con 2.
# 1.2 Validar cada valor contra su lista permitida (haproxy-common.sh); si alguno falla, salir con 2.
# 1.3 Exigir que APP_GUEST_PASSWORD_FILE sea un archivo legible y que exista
#     "$APP_SSH_KEY_PATH.pub"; si no, salir con 2.
# Ninguna validación fallida debe llamar a VBoxManage, a ssh ni a create-vm.sh.

if (($# != 6)); then
  fail 2 "Uso: create-lb.sh <lb> <memory_mb> <cpus> <base_disk> <bridge_adapter> <reserved>"
fi
lb="$1"
memory_mb="$2"
cpus="$3"
base_disk="$4"
bridge_adapter="$5"
reserved="$6"

validate_lb_name "$lb" || exit 2
validate_memory "$memory_mb" || exit 2
validate_cpus "$cpus" || exit 2
validate_base_disk "$base_disk" || exit 2
validate_bridge "$bridge_adapter" || exit 2
validate_reserved "$reserved" || exit 2

if [[ -z "${APP_GUEST_PASSWORD_FILE:-}" || ! -f "$APP_GUEST_PASSWORD_FILE" || ! -r "$APP_GUEST_PASSWORD_FILE" ]]; then
  fail 2 "APP_GUEST_PASSWORD_FILE debe ser un archivo legible"
fi
if [[ -z "${APP_SSH_KEY_PATH:-}" || ! -r "$APP_SSH_KEY_PATH.pub" ]]; then
  fail 2 "No se encontró la llave pública del backend (APP_SSH_KEY_PATH.pub)"
fi

# == 2. Comprobaciones antes de crear nada ==
# Ninguna de estas comprobaciones crea una VM, una red ni toca el disco base.
# 2.1 Si base_disk no es un archivo legible, imprimir "Disco base de HAProxy no encontrado: <ruta>" y
#     salir con 6.
# 2.2 Si "VBoxManage list vms" ya tiene una VM con el nombre <lb>, imprimir "VM ya registrada en
#     VirtualBox" y salir con 6: la limpieza de la sección 4 borra la VM <lb>, así que nunca puede
#     empezar con una VM ajena.
# 2.3 Leer "VBoxManage list dhcpservers" (salir con 4 si falla). Si ya existe un servidor DHCP en la
#     red "stratus-<lb>", imprimir "Red interna ya registrada en VirtualBox" y salir con 13.
# 2.4 Elegir la subred N más baja entre 1 y 254 que no esté en reserved ni aparezca como 10.10.N.x en
#     esa lista (servidores DHCP de VirtualBox que la app no conoce). Si no queda ninguna, imprimir
#     "No hay subredes internas libres" y salir con 8.

if [[ ! -f "$base_disk" || ! -r "$base_disk" ]]; then
  fail 6 "Disco base de HAProxy no encontrado: $base_disk"
fi

registered="$(VBoxManage list vms)" || fail 4 "VBoxManage list vms falló"
if sed -n 's/^"\(.*\)" {[^}]*}$/\1/p' <<<"$registered" | grep -qxF -- "$lb"; then
  fail 6 "VM ya registrada en VirtualBox"
fi

dhcp_servers="$(VBoxManage list dhcpservers)" || fail 4 "VBoxManage list dhcpservers falló"
if grep -qE "^NetworkName:[[:space:]]*stratus-$lb[[:space:]]*\$" <<<"$dhcp_servers"; then
  fail 13 "Red interna ya registrada en VirtualBox"
fi

declare -A used=()
if [[ "$reserved" != "-" ]]; then
  IFS=, read -ra reserved_items <<<"$reserved"
  for n in "${reserved_items[@]}"; do used[$((10#$n))]=1; done
fi
while IFS= read -r n; do
  [[ -n "$n" ]] && used[$((10#$n))]=1
done < <(sed -n 's/.*10\.10\.\([0-9]\{1,3\}\)\.[0-9]\{1,3\}.*/\1/p' <<<"$dhcp_servers")

subnet=""
for ((n = 1; n <= 254; n++)); do
  if [[ -z "${used[$n]:-}" ]]; then
    subnet="$n"
    break
  fi
done
[[ -n "$subnet" ]] || fail 8 "No hay subredes internas libres"

# == 3. Limpieza en caso de fallo ==
# Se ejecuta al salir con un código distinto de cero, una vez creado el servidor DHCP (sección 4).
# Conserva el código original; si la propia limpieza falla, sale con 9.
# 3.1 Si ya se lanzó create-vm.sh, "../prepare/delete-vm.sh <lb>" (apaga, suelta y borra el disco
#     diferencial y desregistra la VM; el disco base no se toca; es idempotente).
# 3.2 "VBoxManage dhcpserver remove --network=stratus-<lb>".

vm_launched=no

cleanup() {
  local status=$? cleanup_failed=no
  trap - EXIT INT TERM
  if ((status == 0)); then
    return 0
  fi
  echo "Falló la creación de $lb; limpiando" >&2
  if [[ "$vm_launched" == yes ]]; then
    "$scripts_dir/prepare/delete-vm.sh" "$lb" >&2 || cleanup_failed=yes
  fi
  VBoxManage dhcpserver remove --network="stratus-$lb" >&2 || cleanup_failed=yes
  if [[ "$cleanup_failed" == yes ]]; then
    echo "La limpieza falló; revise VirtualBox: puede quedar la VM $lb o la red stratus-$lb" >&2
    exit 9
  fi
  exit "$status"
}

# == 4. Crear la red y la VM ==
# 4.1 "VBoxManage dhcpserver add --network=stratus-<lb> --server-ip=10.10.N.2 --netmask=255.255.255.0
#     --lower-ip=10.10.N.10 --upper-ip=10.10.N.200 --enable". Si falla no hay nada que limpiar:
#     salir con 4. La IP .1 es del balanceador y .2 del servidor DHCP, fuera del rango que reparte.
# 4.2 Desde aquí, instalar el "trap" de la sección 3.
# 4.3 "../prepare/create-vm.sh <lb> <memory_mb> <cpus> <base_disk> <bridge_adapter> stratus-<lb>": el
#     bootstrap de 001 (llave del backend, sin contraseña por SSH) con la NIC 2 en la red interna.
#     Cualquier fallo conserva su código.

VBoxManage dhcpserver add --network="stratus-$lb" --server-ip="10.10.$subnet.2" \
  --netmask=255.255.255.0 --lower-ip="10.10.$subnet.10" --upper-ip="10.10.$subnet.200" --enable >&2 ||
  fail 4 "No se pudo crear el servidor DHCP de la red stratus-$lb"

trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

vm_launched=yes
"$scripts_dir/prepare/create-vm.sh" "$lb" "$memory_mb" "$cpus" "$base_disk" "$bridge_adapter" "stratus-$lb" >&2

# == 5. Configurar el balanceador por SSH ==
# Cada paso usa vm_exec directo al balanceador como root con la llave del backend; si falla, el
# script sale con 5 y la sección 3 limpia.
# 5.1 Leer la MAC del adaptador 2 ("macaddress2" de showvminfo) y buscar en el invitado la interfaz que
#     tiene esa MAC (/sys/class/net/*/address): el nombre no se puede suponer.
# 5.2 Escribir /etc/network/interfaces.d/stratus-internal con la IP fija 10.10.N.1/24 en esa interfaz
#     y levantarla con ifup.
# 5.3 Crear la cuenta de sistema: "useradd --system --create-home --home-dir /var/lib/jump --shell
#     /usr/sbin/nologin jump". Sin contraseña (queda bloqueada) y con UID menor que 1000.
# 5.4 Escribir /var/lib/jump/.ssh/authorized_keys (700/600, dueño jump) con la llave del backend
#     "restrict,port-forwarding <tipo> <llave> stratus-backend".
# 5.5 Escribir /etc/ssh/sshd_config.d/20-stratus-jump.conf: jump solo reenvía al puerto 22 de
#     10.10.N.10 a 10.10.N.200, sin TTY, sin comandos y sin otros reenvíos. Revisar con "sshd -t"
#     (si falla, borrar el archivo) y "systemctl reload ssh".
# 5.6 Generar el haproxy.cfg inicial (puerto 80, sin servidores), aplicarlo con apply_cfg (revisión y
#     recarga) y "systemctl enable --now haproxy".

# lbx <comando...>: vm_exec directo al balanceador; cualquier fallo sale con 5.
lbx() {
  local rc=0
  vm_exec "$lb" "$@" || rc=$?
  if ((rc != 0)); then
    fail 5 "Falló un paso en el balanceador $lb (código $rc): $1"
  fi
}

nic2_mac="$(VBoxManage showvminfo "$lb" --machinereadable | sed -n 's/^macaddress2="\(.*\)"$/\1/p')"
if [[ ! "$nic2_mac" =~ ^[0-9A-Fa-f]{12}$ ]]; then
  fail 5 "No se pudo leer la MAC del adaptador 2 de $lb"
fi
nic2_mac="$(tr 'A-F' 'a-f' <<<"$nic2_mac" | sed 's/\(..\)/\1:/g; s/:$//')"

nic2_name="$(lbx sh -c 'for d in /sys/class/net/*; do if [ "$(cat "$d/address" 2>/dev/null)" = "$1" ]; then echo "${d##*/}"; exit 0; fi; done; exit 1' sh "$nic2_mac" </dev/null)"
if [[ ! "$nic2_name" =~ ^[A-Za-z0-9._-]{1,15}$ ]]; then
  fail 5 "No se encontró la interfaz del adaptador 2 de $lb"
fi

printf 'auto %s\niface %s inet static\n  address 10.10.%s.1/24\n' "$nic2_name" "$nic2_name" "$subnet" |
  lbx sh -c 'cat >"$1" && ifup "$2"' sh /etc/network/interfaces.d/stratus-internal "$nic2_name"

lbx useradd --system --create-home --home-dir /var/lib/jump --shell /usr/sbin/nologin jump </dev/null

read -r key_type key_blob _ <"$APP_SSH_KEY_PATH.pub"
printf 'restrict,port-forwarding %s %s stratus-backend\n' "$key_type" "$key_blob" |
  lbx sh -c 'd=$(dirname "$1") && mkdir -p "$d" && chmod 700 "$d" && cat >"$1" && chmod 600 "$1" && chown -R jump:jump "$d"' sh "$JUMP_AUTHORIZED_KEYS"

permit_open=""
for ((n = 10; n <= 200; n++)); do
  permit_open+=" 10.10.$subnet.$n:22"
done
{
  echo "Match User jump"
  echo "  AllowTcpForwarding local"
  echo "  PermitOpen${permit_open}"
  echo "  PermitTTY no"
  echo "  X11Forwarding no"
  echo "  AllowAgentForwarding no"
  echo "  AllowStreamLocalForwarding no"
  echo "  PermitTunnel no"
  echo "  GatewayPorts no"
  echo "  ForceCommand /usr/sbin/nologin"
} | lbx sh -c 'cat >"$1" && { sshd -t || { rm -f "$1"; exit 1; }; } && systemctl reload ssh' sh /etc/ssh/sshd_config.d/20-stratus-jump.conf

render_cfg 80 | apply_cfg "$lb" 80 || fail 5 "No se pudo aplicar el haproxy.cfg inicial en $lb"
lbx systemctl enable --now haproxy </dev/null

# == 6. Resultado ==
# 6.1 Leer la IPv4 en puente del balanceador (adaptador 1) con vm_nic_ip.
# 6.2 Imprimir en stdout "Balanceador <lb> creado y encendido; IP <ip>; subred 10.10.<N>.0/24" y, en
#     la última línea, "subnet=<N>": el backend la guarda como subred del balanceador.

lb_ip="$(vm_nic_ip "$lb" 1)" || fail 5 "El balanceador $lb no tiene dirección IP en puente"
echo "Balanceador $lb creado y encendido; IP $lb_ip; subred 10.10.$subnet.0/24"
echo "subnet=$subnet"

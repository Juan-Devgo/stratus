#!/usr/bin/env bash
# create-dns.sh - Crea el servidor DNS (005): una VM como las de 001 (create-vm.sh) sobre el disco
# base de DNS, con la IPv4 estática de APP_DNS_IP en su adaptador en puente, bind9 autoritativo para
# la zona y reenviador para el resto, y los registros A iniciales. Si un paso falla, borra la VM y
# su disco diferencial; el disco base nunca se modifica.
#
# Uso:
#   create-dns.sh <dns> <memory_mb> <cpus> <base_disk> <bridge_adapter> <dns_cidr> <gateway> <domain> <forwarders> [record...]
#
# Argumentos:
#   dns             nombre del servidor DNS (y de su VM), ^[a-z0-9][a-z0-9-]{0,31}$
#   memory_mb       memoria en MB, entero entre 512 y 8192
#   cpus            número de CPUs, entero entre 1 y 4
#   base_disk       ruta absoluta del disco base de DNS (APP_DNS_BASE_DISK)
#   bridge_adapter  interfaz del host para la red en puente (APP_BRIDGE_ADAPTER), ^[A-Za-z0-9._:-]{1,64}$
#   dns_cidr        IPv4 estática con prefijo 8-30 (APP_DNS_IP), por ejemplo 192.168.1.53/24
#   gateway         puerta de enlace, una IPv4 de la subred (APP_DNS_GATEWAY)
#   domain          zona (APP_DNS_DOMAIN): 1-4 etiquetas ^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$
#   forwarders      1 a 3 IPv4 separadas por comas (APP_DNS_FORWARDERS)
#   record          registro A inicial "<vm>,<ipv4>" (hasta 200, sin repetir VMs ni el servidor DNS)
#
# Variables de entorno:
#   APP_GUEST_PASSWORD_FILE  archivo del host con la contraseña de root de la imagen base (lo lee
#                            create-vm.sh; nunca va en argv)
#   APP_SSH_KEY_PATH         llave privada del backend
#
# Salida:
#   stdout  "Servidor DNS <dns> creado en <ipv4>; zona <domain>"
#   stderr  diagnósticos
#
# Códigos de salida:
#   0   éxito
#   2   argumentos inválidos, variables de entorno faltantes o "Configuración de DNS incompleta"
#   4   fallo de VBoxManage o de SSH, o arping no instalado en el host
#   5   falló un paso (la VM ya se borró); también el código de create-vm.sh
#   6   disco base no encontrado, interfaz de puente no encontrada o VM ya registrada
#   7   la VM quedó sin dirección IP (la VM ya se borró)
#   9   falló un paso y también la limpieza: puede quedar la VM
#   13  la IP del servidor DNS ya está en uso
#   99  operación no implementada
#
# Tiempo máximo: create-vm.sh puede tardar hasta 14 minutos, más el reinicio (hasta 3 minutos de
# espera de la IP estática); si el backend lo detiene por tiempo recibe SIGTERM y la limpieza de la
# sección 3 se ejecuta igual.

set -euo pipefail

scripts_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=../prepare/lib/vm-exec.sh
source "$scripts_dir/prepare/lib/vm-exec.sh"
# shellcheck source=lib/dns-common.sh
source "$scripts_dir/dns/lib/dns-common.sh"

# == 1. Validar argumentos ==
# 1.1 Exigir al menos 9 argumentos; si no, imprimir el uso en stderr y salir con 2.
# 1.2 Validar dns, memoria, CPUs, disco e interfaz contra sus listas permitidas; si alguno falla,
#     salir con 2.
# 1.3 Validar dns_cidr, gateway, domain y forwarders; si alguno falla, imprimir solo "Configuración
#     de DNS incompleta" y salir con 2 (REQ-15).
# 1.4 Validar los registros iniciales (cada uno dentro de la subred de dns_cidr, REQ-22); exigir
#     APP_GUEST_PASSWORD_FILE legible y APP_SSH_KEY_PATH.
# Ninguna validación fallida llama a VBoxManage, ssh, arping, ping ni create-vm.sh.

if (($# < 9)); then
  fail 2 "Uso: create-dns.sh <dns> <memory_mb> <cpus> <base_disk> <bridge_adapter> <dns_cidr> <gateway> <domain> <forwarders> [record...]"
fi
dns="$1"
memory_mb="$2"
cpus="$3"
base_disk="$4"
bridge_adapter="$5"
dns_cidr="$6"
gateway="$7"
domain="$8"
forwarders="$9"
shift 9
records=("$@")

validate_vm_name "$dns" || exit 2
validate_memory "$memory_mb" || exit 2
validate_cpus "$cpus" || exit 2
validate_base_disk "$base_disk" || exit 2
validate_bridge "$bridge_adapter" || exit 2

if ! { validate_cidr "$dns_cidr" && validate_gateway "$dns_cidr" "$gateway" && validate_domain "$domain" &&
  validate_forwarders "$forwarders"; } 2>/dev/null; then
  fail 2 "Configuración de DNS incompleta"
fi
dns_ip="${dns_cidr%/*}"

validate_records "$dns" "${records[@]}" || exit 2
validate_lan_records "$dns_cidr" "${records[@]}" || exit 2
if [[ -z "${APP_GUEST_PASSWORD_FILE:-}" || ! -f "$APP_GUEST_PASSWORD_FILE" || ! -r "$APP_GUEST_PASSWORD_FILE" ]]; then
  fail 2 "APP_GUEST_PASSWORD_FILE debe ser un archivo legible"
fi
if [[ -z "${APP_SSH_KEY_PATH:-}" ]]; then
  fail 2 "Falta la variable de entorno APP_SSH_KEY_PATH"
fi

# == 2. Comprobaciones antes de crear nada ==
# 2.1 Si base_disk no es un archivo legible, imprimir "Disco base de DNS no encontrado" y salir con 6.
# 2.2 Si "VBoxManage list vms" ya tiene una VM <dns>, imprimir "VM ya registrada en VirtualBox" y
#     salir con 6: la limpieza borra la VM <dns>, así que nunca puede empezar con una VM ajena.
# 2.3 ip_in_use <bridge_adapter> <ip>: una sonda ARP y un ping de 1 s; cualquier respuesta sale con
#     13 y arping ausente con 4 (REQ-15).

if [[ ! -f "$base_disk" || ! -r "$base_disk" ]]; then
  fail 6 "Disco base de DNS no encontrado"
fi

registered="$(VBoxManage list vms)" || fail 4 "VBoxManage list vms falló"
if sed -n 's/^"\(.*\)" {[^}]*}$/\1/p' <<<"$registered" | grep -qxF -- "$dns"; then
  fail 6 "VM ya registrada en VirtualBox"
fi

ip_in_use "$bridge_adapter" "$dns_ip" || exit $?

# == 3. Limpieza en caso de fallo ==
# Se ejecuta al salir con un código distinto de cero, una vez lanzado create-vm.sh. Conserva el
# código original; si la propia limpieza falla, sale con 9.
# 3.1 "../prepare/delete-vm.sh <dns>" (apaga, suelta y borra el disco diferencial y desregistra la
#     VM; el disco base no se toca; es idempotente).

cleanup() {
  local status=$?
  trap - EXIT INT TERM
  if ((status == 0)); then
    return 0
  fi
  echo "Falló la creación de $dns; limpiando" >&2
  if ! "$scripts_dir/prepare/delete-vm.sh" "$dns" >&2; then
    echo "La limpieza falló; revise VirtualBox: puede quedar la VM $dns" >&2
    exit 9
  fi
  exit "$status"
}

# == 4. Crear la VM ==
# 4.1 Desde aquí, instalar el "trap" de la sección 3.
# 4.2 "../prepare/create-vm.sh <dns> <memory_mb> <cpus> <base_disk> <bridge_adapter>": red en
#     puente con DHCP y el bootstrap de 001 (llave del backend). Cualquier fallo conserva su código.

trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

"$scripts_dir/prepare/create-vm.sh" "$dns" "$memory_mb" "$cpus" "$base_disk" "$bridge_adapter" >&2

# dnsx <comando...>: vm_exec en el servidor DNS; 2, 4 y 7 salen tal cual y otro fallo sale con 5.
dnsx() {
  local rc=0
  vm_exec "$dns" "$@" || rc=$?
  case "$rc" in
    0) ;;
    2 | 4 | 7) exit "$rc" ;;
    *) fail 5 "Falló un paso en el servidor DNS $dns (código $rc): $1" ;;
  esac
}

# write_file <ruta>: stdin -> archivo de la VM.
write_file() {
  dnsx sh -c 'cat >"$1"' sh "$1"
}

# == 5. IP estática (REQ-14) ==
# 5.1 Buscar la interfaz del adaptador 1 por su MAC ("macaddress1" de showvminfo) en
#     /sys/class/net de la VM; si no aparece, salir con 5.
# 5.2 /etc/network/interfaces queda con "source /etc/network/interfaces.d/*" y el loopback;
#     /etc/network/interfaces.d/stratus-static tiene "auto", "iface <if> inet static", "address
#     <dns_cidr>" y "gateway <gateway>". Se aplica en el reinicio de la sección 7.
# 5.3 /etc/resolv.conf: "nameserver 127.0.0.1" (el propio bind9).

info="$(VBoxManage showvminfo "$dns" --machinereadable)" || fail 4 "VBoxManage showvminfo falló"
mac="$(sed -n 's/^macaddress1="\(.*\)"$/\1/p' <<<"$info")"
mac="$(sed 's/../&:/g; s/:$//' <<<"${mac,,}")"
iface="$(dnsx sh -c 'for d in /sys/class/net/*; do [ "$(cat "$d/address")" = "$1" ] && basename "$d"; done; true' sh "$mac" </dev/null)"
iface="$(head -n 1 <<<"$iface")"
if [[ ! "$iface" =~ ^[A-Za-z0-9._-]{1,15}$ ]]; then
  fail 5 "No se encontró la interfaz del adaptador en puente en $dns"
fi

printf '%s\n' "# Generado por Stratus: la interfaz en puente está en interfaces.d/stratus-static." \
  "source /etc/network/interfaces.d/*" "" "auto lo" "iface lo inet loopback" | write_file /etc/network/interfaces
printf '%s\n' "auto $iface" "iface $iface inet static" "  address $dns_cidr" "  gateway $gateway" |
  write_file /etc/network/interfaces.d/stratus-static
printf 'nameserver 127.0.0.1\n' | write_file /etc/resolv.conf

# == 6. bind9 (REQ-16, REQ-20) ==
# 6.1 /etc/bind/named.conf.options con render_options y /etc/bind/named.conf.local con la zona
#     "<domain>" (type master, file "/var/lib/bind/db.<domain>").
# 6.2 La zona de render_zone con los registros iniciales, de bind:bind.
# 6.3 "named-checkconf" y "named-checkzone <domain> <zona>"; luego "systemctl enable named".

zone_file="/var/lib/bind/db.$domain"
render_options "$dns_cidr" "$forwarders" | write_file /etc/bind/named.conf.options
printf '%s\n' "zone \"$domain\" {" "  type master;" "  file \"$zone_file\";" "};" | write_file /etc/bind/named.conf.local
render_zone "$dns" "$domain" "$dns_ip" "${records[@]}" | write_file "$zone_file"
dnsx chown bind:bind "$zone_file" </dev/null
dnsx named-checkconf </dev/null >&2
dnsx named-checkzone "$domain" "$zone_file" </dev/null >&2
dnsx systemctl enable named </dev/null >&2

# == 7. Reiniciar con la IP estática (REQ-14) ==
# 7.1 "../prepare/stop-vm.sh <dns>"; borrar la propiedad de la IP vieja de Guest Additions para no
#     leerla después; "VBoxManage startvm <dns> --type headless".
# 7.2 Esperar hasta 90 lecturas, cada 2 s, a que vm_nic_ip <dns> 1 sea la IP estática; si no, salir
#     con 5.
# 7.3 Hasta 8 intentos de SSH (vm_exec ... true, ConnectTimeout=10) cada 2 s; si ninguno entra,
#     salir con 5.

"$scripts_dir/prepare/stop-vm.sh" "$dns" >&2
VBoxManage guestproperty delete "$dns" /VirtualBox/GuestInfo/Net/0/V4/IP >&2 ||
  fail 4 "No se pudo borrar la IP anterior de $dns"
VBoxManage startvm "$dns" --type headless >&2 || fail 4 "No se pudo encender $dns"

current=""
for ((poll = 1; poll <= 90; poll++)); do
  current="$(vm_nic_ip "$dns" 1 2>/dev/null)" || current=""
  [[ "$current" == "$dns_ip" ]] && break
  sleep 2
done
if [[ "$current" != "$dns_ip" ]]; then
  fail 5 "El servidor DNS $dns no tomó la IP estática $dns_ip"
fi

ssh_ok=no
for ((try = 1; try <= 8; try++)); do
  if vm_exec "$dns" true </dev/null >/dev/null 2>&1; then
    ssh_ok=yes
    break
  fi
  sleep 2
done
if [[ "$ssh_ok" != yes ]]; then
  fail 5 "El servidor DNS $dns no responde por SSH en $dns_ip"
fi

# == 8. Comprobar y terminar ==
# 8.1 "dig @<ip> <dns>.<domain> A +short" en la VM debe devolver la IP; si no, salir con 5.
# 8.2 Imprimir el mensaje.

answer="$(dnsx dig "@$dns_ip" "$dns.$domain" A +short </dev/null)"
if ! grep -qxF -- "$dns_ip" <<<"$answer"; then
  fail 5 "bind9 no responde con la IP de $dns.$domain"
fi
echo "Servidor DNS $dns creado en $dns_ip; zona $domain"

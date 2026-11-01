#!/usr/bin/env bash
# dns-common.sh - Biblioteca compartida de scripts/dns (005): validadores (P3: la misma lista
# permitida que la UI y el backend), la prueba de IP en uso, la configuración de bind9, la zona
# completa y su aplicación con respaldo, y la espera de IPs.
#
# Uso: desde un script de scripts/dns que ya cargó vm-exec.sh
#   source "$(dirname "${BASH_SOURCE[0]}")/lib/dns-common.sh"
#
# Argumentos: los de cada función
#   fail, validate_vm_name, validate_memory, validate_cpus, validate_base_disk, validate_bridge,
#   validate_domain, parse_jump      de scripts/web/lib/web-common.sh (se carga aquí)
#   validate_ipv4 <ip>                     IPv4 sin ceros a la izquierda
#   validate_cidr <ip/prefijo>             IPv4 con prefijo 8-30, distinta de la red y la difusión
#   validate_gateway <ip/prefijo> <gw>     IPv4 de la subred, distinta de la red, la difusión y la IP
#   validate_forwarders <lista>            1 a 3 IPv4 separadas por comas
#   validate_records <dns> [registro...]   "<vm>,<ipv4>", hasta 200, VMs únicas, ninguna es <dns>
#   validate_wait_vms <lista>              "-" o nombres de VM separados por comas
#   ip_in_use <interfaz> <ip>              sonda ARP e ICMP de 1 s cada una (REQ-15)
#   render_options <ip/prefijo> <lista>    imprime /etc/bind/named.conf.options (REQ-16)
#   render_zone <dns> <dominio> <ip_dns> [registro...]
#                                          imprime la zona completa (REQ-22)
#   apply_zone <dns> <dominio> [--if-changed]
#                                          aplica la zona de stdin en el servidor DNS (REQ-28)
#   wait_ips <vm...>                       imprime "<vm> <ip|->" tras esperar su IPv4 (REQ-27)
#
# Variables de entorno:
#   APP_SSH_KEY_PATH  llave privada del backend (la usa vm_exec)
#
# Salida:
#   stdout  render_options, render_zone, wait_ips y "reloaded=yes|no" de apply_zone
#   stderr  el motivo de un fallo; los validadores nunca repiten el valor
#
# Códigos de salida: valor de retorno de las funciones (los scripts usan la misma tabla)
#   0   éxito
#   1   (solo validadores) valor inválido
#   2   argumentos inválidos o APP_SSH_KEY_PATH faltante
#   4   fallo de comunicación con el servidor DNS (SSH), o arping no instalado o sin permiso
#   5   falló un paso remoto
#   7   el servidor DNS no tiene dirección IP (apagado)
#   9   no se pudo recargar bind9 y tampoco restaurar la zona anterior
#   10  zona DNS inválida; se mantuvo la anterior
#   11  no se pudo recargar bind9; se restauró la zona anterior
#   13  la IP del servidor DNS ya está en uso
#   99  operación no implementada
#
# Se carga con "source" desde scripts que ya están en modo estricto; repetirlo aquí no cambia sus
# opciones.
set -euo pipefail

# shellcheck source=../../web/lib/web-common.sh
source "$(dirname "${BASH_SOURCE[0]}")/../../web/lib/web-common.sh"

# Octeto de una IPv4: 0-255 sin ceros a la izquierda (como net/netip en el backend).
_OCTET='(25[0-5]|2[0-4][0-9]|1[0-9]{2}|[1-9]?[0-9])'
_IPV4_RE="^${_OCTET}(\.${_OCTET}){3}$"

# == 1. Ayudantes de IPv4 ==
# 1.1 _ip_to_int <ip> imprime la IPv4 como entero; _int_to_ip <n> hace lo contrario.
# 1.2 _mask <prefijo> imprime la máscara como entero.

_ip_to_int() {
  local a b c d
  IFS=. read -r a b c d <<<"$1"
  echo $(((a << 24) | (b << 16) | (c << 8) | d))
}

_int_to_ip() {
  echo "$((($1 >> 24) & 255)).$((($1 >> 16) & 255)).$((($1 >> 8) & 255)).$(($1 & 255))"
}

_mask() {
  echo $(((0xFFFFFFFF << (32 - $1)) & 0xFFFFFFFF))
}

# == 2. Validadores ==
# Cada uno devuelve 0 si el valor es válido, o 1 con el motivo en stderr.

# 2.1 validate_ipv4 <ip>.
validate_ipv4() {
  if [[ ! "${1-}" =~ $_IPV4_RE ]]; then
    echo "IPv4 inválida" >&2
    return 1
  fi
}

# 2.2 validate_cidr <ip/prefijo>: prefijo 8-30 y la IP no es la red ni la difusión.
validate_cidr() {
  local ip="${1%/*}" prefix="${1##*/}" ip_n mask
  if [[ "${1-}" != */* || ! "$ip" =~ $_IPV4_RE || ! "$prefix" =~ ^([89]|[12][0-9]|30)$ ]]; then
    echo "IP del servidor DNS inválida: IPv4 con prefijo entre 8 y 30" >&2
    return 1
  fi
  ip_n="$(_ip_to_int "$ip")"
  mask="$(_mask "$prefix")"
  if ((ip_n == (ip_n & mask) || ip_n == ((ip_n & mask) | (~mask & 0xFFFFFFFF)))); then
    echo "IP del servidor DNS inválida: es la dirección de red o de difusión" >&2
    return 1
  fi
}

# 2.3 validate_gateway <ip/prefijo> <gw>: dentro de la subred, sin ser la red, la difusión ni la IP.
validate_gateway() {
  local ip="${1%/*}" prefix="${1##*/}" gw="${2-}" ip_n gw_n mask net bcast
  if [[ ! "$gw" =~ $_IPV4_RE ]]; then
    echo "Puerta de enlace inválida: debe ser una IPv4" >&2
    return 1
  fi
  ip_n="$(_ip_to_int "$ip")"
  gw_n="$(_ip_to_int "$gw")"
  mask="$(_mask "$prefix")"
  net=$((ip_n & mask))
  bcast=$((net | (~mask & 0xFFFFFFFF)))
  if (((gw_n & mask) != net || gw_n == net || gw_n == bcast || gw_n == ip_n)); then
    echo "Puerta de enlace inválida: debe ser un equipo de la subred del servidor DNS" >&2
    return 1
  fi
}

# 2.4 validate_forwarders <lista>: 1 a 3 IPv4 separadas por comas, sin espacios.
validate_forwarders() {
  local list="${1-}" item
  local -a items
  IFS=, read -ra items <<<"$list"
  if [[ -z "$list" || "$list" == *, || "$list" == ,* || "$list" == *,,* || ${#items[@]} -gt 3 ]]; then
    echo "Reenviadores inválidos: de 1 a 3 IPv4 separadas por comas" >&2
    return 1
  fi
  for item in "${items[@]}"; do
    if [[ ! "$item" =~ $_IPV4_RE ]]; then
      echo "Reenviadores inválidos: de 1 a 3 IPv4 separadas por comas" >&2
      return 1
    fi
  done
}

# 2.5 validate_records <dns> [registro...]: "<vm>,<ipv4>", como máximo 200, VMs únicas y ninguna
#     es el servidor DNS (su registro A propio lo agrega render_zone).
validate_records() {
  local dns="$1" rec vm ip
  local -A seen=()
  shift
  if (($# > 200)); then
    echo "Demasiados registros: el máximo es 200" >&2
    return 1
  fi
  for rec in "$@"; do
    vm="${rec%%,*}"
    ip="${rec#*,}"
    if [[ "$rec" != *,* || ! "$vm" =~ ^[a-z0-9][a-z0-9-]{0,31}$ || ! "$ip" =~ $_IPV4_RE ]]; then
      echo "Registro inválido: debe ser <vm>,<ipv4>" >&2
      return 1
    fi
    if [[ "$vm" == "$dns" || -n "${seen[$vm]:-}" ]]; then
      echo "Registro inválido: VM repetida o igual al servidor DNS" >&2
      return 1
    fi
    seen[$vm]=1
  done
}

# 2.6 validate_wait_vms <lista>: "-" (ninguna) o nombres de VM separados por comas.
validate_wait_vms() {
  local name='[a-z0-9][a-z0-9-]{0,31}'
  if [[ "${1-}" != "-" && ! "${1-}" =~ ^${name}(,${name})*$ ]]; then
    echo 'wait_vms inválido: "-" o nombres de VM separados por comas' >&2
    return 1
  fi
}

# == 3. ip_in_use <interfaz> <ip> (REQ-15, AC-25) ==
# 3.1 Sin arping en el host, devolver 4. Una sola sonda ARP por la interfaz de puente
#     ("arping -c 1 -w 1 -I <interfaz> <ip>"): 0 es respuesta, 1 es silencio y otro código es un
#     error del host (por ejemplo sin cap_net_raw), que devuelve 4.
# 3.2 Un solo eco ICMP ("ping -c 1 -W 1 <ip>"): 0 es respuesta.
# 3.3 Cualquier respuesta: "La IP del servidor DNS ya está en uso" y 13. ARP detecta también un
#     equipo que bloquea ICMP.

ip_in_use() {
  local bridge="$1" ip="$2" rc=0
  if ! command -v arping >/dev/null 2>&1; then
    echo "arping no está instalado en el host" >&2
    return 4
  fi
  arping -c 1 -w 1 -I "$bridge" "$ip" >/dev/null 2>&1 </dev/null || rc=$?
  if ((rc > 1)); then
    echo "arping no pudo enviar la sonda ARP (código $rc); revise cap_net_raw" >&2
    return 4
  fi
  if ((rc == 0)) || ping -c 1 -W 1 "$ip" >/dev/null 2>&1 </dev/null; then
    echo "La IP del servidor DNS ya está en uso" >&2
    return 13
  fi
}

# == 4. render_options <ip/prefijo> <reenviadores> (REQ-16) ==
# 4.1 La ACL acepta la subred de la LAN del servidor DNS, las redes internas de 004
#     (10.10.0.0/16) y el propio equipo; el resto se rechaza.
# 4.2 Recursión solo para esos clientes, siempre por los reenviadores ("forward only").

render_options() {
  local ip="${1%/*}" prefix="${1##*/}" net fwd=""
  local -a items
  net="$(_int_to_ip $(($(_ip_to_int "$ip") & $(_mask "$prefix"))))"
  IFS=, read -ra items <<<"$2"
  printf -v fwd '%s; ' "${items[@]}"
  printf 'acl stratus_clients { %s/%s; 10.10.0.0/16; localhost; };\n' "$net" "$prefix"
  printf 'options {\n'
  printf '  directory "/var/cache/bind";\n'
  printf '  recursion yes;\n'
  printf '  allow-query { stratus_clients; };\n'
  printf '  allow-recursion { stratus_clients; };\n'
  printf '  forwarders { %s};\n' "$fwd"
  printf '  forward only;\n'
  printf '  dnssec-validation auto;\n'
  printf '  listen-on { any; };\n'
  printf '  listen-on-v6 { none; };\n'
  printf '};\n'
}

# == 5. render_zone <dns> <dominio> <ip_dns> [registro...] (REQ-22, REQ-29) ==
# 5.1 Toda la zona sale de los registros recibidos (los de SQLite): TTL 60, SOA con el serial
#     "$(date +%s)" en su propia línea terminada en "; serial", NS al registro A del servidor DNS.
# 5.2 Un registro A por VM, ordenados por nombre.

render_zone() {
  local dns="$1" domain="$2" dns_ip="$3" rec
  shift 3
  printf '$TTL 60\n'
  printf '@ IN SOA %s.%s. hostmaster.%s. (\n' "$dns" "$domain" "$domain"
  printf '  %s ; serial\n' "$(date +%s)"
  printf '  3600 600 86400 60 )\n'
  printf '@ IN NS %s.%s.\n' "$dns" "$domain"
  printf '%s IN A %s\n' "$dns" "$dns_ip"
  (($# > 0)) || return 0
  while IFS=, read -r vm ip; do
    printf '%s IN A %s\n' "$vm" "$ip"
  done < <(printf '%s\n' "$@" | LC_ALL=C sort -t, -k1,1)
}

# == 6. apply_zone <dns> <dominio> [--if-changed] (REQ-28) ==
# La zona llega por stdin. La ruta es /var/lib/bind/db.<dominio>.
# 6.1 Con --if-changed, leer la zona actual y comparar ambas sin la línea "; serial": si son
#     iguales imprimir "reloaded=no" y terminar sin subir nada. Una zona ausente cuenta como cambio.
# 6.2 Subir la zona a <ruta>.new y revisarla con "named-checkzone". Si falla, borrar el .new,
#     imprimir la salida de la revisión en stderr y devolver 10: la zona servida no cambia.
# 6.3 Copiar la zona actual a <ruta>.bak (si existe), mover el .new encima y dejarla de bind.
# 6.4 "rndc reload <dominio>". Si falla, mover el .bak de vuelta y recargar otra vez: 11, o 9 si
#     tampoco eso funciona. Si funciona, borrar el .bak e imprimir "reloaded=yes".
# Los códigos 2, 4, 7 y 12 de vm_exec pasan tal cual; otro fallo remoto devuelve 5.

# _zone_exec <dns> <comando...>: vm_exec; devuelve 2, 4, 7 y 12 tal cual y 5 para el resto.
_zone_exec() {
  local rc=0
  vm_exec "$@" || rc=$?
  case "$rc" in
    0 | 2 | 4 | 7 | 12) return "$rc" ;;
  esac
  return 5
}

_strip_serial() {
  sed '/ ; serial$/d' <<<"$1"
}

apply_zone() {
  local dns="$1" domain="$2" if_changed="${3:-}" zone current rc=0 check
  local path="/var/lib/bind/db.$domain"
  zone="$(cat)"

  if [[ "$if_changed" == --if-changed ]]; then
    current="$(vm_exec "$dns" cat "$path" 2>/dev/null </dev/null)" || rc=$?
    case "$rc" in
      0)
        if [[ "$(_strip_serial "$current")" == "$(_strip_serial "$zone")" ]]; then
          echo "reloaded=no"
          return 0
        fi
        ;;
      2 | 4 | 7 | 12) return "$rc" ;;
    esac
    rc=0
  fi

  printf '%s\n' "$zone" | _zone_exec "$dns" sh -c 'cat >"$1"' sh "$path.new" || return $?
  check="$(vm_exec "$dns" named-checkzone "$domain" "$path.new" 2>&1 </dev/null)" || rc=$?
  if ((rc != 0)); then
    case "$rc" in
      2 | 4 | 7 | 12) return "$rc" ;;
    esac
    vm_exec "$dns" rm -f "$path.new" </dev/null || true
    printf '%s\n' "$check" >&2
    echo "Zona DNS inválida; se mantuvo la anterior" >&2
    return 10
  fi

  _zone_exec "$dns" sh -c 'if [ -e "$1" ]; then cp -p "$1" "$1.bak"; fi; mv "$1.new" "$1" && chown bind:bind "$1"' sh "$path" </dev/null || return $?
  if ! vm_exec "$dns" rndc reload "$domain" </dev/null >&2; then
    if vm_exec "$dns" mv "$path.bak" "$path" </dev/null && vm_exec "$dns" rndc reload "$domain" </dev/null >&2; then
      echo "No se pudo recargar bind9; se restauró la zona anterior" >&2
      return 11
    fi
    echo "No se pudo recargar bind9 ni restaurar la zona anterior" >&2
    return 9
  fi
  _zone_exec "$dns" rm -f "$path.bak" </dev/null || return $?
  echo "reloaded=yes"
}

# == 7. wait_ips <vm...> (REQ-27, AC-40) ==
# 7.1 Por cada VM, en paralelo, sondear la IPv4 de su adaptador 1 (vm_nic_ip) hasta 60 veces con
#     "sleep 2" entre sondeos: todas comparten la misma ventana de 120 s porque corren a la vez.
# 7.2 Imprimir "<vm> <ip>" por VM, en el orden recibido, o "<vm> -" si no llegó una IPv4.

wait_ips() {
  local dir i vm pids=()
  (($# > 0)) || return 0
  dir="$(mktemp -d)"
  i=0
  for vm in "$@"; do
    (
      echo "-" >"$dir/$i"
      for ((n = 0; n < 60; n++)); do
        ip="$(vm_nic_ip "$vm" 1 2>/dev/null </dev/null)" || ip=""
        if [[ -n "$ip" ]]; then
          echo "$ip" >"$dir/$i"
          exit 0
        fi
        ((n == 59)) || sleep 2
      done
    ) &
    pids+=("$!")
    i=$((i + 1))
  done
  wait "${pids[@]}"
  i=0
  for vm in "$@"; do
    echo "$vm $(cat "$dir/$i")"
    i=$((i + 1))
  done
  rm -rf "$dir"
}

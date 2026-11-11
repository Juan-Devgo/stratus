#!/usr/bin/env bash
# remove-record.sh - Quita el registro A de una VM de la zona del servidor DNS (005). Recibe los
# registros que quedan, renderiza la zona entera sin la VM, la comprueba con named-checkzone y
# recarga bind9; una zona rechazada deja la anterior (REQ-24, REQ-28, REQ-29).
#
# Uso:
#   remove-record.sh <dns> <domain> <dns_cidr> <vm> [record...]
#
# Argumentos:
#   dns       nombre del servidor DNS, ^[a-z0-9][a-z0-9-]{0,31}$
#   domain    zona: 1-4 etiquetas ^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$ unidas por puntos
#   dns_cidr  IPv4/prefijo (8-30) del servidor DNS (APP_DNS_IP): su IPv4 es su propio registro A
#             y su subred es la LAN de los registros
#   vm        VM cuyo registro se quita, ^[a-z0-9][a-z0-9-]{0,31}$
#   record    registro restante "<vm>,<ipv4>" (hasta 200, sin repetir VMs, ni el servidor DNS ni <vm>)
#
# Variables de entorno:
#   APP_SSH_KEY_PATH  llave privada del backend
#
# Salida:
#   stdout  "Registro de <vm> quitado" y, en la última línea, "reloaded=yes"
#   stderr  diagnósticos (con la salida de named-checkzone si la zona es inválida)
#
# Códigos de salida:
#   0   éxito
#   2   argumentos inválidos o variables de entorno faltantes
#   3   VM no encontrada en VirtualBox
#   4   fallo de comunicación con la VM (SSH)
#   5   falló un paso en la VM
#   7   el servidor DNS no tiene dirección IP (está apagado)
#   9   falló la recarga y también la restauración de la zona anterior
#   10  zona inválida; se mantuvo la anterior
#   11  falló la recarga; se restauró la zona anterior
#   99  operación no implementada

set -euo pipefail

scripts_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=../prepare/lib/vm-exec.sh
source "$scripts_dir/prepare/lib/vm-exec.sh"
# shellcheck source=lib/dns-common.sh
source "$scripts_dir/dns/lib/dns-common.sh"

# == 1. Validar argumentos ==
# 1.1 Exigir al menos 4 argumentos; si no, imprimir el uso en stderr y salir con 2.
# 1.2 dns, domain, dns_cidr, vm y los registros contra sus listas permitidas (ninguno de <vm>), cada
#     registro dentro de la subred de dns_cidr (REQ-22) y APP_SSH_KEY_PATH definida; si no, salir con 2.
# Ninguna validación fallida llama a VBoxManage ni a ssh.

if (($# < 4)); then
  fail 2 "Uso: remove-record.sh <dns> <domain> <dns_cidr> <vm> [record...]"
fi
dns="$1"
domain="$2"
dns_cidr="$3"
vm="$4"
shift 4

validate_vm_name "$dns" || exit 2
validate_domain "$domain" || exit 2
validate_cidr "$dns_cidr" || exit 2
dns_ip="${dns_cidr%/*}"
validate_vm_name "$vm" || exit 2
validate_records "$dns" "$@" || exit 2
validate_lan_records "$dns_cidr" "$@" || exit 2
for rec in "$@"; do
  if [[ "${rec%%,*}" == "$vm" ]]; then
    fail 2 "Registro inválido: $vm no puede estar entre los registros restantes"
  fi
done
if [[ -z "${APP_SSH_KEY_PATH:-}" ]]; then
  fail 2 "Falta la variable de entorno APP_SSH_KEY_PATH"
fi

# == 2. Preparar la ejecución en la VM ==
# 2.1 Comprobar que la VM exista con "VBoxManage showvminfo <dns>"; si no, salir con 3.

if ! VBoxManage showvminfo "$dns" >/dev/null 2>&1; then
  fail 3 "VM no encontrada en VirtualBox"
fi

# == 3. Aplicar la zona ==
# 3.1 "render_zone <dns> <domain> <dns_ip> [record...] | apply_zone <dns> <domain>": sube la zona
#     sin la VM, la comprueba, guarda la anterior y recarga; su código es el del script (7 sin IP).
# 3.2 Imprimir "Registro de <vm> quitado" y la línea "reloaded=yes" de apply_zone.

result="$(render_zone "$dns" "$domain" "$dns_ip" "$@" | apply_zone "$dns" "$domain")" || exit $?
echo "Registro de $vm quitado"
echo "$result"

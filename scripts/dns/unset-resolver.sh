#!/usr/bin/env bash
# unset-resolver.sh - Deja de usar el servidor DNS como resolvedor de una VM (005). Quita la
# configuración que dejó set-resolver.sh (bloque de dhclient o hook de dhcpcd) y las líneas del
# servidor DNS en /etc/resolv.conf: quedan solo los servidores de DHCP (REQ-31, REQ-32).
#
# Uso:
#   unset-resolver.sh [--jump <lb>] <vm> <dns_ip>
#
# Argumentos:
#   --jump <lb>  balanceador por el que se llega a la VM (servidor asignado, 004)
#   vm           nombre de la VM, ^[a-z0-9][a-z0-9-]{0,31}$
#   dns_ip       IPv4 del servidor DNS
#
# Variables de entorno:
#   APP_SSH_KEY_PATH  llave privada del backend
#
# Salida:
#   stdout  "Resolvedor DNS <dns_ip> quitado de <vm>"
#   stderr  diagnósticos
#
# Códigos de salida:
#   0   éxito (también si no había nada que quitar)
#   2   argumentos inválidos o variables de entorno faltantes
#   3   VM no encontrada en VirtualBox
#   4   fallo de comunicación con la VM (SSH)
#   5   falló un paso en la VM
#   7   la VM no tiene dirección IP (está apagada)
#   12  la VM tiene ruta y su balanceador está apagado o sin IPv4
#   99  operación no implementada

set -euo pipefail

scripts_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=../prepare/lib/vm-exec.sh
source "$scripts_dir/prepare/lib/vm-exec.sh"
# shellcheck source=lib/dns-common.sh
source "$scripts_dir/dns/lib/dns-common.sh"

# == 1. Validar argumentos ==
# 1.0 "--jump <lb>" es opcional y va siempre primero; parse_jump lo valida y sale con 2.
# 1.1 Exigir exactamente 2 argumentos; si no, imprimir el uso en stderr y salir con 2.
# 1.2 vm y dns_ip contra sus listas permitidas y APP_SSH_KEY_PATH definida; si no, salir con 2.
# Ninguna validación fallida llama a VBoxManage ni a ssh.

parse_jump "$@"
shift "$jump_shift"

if (($# != 2)); then
  fail 2 "Uso: unset-resolver.sh [--jump <lb>] <vm> <dns_ip>"
fi
vm="$1"
dns_ip="$2"

validate_vm_name "$vm" || exit 2
validate_ipv4 "$dns_ip" || exit 2
if [[ -z "${APP_SSH_KEY_PATH:-}" ]]; then
  fail 2 "Falta la variable de entorno APP_SSH_KEY_PATH"
fi

# == 2. Preparar la ejecución en la VM ==
# 2.1 Con --jump, registrar la ruta antes de cualquier vm_exec: la VM se alcanza por el balanceador.
# 2.2 Comprobar que la VM exista con "VBoxManage showvminfo <vm>"; si no, salir con 3.

[[ -z "$jump_lb" ]] || vm_route "$vm" "$jump_lb"

if ! VBoxManage showvminfo "$vm" >/dev/null 2>&1; then
  fail 3 "VM no encontrada en VirtualBox"
fi

# == 3. Quitar el resolvedor ==
# 3.1 Enviar resolver_program por stdin con "sh -s stratus-resolver unset <dns_ip>"; su código es
#     el del script (7 sin IP, 12 con el balanceador apagado, 5 si falla).
# 3.2 Imprimir el mensaje de éxito.

vm_exec "$vm" sh -s stratus-resolver unset "$dns_ip" <<<"$(resolver_program)" || exit $?
echo "Resolvedor DNS $dns_ip quitado de $vm"

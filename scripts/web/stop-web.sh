#!/usr/bin/env bash
# stop-web.sh - Detiene el servicio web (apache2 o nginx) de un servidor web (005) con "systemctl stop".
#
# Uso:
#   stop-web.sh [--jump <lb>] <vm> <apache2|nginx>
#
# Argumentos:
#   --jump <lb>  (opcional, siempre primero) balanceador por el que se llega a la VM cuando es un
#                servidor asignado a él (004); el backend lo agrega
#   vm           nombre del servidor web, ^[a-z0-9][a-z0-9-]{0,31}$
#   unidad       unidad del tipo del servidor web: apache2 o nginx (el backend la elige)
#
# Variables de entorno:
#   APP_SSH_KEY_PATH  llave privada del backend
#
# Salida:
#   stdout  "Servicio <unidad> detenido en <vm>"
#   stderr  diagnósticos
#
# Códigos de salida:
#   0   éxito
#   2   argumentos inválidos o variables de entorno faltantes
#   3   VM no encontrada en VirtualBox
#   4   fallo de comunicación con la VM (SSH)
#   5   systemctl falló en la VM
#   7   VM sin dirección IP; verifique que esté encendida
#   12  el balanceador (--jump) está apagado o sin IPv4 en puente
#   99  operación no implementada

set -euo pipefail

scripts_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=../prepare/lib/vm-exec.sh
source "$scripts_dir/prepare/lib/vm-exec.sh"
# shellcheck source=../prepare/lib/systemd-unit.sh
source "$scripts_dir/prepare/lib/systemd-unit.sh"
# shellcheck source=lib/web-common.sh
source "$scripts_dir/web/lib/web-common.sh"

# == 1. Validar argumentos ==
# 1.0 "--jump <lb>" es opcional y va siempre primero; parse_jump lo valida y sale con 2.
# 1.1 Exigir exactamente 2 argumentos; si no, imprimir el uso en stderr y salir con 2.
# 1.2 vm y unidad contra sus listas permitidas y APP_SSH_KEY_PATH definida; si no, salir con 2.
# Ninguna validación fallida llama a VBoxManage ni a ssh.

parse_jump "$@"
shift "$jump_shift"

if (($# != 2)); then
  fail 2 "Uso: stop-web.sh [--jump <lb>] <vm> <apache2|nginx>"
fi
vm="$1"
unit="$2"

validate_vm_name "$vm" || exit 2
validate_unit "$unit" || exit 2
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

# == 3. Ejecutar ==
# 3.1 "unit_action <vm> <unidad> stop" (systemd-unit.sh); su código es el del script (7 sin IP, 12 con el balanceador apagado).

unit_action "$vm" "$unit" stop || exit $?

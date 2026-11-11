#!/usr/bin/env bash
# start-bind.sh - Inicia bind9 (unidad named) del servidor DNS (005) con "systemctl start" y, unos 3 segundos después, informa su estado activo (un servicio
# que cae justo después de iniciar no es un fallo de la operación).
#
# Uso:
#   start-bind.sh <dns>
#
# Argumentos:
#   dns  nombre del servidor DNS, ^[a-z0-9][a-z0-9-]{0,31}$ (nunca está detrás de un balanceador)
#
# Variables de entorno:
#   APP_SSH_KEY_PATH  llave privada del backend
#
# Salida:
#   stdout  "Servicio named iniciado en <dns>" y, en la última línea, "active_state=<estado>"
#   stderr  diagnósticos
#
# Códigos de salida:
#   0   éxito
#   2   argumentos inválidos o variables de entorno faltantes
#   3   VM no encontrada en VirtualBox
#   4   fallo de comunicación con la VM (SSH)
#   5   systemctl falló en la VM
#   7   el servidor DNS no tiene dirección IP; verifique que esté encendido
#   99  operación no implementada

set -euo pipefail

scripts_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=../prepare/lib/vm-exec.sh
source "$scripts_dir/prepare/lib/vm-exec.sh"
# shellcheck source=../prepare/lib/systemd-unit.sh
source "$scripts_dir/prepare/lib/systemd-unit.sh"
# shellcheck source=lib/dns-common.sh
source "$scripts_dir/dns/lib/dns-common.sh"

# == 1. Validar argumentos ==
# 1.1 Exigir exactamente 1 argumento; si no, imprimir el uso en stderr y salir con 2.
# 1.2 dns contra su lista permitida y APP_SSH_KEY_PATH definida; si no, salir con 2.
# Ninguna validación fallida llama a VBoxManage ni a ssh.

if (($# != 1)); then
  fail 2 "Uso: start-bind.sh <dns>"
fi
dns="$1"

validate_vm_name "$dns" || exit 2
if [[ -z "${APP_SSH_KEY_PATH:-}" ]]; then
  fail 2 "Falta la variable de entorno APP_SSH_KEY_PATH"
fi

# == 2. Preparar la ejecución en la VM ==
# 2.1 Comprobar que la VM exista con "VBoxManage showvminfo <dns>"; si no, salir con 3.

if ! VBoxManage showvminfo "$dns" >/dev/null 2>&1; then
  fail 3 "VM no encontrada en VirtualBox"
fi

# == 3. Ejecutar ==
# 3.1 "unit_action <dns> named start" (systemd-unit.sh); su código es el del script (7 sin IP).

unit_action "$dns" named start || exit $?

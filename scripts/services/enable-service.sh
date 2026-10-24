#!/usr/bin/env bash
# enable-service.sh - Habilita un servicio creado por Stratus para que arranque con la VM (systemctl enable). No lo inicia.
# Solo acepta unidades con la marca de Stratus (REQ-18). Entra por SSH con la llave del backend
# contra la IP de la VM.
#
# Uso:
#   enable-service.sh [--jump <lb>] <vm_name> <name>
#
# Argumentos:
#   --jump <lb>  (opcional, siempre primero) balanceador por el que se llega a la VM cuando es un
#                servidor asignado a él (004); el backend lo agrega
#   vm_name     nombre de la VM, ^[a-z0-9][a-z0-9-]{0,31}$
#   name        nombre del servicio, ^[a-z][a-z0-9-]{0,31}$
#
# Variables de entorno:
#   APP_SSH_KEY_PATH  llave privada del backend
#
# Salida:
#   stdout  "Servicio <name> habilitado en <vm_name>"
#   stderr  diagnósticos
#
# Códigos de salida:
#   0   éxito
#   2   argumentos inválidos o variables de entorno faltantes
#   3   VM no encontrada en VirtualBox
#   4   fallo de comunicación con la VM (SSH)
#   5   falló systemctl enable en la VM
#   6   la unidad no existe o no la creó Stratus
#   7   VM sin dirección IP; verifique que esté encendida
#   12  el balanceador (--jump) está apagado o sin IPv4 en puente
#   99  operación no implementada

set -euo pipefail

# shellcheck source=../prepare/lib/vm-exec.sh
source "$(dirname "${BASH_SOURCE[0]}")/../prepare/lib/vm-exec.sh"
# shellcheck source=lib/service-common.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/service-common.sh"

# == 1. Validar argumentos ==
# 1.0 Opciones: "--jump <lb>" es opcional y va siempre primero (REQ-19). Con él, vm_name es un
#     servidor asignado al balanceador <lb> y se alcanza por SSH a través de su cuenta jump. El
#     backend lo agrega; el script no deduce el rol. parse_jump (service-common.sh) lo valida
#     (^[a-z0-9][a-z0-9-]{0,31}$) y sale con 2 si falta el valor o no cumple.
# 1.1 Exigir exactamente 2 argumentos; si no, imprimir el uso en stderr y salir con 2.
# 1.2 Validar vm_name contra ^[a-z0-9][a-z0-9-]{0,31}$ y name contra ^[a-z][a-z0-9-]{0,31}$.
# 1.3 Exigir APP_SSH_KEY_PATH; si falta, salir con 2.
# Ninguna validación fallida debe llamar a VBoxManage ni a ssh.

parse_jump "$@"
shift "$jump_shift"

if (($# != 2)); then
  fail 2 "Uso: enable-service.sh [--jump <lb>] <vm_name> <name>"
fi
vm_name="$1"
name="$2"

if [[ ! "$vm_name" =~ ^[a-z0-9][a-z0-9-]{0,31}$ ]]; then
  fail 2 'vm_name inválido: debe cumplir ^[a-z0-9][a-z0-9-]{0,31}$'
fi
validate_name "$name" || exit 2
if [[ -z "${APP_SSH_KEY_PATH:-}" ]]; then
  fail 2 "Falta la variable de entorno APP_SSH_KEY_PATH"
fi

# == 2. Preparar la ejecución en la VM ==
# 2.1 Comprobar que la VM exista con "VBoxManage showvminfo <vm_name>"; si falla, salir con 3.
# 2.2 Comprobar con "vm_ip" que la VM tenga IPv4; si no, salir con 7 (vm_ip ya imprimió el motivo).
# 2.3 Comprobar con require_managed que la unidad tenga la marca de Stratus; si no, salir con 6.

# Con --jump, registrar la ruta antes de cualquier vm_ip o vm_exec: la VM se alcanza por el balanceador.
[[ -z "$jump_lb" ]] || vm_route "$vm_name" "$jump_lb"

if ! VBoxManage showvminfo "$vm_name" >/dev/null 2>&1; then
  fail 3 "VM no encontrada en VirtualBox"
fi

vm_ip "$vm_name" >/dev/null || exit $?
require_managed

# == 3. Ejecutar la acción (REQ-17) ==
# 3.1 "systemctl enable <name>"; si falla, salir con 5. Una acción que deja el estado igual
#     (por ejemplo iniciar un servicio activo) termina bien.

guest 5 systemctl enable "$name"

# == 4. Resultado ==
# 4.1 Imprimir el mensaje.

echo "Servicio $name habilitado en $vm_name"

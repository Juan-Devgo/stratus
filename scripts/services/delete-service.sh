#!/usr/bin/env bash
# delete-service.sh - Elimina un servicio creado por Stratus con todo lo que instaló: lo detiene y
# deshabilita, y quita /etc/<name>.env, /opt/<name>, el usuario, el grupo y, al final, la unidad.
# Cada paso tolera que su parte ya no exista, y la unidad se quita en último lugar: si un paso falla,
# el servicio sigue listado y se puede eliminar otra vez. Entra por SSH con la llave del backend
# contra la IP de la VM.
#
# Uso:
#   delete-service.sh [--jump <lb>] <vm_name> <name>
#
# Argumentos:
#   --jump <lb>  (opcional, siempre primero) balanceador por el que se llega a la VM cuando es un
#                servidor asignado a él (004); el backend lo agrega
#   vm_name     nombre de la VM, ^[a-z0-9][a-z0-9-]{0,31}$
#   name        nombre del servicio, usuario y grupo, ^[a-z][a-z0-9-]{0,31}$
#
# Variables de entorno:
#   APP_SSH_KEY_PATH  llave privada del backend
#
# Salida:
#   stdout  "Servicio <name> eliminado de <vm_name>"
#   stderr  diagnósticos; si falla un paso, "No se pudo eliminar el servicio <name>: falló el paso <n>"
#
# Códigos de salida:
#   0   éxito
#   2   argumentos inválidos o variables de entorno faltantes
#   3   VM no encontrada en VirtualBox
#   4   fallo de comunicación con la VM (SSH)
#   5   falló un paso en la VM (los pasos anteriores ya se aplicaron; se puede repetir)
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
  fail 2 "Uso: delete-service.sh [--jump <lb>] <vm_name> <name>"
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

# == 3. Eliminar (REQ-25) ==
# Cada paso fija fail_msg con su número: si falla, guest imprime "No se pudo eliminar el servicio
# <name>: falló el paso <n>" y sale con 5. Los pasos tolerantes no fallan si su parte ya no existe.
#   3.1 (paso 2) "systemctl disable --now": detiene y deshabilita; un servicio ya detenido no falla.
#   3.2 (paso 3) "rm -f /etc/<name>.env": no falla si falta.
#   3.3 (paso 4) "rm -rf /opt/<name>": no falla si falta.
#   3.4 (paso 5) userdel solo si el usuario existe.
#   3.5 (paso 6) groupdel solo si el grupo existe (userdel ya lo quita si era su grupo primario).
#   3.6 (paso 7) "rm -f" de la unidad, a propósito al final: mientras exista, el servicio sigue
#        listado y el borrado se puede repetir.
#   3.7 (paso 8) daemon-reload y reset-failed; reset-failed no hace fallar el paso.

step_failed() {
  fail_msg="No se pudo eliminar el servicio $name: falló el paso $1"
}

step_failed 2
guest 5 systemctl disable --now "$name"
step_failed 3
guest 5 rm -f "/etc/$name.env"
step_failed 4
guest 5 rm -rf "/opt/$name"
step_failed 5
guest 5 sh -c '! getent passwd "$1" >/dev/null || userdel "$1"' sh "$name"
step_failed 6
guest 5 sh -c '! getent group "$1" >/dev/null || groupdel "$1"' sh "$name"
step_failed 7
guest 5 rm -f "/etc/systemd/system/$name.service"
step_failed 8
guest 5 sh -c 'systemctl daemon-reload && { systemctl reset-failed "$1.service" 2>/dev/null || true; }' sh "$name"

# == 4. Resultado ==
# 4.1 Imprimir el mensaje.

echo "Servicio $name eliminado de $vm_name"

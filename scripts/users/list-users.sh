#!/usr/bin/env bash
# list-users.sh - Lista los usuarios regulares (UID 1000-59999) de una VM con su estado de sudo
# y de llave SSH. Solo lee: no cambia nada en la VM. Entra por SSH con la llave del backend
# contra la IP de la VM.
#
# Uso:
#   list-users.sh [--jump <lb>] <vm_name>
#
# Argumentos:
#   --jump <lb>  (opcional, siempre primero) balanceador por el que se llega a la VM cuando es un
#                servidor asignado a él (004); el backend lo agrega
#   vm_name     nombre de la VM, ^[a-z0-9][a-z0-9-]{0,31}$
#
# Variables de entorno:
#   APP_SSH_KEY_PATH  llave privada del backend
#
# Salida:
#   stdout  una línea por usuario: "<usuario> <uid> <yes|no> <yes|no>" (sudo, llave), ordenadas
#           por UID
#   stderr  diagnósticos
#
# Códigos de salida:
#   0   éxito (también si no hay usuarios regulares: stdout queda vacío)
#   2   argumentos inválidos o variables de entorno faltantes
#   3   VM no encontrada en VirtualBox
#   4   fallo de comunicación con la VM (SSH)
#   5   fallo del comando remoto
#   7   VM sin dirección IP; verifique que esté encendida
#   12  el balanceador (--jump) está apagado o sin IPv4 en puente
#   99  operación no implementada

set -euo pipefail

# == 1. Validar argumentos ==
# 1.0 Opciones: "--jump <lb>" es opcional y va siempre primero (REQ-19). Con él, vm_name es un
#     servidor asignado al balanceador <lb> y se alcanza por SSH a través de su cuenta jump. El
#     backend lo agrega; el script no deduce el rol. <lb> cumple ^[a-z0-9][a-z0-9-]{0,31}$;
#     si falta o no cumple, salir con 2.
# 1.1 Exigir exactamente 1 argumento; si no, imprimir el uso en stderr y salir con 2.
# 1.2 Validar vm_name contra ^[a-z0-9][a-z0-9-]{0,31}$; si no coincide, salir con 2.
# 1.3 Exigir APP_SSH_KEY_PATH; si falta, salir con 2.
# Ninguna validación fallida debe llamar a VBoxManage ni a ssh.

fail() {
  echo "$2" >&2
  exit "$1"
}

jump_lb=""
if [[ "${1-}" == --jump ]]; then
  if [[ ! "${2-}" =~ ^[a-z0-9][a-z0-9-]{0,31}$ ]]; then
    fail 2 '--jump inválido: debe cumplir ^[a-z0-9][a-z0-9-]{0,31}$'
  fi
  jump_lb="$2"
  shift 2
fi

if (($# != 1)); then
  fail 2 "Uso: list-users.sh [--jump <lb>] <vm_name>"
fi
vm_name="$1"

if [[ ! "$vm_name" =~ ^[a-z0-9][a-z0-9-]{0,31}$ ]]; then
  fail 2 'vm_name inválido: debe cumplir ^[a-z0-9][a-z0-9-]{0,31}$'
fi

if [[ -z "${APP_SSH_KEY_PATH:-}" ]]; then
  fail 2 "Falta la variable de entorno APP_SSH_KEY_PATH"
fi

# == 2. Preparar la ejecución en la VM ==
# 2.1 Cargar la biblioteca ../prepare/lib/vm-exec.sh (vm_ip y vm_exec).
# 2.2 Comprobar que la VM exista con "VBoxManage showvminfo <vm_name>"; si falla, salir con 3.
# 2.3 Comprobar con "vm_ip" que la VM tenga IPv4; si no, salir con 7 (vm_ip ya imprimió el motivo).

# shellcheck source=../prepare/lib/vm-exec.sh
source "$(dirname "${BASH_SOURCE[0]}")/../prepare/lib/vm-exec.sh"

# Con --jump, registrar la ruta antes de cualquier vm_ip o vm_exec: la VM se alcanza por el balanceador.
[[ -z "$jump_lb" ]] || vm_route "$vm_name" "$jump_lb"

if ! VBoxManage showvminfo "$vm_name" >/dev/null 2>&1; then
  fail 3 "VM no encontrada en VirtualBox"
fi

vm_ip "$vm_name" >/dev/null || exit $?

# guest <código_de_fallo> <comando> [argumentos...]: ejecuta el comando como root en la VM. Sale con
# 2, 4 o 7 tal como los devuelve vm_exec, y con <código_de_fallo> si el comando termina con error.
guest() {
  local fail_code="$1" rc=0
  shift
  vm_exec "$vm_name" "$@" || rc=$?
  case "$rc" in
    0) ;;
    2 | 4 | 7 | 12) exit "$rc" ;;
    *) exit "$fail_code" ;;
  esac
}

# == 3. Leer los usuarios (REQ-1, REQ-2) ==
# 3.1 Un solo comando remoto "sh -c <programa>" con un programa constante: nada del usuario se
#     interpola en él.
# 3.2 El programa toma de "getent passwd" los usuarios con UID entre 1000 y 59999 (nombre, UID
#     y home), ordenados por UID.
# 3.3 Sudo es "yes" si sudo existe y "sudo -n -l -U <usuario>" dice "may run the following
#     commands": cubre el grupo sudo y las reglas de sudoers.
# 3.4 Llave es "yes" si <home>/.ssh/authorized_keys tiene alguna línea que no esté en blanco
#     ni sea un comentario.
# 3.5 Si el comando remoto falla, salir con 5.

LIST="$(
  cat <<'PROGRAM'
export LC_ALL=C
getent passwd | awk -F: '$3 >= 1000 && $3 <= 59999 { print $1, $3, $6 }' | sort -k2,2n |
  while read -r name uid home; do
    sudo=no
    if command -v sudo >/dev/null 2>&1 &&
      sudo -n -l -U "$name" 2>/dev/null | grep -q 'may run the following commands'; then
      sudo=yes
    fi
    key=no
    if [ -f "$home/.ssh/authorized_keys" ] &&
      grep -qvE '^[[:space:]]*(#|$)' "$home/.ssh/authorized_keys"; then
      key=yes
    fi
    echo "$name $uid $sudo $key"
  done
PROGRAM
)"

guest 5 sh -c "$LIST" sh

# == 4. Resultado ==
# 4.1 La salida del paso 3 ya es el resultado (stdout); no se imprime nada más.

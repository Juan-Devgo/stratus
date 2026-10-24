#!/usr/bin/env bash
# install-package.sh - Instala un paquete en la VM con apt-get, por SSH con la llave del backend
# contra la IP de la VM. Si el paquete es ufw, además permite el puerto 22/tcp y, con --frontend,
# el puerto del frontend de HAProxy.
#
# Uso:
#   install-package.sh [--jump <lb>] [--frontend <port>] <vm_name> <package>
#
# Argumentos:
#   --jump <lb>  (opcional, siempre primero) balanceador por el que se llega a la VM cuando es un
#                servidor asignado a él (004); el backend lo agrega
#   --frontend <port>  (opcional, tras --jump) puerto del frontend de HAProxy cuando la VM es un
#                balanceador (004); el backend lo agrega, 1-65535 y distinto de 22
#   vm_name  nombre de la VM, ^[a-z0-9][a-z0-9-]{0,31}$
#   package  nombre del paquete Debian, ^[a-z0-9][a-z0-9+.-]{1,62}$
#
# Variables de entorno:
#   APP_SSH_KEY_PATH  llave privada del backend
#
# Salida:
#   stdout  mensaje de resultado
#   stderr  diagnósticos
#
# Códigos de salida:
#   0   éxito: el paquete quedó instalado (también si ya lo estaba)
#   2   argumentos inválidos o variables de entorno faltantes
#   3   VM no encontrada en VirtualBox
#   4   fallo de comunicación con la VM (SSH)
#   5   fallo de apt-get o de ufw en la VM
#   7   VM sin dirección IP; verifique que esté encendida
#   12  el balanceador (--jump) está apagado o sin IPv4 en puente
#   99  operación no implementada

set -euo pipefail

# == 1. Validar argumentos ==
# 1.0 Opciones: "--jump <lb>" es opcional y va siempre primero (REQ-19). Con él, vm_name es un
#     servidor asignado al balanceador <lb> y se alcanza por SSH a través de su cuenta jump. El
#     backend lo agrega; el script no deduce el rol. <lb> cumple ^[a-z0-9][a-z0-9-]{0,31}$;
#     si falta o no cumple, salir con 2.
#     Después de --jump puede ir "--frontend <port>" (REQ-5): el backend lo agrega cuando vm_name
#     es un balanceador. Debe ser un puerto de 1 a 65535 distinto de 22; si no, salir con 2.
# 1.1 Exigir exactamente 2 argumentos; si no, imprimir el uso en stderr y salir con 2.
# 1.2 Validar vm_name contra ^[a-z0-9][a-z0-9-]{0,31}$; si no coincide, salir con 2.
# 1.3 Validar package contra ^[a-z0-9][a-z0-9+.-]{1,62}$ (rechaza vacío, espacios y ";");
#     si no coincide, salir con 2.
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

frontend_port=""
if [[ "${1-}" == --frontend ]]; then
  if [[ ! "${2-}" =~ ^[1-9][0-9]{0,4}$ ]] || ((10#$2 > 65535 || 10#$2 == 22)); then
    fail 2 '--frontend inválido: debe ser un puerto de 1 a 65535 distinto de 22'
  fi
  frontend_port="$2"
  shift 2
fi

if (($# != 2)); then
  fail 2 "Uso: install-package.sh [--jump <lb>] [--frontend <port>] <vm_name> <package>"
fi
vm_name="$1"
package="$2"

if [[ ! "$vm_name" =~ ^[a-z0-9][a-z0-9-]{0,31}$ ]]; then
  fail 2 'vm_name inválido: debe cumplir ^[a-z0-9][a-z0-9-]{0,31}$'
fi

if [[ ! "$package" =~ ^[a-z0-9][a-z0-9+.-]{1,62}$ ]]; then
  fail 2 'package inválido: debe cumplir ^[a-z0-9][a-z0-9+.-]{1,62}$'
fi

# == 2. Preparar la ejecución en la VM ==
# 2.1 Cargar la biblioteca con: source "$(dirname "${BASH_SOURCE[0]}")/lib/vm-exec.sh".
# 2.2 Comprobar que la VM exista con "VBoxManage showvminfo <vm_name>"; si falla, salir con 3.
# 2.3 Comprobar con "vm_ip" que la VM tenga IPv4; si no, salir con 7 antes de cualquier comando
#     remoto (vm_ip ya imprimió el motivo).
# 2.4 Cada comando en la VM se ejecuta como root con "vm_exec <vm_name> <comando> <argumentos...>";
#     si vm_exec devuelve 4 (SSH), 2, 7 o 12, el script sale con ese mismo código.

# shellcheck source=lib/vm-exec.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/vm-exec.sh"

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

# == 3. Instalar el paquete ==
# 3.1 "vm_exec ... env DEBIAN_FRONTEND=noninteractive apt-get update".
# 3.2 "vm_exec ... env DEBIAN_FRONTEND=noninteractive apt-get install -y <package>"; apt-get
#     no hace nada si el paquete ya está instalado, así la operación es idempotente. Instalar
#     openssh-server es una instalación normal: la VM ya tiene el acceso con llave desde su creación.
# 3.3 Si apt-get falla, reenviar su stderr y salir con 5.

guest 5 env DEBIAN_FRONTEND=noninteractive apt-get update
guest 5 env DEBIAN_FRONTEND=noninteractive apt-get install -y "$package"

# == 4. Permitir SSH al instalar ufw (REQ-25) ==
# 4.1 Solo si package es ufw; si no, saltar a la sección 5.
# 4.2 "vm_exec ... ufw allow 22/tcp" antes de cualquier "ufw enable", para que el backend
#     no pierda el acceso por SSH; la regla repetida no cambia nada (idempotente).
# 4.3 Con --frontend, también "vm_exec ... ufw allow <port>/tcp": el balanceador no debe quedar
#     sin su puerto al activar ufw (REQ-5).
# 4.4 Si falla, salir con 5.

if [[ "$package" == ufw ]]; then
  guest 5 ufw allow 22/tcp
  if [[ -n "$frontend_port" ]]; then
    guest 5 ufw allow "$frontend_port/tcp"
  fi
fi

# == 5. Resultado ==
# 5.1 Imprimir en stdout "Paquete <package> instalado en <vm_name>".

echo "Paquete $package instalado en $vm_name"

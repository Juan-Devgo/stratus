#!/usr/bin/env bash
# set-hostname.sh - Cambia el hostname de la VM con hostnamectl, por SSH con la llave del backend
# contra la IP de la VM, y deja en /etc/hosts
# la línea "127.0.1.1 <hostname>", reemplazando la existente o agregándola si falta, sin
# tocar las demás líneas.
#
# Uso:
#   set-hostname.sh [--jump <lb>] <vm_name> <hostname>
#
# Argumentos:
#   --jump <lb>  (opcional, siempre primero) balanceador por el que se llega a la VM cuando es un
#                servidor asignado a él (004); el backend lo agrega
#   vm_name     nombre de la VM, ^[a-z0-9][a-z0-9-]{0,31}$
#   hostname    nuevo nombre de una sola etiqueta, ^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$
#
# Variables de entorno:
#   APP_SSH_KEY_PATH  llave privada del backend
#
# Salida:
#   stdout  mensaje de resultado
#   stderr  diagnósticos
#
# Códigos de salida:
#   0   éxito: la VM tiene el nuevo hostname (también si ya lo tenía)
#   2   argumentos inválidos o variables de entorno faltantes
#   3   VM no encontrada en VirtualBox
#   4   fallo de comunicación con la VM (SSH)
#   5   fallo de hostnamectl en la VM
#   6   fallo al actualizar /etc/hosts
#   7   VM sin dirección IP; verifique que esté encendida
#   12  el balanceador (--jump) está apagado o sin IPv4 en puente
#   99  operación no implementada

set -euo pipefail

# == 1. Validar argumentos ==
# 1.0 Opciones: "--jump <lb>" es opcional y va siempre primero (REQ-19). Con él, vm_name es un
#     servidor asignado al balanceador <lb> y se alcanza por SSH a través de su cuenta jump. El
#     backend lo agrega; el script no deduce el rol. <lb> cumple ^[a-z0-9][a-z0-9-]{0,31}$;
#     si falta o no cumple, salir con 2.
# 1.1 Exigir exactamente 2 argumentos; si no, imprimir el uso en stderr y salir con 2.
# 1.2 Validar vm_name contra ^[a-z0-9][a-z0-9-]{0,31}$; si no coincide, salir con 2.
# 1.3 Validar hostname contra ^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$ (una sola etiqueta de
#     1 a 63 caracteres, sin mayúsculas, "_", puntos ni guion al inicio o al final); si no
#     coincide, salir con 2.
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

if (($# != 2)); then
  fail 2 "Uso: set-hostname.sh [--jump <lb>] <vm_name> <hostname>"
fi
vm_name="$1"
new_hostname="$2"

if [[ ! "$vm_name" =~ ^[a-z0-9][a-z0-9-]{0,31}$ ]]; then
  fail 2 'vm_name inválido: debe cumplir ^[a-z0-9][a-z0-9-]{0,31}$'
fi

if [[ ! "$new_hostname" =~ ^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$ ]]; then
  fail 2 'hostname inválido: debe cumplir ^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$'
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

# == 3. Cambiar el hostname ==
# 3.1 "vm_exec ... hostnamectl set-hostname <hostname>"; si el nombre ya es el mismo no
#     cambia nada, así la operación es idempotente.
# 3.2 Si hostnamectl falla, reenviar su stderr y salir con 5.

guest 5 hostnamectl set-hostname "$new_hostname"

# == 4. Actualizar /etc/hosts (REQ-13) ==
# 4.1 Si /etc/hosts tiene una línea que empieza con "127.0.1.1", reemplazarla completa por
#     "127.0.1.1<TAB><hostname>" (por ejemplo con "sed -i 's/^127\.0\.1\.1[[:space:]].*/.../'").
# 4.2 Si no la tiene, agregar "127.0.1.1<TAB><hostname>" al final del archivo.
# 4.3 Las demás líneas (127.0.0.1, IPv6, entradas propias) no se modifican.
# 4.4 Repetir la operación con el mismo nombre deja el archivo igual (idempotente).
# 4.5 Si la edición falla, reenviar el stderr y salir con 6.

# El script remoto recibe el hostname como argumento ($1), no dentro del texto del comando.
guest 6 sh -c 'if grep -q "^127\.0\.1\.1[[:space:]]" /etc/hosts; then sed -i "s/^127\.0\.1\.1[[:space:]].*/127.0.1.1\t$1/" /etc/hosts; else printf "127.0.1.1\t%s\n" "$1" >>/etc/hosts; fi' sh "$new_hostname"

# == 5. Resultado ==
# 5.1 Imprimir en stdout "Hostname de <vm_name> cambiado a <hostname>".

echo "Hostname de $vm_name cambiado a $new_hostname"

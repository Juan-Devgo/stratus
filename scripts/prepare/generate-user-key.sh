#!/usr/bin/env bash
# generate-user-key.sh - Genera un par de llaves ed25519 sin frase de paso para el acceso del
# usuario como root a la VM, reemplaza la llave anterior del usuario en authorized_keys,
# conserva (o agrega) la llave del backend y reafirma que el acceso con contraseña esté
# deshabilitado. Todo va por SSH con la llave del backend contra la IP de la VM. La llave privada
# solo sale por stdout; el backend la guarda en memoria para una única descarga.
#
# Uso:
#   generate-user-key.sh [--jump <lb>] <vm_name>
#
# Argumentos:
#   --jump <lb>  (opcional, siempre primero) balanceador por el que se llega a la VM cuando es un
#                servidor asignado a él (004); el backend lo agrega
#   vm_name  nombre de la VM, ^[a-z0-9][a-z0-9-]{0,31}$
#
# Variables de entorno:
#   APP_SSH_KEY_PATH  llave privada del backend; su llave pública está en "<ruta>.pub"
#
# Salida:
#   stdout  solo la llave privada del usuario (formato OpenSSH), nada más
#   stderr  diagnósticos; nunca la llave privada
#
# Códigos de salida:
#   0   éxito: la nueva llave del usuario está en authorized_keys y se imprimió la privada
#   2   argumentos inválidos o variables de entorno faltantes
#   3   VM no encontrada en VirtualBox
#   4   fallo de comunicación con la VM (SSH)
#   5   fallo de ssh-keygen o de la configuración de SSH en la VM
#   7   VM sin dirección IP; verifique que esté encendida
#   12  el balanceador (--jump) está apagado o sin IPv4 en puente
#   15  con --jump: no se pudo actualizar la cuenta jump del balanceador; se restauró authorized_keys
#   99  operación no implementada

set -euo pipefail

# == 1. Validar argumentos ==
# 1.0 Opciones: "--jump <lb>" es opcional y va siempre primero (REQ-19). Con él, vm_name es un
#     servidor asignado al balanceador <lb> y se alcanza por SSH a través de su cuenta jump. El
#     backend lo agrega; el script no deduce el rol. <lb> cumple ^[a-z0-9][a-z0-9-]{0,31}$;
#     si falta o no cumple, salir con 2.
# 1.1 Exigir exactamente 1 argumento; si no, imprimir el uso en stderr y salir con 2.
# 1.2 Validar vm_name contra ^[a-z0-9][a-z0-9-]{0,31}$; si no coincide, salir con 2.
# 1.3 Exigir que exista "$APP_SSH_KEY_PATH.pub"; si no, salir con 2.
# Ninguna validación fallida debe llamar a VBoxManage, ssh ni ssh-keygen.

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
  fail 2 "Uso: generate-user-key.sh [--jump <lb>] <vm_name>"
fi
vm_name="$1"

if [[ ! "$vm_name" =~ ^[a-z0-9][a-z0-9-]{0,31}$ ]]; then
  fail 2 'vm_name inválido: debe cumplir ^[a-z0-9][a-z0-9-]{0,31}$'
fi

if [[ -z "${APP_SSH_KEY_PATH:-}" || ! -f "$APP_SSH_KEY_PATH.pub" ]]; then
  fail 2 "Falta la llave pública del backend (APP_SSH_KEY_PATH.pub)"
fi

# == 2. Preparar la ejecución en la VM ==
# 2.1 Cargar la biblioteca con: source "$(dirname "${BASH_SOURCE[0]}")/lib/vm-exec.sh".
# 2.2 Comprobar que la VM exista con "VBoxManage showvminfo <vm_name>"; si falla, salir con 3.
# 2.3 Comprobar con "vm_ip" que la VM tenga IPv4; si no, salir con 7 antes de generar la llave y
#     antes de cualquier comando remoto (vm_ip ya imprimió el motivo).
# 2.4 Cada comando en la VM se ejecuta como root con "vm_exec <vm_name> <comando> <argumentos...>";
#     si vm_exec devuelve 4 (SSH), 2, 7 o 12, el script sale con ese mismo código.
# 2.5 Toda la salida de vm_exec se redirige a stderr ("vm_exec ... >&2"), porque stdout queda
#     reservado para la llave privada.

# shellcheck source=lib/vm-exec.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/vm-exec.sh"
# shellcheck source=../haproxy/lib/jump-keys.sh
source "$(dirname "${BASH_SOURCE[0]}")/../haproxy/lib/jump-keys.sh"

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
  vm_exec "$vm_name" "$@" >&2 || rc=$?
  case "$rc" in
    0) ;;
    2 | 4 | 7 | 12) exit "$rc" ;;
    *) exit "$fail_code" ;;
  esac
}

# == 3. Funciones de llaves ==
# 3.1 La llave pública del backend se lee de "$APP_SSH_KEY_PATH.pub" (una línea terminada en el
#     comentario "stratus-backend"); la llave privada del backend nunca se lee ni se envía.
# 3.2 Agregar una llave a /root/.ssh/authorized_keys solo si esa línea no está ya (con "grep -qxF"
#     antes de agregar), para no duplicarla.

# Lee la llave pública del backend; la privada nunca se lee ni se envía.
read_backend_pub() {
  IFS= read -r backend_pub <"$APP_SSH_KEY_PATH.pub" || true
  if [[ -z "$backend_pub" ]]; then
    fail 5 "La llave pública del backend está vacía: $APP_SSH_KEY_PATH.pub"
  fi
}

# add_authorized_key <llave_pública>: la agrega a authorized_keys de root si no está ya.
add_authorized_key() {
  guest 5 sh -c 'grep -qxF -- "$1" /root/.ssh/authorized_keys || printf "%s\n" "$1" >>/root/.ssh/authorized_keys' sh "$1"
}

# == 4. Generar el par del usuario en un directorio temporal ==
# 4.1 Crear un directorio con "mktemp -d" y registrar de inmediato
#     "trap 'rm -rf -- "$tmp_dir"' EXIT" para borrarlo al salir, también ante un error.
# 4.2 Fijar "umask 077" antes de generar, para que nadie más pueda leer la llave.
# 4.3 Ejecutar "ssh-keygen -q -t ed25519 -N "" -C stratus-user -f "$tmp_dir/id_ed25519""
#     con su salida a stderr; si falla, salir con 5.
# 4.4 Leer la llave pública de "$tmp_dir/id_ed25519.pub"; es lo único que viaja a la VM.
#     La llave privada nunca se envía a la VM ni se escribe fuera de "$tmp_dir".

tmp_dir="$(mktemp -d)" || fail 5 "No se pudo crear el directorio temporal"
trap 'rm -rf -- "$tmp_dir"' EXIT

umask 077
ssh-keygen -q -t ed25519 -N "" -C stratus-user -f "$tmp_dir/id_ed25519" >&2 || fail 5 "ssh-keygen falló"
IFS= read -r user_pub <"$tmp_dir/id_ed25519.pub" || true
if [[ -z "$user_pub" ]]; then
  fail 5 "ssh-keygen no generó la llave pública del usuario"
fi

# == 5. Reemplazar la llave del usuario en authorized_keys (REQ-6, AC-27) ==
# 5.0 Asegurar que /root/.ssh (700) y authorized_keys (600) existan (ya los crea create-vm.sh). Con
#     --jump, además "cp -p" de authorized_keys a authorized_keys.stratus-backup.
# 5.1 Quitar de /root/.ssh/authorized_keys todas las líneas con el comentario "stratus-user"
#     (la llave anterior generada por la app); por ejemplo con "sed -i '/ stratus-user$/d'".
#     Las líneas "stratus-backend" y las agregadas a mano no se tocan.
# 5.2 Agregar la nueva llave pública del usuario (comentario "stratus-user").
# 5.3 Agregar la llave pública del backend ("stratus-backend") si no está, como en 3.2, para
#     que el backend siga conectándose después de regenerar (REQ-23).
# 5.4 Si cualquier paso falla, salir con 5.

guest 5 install -d -m 700 /root/.ssh
guest 5 sh -c 'touch /root/.ssh/authorized_keys && chmod 600 /root/.ssh/authorized_keys'
# Con --jump: copia de seguridad de authorized_keys para deshacer el cambio si jump no se actualiza (7.1).
authorized_backup=/root/.ssh/authorized_keys.stratus-backup
if [[ -n "$jump_lb" ]]; then
  guest 5 cp -p /root/.ssh/authorized_keys "$authorized_backup"
fi
guest 5 sed -i '/ stratus-user$/d' /root/.ssh/authorized_keys
add_authorized_key "$user_pub"
read_backend_pub
add_authorized_key "$backend_pub"

# == 6. Deshabilitar el acceso con contraseña (P4) ==
# 6.1 En /etc/ssh/sshd_config (o un archivo en /etc/ssh/sshd_config.d/) dejar
#     "PasswordAuthentication no" y "PermitRootLogin prohibit-password", reemplazando las
#     líneas existentes en lugar de repetirlas (idempotente).
# 6.2 Validar la configuración con "sshd -t" y reiniciar con "systemctl restart ssh".
# 6.3 Si falla, salir con 5: el backend no marcará la VM ni guardará la llave.

# Cada par clave/valor va como argumentos separados; printf repite el formato por cada par.
guest 5 sh -c 'printf "%s %s\n" "$@" >/etc/ssh/sshd_config.d/10-stratus.conf' sh \
  PasswordAuthentication no PermitRootLogin prohibit-password
guest 5 sshd -t
guest 5 systemctl restart ssh

# == 7. Sincronizar jump (solo con --jump, REQ-17) ==
# 7.1 Copiar a la cuenta jump del balanceador las llaves de root y de los usuarios del servidor, incluida
#     la nueva (jump_sync lee authorized_keys ya cambiado, por eso va después de 5 y de 6.3). Si falla,
#     restaurar authorized_keys desde la copia de 5.0 ("mv"), no imprimir la llave y salir con 15.
# 7.2 Si todo salió bien, borrar la copia de seguridad.

if [[ -n "$jump_lb" ]]; then
  if ! jump_sync "$jump_lb" "$vm_name" >&2; then
    vm_exec "$vm_name" mv "$authorized_backup" /root/.ssh/authorized_keys >&2 ||
      echo "No se pudo restaurar /root/.ssh/authorized_keys en $vm_name; la copia está en $authorized_backup" >&2
    fail 15 "No se pudo actualizar la cuenta jump de $jump_lb; se restauró authorized_keys de $vm_name"
  fi
  guest 5 rm -f "$authorized_backup"
fi

# == 8. Resultado ==
# 8.1 Imprimir en stdout solo el contenido de "$tmp_dir/id_ed25519" ("cat"), sin mensajes
#     adicionales: el backend toma stdout completo como la llave para descargar.
# 8.2 El trap de 4.1 borra el directorio temporal al salir; la llave no queda en disco.
# 8.3 Los mensajes de progreso, si los hay, van a stderr y nunca incluyen la llave privada.

cat "$tmp_dir/id_ed25519"

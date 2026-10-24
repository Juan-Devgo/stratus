#!/usr/bin/env bash
# delete-user.sh - Elimina un usuario regular (UID 1000-59999) de la VM: termina sus procesos,
# opcionalmente borra todos sus archivos, quita su regla sudo y elimina la cuenta con su grupo
# primario. Entra por SSH con la llave del backend contra la IP de la VM.
#
# Uso:
#   delete-user.sh [--jump <lb>] <vm_name> <username> <remove_files>
#
# Argumentos:
#   --jump <lb>  (opcional, siempre primero) balanceador por el que se llega a la VM cuando es un
#                servidor asignado a él (004); el backend lo agrega
#   vm_name       nombre de la VM, ^[a-z0-9][a-z0-9-]{0,31}$
#   username      nombre del usuario, ^[a-z][a-z0-9_-]{0,31}$
#   remove_files  yes o no: con yes también se borran /home/<username>, /var/mail/<username> y los
#                 archivos del usuario en los sistemas de archivos locales; los directorios que
#                 no queden vacíos pasan a root:root con sus permisos
#
# Variables de entorno:
#   APP_SSH_KEY_PATH  llave privada del backend
#
# Salida:
#   stdout  "Usuario <username> eliminado de <vm_name>", más " junto con todos sus archivos"
#           con remove_files=yes
#   stderr  diagnósticos
#
# Códigos de salida:
#   0   éxito
#   2   argumentos inválidos o variables de entorno faltantes
#   3   VM no encontrada en VirtualBox
#   4   fallo de comunicación con la VM (SSH)
#   5   fallo al eliminar (procesos, archivos, regla sudo, cuenta o visudo)
#   6   no es un usuario regular existente (falta o su UID está fuera de 1000-59999)
#   7   VM sin dirección IP; verifique que esté encendida
#   12  el balanceador (--jump) está apagado o sin IPv4 en puente
#   15  con --jump: no se pudo actualizar la cuenta jump; no se tocó la VM
#   99  operación no implementada

set -euo pipefail

# == 1. Validar argumentos ==
# 1.0 Opciones: "--jump <lb>" es opcional y va siempre primero (REQ-19). Con él, vm_name es un
#     servidor asignado al balanceador <lb> y se alcanza por SSH a través de su cuenta jump. El
#     backend lo agrega; el script no deduce el rol. <lb> cumple ^[a-z0-9][a-z0-9-]{0,31}$;
#     si falta o no cumple, salir con 2.
# 1.1 Exigir exactamente 3 argumentos; si no, imprimir el uso en stderr y salir con 2.
# 1.2 Validar vm_name contra ^[a-z0-9][a-z0-9-]{0,31}$ y username contra ^[a-z][a-z0-9_-]{0,31}$.
# 1.3 Validar que remove_files sea yes o no.
# 1.4 Exigir APP_SSH_KEY_PATH.
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

if (($# != 3)); then
  fail 2 "Uso: delete-user.sh [--jump <lb>] <vm_name> <username> <remove_files yes|no>"
fi
vm_name="$1"
username="$2"
remove_files="$3"

if [[ ! "$vm_name" =~ ^[a-z0-9][a-z0-9-]{0,31}$ ]]; then
  fail 2 'vm_name inválido: debe cumplir ^[a-z0-9][a-z0-9-]{0,31}$'
fi
if [[ ! "$username" =~ ^[a-z][a-z0-9_-]{0,31}$ ]]; then
  fail 2 'username inválido: debe cumplir ^[a-z][a-z0-9_-]{0,31}$'
fi
if [[ "$remove_files" != yes && "$remove_files" != no ]]; then
  fail 2 "remove_files inválido: debe ser yes o no"
fi
if [[ -z "${APP_SSH_KEY_PATH:-}" ]]; then
  fail 2 "Falta la variable de entorno APP_SSH_KEY_PATH"
fi

# == 2. Preparar la ejecución en la VM ==
# 2.1 Cargar la biblioteca ../prepare/lib/vm-exec.sh (vm_ip y vm_exec).
# 2.2 Comprobar que la VM exista con "VBoxManage showvminfo <vm_name>"; si falla, salir con 3.
# 2.3 Comprobar con "vm_ip" que la VM tenga IPv4; si no, salir con 7 (vm_ip ya imprimió el motivo).
# 2.4 Cada comando en la VM va por "guest <código_de_fallo> <comando...>", que sale con 2, 4 o 7
#     tal como los devuelve vm_exec y con <código_de_fallo> si el comando termina con error.

# shellcheck source=../prepare/lib/vm-exec.sh
source "$(dirname "${BASH_SOURCE[0]}")/../prepare/lib/vm-exec.sh"
# shellcheck source=../haproxy/lib/jump-keys.sh
source "$(dirname "${BASH_SOURCE[0]}")/../haproxy/lib/jump-keys.sh"

# Con --jump, registrar la ruta antes de cualquier vm_ip o vm_exec: la VM se alcanza por el balanceador.
[[ -z "$jump_lb" ]] || vm_route "$vm_name" "$jump_lb"

if ! VBoxManage showvminfo "$vm_name" >/dev/null 2>&1; then
  fail 3 "VM no encontrada en VirtualBox"
fi

vm_ip "$vm_name" >/dev/null || exit $?

guest() {
  local fail_code="$1" rc=0
  shift
  vm_exec "$vm_name" "$@" || rc=$?
  case "$rc" in
    0) ;;
    2 | 4 | 7 | 12) exit "$rc" ;;
    *)
      # Sin mensaje propio (fail_msg) solo se nombra el programa, nunca sus argumentos.
      echo "${fail_msg:-Falló el paso $1 en la VM (código $rc)}" >&2
      exit "$fail_code"
      ;;
  esac
}

# == 3. Comprobar que sea un usuario regular (REQ-14) ==
# 3.1 Leer el UID con "id -u <username>" en la VM; si el usuario no existe el programa remoto
#     responde "none". El nombre llega como argumento.
# 3.2 Si el resultado no es un número entre 1000 y 59999, salir con 6 sin tocar nada: así root,
#     los usuarios del sistema y los usuarios inexistentes quedan fuera.

user_uid="$(guest 5 sh -c 'id -u "$1" 2>/dev/null || echo none' sh "$username")" || exit $?
if [[ ! "$user_uid" =~ ^[0-9]+$ ]] || ((10#$user_uid < 1000 || 10#$user_uid > 59999)); then
  fail 6 "Solo se pueden eliminar usuarios regulares existentes"
fi
user_uid=$((10#$user_uid))

# == 3b. Quitar su llave de jump (solo con --jump, REQ-17) ==
# 3b.1 jump_sync <lb> <vm> <username>: deja en jump las llaves del servidor SIN las del usuario. Va
#      antes de cualquier cambio en la VM; si falla, salir con 15 sin tocar nada.
# 3b.2 Si un paso posterior falla y el usuario sigue existiendo, el trap de salida vuelve a sincronizar
#      sin exclusión para que su llave siga valiendo en jump.

jump_synced=no

# restore_jump corre al salir: con error y la llave ya quitada de jump, la vuelve a copiar.
restore_jump() {
  local rc=$?
  trap - EXIT
  if ((rc != 0)) && [[ "$jump_synced" == yes ]]; then
    jump_sync "$jump_lb" "$vm_name" >&2 ||
      echo "No se pudo restaurar la llave de $username en la cuenta jump de $jump_lb" >&2
  fi
  exit "$rc"
}

if [[ -n "$jump_lb" ]]; then
  trap restore_jump EXIT
  trap 'exit 143' TERM
  trap 'exit 130' INT
  jump_sync "$jump_lb" "$vm_name" "$username" >&2 ||
    fail 15 "No se pudo actualizar la cuenta jump de $jump_lb; no se eliminó a $username"
  jump_synced=yes
fi

# == 4. Terminar los procesos del usuario (REQ-15) ==
# 4.1 "pkill -KILL -u <username>" mata todo lo que corre como el usuario. Después se espera hasta 10 s
#     (sondeos de 1 s con pgrep) a que no quede ninguno: el gestor de systemd del usuario puede
#     tardar un instante en desaparecer.
# 4.2 Si quedan procesos tras esa espera, salir con 5 sin tocar la cuenta: el borrado se puede
#     reintentar.

fail_msg="No se pudieron terminar los procesos de $username" guest 5 sh -c 'pkill -KILL -u "$1" || true; i=0; while pgrep -u "$1" >/dev/null; do i=$((i+1)); [ "$i" -le 10 ] || exit 1; sleep 1; done' sh "$username"

# == 5. Borrar los archivos del usuario (REQ-16) ==
# Solo con remove_files=yes; con no, ningún archivo se toca (AC-19). Todos los pasos buscan en
# todo el árbol local y saltan /proc, /sys, /dev y /run y los sistemas de archivos de red.
# 5.1 "rm -rf -- /home/<username> /var/mail/<username>": el home y el buzón de correo.
# 5.2 "find / <prune> -uid <uid> ! -type d -exec rm -f -- {} +": borra todo archivo que no sea
#     directorio y pertenezca al UID (incluye /tmp, aunque sea tmpfs).
# 5.3 Directorios del UID, del más profundo al más alto (sort -rz): "rmdir" borra los vacíos y a
#     los que no lo están ("chown root:root") los entrega a root con sus permisos intactos, para
#     no borrar archivos de otros usuarios ni dejar un directorio con el UID eliminado. El UID
#     llega como argumento del programa remoto.
# 5.4 Si algún paso falla, salir con 5: la cuenta sigue existiendo y el borrado se puede repetir.

if [[ "$remove_files" == yes ]]; then
  # Expresión de find que salta lo que no se debe recorrer (se usa en 5.2; el programa de 5.3
  # lleva la misma expresión escrita en su texto).
  prune=(\( -path /proc -o -path /sys -o -path /dev -o -path /run
    -o -fstype nfs -o -fstype nfs4 -o -fstype cifs -o -fstype smb3 -o -fstype fuse.sshfs \) -prune -o)

  guest 5 rm -rf -- "/home/$username" "/var/mail/$username"
  guest 5 find / "${prune[@]}" -uid "$user_uid" ! -type d -exec rm -f -- {} +
  guest 5 bash -c 'find / \( -path /proc -o -path /sys -o -path /dev -o -path /run -o -fstype nfs -o -fstype nfs4 -o -fstype cifs -o -fstype smb3 -o -fstype fuse.sshfs \) -prune -o -uid "$1" -type d -print0 | sort -rz | while IFS= read -r -d "" d; do rmdir -- "$d" 2>/dev/null || chown root:root -- "$d"; done' bash "$user_uid"
fi

# == 6. Regla sudo y cuenta (REQ-15) ==
# 6.1 "rm -f /etc/sudoers.d/stratus-<username>": quita la regla que creó create-user.sh (si no
#     existe no pasa nada).
# 6.2 "userdel <username>": elimina la cuenta y su grupo primario, salvo que otro usuario lo use
#     como grupo primario. Los archivos no se tocan aquí.
# 6.3 "visudo -c" comprueba que la configuración de sudo siga siendo válida.
# 6.4 Si alguno falla, salir con 5.

guest 5 rm -f "/etc/sudoers.d/stratus-$username"
guest 5 userdel "$username"
guest 5 visudo -c

# == 7. Resultado ==
# 7.1 Imprimir "Usuario <username> eliminado de <vm_name>", más " junto con todos sus archivos"
#     si remove_files es yes.

message="Usuario $username eliminado de $vm_name"
if [[ "$remove_files" == yes ]]; then
  message+=" junto con todos sus archivos"
fi
echo "$message"

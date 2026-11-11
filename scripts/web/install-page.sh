#!/usr/bin/env bash
# install-page.sh - Instala una página en un servidor web (005): reemplaza todo /var/www/html con el
# contenido de un archivo .tar.gz que tiene index.html en la raíz, con dueño root y permisos 755
# (directorios) y 644 (archivos). Guarda una copia del contenido anterior y la devuelve si algo
# falla. No reinicia el servicio web: sirve los archivos nuevos de inmediato.
#
# Uso:
#   install-page.sh [--jump <lb>] <vm> <archive_file>
#
# Argumentos:
#   --jump <lb>   (opcional, siempre primero) balanceador por el que se llega a la VM cuando es un
#                 servidor asignado a él (004); el backend lo agrega
#   vm            nombre del servidor web, ^[a-z0-9][a-z0-9-]{0,31}$
#   archive_file  archivo .tar.gz del host (legible); se revisa antes de tocar la VM
#
# Variables de entorno:
#   APP_SSH_KEY_PATH  llave privada del backend
#
# Salida:
#   stdout  "Página instalada en <vm>"
#   stderr  diagnósticos
#
# Códigos de salida:
#   0   éxito
#   2   argumentos inválidos o variables de entorno faltantes
#   3   VM no encontrada en VirtualBox
#   4   fallo de comunicación con la VM (SSH o scp)
#   5   falló un paso en la VM (el contenido anterior se restauró)
#   7   VM sin dirección IP; verifique que esté encendida
#   9   falló un paso y también la restauración: queda la copia /var/www/.stratus-page-bak
#   10  archivo comprimido inválido
#   11  el archivo no tiene index.html en la raíz
#   12  el balanceador (--jump) está apagado o sin IPv4 en puente
#   99  operación no implementada

set -euo pipefail

scripts_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=../prepare/lib/vm-exec.sh
source "$scripts_dir/prepare/lib/vm-exec.sh"
# shellcheck source=../services/lib/service-common.sh
source "$scripts_dir/services/lib/service-common.sh"
# shellcheck source=lib/web-common.sh
source "$scripts_dir/web/lib/web-common.sh"

# == 1. Validar argumentos ==
# 1.0 "--jump <lb>" es opcional y va siempre primero; parse_jump lo valida y sale con 2.
# 1.1 Exigir exactamente 2 argumentos; si no, imprimir el uso en stderr y salir con 2.
# 1.2 vm contra su lista permitida; archive_file debe ser un archivo legible; APP_SSH_KEY_PATH
#     debe estar definida. Si algo falla, salir con 2.
# 1.3 Revisar el .tar.gz en el host con check_archive: sale con 10 (archivo inválido) o con 11 (sin
#     index.html regular en la raíz). Ninguna validación fallida llama a VBoxManage, ssh ni scp.

parse_jump "$@"
shift "$jump_shift"

if (($# != 2)); then
  fail 2 "Uso: install-page.sh [--jump <lb>] <vm> <archive_file>"
fi
vm="$1"
archive_file="$2"

validate_vm_name "$vm" || exit 2
if [[ ! -f "$archive_file" || ! -r "$archive_file" ]]; then
  fail 2 "El archivo de la página no existe o no se puede leer"
fi
if [[ -z "${APP_SSH_KEY_PATH:-}" ]]; then
  fail 2 "Falta la variable de entorno APP_SSH_KEY_PATH"
fi

check_archive "$archive_file" index.html "El archivo debe contener index.html en la raíz" || exit $?

# == 2. Preparar la ejecución en la VM ==
# 2.1 Con --jump, registrar la ruta antes de cualquier vm_ip, vm_exec o vm_scp.
# 2.2 Comprobar que la VM exista con "VBoxManage showvminfo <vm>"; si no, salir con 3.

[[ -z "$jump_lb" ]] || vm_route "$vm" "$jump_lb"

if ! VBoxManage showvminfo "$vm" >/dev/null 2>&1; then
  fail 3 "VM no encontrada en VirtualBox"
fi

root=/var/www/html
new=/var/www/.stratus-page-new
bak=/var/www/.stratus-page-bak
remote_archive=/var/tmp/stratus-page.tar.gz

# webx <comando...>: vm_exec en el servidor web; 2, 4, 7 y 12 salen tal cual y otro fallo sale con 5.
webx() {
  local rc=0
  vm_exec "$vm" "$@" </dev/null >&2 || rc=$?
  case "$rc" in
    0) ;;
    2 | 4 | 7 | 12) exit "$rc" ;;
    *) fail 5 "Falló un paso en el servidor web $vm (código $rc): $1" ;;
  esac
}

# == 3. Limpieza y restauración ==
# on_exit corre al terminar por cualquier camino (éxito, error o SIGTERM, que sale con 143):
# 3.1 En la fase armed (ya existe la copia): si el script falla, devolver la copia a /var/www/html
#     con "rm -rf /var/www/html && mv /var/www/.stratus-page-bak /var/www/html". Si la restauración
#     falla, salir con 9 y conservar la copia para recuperarla a mano.
# 3.2 Desde la fase copied (el archivo ya está en la VM): borrar .stratus-page-new, la copia y el
#     archivo remoto, también tras un éxito. Un fallo de este borrado no cambia el código.

phase=none

on_exit() {
  local rc=$?
  trap - EXIT
  if [[ "$phase" == none ]]; then
    exit "$rc"
  fi
  if ((rc != 0)) && [[ "$phase" == armed ]]; then
    if ! vm_exec "$vm" sh -c "rm -rf $root && mv $bak $root" </dev/null >&2; then
      echo "Restauración fallida; el contenido anterior quedó en $bak del servidor web $vm" >&2
      exit 9
    fi
  fi
  vm_exec "$vm" rm -rf "$new" "$bak" "$remote_archive" </dev/null >&2 || true
  exit "$rc"
}
trap on_exit EXIT
trap 'exit 143' TERM
trap 'exit 130' INT

# == 4. Copiar el archivo a la VM ==
# 4.1 vm_scp <vm> <archive_file> /var/tmp/stratus-page.tar.gz (por el balanceador con --jump).
#     Devuelve 4, 7 o 12; otro código sale con 4.

rc=0
vm_scp "$vm" "$archive_file" "$remote_archive" >&2 || rc=$?
case "$rc" in
  0) ;;
  2 | 7 | 12) exit "$rc" ;;
  *) fail 4 "No se pudo copiar el archivo al servidor web $vm" ;;
esac
phase=copied

# == 5. Copia del contenido actual ==
# 5.1 Borrar restos de una instalación anterior y copiar /var/www/html a .stratus-page-bak
#     ("cp -a" conserva dueños y permisos). Desde aquí la restauración queda armada.

webx rm -rf "$new" "$bak"
webx cp -a "$root" "$bak"
phase=armed

# == 6. Extraer y reemplazar (REQ-10) ==
# 6.1 Extraer en .stratus-page-new con "--no-same-owner"; dueño root:root; directorios 755 y
#     archivos 644.
# 6.2 Reemplazar todo /var/www/html: "rm -rf /var/www/html && mv .stratus-page-new /var/www/html"
#     (nunca se mezcla con el contenido anterior). No se llama a systemctl.

webx mkdir "$new"
webx tar -xzf "$remote_archive" --no-same-owner -C "$new"
webx chown -R root:root "$new"
webx find "$new" -type d -exec chmod 755 {} +
webx find "$new" -type f -exec chmod 644 {} +
webx sh -c "rm -rf $root && mv $new $root"

# == 7. Resultado ==
# 7.1 Desarmar la restauración e imprimir el mensaje; on_exit borra la copia y el archivo remoto.

phase=copied
echo "Página instalada en $vm"

#!/usr/bin/env bash
# redeploy-service.sh - Reemplaza el contenido de /opt/<name> de un servicio creado por Stratus con un
# archivo .tar.gz nuevo: detiene el servicio si estaba en ejecución, guarda una copia del contenido y
# de la unidad, extrae el archivo, devuelve el dueño y, si cambió el ejecutable, reescribe el
# ExecStart de la unidad (con los mismos argumentos). Si algo falla, restaura el contenido, la unidad
# y el estado anterior. Entra por SSH con la llave del backend contra la IP de la VM.
#
# Uso:
#   redeploy-service.sh [--jump <lb>] <vm_name> <name> <archive_file> <executable>
#
# Argumentos:
#   --jump <lb>  (opcional, siempre primero) balanceador por el que se llega a la VM cuando es un
#                servidor asignado a él (004); el backend lo agrega
#   vm_name       nombre de la VM, ^[a-z0-9][a-z0-9-]{0,31}$
#   name          nombre del servicio, ^[a-z][a-z0-9-]{0,31}$
#   archive_file  archivo .tar.gz del host (legible); se revisa antes de tocar la VM
#   executable    ruta relativa a /opt/<name>, debe ser un archivo regular del .tar.gz nuevo
#
# Variables de entorno:
#   APP_SSH_KEY_PATH  llave privada del backend
#
# Salida:
#   stdout  "Servicio <name> redesplegado en <vm_name>" y, solo si se volvió a iniciar, unos 3
#           segundos después, la línea "active_state=<estado>"
#   stderr  diagnósticos
#
# Códigos de salida:
#   0   éxito (también si la aplicación nueva termina después de iniciar: el contenido nuevo se queda)
#   2   argumentos inválidos o variables de entorno faltantes
#   3   VM no encontrada en VirtualBox
#   4   fallo de comunicación con la VM (SSH)
#   5   falló un paso en la VM (la restauración terminó bien)
#   6   la unidad no existe o no la creó Stratus
#   7   VM sin dirección IP; verifique que esté encendida
#   12  el balanceador (--jump) está apagado o sin IPv4 en puente
#   9   el redespliegue falló y también la restauración: puede quedar contenido o unidad parciales
#   10  archivo comprimido inválido
#   11  el ejecutable no existe en el archivo nuevo
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
# 1.1 Exigir exactamente 4 argumentos; si no, imprimir el uso en stderr y salir con 2.
# 1.2 Validar vm_name, name y executable contra sus listas permitidas; si alguno falla, salir con 2.
# 1.3 archive_file debe ser un archivo legible; APP_SSH_KEY_PATH debe estar definida.
# 1.4 Revisar el .tar.gz en el host con check_archive: sale con 10 (archivo inválido) o con 11 (el
#     ejecutable no es un archivo regular del .tar.gz).
# Ninguna validación fallida debe llamar a VBoxManage ni a ssh.

parse_jump "$@"
shift "$jump_shift"

if (($# != 4)); then
  fail 2 "Uso: redeploy-service.sh [--jump <lb>] <vm_name> <name> <archive_file> <executable>"
fi
vm_name="$1"
name="$2"
archive_file="$3"
executable="$4"

if [[ ! "$vm_name" =~ ^[a-z0-9][a-z0-9-]{0,31}$ ]]; then
  fail 2 'vm_name inválido: debe cumplir ^[a-z0-9][a-z0-9-]{0,31}$'
fi
validate_name "$name" || exit 2
validate_executable "$executable" || exit 2
if [[ ! -f "$archive_file" || ! -r "$archive_file" ]]; then
  fail 2 "El archivo de la aplicación no existe o no se puede leer"
fi
if [[ -z "${APP_SSH_KEY_PATH:-}" ]]; then
  fail 2 "Falta la variable de entorno APP_SSH_KEY_PATH"
fi

check_archive "$archive_file" "$executable" || exit $?

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

# == 3. Leer la unidad y el estado previo (REQ-24) ==
# 3.1 Leer descripción, ejecutable, argumentos, Restart y RestartSec de la unidad actual. Los campos
#     vacíos (argumentos) se separan con cut, no con "read".
# 3.2 Leer el estado activo: "en ejecución" es active o activating (por ejemplo "activating
#     (auto-restart)"). Solo entonces se detiene antes de reemplazar y se vuelve a iniciar al final.
#     Si el estado no se puede leer, salir con 5: no se sabría si detener.

unit_fields="$(read_unit_fields)" || exit $?
current_description="$(cut -f1 <<<"$unit_fields")"
current_executable="$(cut -f2 <<<"$unit_fields")"
current_args="$(cut -f3 <<<"$unit_fields")"
current_restart="$(cut -f4 <<<"$unit_fields")"
current_restart_sec="$(cut -f5 <<<"$unit_fields")"

previous_state="$(read_state)"
if [[ "$previous_state" == desconocido ]]; then
  fail 5 "No se pudo leer el estado del servicio $name"
fi
was_running=no
if is_running "$previous_state"; then
  was_running=yes
fi

# == 4. Restauración si algo falla (REQ-24) ==
# on_exit corre al terminar el script por cualquier camino (éxito, error o SIGTERM, que sale con
# 143). Según la fase:
#   backup  falló la copia de la unidad: se borran las copias parciales; la VM no cambió.
#   armed   ya se pudo cambiar algo. Se intenta todo aunque un paso falle:
#     4.1 "systemctl stop" (tolerante);
#     4.2 devolver el contenido anterior a /opt/<name> SOLO si existe la copia: si el fallo fue antes
#         del "mv", /opt/<name> sigue siendo el contenido vivo y no se debe borrar;
#     4.3 copiar de vuelta la unidad y "systemctl daemon-reload";
#     4.4 volver a iniciar si estaba en ejecución;
#     4.5 borrar las copias.
# Si todo salió bien se conserva el código original; si algo falló, se imprime qué puede quedar y se
# sale con 9. Los comandos usan vm_exec y no guest porque guest sale en el primer fallo.

backup="/var/lib/stratus/backup/$name"
content_backup="/opt/.stratus-backup-$name"
phase=none

on_exit() {
  local rc=$?
  trap - EXIT
  if ((rc == 0)); then
    exit 0
  fi
  if [[ "$phase" == backup ]]; then
    vm_exec "$vm_name" rm -rf "$backup" "$content_backup" >&2 || true
  elif [[ "$phase" == armed ]]; then
    local restore_failed=no
    vm_exec "$vm_name" systemctl stop "$name" >&2 || true
    vm_exec "$vm_name" sh -c '[ ! -e "$2" ] || { rm -rf "$1" && mv "$2" "$1"; }' sh "/opt/$name" "$content_backup" >&2 ||
      restore_failed=yes
    vm_exec "$vm_name" cp -p "$backup/$name.service" /etc/systemd/system/ >&2 || restore_failed=yes
    vm_exec "$vm_name" systemctl daemon-reload >&2 || restore_failed=yes
    if [[ "$was_running" == yes ]]; then
      vm_exec "$vm_name" systemctl start "$name" >&2 || restore_failed=yes
    fi
    vm_exec "$vm_name" rm -rf "$backup" "$content_backup" >&2 || restore_failed=yes
    if [[ "$restore_failed" == yes ]]; then
      echo "Restauración fallida; puede quedar en la VM: /opt/$name, $content_backup, /etc/systemd/system/$name.service, $backup" >&2
      exit 9
    fi
  fi
  exit "$rc"
}
# SIGTERM (el límite de tiempo del backend) y SIGINT salen con 143 y 130 para pasar por on_exit.
trap on_exit EXIT
trap 'exit 143' TERM
trap 'exit 130' INT

# == 5. Copiar la unidad (REQ-24) ==
# 5.1 Vaciar copias viejas, crear /var/lib/stratus/backup/<name> (modo 700) y copiar ahí la unidad
#     con "cp -p". El contenido de /opt/<name> se guarda con un "mv" en el paso 6.
# 5.2 Si falla un paso, salir con 5; on_exit borra las copias parciales. Con la unidad copiada la
#     restauración queda armada: cualquier fallo posterior la ejecuta.

phase=backup
guest 5 rm -rf "$backup" "$content_backup"
guest 5 mkdir -p -m 700 "$backup"
guest 5 cp -p "/etc/systemd/system/$name.service" "$backup/"
phase=armed

# == 6. Reemplazar el contenido (REQ-24) ==
# 6.1 Si estaba en ejecución, "systemctl stop": nada escribe en /opt/<name> mientras se reemplaza.
# 6.2 "mv /opt/<name> /opt/.stratus-backup-<name>" y crear /opt/<name> nuevo (755, dueño <name>).
# 6.3 Extraer el .tar.gz con tar leyendo el archivo del host por la entrada estándar de ssh: no queda
#     ninguna copia temporal en la VM. "--no-same-owner" evita que el archivo imponga dueños.
# 6.4 "chown -R <name>:<name>" y permiso de ejecución para el dueño del ejecutable.

if [[ "$was_running" == yes ]]; then
  guest 5 systemctl stop "$name"
fi
guest 5 mv "/opt/$name" "$content_backup"
guest 5 install -d -m 755 -o "$name" -g "$name" "/opt/$name"
guest 5 tar -xzf - -C "/opt/$name" --no-same-owner <"$archive_file"
guest 5 chown -R "$name:$name" "/opt/$name"
guest 5 chmod u+x "/opt/$name/$executable"

# == 7. Reescribir ExecStart si cambió el ejecutable (REQ-24) ==
# 7.1 Solo si el ejecutable es otro: reescribir la unidad con la marca y las líneas de REQ-11, con
#     el ejecutable nuevo y la descripción, los argumentos, Restart y RestartSec actuales. Esos
#     valores se leen de la VM, así que se validan otra vez antes de usarlos.
# 7.2 daemon-reload después de escribir la unidad.

if [[ "$executable" != "$current_executable" ]]; then
  validate_description "$current_description" && validate_args "$current_args" &&
    validate_restart "$current_restart" && validate_restart_sec "$current_restart_sec" ||
    fail 5 "La unidad actual de $name tiene valores inválidos; no se reescribió"
  render_unit "$name" "$current_description" "$executable" "$current_args" "$current_restart" "$current_restart_sec" |
    write_remote_file "/etc/systemd/system/$name.service" 644
  guest 5 systemctl daemon-reload
fi

# == 8. Iniciar de nuevo (REQ-24) ==
# 8.1 Solo si estaba en ejecución: "systemctl start". El estado habilitado no cambia.

if [[ "$was_running" == yes ]]; then
  guest 5 systemctl start "$name"
fi

# == 9. Cerrar (REQ-24) ==
# 9.1 El redespliegue ya se aplicó: se desactiva la restauración y se borran las copias. Si no se
#     pueden borrar, salir con 5 sin restaurar (el contenido nuevo es válido; las copias quedan).
# 9.2 Si se inició: esperar 3 segundos y leer el estado activo ("desconocido" si no se puede). Una
#     aplicación que termina después de iniciar no es un fallo.
# 9.3 Imprimir el mensaje y, solo si se inició, la línea "active_state=<estado>".

phase=done
vm_exec "$vm_name" rm -rf "$backup" "$content_backup" >&2 ||
  fail 5 "No se pudieron borrar las copias de la VM ($backup, $content_backup); bórrelas a mano"

state=""
if [[ "$was_running" == yes ]]; then
  sleep 3
  state="$(read_state)"
fi
echo "Servicio $name redesplegado en $vm_name"
if [[ -n "$state" ]]; then
  echo "active_state=$state"
fi

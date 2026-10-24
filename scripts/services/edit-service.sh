#!/usr/bin/env bash
# edit-service.sh - Cambia la configuración de un servicio creado por Stratus: reescribe su unidad
# (descripción, ejecutable, argumentos, Restart y RestartSec) y, si se pide, su archivo de variables;
# recarga systemd y reinicia el servicio solo si estaba en ejecución. Antes de escribir respalda la
# unidad y el archivo de variables en la VM; si algo falla, los restaura. Entra por SSH con la llave
# del backend contra la IP de la VM.
#
# Uso:
#   edit-service.sh [--jump <lb>] <vm_name> <name> <description> <executable> <args> <restart> <restart_sec> <env_file>
#
# Argumentos:
#   --jump <lb>  (opcional, siempre primero) balanceador por el que se llega a la VM cuando es un
#                servidor asignado a él (004); el backend lo agrega
#   vm_name       nombre de la VM, ^[a-z0-9][a-z0-9-]{0,31}$
#   name          nombre del servicio, ^[a-z][a-z0-9-]{0,31}$
#   description   de 1 a 100 caracteres UTF-8 sin control, % \ ni "
#   executable    ruta relativa a /opt/<name>; debe ser un archivo regular de /opt/<name>
#   args          argumentos del ejecutable: vacío, o hasta 16 tokens separados por un espacio
#   restart       no, always u on-failure
#   restart_sec   entero de 0 a 3600
#   env_file      "-" conserva /etc/<name>.env; si no, archivo del host con hasta 50 filas KEY=VALUE
#                 que reemplaza el archivo completo. Los valores son secretos: solo viajan por la
#                 entrada estándar de ssh, nunca en argv ni en la salida.
#
# Variables de entorno:
#   APP_SSH_KEY_PATH  llave privada del backend
#
# Salida:
#   stdout  "Servicio <name> actualizado en <vm_name>" y, solo si se reinició, unos 3 segundos
#           después, la línea "active_state=<estado>"
#   stderr  diagnósticos; nunca los valores de las variables
#
# Códigos de salida:
#   0   éxito (también si la aplicación termina después de reiniciar)
#   2   argumentos inválidos o variables de entorno faltantes
#   3   VM no encontrada en VirtualBox
#   4   fallo de comunicación con la VM (SSH)
#   5   falló un paso en la VM (la restauración terminó bien)
#   6   la unidad no existe o no la creó Stratus
#   7   VM sin dirección IP; verifique que esté encendida
#   12  el balanceador (--jump) está apagado o sin IPv4 en puente
#   9   la edición falló y también la restauración: puede quedar una configuración parcial
#   11  el ejecutable no existe en el directorio de la aplicación
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
# 1.1 Exigir exactamente 8 argumentos; si no, imprimir el uso en stderr y salir con 2.
# 1.2 Validar cada valor contra su lista permitida (service-common.sh); si alguno falla, salir con
#     2. Si env_file no es "-", debe ser un archivo válido de variables.
# 1.3 Exigir APP_SSH_KEY_PATH; si falta, salir con 2.
# Ninguna validación fallida debe llamar a VBoxManage ni a ssh.

parse_jump "$@"
shift "$jump_shift"

if (($# != 8)); then
  fail 2 "Uso: edit-service.sh [--jump <lb>] <vm_name> <name> <description> <executable> <args> <restart> <restart_sec> <env_file>"
fi
vm_name="$1"
name="$2"
description="$3"
executable="$4"
args="$5"
restart="$6"
restart_sec="$7"
env_file="$8"

if [[ ! "$vm_name" =~ ^[a-z0-9][a-z0-9-]{0,31}$ ]]; then
  fail 2 'vm_name inválido: debe cumplir ^[a-z0-9][a-z0-9-]{0,31}$'
fi
validate_name "$name" || exit 2
validate_description "$description" || exit 2
validate_executable "$executable" || exit 2
validate_args "$args" || exit 2
validate_restart "$restart" || exit 2
validate_restart_sec "$restart_sec" || exit 2
if [[ "$env_file" != - ]]; then
  validate_env_file "$env_file" || exit 2
fi
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

# == 3. Comprobar el ejecutable y el estado previo (REQ-6, REQ-23) ==
# 3.1 El ejecutable debe ser un archivo regular de /opt/<name>; si no, salir con 11 sin cambiar nada.
# 3.2 Leer el estado activo: "en ejecución" es active o activating (por ejemplo "activating
#     (auto-restart)"). Solo entonces se reinicia al final y, si algo falla, se vuelve a ese estado.
#     Si el estado no se puede leer, salir con 5: no se sabría si reiniciar.

exe_present="$(guest 5 sh -c '[ -f "/opt/$1/$2" ] && echo yes || echo no' sh "$name" "$executable")" || exit $?
if [[ "$exe_present" != yes ]]; then
  fail 11 "El ejecutable no existe en el directorio de la aplicación"
fi

previous_state="$(read_state)"
if [[ "$previous_state" == desconocido ]]; then
  fail 5 "No se pudo leer el estado del servicio $name"
fi
was_running=no
if is_running "$previous_state"; then
  was_running=yes
fi

# == 4. Restauración si algo falla (REQ-23) ==
# on_exit corre al terminar el script por cualquier camino (éxito, error o SIGTERM, que sale con
# 143). Según la fase:
#   backup  falló el respaldo: se borra lo que alcanzó a crearse; la VM no cambió.
#   armed   ya se pudo cambiar algo. Se intenta todo aunque un paso falle:
#     4.1 copiar de vuelta la unidad y el archivo de variables;
#     4.2 "systemctl daemon-reload";
#     4.3 volver al estado previo: "restart" si estaba en ejecución, "stop" si no;
#     4.4 borrar el respaldo.
# Si todo salió bien se conserva el código original; si algo falló, se imprime qué puede quedar y se
# sale con 9. Los comandos usan vm_exec y no guest porque guest sale en el primer fallo.

backup="/var/lib/stratus/backup/$name"
phase=none

on_exit() {
  local rc=$?
  trap - EXIT
  if ((rc == 0)); then
    exit 0
  fi
  if [[ "$phase" == backup ]]; then
    vm_exec "$vm_name" rm -rf "$backup" >&2 || true
  elif [[ "$phase" == armed ]]; then
    local restore_failed=no
    vm_exec "$vm_name" cp -p "$backup/$name.service" /etc/systemd/system/ >&2 || restore_failed=yes
    vm_exec "$vm_name" sh -c '[ ! -e "$1" ] || cp -p "$1" "$2"' sh "$backup/$name.env" /etc/ >&2 || restore_failed=yes
    vm_exec "$vm_name" systemctl daemon-reload >&2 || restore_failed=yes
    if [[ "$was_running" == yes ]]; then
      vm_exec "$vm_name" systemctl restart "$name" >&2 || restore_failed=yes
    else
      vm_exec "$vm_name" systemctl stop "$name" >&2 || restore_failed=yes
    fi
    vm_exec "$vm_name" rm -rf "$backup" >&2 || restore_failed=yes
    if [[ "$restore_failed" == yes ]]; then
      echo "Restauración fallida; puede quedar en la VM: /etc/systemd/system/$name.service, /etc/$name.env, $backup" >&2
      exit 9
    fi
  fi
  exit "$rc"
}
# SIGTERM (el límite de tiempo del backend) y SIGINT salen con 143 y 130 para pasar por on_exit.
trap on_exit EXIT
trap 'exit 143' TERM
trap 'exit 130' INT

# == 5. Respaldar (REQ-23) ==
# 5.1 Vaciar y crear /var/lib/stratus/backup/<name> (modo 700).
# 5.2 Copiar con "cp -p" la unidad y, si existe, /etc/<name>.env.
# 5.3 Si falla un paso, salir con 5; on_exit borra el respaldo parcial. Con el respaldo completo la
#     restauración queda armada: cualquier fallo posterior la ejecuta.

phase=backup
guest 5 rm -rf "$backup"
guest 5 mkdir -p -m 700 "$backup"
guest 5 cp -p "/etc/systemd/system/$name.service" "$backup/"
guest 5 sh -c '[ ! -e "$1" ] || cp -p "$1" "$2"' sh "/etc/$name.env" "$backup/"
phase=armed

# == 6. Aplicar los cambios (REQ-6, REQ-11, REQ-13, REQ-22, REQ-23) ==
# 6.1 Permiso de ejecución para el dueño del ejecutable.
# 6.2 Reescribir la unidad con la marca y las líneas de REQ-11.
# 6.3 Si env_file no es "-", reescribir /etc/<name>.env (600 root:root) completo: los valores
#     llegan por la entrada estándar de ssh, no en argv.
# 6.4 daemon-reload y, solo si estaba en ejecución, "systemctl restart". El estado habilitado no cambia.

guest 5 chmod u+x "/opt/$name/$executable"
render_unit "$name" "$description" "$executable" "$args" "$restart" "$restart_sec" |
  write_remote_file "/etc/systemd/system/$name.service" 644
if [[ "$env_file" != - ]]; then
  encode_env "$env_file" | write_remote_file "/etc/$name.env" 600
fi
guest 5 systemctl daemon-reload
if [[ "$was_running" == yes ]]; then
  guest 5 systemctl restart "$name"
fi

# == 7. Cerrar (REQ-23) ==
# 7.1 La edición ya se aplicó: se desactiva la restauración y se borra el respaldo. Si no se puede
#     borrar, salir con 5 sin restaurar (el cambio es válido; el respaldo queda en la VM).
# 7.2 Si se reinició: esperar 3 segundos y leer el estado activo ("desconocido" si no se puede).
#     Una aplicación que termina después de reiniciar no es un fallo.
# 7.3 Imprimir el mensaje y, solo si se reinició, la línea "active_state=<estado>".

phase=done
vm_exec "$vm_name" rm -rf "$backup" >&2 || fail 5 "No se pudo borrar el respaldo $backup de la VM; bórrelo a mano"

state=""
if [[ "$was_running" == yes ]]; then
  sleep 3
  state="$(read_state)"
fi
echo "Servicio $name actualizado en $vm_name"
if [[ -n "$state" ]]; then
  echo "active_state=$state"
fi

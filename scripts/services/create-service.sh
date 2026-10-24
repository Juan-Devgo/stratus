#!/usr/bin/env bash
# create-service.sh - Despliega una aplicación como servicio systemd en una VM: crea un usuario de
# sistema con el nombre del servicio, extrae el .tar.gz en /opt/<name>, escribe el archivo de
# variables (600 root:root) y una unidad endurecida, y la habilita e inicia. Entra por SSH con la
# llave del backend contra la IP de la VM. Si algo falla después del primer cambio, la limpieza
# quita todo lo que se creó, para que no quede un servicio a medias.
#
# Uso:
#   create-service.sh [--jump <lb>] <vm_name> <name> <description> <archive_file> <executable> <args> <restart>
#                     <restart_sec> <env_file>
#
# Argumentos:
#   --jump <lb>  (opcional, siempre primero) balanceador por el que se llega a la VM cuando es un
#                servidor asignado a él (004); el backend lo agrega
#   vm_name       nombre de la VM, ^[a-z0-9][a-z0-9-]{0,31}$
#   name          nombre del servicio, usuario y grupo, ^[a-z][a-z0-9-]{0,31}$
#   description   de 1 a 100 caracteres UTF-8 sin control, % \ ni "
#   archive_file  archivo .tar.gz del host (legible); se revisa antes de tocar la VM
#   executable    ruta relativa a /opt/<name>, debe ser un archivo regular del .tar.gz
#   args          argumentos del ejecutable: vacío, o hasta 16 tokens separados por un espacio
#   restart       no, always u on-failure
#   restart_sec   entero de 0 a 3600
#   env_file      archivo del host con hasta 50 filas KEY=VALUE (puede estar vacío). Los valores son
#                 secretos: solo viajan por la entrada estándar de ssh, nunca en argv ni en la salida.
#
# Variables de entorno:
#   APP_SSH_KEY_PATH  llave privada del backend
#
# Salida:
#   stdout  "Servicio <name> creado en <vm_name>" y, unos 3 segundos después de iniciar, la línea
#           "active_state=<estado>" (active, failed, "activating (auto-restart)"...)
#   stderr  diagnósticos; nunca los valores de las variables
#
# Códigos de salida:
#   0   éxito (también si la aplicación termina después de iniciar: el servicio queda instalado)
#   2   argumentos inválidos o variables de entorno faltantes
#   3   VM no encontrada en VirtualBox
#   4   fallo de comunicación con la VM (SSH)
#   5   falló un paso en la VM (la limpieza terminó bien)
#   7   VM sin dirección IP; verifique que esté encendida
#   12  el balanceador (--jump) está apagado o sin IPv4 en puente
#   8   ya existe un servicio, usuario, grupo, /opt/<name> o /etc/<name>.env con ese nombre
#   9   la creación falló y también la limpieza: puede quedar un servicio parcial
#   10  archivo comprimido inválido
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
# 1.1 Exigir exactamente 9 argumentos; si no, imprimir el uso en stderr y salir con 2.
# 1.2 Validar cada valor contra su lista permitida (service-common.sh); si alguno falla, salir con
#     2. Los mensajes nunca repiten los valores de las variables.
# 1.3 archive_file debe ser un archivo legible; APP_SSH_KEY_PATH debe estar definida.
# 1.4 Revisar el .tar.gz en el host con check_archive: sale con 10 (archivo inválido) o con 11 (el
#     ejecutable no es un archivo regular del .tar.gz).
# Ninguna validación fallida debe llamar a VBoxManage ni a ssh.

parse_jump "$@"
shift "$jump_shift"

if (($# != 9)); then
  fail 2 "Uso: create-service.sh [--jump <lb>] <vm_name> <name> <description> <archive_file> <executable> <args> <restart> <restart_sec> <env_file>"
fi
vm_name="$1"
name="$2"
description="$3"
archive_file="$4"
executable="$5"
args="$6"
restart="$7"
restart_sec="$8"
env_file="$9"

if [[ ! "$vm_name" =~ ^[a-z0-9][a-z0-9-]{0,31}$ ]]; then
  fail 2 'vm_name inválido: debe cumplir ^[a-z0-9][a-z0-9-]{0,31}$'
fi
validate_name "$name" || exit 2
validate_description "$description" || exit 2
validate_executable "$executable" || exit 2
validate_args "$args" || exit 2
validate_restart "$restart" || exit 2
validate_restart_sec "$restart_sec" || exit 2
validate_env_file "$env_file" || exit 2
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
# 2.3 Cada comando en la VM va por "guest <código_de_fallo> <comando...>" (service-common.sh).

# Con --jump, registrar la ruta antes de cualquier vm_ip o vm_exec: la VM se alcanza por el balanceador.
[[ -z "$jump_lb" ]] || vm_route "$vm_name" "$jump_lb"

if ! VBoxManage showvminfo "$vm_name" >/dev/null 2>&1; then
  fail 3 "VM no encontrada en VirtualBox"
fi

vm_ip "$vm_name" >/dev/null || exit $?

# == 3. Comprobar que el nombre esté libre (REQ-2) ==
# 3.1 Un solo comando remoto responde "exists" si hay una unidad <name>.service (de cualquier
#     origen), un usuario, un grupo, /opt/<name> o /etc/<name>.env con ese nombre, o "free". El
#     nombre llega como argumento.
# 3.2 "exists" sale con 8; nada ha cambiado en la VM.

lookup="$(guest 5 sh -c 'if systemctl cat "$1.service" >/dev/null 2>&1 || [ -e "/etc/systemd/system/$1.service" ] || getent passwd "$1" >/dev/null || getent group "$1" >/dev/null || [ -e "/opt/$1" ] || [ -e "/etc/$1.env" ]; then echo exists; else echo free; fi' sh "$name")" || exit $?
case "$lookup" in
  free) ;;
  exists) fail 8 "Ya existe un servicio o usuario con ese nombre en la VM" ;;
  *) fail 5 "Respuesta inesperada al buscar el servicio en la VM" ;;
esac

# == 4. Limpieza si algo falla (REQ-14) ==
# on_exit corre al terminar el script por cualquier camino (éxito, error o SIGTERM, que sale con
# 143). Solo actúa si ya se creó el usuario (service_created=yes) y el script termina con error.
# Intenta todos los pasos aunque uno falle y su salida va a stderr:
#   4.1 "systemctl disable --now <name>" (tolerante: puede no existir aún).
#   4.2 "rm -f" de la unidad y del archivo de variables, con sus archivos .tmp.
#   4.3 "rm -rf /opt/<name>".
#   4.4 "userdel" si existe el usuario y "groupdel" si existe el grupo.
#   4.5 "systemctl daemon-reload".
#   4.6 Verificación: no queda la unidad, el archivo de variables, /opt/<name>, el usuario ni el grupo.
# Si todo salió bien se conserva el código original; si algo falló, se imprime qué puede quedar y
# se sale con 9. Los comandos usan vm_exec y no guest porque guest sale en el primer fallo.

service_created=no

on_exit() {
  local rc=$?
  trap - EXIT
  if ((rc == 0)) || [[ "$service_created" != yes ]]; then
    exit "$rc"
  fi
  local cleanup_failed=no
  vm_exec "$vm_name" systemctl disable --now "$name" >&2 || true
  vm_exec "$vm_name" rm -f "/etc/systemd/system/$name.service" "/etc/systemd/system/$name.service.tmp" \
    "/etc/$name.env" "/etc/$name.env.tmp" >&2 || cleanup_failed=yes
  vm_exec "$vm_name" rm -rf "/opt/$name" >&2 || cleanup_failed=yes
  vm_exec "$vm_name" sh -c '! getent passwd "$1" >/dev/null || userdel "$1"' sh "$name" >&2 || cleanup_failed=yes
  vm_exec "$vm_name" sh -c '! getent group "$1" >/dev/null || groupdel "$1"' sh "$name" >&2 || cleanup_failed=yes
  vm_exec "$vm_name" systemctl daemon-reload >&2 || cleanup_failed=yes
  vm_exec "$vm_name" sh -c '[ ! -e "/etc/systemd/system/$1.service" ] && [ ! -e "/etc/$1.env" ] && [ ! -e "/opt/$1" ] && ! getent passwd "$1" >/dev/null && ! getent group "$1" >/dev/null' sh "$name" >&2 ||
    cleanup_failed=yes
  if [[ "$cleanup_failed" == yes ]]; then
    echo "Limpieza fallida; puede quedar en la VM: /etc/systemd/system/$name.service, /etc/$name.env, /opt/$name, usuario y grupo $name" >&2
    exit 9
  fi
  exit "$rc"
}
# SIGTERM (el límite de tiempo del backend) y SIGINT salen con 143 y 130 para pasar por on_exit.
trap on_exit EXIT
trap 'exit 143' TERM
trap 'exit 130' INT

# == 5. Crear el usuario y la aplicación (REQ-10) ==
# 5.1 "useradd --system --user-group" con home /opt/<name> (sin crearlo) y shell nologin. Si falla,
#     salir con 5; aún no se cambió nada.
# 5.2 Desde aquí cualquier fallo pasa por on_exit, que deshace todo (sección 4).
# 5.3 Crear /opt/<name> (755, dueño <name>) y extraer el .tar.gz con tar leyendo el archivo del host
#     por la entrada estándar de ssh: no queda ninguna copia temporal en la VM. "--no-same-owner"
#     evita que el archivo imponga dueños.
# 5.4 "chown -R <name>:<name>" sobre /opt/<name> y permiso de ejecución para el dueño del ejecutable.

guest 5 useradd --system --user-group --home-dir "/opt/$name" --no-create-home --shell /usr/sbin/nologin "$name"
service_created=yes

guest 5 install -d -m 755 -o "$name" -g "$name" "/opt/$name"
guest 5 tar -xzf - -C "/opt/$name" --no-same-owner <"$archive_file"
guest 5 chown -R "$name:$name" "/opt/$name"
guest 5 chmod u+x "/opt/$name/$executable"

# == 6. Escribir el archivo de variables y la unidad, y arrancar (REQ-10, REQ-11, REQ-13) ==
# 6.1 /etc/<name>.env (600 root:root): las variables codificadas para systemd llegan por la
#     entrada estándar de ssh, así que los valores no aparecen en argv de ningún proceso.
# 6.2 /etc/systemd/system/<name>.service (644) con la marca y las líneas de REQ-11.
# 6.3 daemon-reload, y "enable --now": habilita el servicio y lo inicia.
# 6.4 Un fallo en cualquiera de estos pasos sale con 5 y pasa por la limpieza.

encode_env "$env_file" | write_remote_file "/etc/$name.env" 600
render_unit "$name" "$description" "$executable" "$args" "$restart" "$restart_sec" |
  write_remote_file "/etc/systemd/system/$name.service" 644
guest 5 systemctl daemon-reload
guest 5 systemctl enable --now "$name"

# == 7. Resultado (REQ-15) ==
# 7.1 Todos los pasos de creación terminaron bien: se desactiva la limpieza (on_exit). Si la aplicación sale
#     después de iniciar, el servicio sigue instalado (no es un fallo).
# 7.2 Esperar 3 segundos y leer el estado activo; "desconocido" si no se puede leer.
# 7.3 Imprimir el mensaje y la línea "active_state=<estado>".

service_created=no
sleep 3
state="$(read_state)"
echo "Servicio $name creado en $vm_name"
echo "active_state=$state"

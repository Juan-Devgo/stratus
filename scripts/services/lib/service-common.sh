#!/usr/bin/env bash
# service-common.sh - Biblioteca compartida de los 12 scripts de scripts/services: listas
# permitidas, revisión del archivo .tar.gz en el host, acceso a la VM y generación de la unidad
# systemd. Hay una sola implementación aquí para no copiar los validadores en cada script.
#
# Uso (desde un script de scripts/services, que ya tiene "set -euo pipefail" y ya cargó vm-exec.sh):
#   source "$(dirname "${BASH_SOURCE[0]}")/../prepare/lib/vm-exec.sh"
#   source "$(dirname "${BASH_SOURCE[0]}")/lib/service-common.sh"
#
# Variables globales que lee (las define el script que la carga):
#   vm_name   nombre de la VM
#   name      nombre del servicio
#   fail_msg  (opcional) mensaje que imprime guest cuando falla un paso remoto
# Variables globales que deja parse_jump:
#   jump_lb     balanceador por el que se llega a la VM (004), o vacío si la VM es directa
#   jump_shift  argumentos que el script debe descartar con "shift" (0 o 2)
#
# Cómo usa parse_jump cada script de servicios, antes de contar sus argumentos:
#   parse_jump "$@"
#   shift "$jump_shift"
#   ...validar vm_name...
#   [[ -z "$jump_lb" ]] || vm_route "$vm_name" "$jump_lb"
#
# Funciones:
#   fail <código> <mensaje>              mensaje a stderr y salida con el código
#   validate_name|validate_description|validate_executable|validate_args|validate_restart|
#   validate_restart_sec <valor>         devuelven 1 con el motivo en stderr; el script sale con 2
#   validate_env_file <archivo>          idem, para el archivo de variables
#   check_archive <archivo> <ejecutable> solo en el host: devuelve 10 u 11
#   parse_jump "$@"                      lee "--jump <lb>" si va primero: deja jump_lb y jump_shift
#   guest <código> <comando...>          vm_exec; 2, 4, 7 y 12 pasan tal cual, otro fallo sale con <código>
#   require_managed                      sale con 6 si la unidad no tiene la marca de Stratus
#   read_state                           imprime el estado activo (o "activating (<subestado>)")
#   is_running <estado>                  verdadero para active y activating*
#   render_unit <name> <desc> <exe> <args> <restart> <restart_sec>   imprime la unidad (REQ-11)
#   encode_env <archivo>                 imprime KEY="valor" con \ " ` $ escapados
#   write_remote_file <destino> <modo>   stdin -> archivo root:root en la VM
#   read_unit_fields                     imprime descripción⇥ejecutable⇥args⇥restart⇥restart_sec
#
# Códigos de salida (los que usa cada función; los scripts usan la misma tabla):
#   0   éxito
#   2   argumentos inválidos (lo decide el script que llama a los validadores)
#   3   VM no encontrada
#   4   fallo de comunicación con la VM (SSH)
#   5   falló un paso remoto (la limpieza o la restauración terminó bien)
#   6   la unidad no existe o no la creó Stratus
#   7   la VM no tiene dirección IP
#   12  el balanceador de la VM (--jump) está apagado o sin IPv4 en puente
#   8   ya existe un servicio, usuario, grupo, /opt/<name> o /etc/<name>.env con ese nombre
#   9   falló la operación y también la limpieza o la restauración
#   10  archivo comprimido inválido
#   11  el ejecutable no existe en el directorio de la aplicación
#   99  operación no implementada
#
# Este archivo se carga con "source": no activa "set -euo pipefail" porque hereda el modo
# estricto del script que lo carga. Necesita vm-exec.sh ya cargado para guest y los ayudantes
# remotos.

# Primera línea de toda unidad creada por Stratus. require_managed y list-services la buscan
# como línea exacta, así que no se puede cambiar sin migrar las unidades existentes.
MARKER='# stratus-managed: servicio creado por Stratus'

# == 1. fail <código> <mensaje> ==
# 1.1 Imprimir el mensaje en stderr y salir con el código.

fail() {
  echo "$2" >&2
  exit "$1"
}

# == 2. Validadores (P3: la misma lista permitida que la UI y el backend) ==
# Cada uno devuelve 0 si el valor es válido, o 1 con el motivo en stderr. Nunca repiten el valor.

# 2.1 validate_name: ^[a-z][a-z0-9-]{0,31}$ (REQ-2).
validate_name() {
  if [[ ! "${1-}" =~ ^[a-z][a-z0-9-]{0,31}$ ]]; then
    echo "name inválido: debe cumplir ^[a-z][a-z0-9-]{0,31}$" >&2
    return 1
  fi
}

# 2.2 validate_description: de 1 a 100 caracteres UTF-8 sin control, % \ ni " (REQ-3).
# El conteo es en caracteres, no en bytes, por eso se fija una locale UTF-8 solo dentro de la función.
validate_description() {
  local LC_ALL=C.UTF-8
  local value="${1-}"
  if ((${#value} < 1 || ${#value} > 100)) || [[ "$value" =~ [[:cntrl:]%\\\"] ]]; then
    echo 'description inválida: de 1 a 100 caracteres, sin %, \, " ni caracteres de control' >&2
    return 1
  fi
}

# 2.3 validate_executable: ruta relativa, hasta 255 caracteres, sin segmentos "." ni ".." (REQ-6).
validate_executable() {
  local value="${1-}" segment
  if [[ ! "$value" =~ ^[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)*$ ]] || ((${#value} > 255)); then
    echo "executable inválido: ruta relativa de hasta 255 caracteres con letras, números, . _ - y /" >&2
    return 1
  fi
  local -a segments
  IFS=/ read -ra segments <<<"$value"
  for segment in "${segments[@]}"; do
    if [[ "$segment" == . || "$segment" == .. ]]; then
      echo "executable inválido: no puede tener segmentos . ni .." >&2
      return 1
    fi
  done
}

# 2.4 validate_args: vacío, o de 1 a 16 tokens separados por un solo espacio (REQ-7).
# Un espacio al inicio, al final o doble es inválido. Primero se revisa la cadena completa para que
# un salto de línea no se pierda en "read".
validate_args() {
  local value="${1-}" token
  [[ -z "$value" ]] && return 0
  if [[ ! "$value" =~ ^[A-Za-z0-9._/:=,@+\ -]+$ || "$value" == " "* || "$value" == *" " || "$value" == *"  "* ]]; then
    echo "args inválidos: tokens de A-Za-z0-9._/:=,@+- separados por un solo espacio" >&2
    return 1
  fi
  local -a tokens
  IFS=' ' read -ra tokens <<<"$value"
  if ((${#tokens[@]} > 16)); then
    echo "args inválidos: máximo 16 tokens" >&2
    return 1
  fi
  for token in "${tokens[@]}"; do
    if [[ ! "$token" =~ ^[A-Za-z0-9._/:=,@+-]{1,128}$ ]]; then
      echo "args inválidos: cada token debe tener de 1 a 128 caracteres permitidos" >&2
      return 1
    fi
  done
}

# 2.5 validate_restart: no, always u on-failure (REQ-8).
validate_restart() {
  case "${1-}" in
    no | always | on-failure) ;;
    *)
      echo "restart inválido: debe ser no, always u on-failure" >&2
      return 1
      ;;
  esac
}

# 2.6 validate_restart_sec: entero de 0 a 3600 sin ceros a la izquierda (REQ-8).
validate_restart_sec() {
  local value="${1-}"
  if [[ ! "$value" =~ ^(0|[1-9][0-9]{0,3})$ ]] || ((value > 3600)); then
    echo "restart_sec inválido: debe ser un entero de 0 a 3600" >&2
    return 1
  fi
}

# 2.7 validate_env_file: archivo legible de hasta 50 filas KEY=VALUE (REQ-9).
# La clave cumple ^[A-Z_][A-Z0-9_]{0,63}$ y no se repite; el valor tiene hasta 1024 caracteres
# ASCII imprimibles (espacio a ~), comprobado con LC_ALL=C. Los mensajes nombran la fila y la
# clave, nunca el valor, porque puede ser un secreto (REQ-13).
validate_env_file() {
  local LC_ALL=C
  local file="${1-}" line key value row=0
  local -A seen=()
  if [[ -z "$file" || ! -f "$file" || ! -r "$file" ]]; then
    echo "env_file inválido: el archivo no existe o no se puede leer" >&2
    return 1
  fi
  while IFS= read -r line || [[ -n "$line" ]]; do
    row=$((row + 1))
    if ((row > 50)); then
      echo "env_file inválido: máximo 50 variables" >&2
      return 1
    fi
    if [[ "$line" != *=* ]]; then
      echo "env_file inválido: la fila $row no tiene el formato KEY=VALUE" >&2
      return 1
    fi
    key="${line%%=*}"
    value="${line#*=}"
    if [[ ! "$key" =~ ^[A-Z_][A-Z0-9_]{0,63}$ ]]; then
      echo "env_file inválido: la clave de la fila $row no cumple ^[A-Z_][A-Z0-9_]{0,63}$" >&2
      return 1
    fi
    if [[ -n "${seen[$key]-}" ]]; then
      echo "env_file inválido: la clave $key está repetida" >&2
      return 1
    fi
    seen[$key]=1
    if [[ ! "$value" =~ ^[\ -~]*$ ]] || ((${#value} > 1024)); then
      echo "env_file inválido: el valor de $key debe tener hasta 1024 caracteres ASCII imprimibles" >&2
      return 1
    fi
  done <"$file"
}

# == 3. check_archive <archivo> <ejecutable> ==
# Revisa en el host, antes de tocar la VM, que el .tar.gz sea seguro de extraer (REQ-5, REQ-6).
# 3.1 "gzip -t" debe aceptar el archivo.
# 3.2 Dos listados, "tar -tzf" (nombres) y "tar -tvzf" (el primer carácter es el tipo), deben
#     tener el mismo número de líneas; un nombre con salto de línea los desalinearía.
# 3.3 Solo se permiten entradas "-" (archivo regular) y "d" (directorio): nada de enlaces
#     simbólicos o duros, dispositivos, tuberías ni otros tipos.
# 3.4 Ningún nombre empieza con "/" ni tiene un segmento "..".
# 3.5 El ejecutable, sin el prefijo "./", debe ser una entrada "-".
# Devuelve 10 (mensaje "Archivo comprimido inválido") o 11 (mensaje del ejecutable); 0 si todo está bien.

check_archive() {
  local file="$1" exe="$2" names verbose i entry segment found=no
  local -a name_lines type_lines segments
  local invalid="Archivo comprimido inválido"

  gzip -t -- "$file" >/dev/null 2>&1 || {
    echo "$invalid" >&2
    return 10
  }
  names="$(tar -tzf "$file" 2>/dev/null)" || {
    echo "$invalid" >&2
    return 10
  }
  verbose="$(tar -tvzf "$file" 2>/dev/null)" || {
    echo "$invalid" >&2
    return 10
  }
  mapfile -t name_lines <<<"$names"
  mapfile -t type_lines <<<"$verbose"
  if ((${#name_lines[@]} != ${#type_lines[@]})); then
    echo "$invalid" >&2
    return 10
  fi

  for i in "${!name_lines[@]}"; do
    # Un archivo vacío da una sola línea vacía: no hay nada que revisar.
    [[ -z "${name_lines[i]}" ]] && continue
    entry="${name_lines[i]}"
    case "${type_lines[i]:0:1}" in
      - | d) ;;
      *)
        echo "$invalid" >&2
        return 10
        ;;
    esac
    if [[ "$entry" == /* ]]; then
      echo "$invalid" >&2
      return 10
    fi
    IFS=/ read -ra segments <<<"$entry"
    for segment in "${segments[@]}"; do
      if [[ "$segment" == .. ]]; then
        echo "$invalid" >&2
        return 10
      fi
    done
    while [[ "$entry" == ./* ]]; do entry="${entry#./}"; done
    if [[ "$entry" == "$exe" && "${type_lines[i]:0:1}" == - ]]; then
      found=yes
    fi
  done

  if [[ "$found" != yes ]]; then
    echo "El ejecutable no existe en el directorio de la aplicación" >&2
    return 11
  fi
}

# == 3b. parse_jump "$@" (REQ-19) ==
# 3b.1 Si el primer argumento es "--jump", el segundo debe cumplir ^[a-z0-9][a-z0-9-]{0,31}$ (la regla
#      del nombre de una VM); si falta o no cumple, salir con 2. Se guarda en jump_lb y jump_shift
#      queda en 2.
# 3b.2 Sin "--jump", jump_lb queda vacío y jump_shift en 0.
# El backend pasa "--jump <lb>" solo cuando la VM es un servidor asignado a un balanceador; el script
# nunca deduce el rol por su cuenta. Un "--jump" en otra posición no se reconoce y falla la
# validación de vm_name.

parse_jump() {
  jump_lb=""
  jump_shift=0
  if [[ "${1-}" == --jump ]]; then
    if [[ ! "${2-}" =~ ^[a-z0-9][a-z0-9-]{0,31}$ ]]; then
      fail 2 '--jump inválido: debe cumplir ^[a-z0-9][a-z0-9-]{0,31}$'
    fi
    jump_lb="$2"
    jump_shift=2
  fi
}

# == 4. guest <código> <comando...> ==
# Ejecuta el comando en la VM con vm_exec (SSH como root con la llave del backend).
# 4.1 Si vm_exec termina bien, devolver 0 (la salida y la entrada estándar pasan sin cambios).
# 4.2 Los códigos 2 (llave del backend), 4 (SSH), 7 (sin IP) y 12 (balanceador apagado) salen tal cual.
# 4.3 Cualquier otro fallo imprime ${fail_msg} o "Falló el paso <programa> en la VM (código N)"
#     en stderr y sale con <código>. Llamarlo en un pipe o en $( ) solo sale de ese subshell:
#     el script debe revisar el estado (con set -e o "|| exit $?").

guest() {
  local code="$1" rc=0
  shift
  vm_exec "$vm_name" "$@" || rc=$?
  ((rc == 0)) && return 0
  case "$rc" in
    2 | 4 | 7 | 12) exit "$rc" ;;
  esac
  echo "${fail_msg:-Falló el paso $1 en la VM (código $rc)}" >&2
  exit "$code"
}

# == 5. require_managed ==
# 5.1 Preguntar en la VM si /etc/systemd/system/<name>.service tiene la marca como línea exacta.
# 5.2 Si no, salir con 6 (REQ-18). El archivo ausente cuenta como "no".

require_managed() {
  local answer
  answer="$(guest 5 sh -c 'grep -qxF "$2" "/etc/systemd/system/$1.service" 2>/dev/null && echo yes || echo no' sh "$name" "$MARKER")" || exit $?
  if [[ "$answer" != yes ]]; then
    fail 6 "Solo se pueden gestionar servicios creados por Stratus"
  fi
}

# == 6. read_state / is_running ==
# 6.1 read_state imprime ActiveState y, si es "activating", agrega " (<SubState>)", por ejemplo
#     "activating (auto-restart)". Si la lectura falla imprime "desconocido": la operación que
#     ya tuvo éxito no debe fallar por eso (REQ-15).
# 6.2 is_running: un servicio "en ejecución" es active o activating (REQ-23, REQ-24).

read_state() {
  local state
  state="$(vm_exec "$vm_name" sh -c 's=$(systemctl show -p ActiveState --value "$1.service"); if [ "$s" = activating ]; then s="$s ($(systemctl show -p SubState --value "$1.service"))"; fi; echo "$s"' sh "$name" 2>/dev/null </dev/null)" || state=""
  echo "${state:-desconocido}"
}

is_running() {
  [[ "${1-}" == active || "${1-}" == activating* ]]
}

# == 7. render_unit <name> <desc> <exe> <args> <restart> <restart_sec> ==
# Imprime la marca y todas las líneas de REQ-11. Solo la descripción, el ejecutable, los
# argumentos, Restart y RestartSec cambian; el endurecimiento queda siempre activo.

render_unit() {
  local svc="$1" desc="$2" exe="$3" args="$4" restart="$5" restart_sec="$6"
  printf '%s\n' "$MARKER"
  printf '[Unit]\n'
  printf 'Description=%s\n' "$desc"
  printf 'After=network.target\n'
  printf '\n[Service]\n'
  printf 'Type=simple\n'
  printf 'User=%s\n' "$svc"
  printf 'Group=%s\n' "$svc"
  printf 'WorkingDirectory=/opt/%s\n' "$svc"
  printf 'EnvironmentFile=/etc/%s.env\n' "$svc"
  printf 'ExecStart=/opt/%s/%s%s\n' "$svc" "$exe" "${args:+ $args}"
  printf 'Restart=%s\n' "$restart"
  printf 'RestartSec=%s\n' "$restart_sec"
  printf 'NoNewPrivileges=true\n'
  printf 'ProtectSystem=strict\n'
  printf 'ProtectHome=true\n'
  printf 'PrivateTmp=true\n'
  printf 'ReadWritePaths=/opt/%s\n' "$svc"
  printf '\n[Install]\n'
  printf 'WantedBy=multi-user.target\n'
}

# == 8. encode_env <archivo> ==
# Imprime cada fila KEY=VALUE como KEY="valor" con \ " ` y $ escapados con \. Es la regla de
# comillas dobles de EnvironmentFile de systemd, así el servicio recibe el valor tal cual se
# escribió (AC-10). Solo usa construcciones internas de bash.

encode_env() {
  local line key value
  while IFS= read -r line || [[ -n "$line" ]]; do
    key="${line%%=*}"
    value="${line#*=}"
    value="${value//\\/\\\\}"
    value="${value//\"/\\\"}"
    value="${value//\`/\\\`}"
    value="${value//\$/\\\$}"
    printf '%s="%s"\n' "$key" "$value"
  done <"$1"
}

# == 9. write_remote_file <destino> <modo> ==
# 9.1 Leer stdin hacia <destino>.tmp con umask 077, dejarlo root:root con el modo pedido y
#     moverlo con "mv -f": nunca queda un archivo a medias en la ruta final ni con permisos amplios.

write_remote_file() {
  guest 5 sh -c 'umask 077; cat >"$1.tmp" && chown root:root "$1.tmp" && chmod "$2" "$1.tmp" && mv -f "$1.tmp" "$1"' sh "$1" "$2"
}

# == 10. read_unit_fields ==
# Imprime en una línea, separados por tabuladores: descripción, ejecutable (relativo a
# /opt/<name>), argumentos, Restart y RestartSec, leídos del archivo de la unidad. Los argumentos
# pueden estar vacíos: quien lo lea debe separar con cut o awk -F, no con "IFS=$'\t' read" (junta
# los tabuladores seguidos).

read_unit_fields() {
  guest 5 sh -c 'awk -v n="$1" '\''/^Description=/ { d = substr($0, 13) } /^ExecStart=/ { s = substr($0, 11); p = "/opt/" n "/"; if (index(s, p) == 1) s = substr(s, length(p) + 1); i = index(s, " "); if (i) { e = substr(s, 1, i - 1); a = substr(s, i + 1) } else { e = s; a = "" } } /^Restart=/ { r = substr($0, 9) } /^RestartSec=/ { t = substr($0, 12) } END { printf "%s\t%s\t%s\t%s\t%s\n", d, e, a, r, t }'\'' "/etc/systemd/system/$1.service"' sh "$name"
}

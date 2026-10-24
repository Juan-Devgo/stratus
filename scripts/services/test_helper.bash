# Utilidades compartidas por las pruebas bats de scripts/services.
# Uso en cada archivo .bats:
#   load test_helper
#
# Carga el helper de scripts/prepare (assert_script_structure, setup_stubs, stub_command,
# stub_vm_ip). Los scripts bajo prueba se ejecutan con "</dev/null", salvo que la prueba
# entregue su propia entrada estándar (por ejemplo un archivo .tar.gz para tar).
#
# Funciones:
#   setup_services_stubs <vm> <ip|->  setup_stubs + stub_vm_ip, un stub de ssh que responde según
#                                     variables de entorno (ver abajo) y un stub de systemctl.
#   setup_env_file <KEY=VALUE>...     escribe "$BATS_TEST_TMPDIR/env" (permisos 600) y exporta
#                                     ENV_FILE.
#   make_archive <salida> <ruta>...   crea un .tar.gz real con "tar -C <dir> -czf <salida> .", así
#                                     que los nombres llevan el prefijo "./". Una ruta que termina
#                                     en "/" es un directorio; las demás son archivos regulares.
#   make_bad_archive <tipo> <salida>  crea un .tar.gz inválido con un archivo "app" válido dentro:
#                                     dotdot, absolute (tar -P), symlink, hardlink, fifo o corrupt.
#   run_remote_locally                solo para list-services: el stub de ssh ejecuta el programa
#                                     remoto en el equipo local, con directorios de prueba en lugar
#                                     de /etc/systemd/system y /etc, y con el stub de systemctl.
#
# Archivos que deja el stub de ssh en "$BATS_TEST_TMPDIR":
#   calls.log       argv de cada llamada (lo escribe setup_stubs; los argumentos remotos vienen
#                   citados con "printf '%q '", igual que los envía vm_exec).
#   remote.log      el comando remoto de cada llamada ya descitado, una línea por llamada (los
#                   saltos de línea de un programa "sh -c" se cambian por espacios).
#   ssh-stdin.log   la entrada estándar de todas las llamadas, una detrás de otra.
#   stdin/<n>       la entrada estándar de la llamada número n (n empieza en 1).
#
# Variables de entorno que lee el stub de ssh (las responde según el texto del comando remoto):
#   LOOKUP=exists|free   revisión del nombre en create-service ("then echo exists"; por defecto free).
#   MANAGED=yes|no       require_managed ("grep -qxF"; por defecto yes).
#   EXE_PRESENT=yes|no   comprobación del ejecutable en edit-service ('[ -f "/opt/'; por defecto yes).
#   ACTIVE=<estado>      read_state ("systemctl show"; por defecto active). Se imprime tal cual,
#                        por ejemplo "failed" o "activating (auto-restart)".
#   ACTIVE_AFTER=<est.>  estado que se devuelve desde la segunda lectura (ACTIVE solo la primera).
#   UNIT_FIELDS=<tsv>    read_unit_fields ("ExecStart="): descripción, ejecutable, argumentos,
#                        Restart y RestartSec separados por tabuladores. Por defecto
#                        "Servidor de producción<TAB>demian-lee<TAB><TAB>on-failure<TAB>5".
#                        Ojo: un campo vacío entre tabuladores se pierde con "IFS=$'\t' read".
#   FAIL_REMOTE=<ERE>    la PRIMERA llamada cuyo comando coincide termina con 1 (una sola vez, así
#                        la restauración que repite el mismo comando, por ejemplo daemon-reload,
#                        sí funciona). Después de ese fallo empieza la fase de limpieza o
#                        restauración.
#   FAIL_SKIP=<n>        cuántas llamadas que coinciden con FAIL_REMOTE se dejan pasar antes de fallar
#                        (por ejemplo 1 para fallar en el segundo "rm -rf" y no en el primero).
#   FAIL_RESTORE=<ERE>   desde que FAIL_REMOTE falló, toda llamada que coincide termina con 1 (la
#                        limpieza o la restauración que también falla).
#   SSH_RC=<n>           toda llamada termina con ese código sin responder (255: fallo de SSH).
#   STATUS_RC=<n>        código de salida de "systemctl status" (por defecto 0). El stub ejecuta el
#                        programa remoto de status-service aquí, con el systemctl falso.

# Se carga con "source" y una ruta absoluta, no con "load": "load" resuelve la ruta relativa al
# archivo .bats, y service-common.bats vive un nivel más abajo (scripts/services/lib).
# shellcheck source=../prepare/test_helper.bash
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/../prepare/test_helper.bash"

# Directorio de los scripts del módulo de servicios (donde vive este helper).
SERVICES_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Marca que lleva toda unidad creada por Stratus (la misma de service-common.sh).
SERVICE_MARKER='# stratus-managed: servicio creado por Stratus'

# setup_services_stubs <vm> <ipv4|->
# Deja la VM <vm> encendida con esa IP (o apagada sin IP si es "-"), agrega el comportamiento del
# encabezado al stub de ssh y crea un stub de systemctl (status, show, is-enabled).
setup_services_stubs() {
  setup_stubs
  stub_vm_ip "$1" "$2"
  : >"$BATS_TEST_TMPDIR/remote.log"
  : >"$BATS_TEST_TMPDIR/ssh-stdin.log"
  mkdir -p "$BATS_TEST_TMPDIR/stdin"

  # El directorio de la prueba se fija al crear el stub, como hace setup_stubs con calls.log.
  {
    printf 'tmp=%q\n' "$BATS_TEST_TMPDIR"
    cat <<'EOF'
# Número de llamada y entrada estándar de esta llamada (puede ser un .tar.gz binario).
n=$(($(cat "$tmp/counter" 2>/dev/null || echo 0) + 1))
echo "$n" >"$tmp/counter"
cat >"$tmp/stdin/$n"
cat "$tmp/stdin/$n" >>"$tmp/ssh-stdin.log"
# El comando remoto es el último argumento, citado con %q: eval lo separa en palabras.
remote="${@: -1}"
eval "words=($remote)"
flat="${words[*]}"
flat="${flat//$'\n'/ }"
printf '%s\n' "$flat" >>"$tmp/remote.log"

# SSH_RC: simula un fallo de conexión (255 hace que vm_exec devuelva 4).
[[ -n "${SSH_RC:-}" ]] && exit "$SSH_RC"

# run_remote_locally: ejecuta el programa de list-services aquí, con directorios de prueba.
if [[ -e "$tmp/run-locally" ]]; then
  last=$((${#words[@]} - 1))
  if [[ "${words[last]}" == /etc && "${words[last - 1]}" == /etc/systemd/system ]]; then
    words[last - 1]="$tmp/units"
    words[last]="$tmp/etc"
  fi
  "${words[@]}"
  exit $?
fi

# Fallos simulados. FAIL_REMOTE falla una sola vez; después empieza la fase de limpieza.
if [[ -e "$tmp/failed" && -n "${FAIL_RESTORE:-}" && "$flat" =~ $FAIL_RESTORE ]]; then
  exit 1
fi
if [[ ! -e "$tmp/failed" && -n "${FAIL_REMOTE:-}" && "$flat" =~ $FAIL_REMOTE ]]; then
  skipped="$(cat "$tmp/skipped" 2>/dev/null || echo 0)"
  if ((skipped < ${FAIL_SKIP:-0})); then
    echo $((skipped + 1)) >"$tmp/skipped"
  else
    : >"$tmp/failed"
    exit 1
  fi
fi

# Respuestas de los programas remotos que devuelven un dato.
case "$flat" in
  *'then echo exists'*)
    echo "${LOOKUP:-free}"
    ;;
  *'grep -qxF'*)
    echo "${MANAGED:-yes}"
    ;;
  *'[ -f "/opt/'*)
    echo "${EXE_PRESENT:-yes}"
    ;;
  *'systemctl show'*)
    # ACTIVE_AFTER: desde la segunda lectura el estado cambia (por ejemplo, la app falla al reiniciar).
    reads="$(cat "$tmp/state-reads" 2>/dev/null || echo 0)"
    echo $((reads + 1)) >"$tmp/state-reads"
    if ((reads >= 1)) && [[ -n "${ACTIVE_AFTER:-}" ]]; then
      echo "$ACTIVE_AFTER"
    else
      echo "${ACTIVE:-active}"
    fi
    ;;
  *'ExecStart='*)
    printf '%s\n' "${UNIT_FIELDS-Servidor de producción$'\t'demian-lee$'\t\t'on-failure$'\t'5}"
    ;;
  *'systemctl status'*)
    # El programa real acepta el código 3 (servicio inactivo o fallido) y falla con otros.
    "${words[@]}"
    exit $?
    ;;
esac
exit 0
EOF
  } >>"$BATS_TEST_TMPDIR/bin/ssh"

  # systemctl falso: solo lo ejecuta el stub de ssh cuando corre un programa remoto aquí.
  # Estados de prueba: demian-lee activo y habilitado, cualquier otro inactivo y deshabilitado.
  cat >"$BATS_TEST_TMPDIR/bin/systemctl" <<'EOF'
#!/usr/bin/env bash
cmd="${1:-}"
shift || true
unit_state() { [[ "$1" == demian-lee ]] && echo active || echo inactive; }
case "$cmd" in
  status)
    printf '● %s - Servicio de prueba\n     Active: failed (Result: exit-code)\n' "${*: -1}"
    exit "${STATUS_RC:-0}"
    ;;
  show)
    props=()
    value=no
    unit=
    while (($#)); do
      case "$1" in
        -p) props+=("$2"); shift ;;
        --value) value=yes ;;
        -*) ;;
        *) unit="${1%.service}" ;;
      esac
      shift
    done
    for p in "${props[@]}"; do
      case "$p" in
        ActiveState) v="$(unit_state "$unit")" ;;
        SubState) [[ "$(unit_state "$unit")" == active ]] && v=running || v=dead ;;
        *) v= ;;
      esac
      if [[ "$value" == yes ]]; then echo "$v"; else echo "$p=$v"; fi
    done
    ;;
  is-enabled)
    unit="${*: -1}"
    unit="${unit%.service}"
    if [[ "$unit" == demian-lee ]]; then
      echo enabled
    else
      echo disabled
      exit 1
    fi
    ;;
esac
exit 0
EOF
  chmod +x "$BATS_TEST_TMPDIR/bin/systemctl"
}

# setup_env_file <KEY=VALUE>...
# Simula el archivo secreto que entrega el backend: una línea KEY=VALUE por argumento, permisos 600.
setup_env_file() {
  ENV_FILE="$BATS_TEST_TMPDIR/env"
  : >"$ENV_FILE"
  local line
  for line in "$@"; do
    printf '%s\n' "$line" >>"$ENV_FILE"
  done
  chmod 600 "$ENV_FILE"
  export ENV_FILE
}

# make_archive <salida> <ruta>...
# Árbol de prueba bajo "$BATS_TEST_TMPDIR/src-<n>" empaquetado con "tar -C <dir> -czf <salida> .".
make_archive() {
  local out="$1" src path
  shift
  src="$(mktemp -d "$BATS_TEST_TMPDIR/src-XXXXXX")"
  for path in "$@"; do
    if [[ "$path" == */ ]]; then
      mkdir -p "$src/$path"
    else
      mkdir -p "$src/$(dirname "$path")"
      printf '#!/bin/sh\nexit 0\n' >"$src/$path"
    fi
  done
  tar -C "$src" -czf "$out" .
}

# make_bad_archive <tipo> <salida>
# Tipos: dotdot (../evil), absolute (/tmp/...), symlink, hardlink, fifo y corrupt (gzip dañado).
# Todos incluyen "app", así que el rechazo viene del contenido y no de un ejecutable ausente.
make_bad_archive() {
  local kind="$1" out="$2" src
  src="$(mktemp -d "$BATS_TEST_TMPDIR/bad-XXXXXX")"
  printf '#!/bin/sh\nexit 0\n' >"$src/app"
  case "$kind" in
    dotdot)
      printf 'x\n' >"$src/evil"
      tar -C "$src" -czf "$out" --transform 's,^\./evil$,../evil,' ./app ./evil
      ;;
    absolute)
      printf 'x\n' >"$src/evil"
      tar -P -C "$src" -czf "$out" --transform "s,^\./evil\$,$src/evil," ./app ./evil
      ;;
    symlink)
      ln -s /etc/passwd "$src/link"
      tar -C "$src" -czf "$out" .
      ;;
    hardlink)
      printf 'x\n' >"$src/data"
      ln "$src/data" "$src/copy"
      tar -C "$src" -czf "$out" ./app ./data ./copy
      ;;
    fifo)
      mkfifo "$src/pipe"
      tar -C "$src" -czf "$out" .
      ;;
    corrupt)
      tar -C "$src" -czf "$out" .
      # Se corta el archivo a la mitad: gzip -t falla por fin de archivo inesperado.
      head -c 30 "$out" >"$out.part"
      mv "$out.part" "$out"
      ;;
    *)
      echo "make_bad_archive: tipo desconocido: $kind" >&2
      return 1
      ;;
  esac
}

# _fixture_unit <archivo> <marca yes|no> <descripción> <ejecutable> <args> <restart> <restart_sec>
# Escribe una unidad con el formato de REQ-11 (con o sin la marca de Stratus).
_fixture_unit() {
  local file="$1" marked="$2" desc="$3" exe="$4" args="$5" restart="$6" sec="$7" name
  name="$(basename "$file" .service)"
  {
    [[ "$marked" == yes ]] && printf '%s\n' "$SERVICE_MARKER"
    printf '[Unit]\nDescription=%s\nAfter=network.target\n\n' "$desc"
    printf '[Service]\nType=simple\nUser=%s\nGroup=%s\nWorkingDirectory=/opt/%s\n' "$name" "$name" "$name"
    printf 'EnvironmentFile=/etc/%s.env\nExecStart=%s%s\n' "$name" "$exe" "${args:+ $args}"
    printf 'Restart=%s\nRestartSec=%s\n\n[Install]\nWantedBy=multi-user.target\n' "$restart" "$sec"
  } >"$file"
}

# run_remote_locally
# Para list-services.bats. Datos:
#   units/demian-lee.service  con marca, "Servidor de producción", Restart on-failure, RestartSec 5,
#                             activo y habilitado; /etc/demian-lee.env con PORT, SERVER_NAME y
#                             ADMIN_PASSWORD="Secreta123".
#   units/api.service         con marca, "API interna", args "--port 9090", Restart always,
#                             RestartSec 10, inactivo y deshabilitado; sin archivo env.
#   units/ssh.service y cron.service  sin marca (no deben aparecer).
run_remote_locally() {
  local units="$BATS_TEST_TMPDIR/units" etc="$BATS_TEST_TMPDIR/etc"
  mkdir -p "$units" "$etc"
  _fixture_unit "$units/demian-lee.service" yes "Servidor de producción" /opt/demian-lee/demian-lee "" on-failure 5
  _fixture_unit "$units/api.service" yes "API interna" /opt/api/bin/server "--port 9090" always 10
  _fixture_unit "$units/ssh.service" no "OpenBSD Secure Shell server" "/usr/sbin/sshd -D" "" on-failure 5
  _fixture_unit "$units/cron.service" no "Regular background program processing daemon" /usr/sbin/cron "" on-failure 5
  printf 'PORT="8080"\nSERVER_NAME="Servidor-Produccion-1"\nADMIN_PASSWORD="Secreta123"\n' >"$etc/demian-lee.env"
  : >"$BATS_TEST_TMPDIR/run-locally"
}

# Número de la primera línea de remote.log (después de la línea $2, si se da) que coincide con la
# expresión regular extendida $1; falla si no hay ninguna. Un "+" literal se escribe "[+]".
line_of() {
  local after="${2:-0}" n
  n="$(awk -v after="$after" -v re="$1" 'NR > after && $0 ~ re { print NR; exit }' "$BATS_TEST_TMPDIR/remote.log")"
  [[ -n "$n" ]] || {
    echo "no hay una línea de remote.log (después de $after) que coincida con: $1" >&2
    return 1
  }
  echo "$n"
}

# assert_in_order <ERE>...: cada expresión aparece en remote.log después de la anterior.
assert_in_order() {
  local last=0 re
  for re in "$@"; do
    last="$(line_of "$re" "$last")" || return 1
  done
}

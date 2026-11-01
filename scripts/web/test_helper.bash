# Utilidades compartidas por las pruebas bats de scripts/web (005).
# Uso en cada archivo .bats:
#   load test_helper          (los de lib/ usan: load ../test_helper)
#
# Carga el helper de scripts/prepare (assert_script_structure, setup_stubs, stub_command,
# stub_vm_ip, stub_vm_nics, stub_jump_topology, assert_routed_ssh).
#
# Funciones:
#   setup_web_stubs           setup_stubs + una COPIA de scripts/ en "$BATS_TEST_TMPDIR/scripts"
#                             (los scripts bajo prueba se ejecutan desde ahí: "$WX") en la que
#                             ../prepare/{create-vm,delete-vm,stop-vm}.sh son stubs que solo
#                             registran su argv en calls.log, y un stub de ssh que registra y
#                             responde según variables de entorno (ver abajo). scp ya lo registra
#                             setup_stubs.
#   make_archive <salida> <ruta>...
#                             crea un .tar.gz real con "tar -C <dir> -czf <salida> .": los nombres
#                             llevan el prefijo "./". Una ruta que termina en "/" es un directorio.
#
# Variables que dejan las funciones:
#   WEB_DIR   scripts/web original (para assert_script_structure)
#   WX        scripts/web de la copia; "$WX/../prepare" es la copia de prepare
#
# Archivos que deja el stub de ssh en "$BATS_TEST_TMPDIR":
#   calls.log      argv de cada llamada (también VBoxManage, scp, sleep y los stubs de prepare).
#   remote.log     el comando remoto de cada llamada ya descitado, una línea por llamada (los
#                  saltos de línea de un programa "sh -c" se cambian por espacios).
#   stdin/<n>      la entrada estándar de la llamada número n (n empieza en 1).
#
# Variables de entorno que lee el stub de ssh:
#   FAIL_REMOTE=<ERE>    la PRIMERA llamada cuyo comando coincide termina con 1 (una sola vez).
#   FAIL_RESTORE=<ERE>   desde que FAIL_REMOTE falló, toda llamada que coincide termina con 1.
#   CONFIGTEST=ok|fail   "nginx -t" y "apache2ctl configtest" (por defecto ok).
#   ACTIVE_STATE=<s>     salida de "systemctl is-active" y "systemctl show -p ActiveState"
#                        (por defecto active); con otro valor is-active sale con 3.
#   STATUS_OUT=<texto>   salida del programa de "systemctl status" (unit_status).
# Variables de entorno de los stubs de prepare:
#   RC_CREATE_VM, RC_DELETE_VM, RC_STOP_VM   código de salida (por defecto 0).

# Directorio de los scripts del módulo (donde vive este helper).
WEB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Se carga con "source" y una ruta absoluta, no con "load": "load" resuelve la ruta relativa al
# archivo .bats, y los de lib/ están un nivel más abajo.
# shellcheck source=../prepare/test_helper.bash
source "$WEB_DIR/../prepare/test_helper.bash"

# _copy_scripts <dir_tmp> <dir_módulo>: copia scripts/<módulo>, prepare y services en
# "<dir_tmp>/scripts" y cambia create-vm.sh, delete-vm.sh y stop-vm.sh de la copia por stubs que
# registran su argv. El stub de create-vm.sh imprime la línea de éxito de 001.
_copy_scripts() {
  local tmp="$1" module_dir="$2" name var
  mkdir -p "$tmp/stdin" "$tmp/scripts"
  : >"$tmp/remote.log"
  cp -R "$module_dir" "$tmp/scripts/"
  cp -R "$PREPARE_DIR" "$tmp/scripts/prepare"
  cp -R "$PREPARE_DIR/../services" "$tmp/scripts/services"
  for name in create-vm delete-vm stop-vm; do
    var="RC_$(tr 'a-z-' 'A-Z_' <<<"$name")"
    {
      echo '#!/usr/bin/env bash'
      printf '{ printf %%s %q; printf " %%s" "$@"; printf "\\n"; } >>%q\n' "$name.sh" "$tmp/calls.log"
      if [[ "$name" == stop-vm ]]; then
        printf 'echo poweroff >%q-"$1"\n' "$tmp/state"
      fi
      if [[ "$name" == create-vm ]]; then
        printf '[[ "${%s:-0}" != 0 ]] || echo "VM $1 creada y encendida; IP 192.168.1.40"\n' "$var"
      fi
      printf 'exit "${%s:-0}"\n' "$var"
    } >"$tmp/scripts/prepare/$name.sh"
    chmod +x "$tmp/scripts/prepare/$name.sh"
  done
}

# _ssh_stub_head <dir_tmp>: comienzo del stub de ssh (número de llamada, stdin, remote.log,
# FAIL_REMOTE y FAIL_RESTORE).
_ssh_stub_head() {
  printf 'tmp=%q\n' "$1"
  cat <<'STUB'
n=$(($(cat "$tmp/counter" 2>/dev/null || echo 0) + 1))
echo "$n" >"$tmp/counter"
cat >"$tmp/stdin/$n"
# El comando remoto es el último argumento, citado con %q: eval lo separa en palabras.
remote="${@: -1}"
eval "words=($remote)"
flat="${words[*]}"
flat="${flat//$'\n'/ }"
printf '%s\n' "$flat" >>"$tmp/remote.log"

if [[ -e "$tmp/failed" && -n "${FAIL_RESTORE:-}" && "$flat" =~ $FAIL_RESTORE ]]; then
  echo "falló la restauración" >&2
  exit 1
fi
if [[ ! -e "$tmp/failed" && -n "${FAIL_REMOTE:-}" && "$flat" =~ $FAIL_REMOTE ]]; then
  : >"$tmp/failed"
  echo "falló el paso remoto" >&2
  exit 1
fi
STUB
}

# _ssh_stub_units: respuestas de systemctl comunes a web y dns.
_ssh_stub_units() {
  cat <<'STUB'
if [[ "$flat" == *"systemctl is-active"* ]]; then
  echo "${ACTIVE_STATE:-active}"
  [[ "${ACTIVE_STATE:-active}" == active ]]
  exit $?
fi
if [[ "$flat" == *"systemctl show -p ActiveState"* ]]; then
  echo "${ACTIVE_STATE:-active}"
  exit 0
fi
if [[ "$flat" == *"systemctl status --no-pager"* ]]; then
  printf '%s\n' "${STATUS_OUT:-● servicio activo}"
  exit 0
fi
STUB
}

# setup_web_stubs
setup_web_stubs() {
  setup_stubs
  local tmp="$BATS_TEST_TMPDIR"
  _copy_scripts "$tmp" "$WEB_DIR"
  WX="$tmp/scripts/web"
  {
    _ssh_stub_head "$tmp"
    cat <<'STUB'
if [[ "$flat" == *"nginx -t"* || "$flat" == *"apache2ctl configtest"* ]]; then
  if [[ "${CONFIGTEST:-ok}" == fail ]]; then
    echo "error de sintaxis en la configuración del sitio" >&2
    exit 1
  fi
  exit 0
fi
STUB
    _ssh_stub_units
    echo 'exit 0'
  } >>"$tmp/bin/ssh"
}

# make_archive <salida> <ruta>...
make_archive() {
  local out="$1" src path
  shift
  src="$(mktemp -d "$BATS_TEST_TMPDIR/src-XXXXXX")"
  for path in "$@"; do
    if [[ "$path" == */ ]]; then
      mkdir -p "$src/$path"
    else
      mkdir -p "$src/$(dirname "$path")"
      printf '<p>%s</p>\n' "$path" >"$src/$path"
    fi
  done
  tar -C "$src" -czf "$out" .
}

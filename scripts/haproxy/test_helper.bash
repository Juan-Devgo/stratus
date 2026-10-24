# Utilidades compartidas por las pruebas bats de scripts/haproxy.
# Uso en cada archivo .bats:
#   load test_helper          (los de lib/ usan: load ../test_helper)
#
# Carga el helper de scripts/prepare (assert_script_structure, setup_stubs, stub_command,
# stub_vm_ip, stub_vm_nics, stub_jump_topology, assert_routed_ssh).
#
# Funciones:
#   setup_haproxy_stubs         setup_stubs + una COPIA de scripts/ en "$BATS_TEST_TMPDIR/scripts"
#                               (los scripts bajo prueba se ejecutan desde ahí: "$HX") en la que
#                               ../prepare/{create-vm,delete-vm,stop-vm}.sh son stubs que solo
#                               registran su argv en calls.log, y un stub de ssh que registra y
#                               responde según variables de entorno (ver abajo).
#   set_vm_state <vm> <running|poweroff>
#                               estado de energía inicial de <vm> para stub_vm_power y
#                               stub_ip_sequence (por defecto running). Cambia con el tiempo:
#                               "startvm" lo deja running; "controlvm poweroff" y el stub de
#                               stop-vm.sh lo dejan poweroff.
#   stub_vm_power <vm> <running|poweroff>
#                               <vm> existe y su VMState sale de ese estado; sin IP ni MAC (para los
#                               scripts que solo la encienden o la apagan).
#   stub_ip_sequence <vm> <ip|->...
#                               respuestas sucesivas de la IPv4 del adaptador 1 de <vm> (una por
#                               sondeo); la última se repite. <vm> tiene dos NICs (adaptador 1 en la
#                               red interna, adaptador 2 NAT con 10.0.3.15), como un servidor ya
#                               asignado; su VMState sale del estado de energía (por defecto running).
#
# Variables que dejan las funciones:
#   HAPROXY_DIR   scripts/haproxy original (para assert_script_structure)
#   HX            scripts/haproxy de la copia; "$HX/../prepare" es la copia de prepare
#
# Archivos que deja el stub de ssh en "$BATS_TEST_TMPDIR":
#   calls.log       argv de cada llamada (también VBoxManage, sleep y los stubs de prepare).
#   remote.log      el comando remoto de cada llamada ya descitado, una línea por llamada (los
#                   saltos de línea de un programa "sh -c" se cambian por espacios).
#   ssh-stdin.log   la entrada estándar de todas las llamadas, una detrás de otra.
#   stdin/<n>       la entrada estándar de la llamada número n (n empieza en 1).
#
# Variables de entorno que lee el stub de ssh (las responde según el texto del comando remoto):
#   FAIL_REMOTE=<ERE>   la PRIMERA llamada cuyo comando coincide termina con 1 (una sola vez).
#                       Después empieza la fase de limpieza o restauración.
#   FAIL_RESTORE=<ERE>  desde que FAIL_REMOTE falló, toda llamada que coincide termina con 1.
#   HAPROXY_CHECK=ok|fail   "haproxy -c" (por defecto ok); fail imprime un error y sale con 1.
#   SS_OUT=<texto>      salida de "ss -Hltnp" (por defecto una línea con haproxy escuchando).
#   CMP=same|diff       "cmp -s" (por defecto diff: termina con 1).
#   STAT_CSV=<texto>    salida del programa que abre /dev/tcp/127.0.0.1/9999 (show stat en CSV).
#   SERVER_KEYS=<líneas> salida del programa que lee las llaves del servidor: "<dueño> <llave>".
#   UFW=yes|no          "command -v ufw" (por defecto no).
#   NIC2_NAME=<nombre>  interfaz que el invitado tiene para el adaptador 2 (por defecto enp0s8).
#   JUMP_LOCAL=yes      los comandos que tocan /var/lib/jump se ejecutan aquí mismo, con esa ruta
#                       cambiada a "$BATS_TEST_TMPDIR/jump", y con su entrada estándar.
# Variables de entorno que lee el stub de VBoxManage:
#   DHCPSERVERS=<texto> salida de "VBoxManage list dhcpservers".
# Variables de entorno de los stubs de prepare:
#   RC_CREATE_VM, RC_DELETE_VM, RC_STOP_VM   código de salida (por defecto 0).

# Directorio de los scripts del módulo (donde vive este helper).
HAPROXY_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Se carga con "source" y una ruta absoluta, no con "load": "load" resuelve la ruta relativa al
# archivo .bats, y los de lib/ están un nivel más abajo.
# shellcheck source=../prepare/test_helper.bash
source "$HAPROXY_DIR/../prepare/test_helper.bash"

# setup_haproxy_stubs
setup_haproxy_stubs() {
  setup_stubs
  local tmp="$BATS_TEST_TMPDIR"
  mkdir -p "$tmp/stdin" "$tmp/scripts"
  : >"$tmp/remote.log"
  : >"$tmp/ssh-stdin.log"

  # Copia de scripts/haproxy y scripts/prepare: los scripts de haproxy llaman a ../prepare/*.sh
  # por su ruta relativa, así que en la copia esos tres son stubs.
  cp -R "$HAPROXY_DIR" "$tmp/scripts/haproxy"
  cp -R "$PREPARE_DIR" "$tmp/scripts/prepare"
  HX="$tmp/scripts/haproxy"
  local name var
  for name in create-vm delete-vm stop-vm; do
    var="RC_$(tr 'a-z-' 'A-Z_' <<<"$name")"
    cat >"$tmp/scripts/prepare/$name.sh" <<EOF
#!/usr/bin/env bash
{
  printf '%s' "$name.sh"
  printf ' %s' "\$@"
  printf '\n'
} >>"$tmp/calls.log"
# stop-vm.sh deja la VM apagada: es el estado de energía de stub_vm_power y stub_ip_sequence.
if [[ "$name" == stop-vm ]]; then echo poweroff >"$tmp/state-\$1"; fi
exit "\${$var:-0}"
EOF
    chmod +x "$tmp/scripts/prepare/$name.sh"
  done

  # VBoxManage: list dhcpservers.
  stub_command VBoxManage 'if [[ "$1 ${2:-}" == "list dhcpservers" ]]; then printf "%s\n" "${DHCPSERVERS:-}"; exit 0; fi'

  # chown falso: solo JUMP_LOCAL lo usa, y la cuenta jump no existe en el equipo de prueba.
  printf '#!/usr/bin/env bash\nexit 0\n' >"$tmp/bin/chown"
  chmod +x "$tmp/bin/chown"

  # El directorio de la prueba se fija al crear el stub, como hace setup_stubs con calls.log.
  {
    printf 'tmp=%q\n' "$tmp"
    cat <<'EOF'
# Número de llamada y entrada estándar de esta llamada.
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

# Fallos provocados: FAIL_REMOTE una sola vez; después, FAIL_RESTORE en cada coincidencia.
if [[ -e "$tmp/failed" && -n "${FAIL_RESTORE:-}" && "$flat" =~ $FAIL_RESTORE ]]; then
  echo "falló la restauración" >&2
  exit 1
fi
if [[ ! -e "$tmp/failed" && -n "${FAIL_REMOTE:-}" && "$flat" =~ $FAIL_REMOTE ]]; then
  : >"$tmp/failed"
  echo "falló el paso remoto" >&2
  exit 1
fi

# JUMP_LOCAL: ejecuta aquí lo que toca /var/lib/jump, con esa ruta cambiada.
if [[ "${JUMP_LOCAL:-}" == yes && "$flat" == */var/lib/jump* ]]; then
  for i in "${!words[@]}"; do words[i]="${words[i]//\/var\/lib\/jump/$tmp/jump}"; done
  mkdir -p "$tmp/jump/.ssh"
  "${words[@]}" <"$tmp/stdin/$n"
  exit $?
fi

if [[ "$flat" == *"haproxy -c -f"* ]]; then
  if [[ "${HAPROXY_CHECK:-ok}" == fail ]]; then
    echo "[ALERT] (1) : Error en la configuración de prueba" >&2
    exit 1
  fi
  exit 0
fi
if [[ "$flat" == *"cmp -s"* ]]; then
  [[ "${CMP:-diff}" == same ]]
  exit $?
fi
if [[ "$flat" == *"ss -Hltnp"* ]]; then
  if [[ -n "${SS_OUT:-}" ]]; then
    printf '%s\n' "$SS_OUT"
  else
    echo 'LISTEN 0 4096 0.0.0.0:80 0.0.0.0:* users:(("haproxy",pid=1,fd=7))'
  fi
  exit 0
fi
if [[ "$flat" == *"/sys/class/net"* ]]; then
  echo "${NIC2_NAME:-enp0s8}"
  exit 0
fi
if [[ "$flat" == *"/dev/tcp/127.0.0.1/9999"* ]]; then
  printf '%s\n' "${STAT_CSV:-}"
  exit 0
fi
if [[ "$flat" == *"command -v ufw"* ]]; then
  [[ "${UFW:-no}" == yes ]]
  exit $?
fi
if [[ "$flat" == *"getent passwd"* ]]; then
  printf '%s\n' "${SERVER_KEYS:-}"
  exit 0
fi
exit 0
EOF
  } >>"$tmp/bin/ssh"
}

# stub_ip_sequence <vm> <ip|->...
# Cada lectura de /Net/1/V4/IP (el adaptador 1 de <vm>, ver vm_nic_ip) responde el siguiente valor
# de la lista; "-" responde "No value set!". Cuando la lista se acaba se repite el último.
stub_ip_sequence() {
  local vm="$1" tmp="$BATS_TEST_TMPDIR" value
  shift
  : >"$tmp/seq-$vm"
  : >"$tmp/seq-$vm.count"
  for value in "$@"; do
    if [[ "$value" == "-" ]]; then
      echo "No value set!" >>"$tmp/seq-$vm"
    else
      echo "Value: $value" >>"$tmp/seq-$vm"
    fi
  done
  _vm_power_handlers "$vm"
  stub_command VBoxManage "if [[ \"\$1\" == showvminfo && \"\${2:-}\" == \"$vm\" ]]; then
  printf 'VMState=\"%s\"\\nnic1=\"intnet\"\\nmacaddress1=\"080027AAAAAA\"\\nnic2=\"nat\"\\nmacaddress2=\"080027BBBBBB\"\\n' \"\$(cat '$tmp/state-$vm' 2>/dev/null || echo running)\"
  exit 0
fi
if [[ \"\$1 \${2:-}\" == \"guestproperty get\" && \"\${3:-}\" == \"$vm\" ]]; then
  case \"\${4:-}\" in
    /VirtualBox/GuestInfo/Net/0/MAC) echo 'Value: 080027BBBBBB' ;;
    /VirtualBox/GuestInfo/Net/0/V4/IP) echo 'Value: 10.0.3.15' ;;
    /VirtualBox/GuestInfo/Net/1/MAC) echo 'Value: 080027AAAAAA' ;;
    /VirtualBox/GuestInfo/Net/1/V4/IP)
      n=\$((\$(cat '$tmp/seq-$vm.count' 2>/dev/null || echo 0) + 1))
      echo \"\$n\" >'$tmp/seq-$vm.count'
      line=\"\$(sed -n \"\${n}p\" '$tmp/seq-$vm')\"
      [[ -n \"\$line\" ]] || line=\"\$(tail -n 1 '$tmp/seq-$vm')\"
      echo \"\$line\" ;;
    *) echo 'No value set!' ;;
  esac
  exit 0
fi"
}

# set_vm_state <vm> <running|poweroff>
set_vm_state() {
  echo "$2" >"$BATS_TEST_TMPDIR/state-$1"
}

# _vm_power_handlers <vm>: "startvm <vm>" deja la VM running y "controlvm <vm> poweroff" poweroff.
_vm_power_handlers() {
  local vm="$1" tmp="$BATS_TEST_TMPDIR"
  stub_command VBoxManage "if [[ \"\$1\" == startvm && \"\${2:-}\" == \"$vm\" ]]; then echo running >'$tmp/state-$vm'; exit 0; fi
if [[ \"\$1\" == controlvm && \"\${2:-}\" == \"$vm\" && \"\${3:-}\" == poweroff ]]; then echo poweroff >'$tmp/state-$vm'; exit 0; fi"
}

# stub_vm_power <vm> <running|poweroff>
stub_vm_power() {
  local vm="$1" tmp="$BATS_TEST_TMPDIR"
  set_vm_state "$vm" "$2"
  _vm_power_handlers "$vm"
  stub_command VBoxManage "if [[ \"\$1\" == showvminfo && \"\${2:-}\" == \"$vm\" ]]; then
  printf 'VMState=\"%s\"\\n' \"\$(cat '$tmp/state-$vm')\"
  exit 0
fi"
}

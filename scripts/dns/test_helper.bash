# Utilidades compartidas por las pruebas bats de scripts/dns (005).
# Uso en cada archivo .bats:
#   load test_helper          (los de lib/ usan: load ../test_helper)
#
# Carga el helper de scripts/web (y con él el de scripts/prepare) y el de scripts/haproxy por
# stub_ip_sequence, set_vm_state y stub_vm_power.
#
# Funciones:
#   setup_dns_stubs           como setup_web_stubs, con la copia de scripts/dns ("$DX"), el stub de
#                             ../prepare/stop-vm.sh y stubs de arping y ping (ver abajo).
#   hide_arping               deja el PATH sin ningún arping (ni el stub ni el del equipo), para
#                             probar "arping no está instalado en el host".
#   stub_dns_vm <vm> <ip_dhcp> <ip|->...
#                             VM de una sola NIC (MAC 080027EEEEEE) para create-dns: antes de
#                             "startvm <vm>" la IP del adaptador 1 es <ip_dhcp>; después, cada
#                             lectura responde el siguiente valor de la lista ("-" sin IP; al
#                             acabarse se repite el último). "startvm" la deja running y el stub de
#                             stop-vm.sh, poweroff.
#   resolver_program_file     imprime la ruta del stdin de la llamada a ssh del programa del
#                             resolvedor (la que lleva "stratus-resolver" en el comando remoto).
#   run_resolver_program <archivo> <dhclient|dhcpcd|none> <set|unset> <ip>
#                             ejecuta ese programa en esta máquina sobre la raíz falsa
#                             "$BATS_TEST_TMPDIR/root" (RROOT): /etc y /usr/lib/dhcpcd se reescriben
#                             a RROOT y el PATH solo trae las herramientas básicas más un
#                             dhclient o un dhcpcd vacío, para no depender del equipo.
#
# Variables que dejan las funciones:
#   DNS_DIR   scripts/dns original (para assert_script_structure)
#   DX        scripts/dns de la copia; "$DX/../prepare" es la copia de prepare
#
# Archivos que deja el stub de ssh en "$BATS_TEST_TMPDIR": calls.log, remote.log y stdin/<n>,
# como en scripts/web.
#
# Variables de entorno que leen los stubs:
#   ARPING=reply|none     arping responde (sale con 0) o no (sale con 1); por defecto none.
#   PING=reply|none       ping responde o no; por defecto none.
#   FAIL_REMOTE, FAIL_RESTORE, ACTIVE_STATE, STATUS_OUT   como en scripts/web.
#   ZONE_CHECK=ok|fail    named-checkzone (por defecto ok); fail imprime el error y sale con 1.
#   CONF_CHECK=ok|fail    named-checkconf (por defecto ok).
#   RNDC=ok|fail          "rndc reload": fail hace fallar SOLO la primera recarga (la de la
#                         restauración funciona); failall las hace fallar todas.
#   CURRENT_ZONE=<texto>  salida de "cat /var/lib/bind/db.*" (la zona actual); sin definir, el
#                         archivo no existe y cat sale con 1.
#   DHCP_CLIENT=dhclient|dhcpcd|none
#                         cliente DHCP del invitado para los programas del resolvedor: con none
#                         el programa remoto sale con 3 (por defecto dhclient).
#   DIG_OUT=<texto>       salida de dig (por defecto la IP del servidor DNS, 192.168.1.53).
#   DNS_IFACE=<nombre>    interfaz con la MAC buscada en /sys/class/net (por defecto enp0s3).

# Directorio de los scripts del módulo (donde vive este helper).
DNS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# shellcheck source=../web/test_helper.bash
source "$DNS_DIR/../web/test_helper.bash"
# shellcheck source=../haproxy/test_helper.bash
source "$DNS_DIR/../haproxy/test_helper.bash"

# setup_dns_stubs
setup_dns_stubs() {
  setup_stubs
  local tmp="$BATS_TEST_TMPDIR" cmd
  _copy_scripts "$tmp" "$DNS_DIR"
  # dns-common.sh carga web-common.sh.
  cp -R "$WEB_DIR" "$tmp/scripts/web"
  DX="$tmp/scripts/dns"

  for cmd in arping ping; do
    {
      echo '#!/usr/bin/env bash'
      printf 'printf "%%s\n" "%s${*:+ $*}" >>%q\n' "$cmd" "$tmp/calls.log"
      printf 'var=%s\n' "$(tr 'a-z' 'A-Z' <<<"$cmd")"
      # shellcheck disable=SC2016 # se expande en el stub
      echo '[[ "${!var:-none}" == reply ]]'
    } >"$tmp/bin/$cmd"
    chmod +x "$tmp/bin/$cmd"
  done

  {
    _ssh_stub_head "$tmp"
    cat <<'STUB'
if [[ "$flat" == *"named-checkzone"* ]]; then
  if [[ "${ZONE_CHECK:-ok}" == fail ]]; then
    echo "zone stratus.lan/IN: loading from master file db.stratus.lan.new failed: bad owner name"
    exit 1
  fi
  echo "zone stratus.lan/IN: loaded serial 1"
  echo "OK"
  exit 0
fi
if [[ "$flat" == *"named-checkconf"* ]]; then
  [[ "${CONF_CHECK:-ok}" == ok ]]
  exit $?
fi
if [[ "$flat" == *"rndc reload"* ]]; then
  case "${RNDC:-ok}" in
    failall) echo "rndc: 'reload' failed: failure" >&2; exit 1 ;;
    fail)
      if [[ ! -e "$tmp/rndc-failed" ]]; then
        : >"$tmp/rndc-failed"
        echo "rndc: 'reload' failed: failure" >&2
        exit 1
      fi
      ;;
  esac
  exit 0
fi
if [[ "$flat" == "cat /var/lib/bind/db."* ]]; then
  [[ -n "${CURRENT_ZONE+x}" ]] || exit 1
  printf '%s\n' "$CURRENT_ZONE"
  exit 0
fi
if [[ "$flat" == *"stratus-resolver"* && "${DHCP_CLIENT:-dhclient}" == none ]]; then
  echo "No se encontró un cliente DHCP compatible" >&2
  exit 3
fi
if [[ "$flat" == *"/sys/class/net"* ]]; then
  printf '%s\n' "${DNS_IFACE:-enp0s3}"
  exit 0
fi
if [[ "$flat" == dig* || "$flat" == *" dig "* ]]; then
  printf '%s\n' "${DIG_OUT:-192.168.1.53}"
  exit 0
fi
STUB
    _ssh_stub_units
    echo 'exit 0'
  } >>"$tmp/bin/ssh"
}

# hide_arping
# Arma "$BATS_TEST_TMPDIR/sys" con enlaces a todo /usr/bin menos arping y deja el PATH en el
# directorio de stubs (sin el stub de arping) más ese directorio.
hide_arping() {
  local tmp="$BATS_TEST_TMPDIR"
  rm -f "$tmp/bin/arping"
  mkdir -p "$tmp/sys"
  ln -sf /usr/bin/* "$tmp/sys/" 2>/dev/null || true
  rm -f "$tmp/sys/arping"
  export PATH="$tmp/bin:$tmp/sys"
}

# stub_dns_vm <vm> <ip_dhcp> <ip|->...
stub_dns_vm() {
  local vm="$1" dhcp="$2" tmp="$BATS_TEST_TMPDIR" value
  shift 2
  : >"$tmp/seq-$vm"
  : >"$tmp/seq-$vm.count"
  for value in "$@"; do
    if [[ "$value" == "-" ]]; then
      echo "No value set!" >>"$tmp/seq-$vm"
    else
      echo "Value: $value" >>"$tmp/seq-$vm"
    fi
  done
  stub_command VBoxManage "if [[ \"\$1\" == startvm && \"\${2:-}\" == \"$vm\" ]]; then echo running >'$tmp/state-$vm'; : >'$tmp/started-$vm'; exit 0; fi
if [[ \"\$1\" == showvminfo && \"\${2:-}\" == \"$vm\" ]]; then
  printf 'VMState=\"%s\"\nnic1=\"bridged\"\nmacaddress1=\"080027EEEEEE\"\nnic2=\"none\"\n' \"\$(cat '$tmp/state-$vm' 2>/dev/null || echo running)\"
  exit 0
fi
if [[ \"\$1 \${2:-}\" == \"guestproperty get\" && \"\${3:-}\" == \"$vm\" && \"\${4:-}\" == /VirtualBox/GuestInfo/Net/0/V4/IP ]]; then
  if [[ ! -e '$tmp/started-$vm' ]]; then
    echo 'Value: $dhcp'
  else
    n=\$((\$(cat '$tmp/seq-$vm.count' 2>/dev/null || echo 0) + 1))
    echo \"\$n\" >'$tmp/seq-$vm.count'
    line=\"\$(sed -n \"\${n}p\" '$tmp/seq-$vm')\"
    [[ -n \"\$line\" ]] || line=\"\$(tail -n 1 '$tmp/seq-$vm')\"
    echo \"\$line\"
  fi
  exit 0
fi"
}

# resolver_program_file
resolver_program_file() {
  local n
  n="$(grep -n -m1 'stratus-resolver' "$BATS_TEST_TMPDIR/remote.log" | cut -d: -f1)"
  [[ -n "$n" ]] || { echo "no hubo llamada con stratus-resolver" >&2; return 1; }
  echo "$BATS_TEST_TMPDIR/stdin/$n"
}

# run_resolver_program <archivo> <dhclient|dhcpcd|none> <set|unset> <ip>
run_resolver_program() {
  local file="$1" client="$2" tmp="$BATS_TEST_TMPDIR" tool
  RROOT="$tmp/root"
  mkdir -p "$RROOT/etc/dhcp" "$tmp/rbin"
  for tool in sh sed grep cat mv rm mkdir head chmod mktemp cp; do
    ln -sf "$(command -v "$tool")" "$tmp/rbin/$tool"
  done
  rm -f "$tmp/rbin/dhclient" "$tmp/rbin/dhcpcd"
  if [[ "$client" != none ]]; then
    printf '#!/bin/sh\n' >"$tmp/rbin/$client"
    chmod +x "$tmp/rbin/$client"
  fi
  sed -e "s#/etc/#$RROOT/etc/#g" -e "s#/usr/lib/dhcpcd/#$RROOT/usr/lib/dhcpcd/#g" "$file" >"$tmp/resolver.sh"
  PATH="$tmp/rbin" sh "$tmp/resolver.sh" stratus-resolver "$3" "$4"
}

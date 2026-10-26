#!/usr/bin/env bash
# haproxy-common.sh - Biblioteca compartida de los scripts de scripts/haproxy: listas permitidas,
# comprobación del balanceador, generación y aplicación de haproxy.cfg y espera de IPs internas.
# Hay una sola implementación aquí para no copiar los validadores ni apply_cfg en cada script.
#
# Uso (desde un script de scripts/haproxy, que ya tiene "set -euo pipefail" y ya cargó vm-exec.sh):
#   source "$(dirname "${BASH_SOURCE[0]}")/../prepare/lib/vm-exec.sh"
#   source "$(dirname "${BASH_SOURCE[0]}")/lib/haproxy-common.sh"
#
# Funciones:
#   fail <código> <mensaje>              mensaje a stderr y salida con el código
#   validate_lb_name|validate_vm_name|validate_memory|validate_cpus|validate_base_disk|
#   validate_bridge|validate_port|validate_frontend_port|validate_server_name|validate_subnet|
#   validate_reserved <valor>            devuelven 1 con el motivo en stderr; el script sale con 2
#   validate_spec <spec> [subnet]        idem, para "<server>,<vm>,<puerto>,<ip>,<on|off>"
#   validate_specs <subnet|-> [spec...]  idem, para la lista completa (hasta 10, nombres únicos)
#   lb_reachable <lb>                    devuelve 12 si el balanceador está apagado o sin IPv4 en puente
#   render_cfg <frontend_port> [spec...] imprime haproxy.cfg completo (REQ-7)
#   apply_cfg <lb> <puerto> [--if-changed]
#                                        aplica el haproxy.cfg de stdin en el balanceador (REQ-16)
#   wait_ips <subnet> <vm...>            imprime "<vm> <ip|->" tras esperar la IP interna (REQ-10)
#
# Variables globales que deja:
#   APPLY_RELOADED  yes|no, lo fija apply_cfg: si recargó HAProxy (con --if-changed puede ser no)
#
# Variables de entorno:
#   APP_SSH_KEY_PATH  llave privada del backend (la usa vm_exec)
#
# Códigos de salida (los que usan las funciones; los scripts usan la misma tabla):
#   0   éxito
#   1   (solo validadores) valor inválido; el motivo va a stderr
#   2   argumentos inválidos (lo decide el script que llama a los validadores)
#   4   fallo de comunicación con el balanceador (SSH)
#   7   la VM no tiene dirección IP
#   9   falló la operación y también la restauración
#   10  configuración de HAProxy inválida; se mantuvo la anterior
#   11  no se pudo recargar HAProxy; se restauró la configuración anterior
#   12  el balanceador está apagado o sin IPv4 en puente
#   99  operación no implementada
#
# Este archivo se carga con "source": no activa "set -euo pipefail" porque hereda el modo
# estricto del script que lo carga. Necesita vm-exec.sh ya cargado.

# Rutas de HAProxy en el balanceador.
HAPROXY_CFG=/etc/haproxy/haproxy.cfg

# == 1. fail <código> <mensaje> ==
# 1.1 Imprimir el mensaje en stderr y salir con el código.

fail() {
  echo "$2" >&2
  exit "$1"
}

# == 2. Validadores (P3: la misma lista permitida que la UI y el backend) ==
# Cada uno devuelve 0 si el valor es válido, o 1 con el motivo en stderr. Los mensajes no repiten
# el valor recibido.

# 2.1 Nombres de VM y de balanceador: ^[a-z0-9][a-z0-9-]{0,31}$ (igual que 001).
validate_lb_name() {
  if [[ ! "${1-}" =~ ^[a-z0-9][a-z0-9-]{0,31}$ ]]; then
    echo 'lb inválido: debe cumplir ^[a-z0-9][a-z0-9-]{0,31}$' >&2
    return 1
  fi
}

validate_vm_name() {
  if [[ ! "${1-}" =~ ^[a-z0-9][a-z0-9-]{0,31}$ ]]; then
    echo 'vm inválido: debe cumplir ^[a-z0-9][a-z0-9-]{0,31}$' >&2
    return 1
  fi
}

# 2.2 memory_mb 512-8192 y cpus 1-4 (igual que 001); solo dígitos antes de comparar.
validate_memory() {
  if [[ ! "${1-}" =~ ^[0-9]{1,9}$ ]] || ((10#$1 < 512 || 10#$1 > 8192)); then
    echo "memory_mb inválido: entero entre 512 y 8192" >&2
    return 1
  fi
}

validate_cpus() {
  if [[ ! "${1-}" =~ ^[0-9]{1,9}$ ]] || ((10#$1 < 1 || 10#$1 > 4)); then
    echo "cpus inválido: entero entre 1 y 4" >&2
    return 1
  fi
}

# 2.3 base_disk: ruta absoluta sin caracteres de control; bridge_adapter: nombre de interfaz.
validate_base_disk() {
  if [[ ! "${1-}" =~ ^/[^[:cntrl:]]*$ ]]; then
    echo "base_disk inválido: debe ser una ruta absoluta" >&2
    return 1
  fi
}

validate_bridge() {
  local pattern='^[A-Za-z0-9._:()+? -]{1,64}$'
  if [[ ! "${1-}" =~ $pattern ]]; then
    echo 'bridge_adapter inválido: use 1-64 letras, números, espacios o . _ : ( ) + ? -' >&2
    return 1
  fi
}

# 2.4 Puertos: ^[1-9][0-9]{0,4}$ y como máximo 65535. El del frontend tampoco puede ser 22.
validate_port() {
  if [[ ! "${1-}" =~ ^[1-9][0-9]{0,4}$ ]] || ((${1} > 65535)); then
    echo "El puerto debe ser un número entre 1 y 65535" >&2
    return 1
  fi
}

validate_frontend_port() {
  validate_port "${1-}" || return 1
  if ((${1} == 22)); then
    echo "El puerto 22 está reservado para SSH" >&2
    return 1
  fi
}

# 2.5 Nombre del servidor en el backend: server1 a server10.
validate_server_name() {
  if [[ ! "${1-}" =~ ^server([1-9]|10)$ ]]; then
    echo "server_name inválido: debe ser server1 a server10" >&2
    return 1
  fi
}

# 2.6 Subred N de 10.10.N.0/24: 1-254.
validate_subnet() {
  if [[ ! "${1-}" =~ ^[1-9][0-9]{0,2}$ ]] || ((${1} > 254)); then
    echo "subnet inválida: entero entre 1 y 254" >&2
    return 1
  fi
}

# 2.7 Subredes reservadas: "-" (ninguna) o ^[0-9]{1,3}(,[0-9]{1,3})*$ con cada valor entre 1 y 254.
validate_reserved() {
  local value="${1-}" n
  [[ "$value" == "-" ]] && return 0
  if [[ ! "$value" =~ ^[0-9]{1,3}(,[0-9]{1,3})*$ ]]; then
    echo "reserved inválido: use - o una lista de subredes separadas por coma" >&2
    return 1
  fi
  local -a items
  IFS=, read -ra items <<<"$value"
  for n in "${items[@]}"; do
    if ((10#$n < 1 || 10#$n > 254)); then
      echo "reserved inválido: cada subred debe estar entre 1 y 254" >&2
      return 1
    fi
  done
}

# 2.8 validate_spec <spec> [subnet]: "<server_name>,<vm>,<port>,<ip>,<on|off>" con 5 campos.
# La IP es 10.10.<subnet>.10-200 si se da la subred, o cualquier IPv4 si no.
validate_spec() {
  local spec="${1-}" subnet="${2-}" name vm port ip state octet rest
  if [[ "${spec//[^,]/}" != ,,,, ]]; then
    echo "spec inválida: deben ser 5 campos <server>,<vm>,<puerto>,<ip>,<on|off>" >&2
    return 1
  fi
  IFS=, read -r name vm port ip state rest <<<"$spec"
  validate_server_name "$name" 2>/dev/null || { echo "spec inválida: server_name" >&2; return 1; }
  validate_vm_name "$vm" 2>/dev/null || { echo "spec inválida: vm" >&2; return 1; }
  validate_port "$port" 2>/dev/null || { echo "spec inválida: puerto" >&2; return 1; }
  if [[ ! "$state" =~ ^(on|off)$ ]]; then
    echo "spec inválida: el último campo debe ser on u off" >&2
    return 1
  fi
  if [[ -n "$subnet" ]]; then
    if [[ ! "$ip" =~ ^10\.10\.${subnet}\.([0-9]{1,3})$ ]] || ((10#${BASH_REMATCH[1]} < 10 || 10#${BASH_REMATCH[1]} > 200)); then
      echo "spec inválida: la IP debe estar en 10.10.${subnet}.10-200" >&2
      return 1
    fi
  else
    if [[ ! "$ip" =~ ^[0-9]{1,3}(\.[0-9]{1,3}){3}$ ]]; then
      echo "spec inválida: ip" >&2
      return 1
    fi
    local -a octets
    IFS=. read -ra octets <<<"$ip"
    for octet in "${octets[@]}"; do
      if ((10#$octet > 255)); then
        echo "spec inválida: ip" >&2
        return 1
      fi
    done
  fi
}

# 2.9 validate_specs <subnet|-> [spec...]: hasta 10 especificaciones válidas y de nombre único.
validate_specs() {
  local subnet="$1" spec name
  shift
  local -A seen=()
  if (($# > 10)); then
    echo "Hay más de 10 servidores" >&2
    return 1
  fi
  [[ "$subnet" == "-" ]] && subnet=""
  for spec in "$@"; do
    validate_spec "$spec" "$subnet" || return 1
    name="${spec%%,*}"
    if [[ -n "${seen[$name]:-}" ]]; then
      echo "spec inválida: nombre de servidor repetido" >&2
      return 1
    fi
    seen[$name]=1
  done
}

# == 3. lb_reachable <lb> (REQ-19) ==
# 3.1 Comprobar que el balanceador esté encendido y tenga IPv4 en puente (adaptador 1). Si no,
#     imprimir "El balanceador <lb> está apagado; enciéndalo para llegar al servidor" en stderr y
#     devolver 12 sin cambiar nada. Usa _lb_ip de vm-exec.sh, el mismo criterio que vm_ip.

lb_reachable() {
  _lb_ip "$1" >/dev/null
}

# == 4. render_cfg <frontend_port> [spec...] (REQ-7, REQ-15) ==
# 4.1 Imprimir haproxy.cfg completo: global con el socket de estadísticas en 127.0.0.1:9999, defaults
#     HTTP, el frontend en "bind :<frontend_port>" y el backend roundrobin.
# 4.2 Una línea "server <nombre> <ip>:<puerto> check" por spec, en el orden recibido; con "off" lleva
#     además "disabled" (HAProxy no le manda tráfico). Sin specs el backend queda vacío y HAProxy
#     responde 503. Siempre se genera el archivo entero: nunca se edita en el sitio.
# Las specs ya deben estar validadas con validate_specs.

render_cfg() {
  local port="$1" spec name vm sport ip state
  shift
  cat <<EOF
global
  log /dev/log local0
  user haproxy
  group haproxy
  daemon
  stats socket ipv4@127.0.0.1:9999 level user
defaults
  mode http
  log global
  option httplog
  timeout connect 5s
  timeout client 50s
  timeout server 50s
frontend stratus_front
  bind :$port
  default_backend stratus_back
backend stratus_back
  balance roundrobin
EOF
  for spec in "$@"; do
    IFS=, read -r name vm sport ip state <<<"$spec"
    if [[ "$state" == off ]]; then
      echo "  server $name $ip:$sport check disabled"
    else
      echo "  server $name $ip:$sport check"
    fi
  done
}

# == 5. apply_cfg <lb> <puerto> [--if-changed] (REQ-16) ==
# Lee haproxy.cfg de stdin y lo aplica en el balanceador por SSH. <puerto> es el del frontend.
# 5.1 Subir stdin a /etc/haproxy/haproxy.cfg.new. Un fallo de SSH devuelve 4 (12 y 7 pasan tal cual).
# 5.2 Con --if-changed, "cmp -s .new haproxy.cfg": si son iguales, borrar .new, dejar
#     APPLY_RELOADED=no y devolver 0 sin revisar ni recargar (REQ-14).
# 5.3 "haproxy -c -f .new". Si falla: borrar .new, imprimir "Configuración de HAProxy inválida; se
#     mantuvo la anterior" y la salida de la revisión en stderr, y devolver 10.
# 5.4 "cp -p haproxy.cfg haproxy.cfg.bak", "mv .new haproxy.cfg" y "systemctl reload haproxy".
# 5.5 Verificar hasta 5 veces, con 1 s entre sondeos, que "ss -Hltnp 'sport = :<puerto>'" muestre
#     haproxy: "reload" devuelve 0 aunque los procesos nuevos no puedan enlazar el puerto (AC-45).
# 5.6 Si 5.4 o 5.5 fallan: devolver el archivo anterior ("mv .bak haproxy.cfg", solo si ya se hizo
#     el mv de 5.4), recargar de nuevo, imprimir "No se pudo recargar HAProxy; se mantuvo la
#     configuración anterior" con el motivo y devolver 11. Si la restauración también falla,
#     imprimir qué quedó y devolver 9.
# 5.7 Éxito: borrar .bak, APPLY_RELOADED=yes y devolver 0.

APPLY_RELOADED=no

apply_cfg() {
  local lb="$1" port="$2" if_changed="${3-}" cfg rc=0 new="$HAPROXY_CFG.new" bak="$HAPROXY_CFG.bak"
  local moved=no ok=no out="" i reason=""
  APPLY_RELOADED=no
  cfg="$(cat)"

  # 5.1 Subir el archivo nuevo por la entrada estándar de ssh.
  printf '%s\n' "$cfg" | vm_exec "$lb" sh -c 'cat > "$1"' sh "$new" || rc=$?
  if ((rc != 0)); then
    ((rc == 5)) && rc=4
    return "$rc"
  fi

  # 5.2 Comparar con el actual.
  if [[ "$if_changed" == --if-changed ]]; then
    rc=0
    vm_exec "$lb" cmp -s "$new" "$HAPROXY_CFG" </dev/null || rc=$?
    case "$rc" in
      0)
        vm_exec "$lb" rm -f "$new" </dev/null || true
        return 0
        ;;
      5) ;; # cmp encontró diferencias
      *) return "$rc" ;;
    esac
  fi

  # 5.3 Revisar la configuración antes de tocar la actual.
  rc=0
  vm_exec "$lb" haproxy -c -f "$new" </dev/null >&2 || rc=$?
  if ((rc != 0)); then
    vm_exec "$lb" rm -f "$new" </dev/null || true
    if ((rc == 5)); then
      echo "Configuración de HAProxy inválida; se mantuvo la anterior" >&2
      return 10
    fi
    return "$rc"
  fi

  # 5.4 y 5.5 Reemplazar, recargar y verificar. Cualquier fallo salta a la restauración.
  if vm_exec "$lb" cp -p "$HAPROXY_CFG" "$bak" </dev/null &&
    vm_exec "$lb" mv "$new" "$HAPROXY_CFG" </dev/null; then
    moved=yes
    if vm_exec "$lb" systemctl reload haproxy </dev/null; then
      for i in 1 2 3 4 5; do
        out="$(vm_exec "$lb" ss -Hltnp "sport = :$port" </dev/null 2>/dev/null)" || out=""
        if grep -q haproxy <<<"$out"; then
          ok=yes
          break
        fi
        sleep 1
      done
      [[ "$ok" == yes ]] || reason="HAProxy no escucha en el puerto $port: $out"
    else
      reason="falló systemctl reload haproxy"
    fi
  else
    reason="no se pudo reemplazar $HAPROXY_CFG"
  fi

  if [[ "$ok" == yes ]]; then
    vm_exec "$lb" rm -f "$bak" </dev/null || true
    APPLY_RELOADED=yes
    return 0
  fi

  # 5.6 Restaurar la configuración anterior.
  local restored=yes
  if [[ "$moved" == yes ]]; then
    vm_exec "$lb" mv "$bak" "$HAPROXY_CFG" </dev/null || restored=no
    vm_exec "$lb" systemctl reload haproxy </dev/null || restored=no
  else
    vm_exec "$lb" rm -f "$new" "$bak" </dev/null || restored=no
  fi
  if [[ "$restored" == no ]]; then
    echo "No se pudo recargar HAProxy y falló la restauración: revise $HAPROXY_CFG y $bak en el balanceador. $reason" >&2
    return 9
  fi
  echo "No se pudo recargar HAProxy; se mantuvo la configuración anterior. $reason" >&2
  return 11
}

# == 6. wait_ips <subnet> <vm...> (REQ-10, REQ-14) ==
# 6.1 Por cada VM, en paralelo, sondear la IPv4 de su adaptador 1 (vm_nic_ip) hasta 60 veces con
#     "sleep 2" entre sondeos: todas comparten el mismo límite de 120 s porque corren a la vez.
# 6.2 Solo vale una IP de 10.10.<subnet>.10 a 10.10.<subnet>.200 (la que reparte el servidor DHCP
#     de la red interna); cualquier otra, incluida la de otra red, se sigue esperando.
# 6.3 Imprimir "<vm> <ip>" por VM, en el orden recibido, o "<vm> -" si no llegó una IP en rango.

wait_ips() {
  local subnet="$1" dir i vm pids=()
  shift
  dir="$(mktemp -d)"
  i=0
  for vm in "$@"; do
    (
      local n ip last
      echo "-" >"$dir/$i"
      for ((n = 0; n < 60; n++)); do
        ip="$(vm_nic_ip "$vm" 1 2>/dev/null </dev/null)" || ip=""
        if [[ "$ip" =~ ^10\.10\.${subnet}\.([0-9]{1,3})$ ]]; then
          last="${BASH_REMATCH[1]}"
          if ((10#$last >= 10 && 10#$last <= 200)); then
            echo "$ip" >"$dir/$i"
            exit 0
          fi
        fi
        sleep 2
      done
    ) &
    pids+=("$!")
    i=$((i + 1))
  done
  ((${#pids[@]} == 0)) || wait "${pids[@]}"
  i=0
  for vm in "$@"; do
    echo "$vm $(cat "$dir/$i")"
    i=$((i + 1))
  done
  rm -rf "$dir"
}

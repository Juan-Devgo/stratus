#!/usr/bin/env bash
# refresh-lb.sh - Pone al día las IPs internas de los servidores de un balanceador: lee la IPv4 actual
# de cada servidor encendido, reescribe haproxy.cfg con ellas y recarga HAProxy solo si el archivo
# cambió. Un servidor apagado, o encendido sin IPv4 en el rango, conserva su última IP y queda con
# "disabled" para que HAProxy no le mande tráfico; en el siguiente refresco con IP vuelve a "on".
#
# Uso:
#   refresh-lb.sh <lb> <subnet> <frontend_port> [<spec>...]
#
# Argumentos:
#   lb             balanceador, ^[a-z0-9][a-z0-9-]{0,31}$; debe estar encendido con IPv4 en puente
#   subnet         N de la red interna 10.10.N.0/24 del balanceador, 1-254
#   frontend_port  puerto del frontend, 1-65535 y distinto de 22
#   spec           (cero a diez) servidores guardados, cada uno
#                  "<server_name>,<vm>,<port>,<ip>,<on|off>"; la IP guardada es la última conocida
#
# Variables de entorno:
#   APP_SSH_KEY_PATH  llave privada del backend
#
# Salida:
#   stdout  una línea "<server_name> <ip> <on|off>" por servidor, en el orden recibido, y al final
#           "reloaded=yes" o "reloaded=no" (si recargó HAProxy)
#   stderr  diagnósticos
#
# Códigos de salida:
#   0   éxito (también si nada cambió y no hubo recarga)
#   2   argumentos inválidos o variables de entorno faltantes
#   4   fallo de comunicación con el balanceador (SSH)
#   9   falló la recarga y también la restauración de haproxy.cfg
#   10  configuración de HAProxy inválida; se mantuvo la anterior
#   11  no se pudo recargar HAProxy; se restauró la configuración anterior
#   12  el balanceador está apagado o sin IPv4 en puente; no se cambió nada
#   99  operación no implementada

set -euo pipefail

scripts_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=../prepare/lib/vm-exec.sh
source "$scripts_dir/prepare/lib/vm-exec.sh"
# shellcheck source=lib/haproxy-common.sh
source "$scripts_dir/haproxy/lib/haproxy-common.sh"

# == 1. Validar argumentos ==
# 1.1 Exigir al menos 3 argumentos; si no, imprimir el uso en stderr y salir con 2.
# 1.2 Validar lb, subnet, frontend_port y las specs (hasta 10, nombres únicos, IP en 10.10.N.10-200)
#     contra sus listas permitidas (haproxy-common.sh); si alguna falla, salir con 2.
# 1.3 Exigir APP_SSH_KEY_PATH; si falta, salir con 2.
# Ninguna validación fallida debe llamar a VBoxManage ni a ssh.

if (($# < 3)); then
  fail 2 "Uso: refresh-lb.sh <lb> <subnet> <frontend_port> [<spec>...]"
fi
lb="$1"
subnet="$2"
frontend_port="$3"
shift 3
specs=("$@")

validate_lb_name "$lb" || exit 2
validate_subnet "$subnet" || exit 2
validate_frontend_port "$frontend_port" || exit 2
validate_specs "$subnet" "${specs[@]}" || exit 2
if [[ -z "${APP_SSH_KEY_PATH:-}" ]]; then
  fail 2 "Falta la variable de entorno APP_SSH_KEY_PATH"
fi

# == 2. Comprobar el balanceador ==
# 2.1 Debe estar encendido con IPv4 en puente (lb_reachable); si no, salir con 12 sin cambiar nada.

lb_reachable "$lb" || exit $?

# == 3. Leer las IPs actuales ==
# 3.1 Por cada spec, leer el estado de energía de su VM (VMState de showvminfo --machinereadable).
# 3.2 Con wait_ips, sondear en paralelo la IPv4 del adaptador 1 de todas las VMs encendidas: hasta 60
#     sondeos de 2 s, un único límite de 120 s para todas, no 120 s por servidor. Las apagadas no
#     se sondean.
# 3.3 Un servidor encendido con IPv4 en 10.10.N.10-200 queda con esa IP y "on". Cualquier otro (apagado
#     o sin IP en el rango) conserva su última IP y queda "off" (disabled).

running_vms=()
for spec in "${specs[@]}"; do
  IFS=, read -r _ spec_vm _ <<<"$spec"
  state="$(VBoxManage showvminfo "$spec_vm" --machinereadable 2>/dev/null | sed -n 's/^VMState="\(.*\)"$/\1/p' || true)"
  [[ "$state" != running ]] || running_vms+=("$spec_vm")
done

declare -A current_ip=()
if ((${#running_vms[@]} > 0)); then
  while read -r polled_vm polled_ip; do
    [[ "$polled_ip" == "-" ]] || current_ip[$polled_vm]="$polled_ip"
  done < <(wait_ips "$subnet" "${running_vms[@]}")
fi

new_specs=()
report=()
for spec in "${specs[@]}"; do
  IFS=, read -r spec_name spec_vm spec_port spec_ip _ <<<"$spec"
  if [[ -n "${current_ip[$spec_vm]:-}" ]]; then
    new_specs+=("$spec_name,$spec_vm,$spec_port,${current_ip[$spec_vm]},on")
    report+=("$spec_name ${current_ip[$spec_vm]} on")
  else
    new_specs+=("$spec_name,$spec_vm,$spec_port,$spec_ip,off")
    report+=("$spec_name $spec_ip off")
  fi
done

# == 4. Aplicar ==
# 4.1 Generar haproxy.cfg con las IPs nuevas y aplicarlo con apply_cfg --if-changed: si el archivo es
#     igual al actual no hay revisión ni recarga (APPLY_RELOADED=no). Su código 10 u 11 sale tal cual,
#     sin imprimir nada en stdout.

cfg="$(render_cfg "$frontend_port" "${new_specs[@]}")"
rc=0
apply_cfg "$lb" "$frontend_port" --if-changed <<<"$cfg" || rc=$?
if ((rc != 0)); then
  exit "$rc"
fi

# == 5. Resultado ==
# 5.1 Imprimir una línea "<server_name> <ip> <on|off>" por servidor y "reloaded=yes|no".

for line in "${report[@]}"; do
  echo "$line"
done
if [[ "$APPLY_RELOADED" == yes ]]; then
  echo "reloaded=yes"
else
  echo "reloaded=no"
fi

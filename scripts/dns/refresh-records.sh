#!/usr/bin/env bash
# refresh-records.sh - Actualiza las IPs de los registros A del servidor DNS (005). Espera, en una
# sola ventana de 120 s compartida, la IPv4 del servidor DNS y la de cada VM indicada que esté
# encendida; una VM que informa IPv4 la toma y las demás conservan la guardada. Renderiza la zona
# completa y solo la sube y recarga bind9 si cambió algo más que el serial (REQ-26, REQ-27).
#
# Uso:
#   refresh-records.sh <dns> <domain> <dns_ip> <wait_vms|-> [record...]
#
# Argumentos:
#   dns       nombre del servidor DNS, ^[a-z0-9][a-z0-9-]{0,31}$
#   domain    zona: 1-4 etiquetas ^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$ unidas por puntos
#   dns_ip    IPv4 del servidor DNS (su propio registro A)
#   wait_vms  VMs cuya IPv4 se espera, separadas por comas ("a,b"), o "-" para ninguna
#   record    registro guardado "<vm>,<ipv4>"; hasta 200, sin repetir VMs ni el servidor DNS
#
# Variables de entorno:
#   APP_SSH_KEY_PATH  llave privada del backend
#
# Salida:
#   stdout  "<vm> <ip>" por registro, en el orden recibido, y en la última línea "reloaded=yes|no"
#   stderr  diagnósticos (con la salida de named-checkzone si la zona es inválida)
#
# Códigos de salida:
#   0   éxito (con o sin recarga)
#   2   argumentos inválidos o variables de entorno faltantes
#   3   VM no encontrada en VirtualBox
#   4   fallo de comunicación con la VM (SSH)
#   5   falló un paso en la VM
#   7   el servidor DNS no tiene dirección IP (está apagado)
#   9   falló la recarga y también la restauración de la zona anterior
#   10  zona inválida; se mantuvo la anterior
#   11  falló la recarga; se restauró la zona anterior
#   99  operación no implementada

set -euo pipefail

scripts_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=../prepare/lib/vm-exec.sh
source "$scripts_dir/prepare/lib/vm-exec.sh"
# shellcheck source=lib/dns-common.sh
source "$scripts_dir/dns/lib/dns-common.sh"

# == 1. Validar argumentos ==
# 1.1 Exigir al menos 4 argumentos; si no, imprimir el uso en stderr y salir con 2.
# 1.2 dns, domain, dns_ip, wait_vms y los registros contra sus listas permitidas, y
#     APP_SSH_KEY_PATH definida; si no, salir con 2.
# Ninguna validación fallida llama a VBoxManage ni a ssh.

if (($# < 4)); then
  fail 2 "Uso: refresh-records.sh <dns> <domain> <dns_ip> <wait_vms|-> [record...]"
fi
dns="$1"
domain="$2"
dns_ip="$3"
wait_vms="$4"
shift 4

validate_vm_name "$dns" || exit 2
validate_domain "$domain" || exit 2
if [[ ! "$dns_ip" =~ $_IPV4_RE ]]; then
  fail 2 "IP del servidor DNS inválida"
fi
validate_wait_vms "$wait_vms" || exit 2
validate_records "$dns" "$@" || exit 2
if [[ -z "${APP_SSH_KEY_PATH:-}" ]]; then
  fail 2 "Falta la variable de entorno APP_SSH_KEY_PATH"
fi

# == 2. Comprobar el servidor DNS ==
# 2.1 Leer "VBoxManage showvminfo <dns> --machinereadable"; si falla, salir con 3.
# 2.2 Si VMState no es "running", salir con 7 sin ssh ni sondeos.

# vm_state <vm>: VMState de la VM, o vacío si no se pudo leer.
vm_state() {
  VBoxManage showvminfo "$1" --machinereadable 2>/dev/null </dev/null | sed -n 's/^VMState="\(.*\)"$/\1/p' || true
}

if ! VBoxManage showvminfo "$dns" --machinereadable >/dev/null 2>&1 </dev/null; then
  fail 3 "VM no encontrada en VirtualBox"
fi
if [[ "$(vm_state "$dns")" != running ]]; then
  fail 7 "El servidor DNS no tiene dirección IP (está apagado)"
fi

# == 3. Esperar las IPs en una sola ventana ==
# 3.1 De wait_vms, tomar solo las VMs con registro que estén "running": una VM apagada conserva
#     su IP guardada y no se sondea.
# 3.2 "wait_ips <dns> <vm...>": todas se sondean en paralelo, con el mismo límite de 60 sondeos
#     de 2 s, para que el servidor DNS que está arrancando no haga fallar la actualización.
# 3.3 Si el servidor DNS no informó IPv4, salir con 7.

declare -A wanted=() polled=()
if [[ "$wait_vms" != "-" ]]; then
  IFS=, read -ra list <<<"$wait_vms"
  for vm in "${list[@]}"; do
    wanted[$vm]=1
  done
fi
to_poll=()
for rec in "$@"; do
  vm="${rec%%,*}"
  if [[ -n "${wanted[$vm]:-}" && "$(vm_state "$vm")" == running ]]; then
    to_poll+=("$vm")
  fi
done

while read -r vm ip; do
  polled[$vm]="$ip"
done < <(wait_ips "$dns" "${to_poll[@]}")
if [[ "${polled[$dns]:--}" == "-" ]]; then
  fail 7 "El servidor DNS no tiene dirección IP (está apagado)"
fi

# == 4. Aplicar la zona si cambió ==
# 4.1 Cada registro toma la IPv4 sondeada o conserva la guardada.
# 4.2 "render_zone ... | apply_zone <dns> <domain> --if-changed": sin cambios imprime
#     "reloaded=no" sin subir nada; su código es el del script.
# 4.3 Imprimir "<vm> <ip>" por registro y la línea "reloaded=yes|no".

records=()
for rec in "$@"; do
  vm="${rec%%,*}"
  ip="${rec#*,}"
  if [[ -n "${polled[$vm]:-}" && "${polled[$vm]}" != "-" ]]; then
    ip="${polled[$vm]}"
  fi
  records+=("$vm,$ip")
done

result="$(render_zone "$dns" "$domain" "$dns_ip" "${records[@]}" | apply_zone "$dns" "$domain" --if-changed)" || exit $?
for rec in "${records[@]}"; do
  echo "${rec%%,*} ${rec#*,}"
done
echo "$result"

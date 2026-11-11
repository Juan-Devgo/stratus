#!/usr/bin/env bash
# refresh-records.sh - Actualiza los registros A del servidor DNS (005). Espera, en una sola
# ventana de 120 s compartida, la IPv4 de la LAN del servidor DNS y la de cada VM indicada que esté
# encendida. Una VM con IPv4 de la LAN la toma; las demás conservan la guardada si está en la LAN
# y, si no, su registro se quita, igual que el de las VMs de drop_vms (servidores de un balanceador).
# Renderiza la zona completa y solo la sube y recarga bind9 si cambió algo más que el serial
# (REQ-26, REQ-27, REQ-62).
#
# Uso:
#   refresh-records.sh <dns> <domain> <dns_cidr> <wait_vms|-> <drop_vms|-> [record...]
#
# Argumentos:
#   dns       nombre del servidor DNS, ^[a-z0-9][a-z0-9-]{0,31}$
#   domain    zona: 1-4 etiquetas ^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$ unidas por puntos
#   dns_cidr  IPv4 del servidor DNS con prefijo ("192.168.1.53/24"); define la LAN
#   wait_vms  VMs cuya IPv4 se espera, separadas por comas ("a,b"), o "-" para ninguna
#   drop_vms  VMs cuyo registro se quita, separadas por comas, o "-" para ninguna
#   record    registro guardado "<vm>,<ipv4>"; hasta 200, sin repetir VMs ni el servidor DNS
#
# Variables de entorno:
#   APP_SSH_KEY_PATH  llave privada del backend
#
# Salida:
#   stdout  "<vm> <ip>" por registro que se conserva, en el orden recibido; "removed=<vm>" por
#           registro quitado; y en la última línea "reloaded=yes|no"
#   stderr  diagnósticos: "Registros quitados: a, b" si se quitó alguno, y la salida de
#           named-checkzone si la zona es inválida
#
# Códigos de salida:
#   0   éxito (con o sin recarga)
#   2   argumentos inválidos o variables de entorno faltantes
#   3   VM no encontrada en VirtualBox
#   4   fallo de comunicación con la VM (SSH)
#   5   falló un paso en la VM
#   7   el servidor DNS no tiene dirección IP en la LAN (está apagado)
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
# 1.1 Exigir al menos 5 argumentos; si no, imprimir el uso en stderr y salir con 2.
# 1.2 dns, domain, dns_cidr, wait_vms, drop_vms y los registros contra sus listas permitidas, y
#     APP_SSH_KEY_PATH definida; si no, salir con 2.
# Ninguna validación fallida llama a VBoxManage ni a ssh.

if (($# < 5)); then
  fail 2 "Uso: refresh-records.sh <dns> <domain> <dns_cidr> <wait_vms|-> <drop_vms|-> [record...]"
fi
dns="$1"
domain="$2"
dns_cidr="$3"
wait_vms="$4"
drop_vms="$5"
shift 5

validate_vm_name "$dns" || exit 2
validate_domain "$domain" || exit 2
validate_cidr "$dns_cidr" || exit 2
dns_ip="${dns_cidr%/*}"
validate_wait_vms "$wait_vms" || exit 2
validate_wait_vms "$drop_vms" || exit 2
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
# 3.1 De wait_vms, tomar solo las VMs con registro, fuera de drop_vms, que estén "running": una
#     VM apagada no se sondea.
# 3.2 "wait_ips <dns_cidr> <dns> <vm...>": todas se sondean en paralelo, con el mismo límite de 60
#     sondeos de 2 s, y solo valen las IPv4 dentro de la LAN.
# 3.3 Si el servidor DNS no informó una IPv4 de la LAN, salir con 7.

declare -A wanted=() dropped=() polled=()
if [[ "$wait_vms" != "-" ]]; then
  IFS=, read -ra list <<<"$wait_vms"
  for vm in "${list[@]}"; do
    wanted[$vm]=1
  done
fi
if [[ "$drop_vms" != "-" ]]; then
  IFS=, read -ra list <<<"$drop_vms"
  for vm in "${list[@]}"; do
    dropped[$vm]=1
  done
fi
to_poll=()
for rec in "$@"; do
  vm="${rec%%,*}"
  if [[ -n "${wanted[$vm]:-}" && -z "${dropped[$vm]:-}" && "$(vm_state "$vm")" == running ]]; then
    to_poll+=("$vm")
  fi
done

while read -r vm ip; do
  polled[$vm]="$ip"
done < <(wait_ips "$dns_cidr" "$dns" "${to_poll[@]}")
if [[ "${polled[$dns]:--}" == "-" ]]; then
  fail 7 "El servidor DNS no tiene dirección IP en la LAN (está apagado)"
fi

# == 4. Aplicar la zona si cambió ==
# 4.1 Un registro de drop_vms se quita. Los demás toman la IPv4 de la LAN sondeada, o conservan
#     la guardada si está en la LAN; si no, se quitan.
# 4.2 "render_zone ... | apply_zone <dns> <domain> --if-changed": sin cambios imprime
#     "reloaded=no" sin subir nada; su código es el del script.
# 4.3 Imprimir "<vm> <ip>" por registro conservado, "removed=<vm>" por cada quitado, la línea
#     "reloaded=yes|no", y avisar en stderr cuáles se quitaron.

records=()
removed=()
for rec in "$@"; do
  vm="${rec%%,*}"
  ip="${rec#*,}"
  if [[ -n "${dropped[$vm]:-}" ]]; then
    removed+=("$vm")
    continue
  fi
  if [[ -n "${polled[$vm]:-}" && "${polled[$vm]}" != "-" ]]; then
    ip="${polled[$vm]}"
  fi
  if ip_in_subnet "$dns_cidr" "$ip"; then
    records+=("$vm,$ip")
  else
    removed+=("$vm")
  fi
done

result="$(render_zone "$dns" "$domain" "$dns_ip" "${records[@]}" | apply_zone "$dns" "$domain" --if-changed)" || exit $?
for rec in "${records[@]}"; do
  echo "${rec%%,*} ${rec#*,}"
done
for vm in "${removed[@]}"; do
  echo "removed=$vm"
done
echo "$result"
if ((${#removed[@]} > 0)); then
  printf 'Registros quitados: %s\n' "$(printf '%s, ' "${removed[@]}" | sed 's/, $//')" >&2
fi

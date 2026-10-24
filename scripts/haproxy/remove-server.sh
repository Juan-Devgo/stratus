#!/usr/bin/env bash
# remove-server.sh - Quita un servidor de un balanceador: reescribe haproxy.cfg sin su línea, quita sus
# llaves de la cuenta jump y devuelve la VM a la red en puente (adaptador 1 en puente y sin el NAT),
# dejándola en el estado de energía en que estaba. Cada paso tolera lo que ya esté deshecho, así que
# repetir la operación después de un fallo la termina.
#
# Uso:
#   remove-server.sh <lb> <vm> <frontend_port> <bridge_adapter> [<spec>...]
#
# Argumentos:
#   lb              balanceador, ^[a-z0-9][a-z0-9-]{0,31}$; debe estar encendido con IPv4 en puente
#   vm              servidor que se quita, misma regla y distinto de lb
#   frontend_port   puerto del frontend del balanceador, 1-65535 y distinto de 22
#   bridge_adapter  interfaz del host a la que vuelve la VM (APP_BRIDGE_ADAPTER)
#   spec            (cero a diez) servidores que quedan, cada uno
#                   "<server_name>,<vm>,<port>,<ip>,<on|off>"; ninguno puede ser <vm>
#
# Variables de entorno:
#   APP_SSH_KEY_PATH  llave privada del backend
#
# Salida:
#   stdout  "Servidor <vm> quitado de <lb>"
#   stderr  diagnósticos
#
# Códigos de salida:
#   0   éxito: el servidor ya no está en el balanceador y la VM volvió al puente
#   2   argumentos inválidos o variables de entorno faltantes
#   3   VM no encontrada en VirtualBox
#   4   fallo de VBoxManage
#   5   no se pudo detener la VM
#   10  configuración de HAProxy inválida; se mantuvo la anterior (no se tocó nada más)
#   11  no se pudo recargar HAProxy; se restauró la configuración anterior (no se tocó nada más)
#   12  el balanceador está apagado o sin IPv4 en puente; no se cambió nada
#   15  no se pudo actualizar la cuenta jump (la configuración de HAProxy ya no tiene el servidor)
#   99  operación no implementada

set -euo pipefail

scripts_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=../prepare/lib/vm-exec.sh
source "$scripts_dir/prepare/lib/vm-exec.sh"
# shellcheck source=lib/haproxy-common.sh
source "$scripts_dir/haproxy/lib/haproxy-common.sh"
# shellcheck source=lib/jump-keys.sh
source "$scripts_dir/haproxy/lib/jump-keys.sh"

# == 1. Validar argumentos ==
# 1.1 Exigir al menos 4 argumentos; si no, imprimir el uso en stderr y salir con 2.
# 1.2 Validar cada valor contra su lista permitida (haproxy-common.sh); lb y vm deben ser distintos.
# 1.3 Validar las specs que quedan (hasta 10, nombres únicos, cualquier IPv4); ninguna puede ser la
#     VM que se quita.
# Ninguna validación fallida debe llamar a VBoxManage ni a ssh.

if (($# < 4)); then
  fail 2 "Uso: remove-server.sh <lb> <vm> <frontend_port> <bridge_adapter> [<spec>...]"
fi
lb="$1"
vm="$2"
frontend_port="$3"
bridge_adapter="$4"
shift 4
specs=("$@")

validate_lb_name "$lb" || exit 2
validate_vm_name "$vm" || exit 2
[[ "$lb" != "$vm" ]] || fail 2 "lb y vm deben ser distintos"
validate_frontend_port "$frontend_port" || exit 2
validate_bridge "$bridge_adapter" || exit 2
validate_specs - "${specs[@]}" || exit 2
for spec in "${specs[@]}"; do
  IFS=, read -r _ spec_vm _ <<<"$spec"
  [[ "$spec_vm" != "$vm" ]] || fail 2 "Las specs que quedan no pueden incluir a la VM que se quita"
done

# == 2. Comprobar el estado actual ==
# 2.1 El balanceador debe estar encendido con IPv4 en puente (lb_reachable); si no, salir con 12 sin
#     cambiar nada.
# 2.2 Comprobar con "VBoxManage showvminfo <vm>" que la VM exista; si no, salir con 3.

lb_reachable "$lb" || exit $?

if ! VBoxManage showvminfo "$vm" >/dev/null 2>&1; then
  fail 3 "VM no encontrada en VirtualBox: $vm"
fi

# == 3. Quitar el servidor del balanceador ==
# 3.1 Generar haproxy.cfg con los servidores que quedan y aplicarlo con apply_cfg --if-changed
#     (revisión, recarga y verificación; sin recarga si ya era así). Su código 10 u 11 sale tal cual.
# 3.2 jump_drop <lb> <vm>: quitar las llaves de la VM de jump (no lee el servidor, que puede estar
#     apagado). Si falla, salir con 15.

rc=0
render_cfg "$frontend_port" "${specs[@]}" | apply_cfg "$lb" "$frontend_port" --if-changed || rc=$?
if ((rc != 0)); then
  exit "$rc"
fi
jump_drop "$lb" "$vm" || exit 15

# == 4. Devolver la VM al puente ==
# 4.1 Leer el estado de energía de la VM; si está encendida, "../prepare/stop-vm.sh <vm>" (si falla,
#     salir con 5).
# 4.2 "VBoxManage modifyvm <vm> --nic2 none --nic1 bridged --bridgeadapter1 <bridge_adapter>". Si
#     falla, salir con 4.
# 4.3 "VBoxManage startvm <vm> --type headless" solo si estaba encendida.

was_running=no
state="$(VBoxManage showvminfo "$vm" --machinereadable 2>/dev/null | sed -n 's/^VMState="\(.*\)"$/\1/p' || true)"
[[ "$state" != running ]] || was_running=yes

if [[ "$was_running" == yes ]]; then
  "$scripts_dir/prepare/stop-vm.sh" "$vm" >&2 || fail 5 "No se pudo detener la VM $vm"
fi
VBoxManage modifyvm "$vm" --nic2 none --nic1 bridged --bridgeadapter1 "$bridge_adapter" >&2 ||
  fail 4 "No se pudo devolver la VM $vm a la red en puente"
if [[ "$was_running" == yes ]]; then
  VBoxManage startvm "$vm" --type headless >&2 || fail 4 "No se pudo encender la VM $vm"
fi

# == 5. Resultado ==
# 5.1 Imprimir en stdout "Servidor <vm> quitado de <lb>".

echo "Servidor $vm quitado de $lb"

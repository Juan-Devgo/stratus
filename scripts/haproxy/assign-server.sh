#!/usr/bin/env bash
# assign-server.sh - Asigna una VM como servidor de un balanceador: la apaga si estaba encendida, pone
# su adaptador 1 en la red interna "stratus-<lb>" y agrega un adaptador 2 NAT (salida a internet, sin
# reenvío de puertos), la enciende, espera su IPv4 interna, copia sus llaves públicas a la cuenta jump
# del balanceador y agrega su línea al backend de haproxy.cfg. Si un paso falla, vuelve a dejar la VM
# con su adaptador en puente, sin el NAT, en su estado de energía anterior, y quita sus llaves de jump.
#
# Uso:
#   assign-server.sh <lb> <subnet> <vm> <server_name> <port> <frontend_port> <bridge_adapter> [<spec>...]
#
# Argumentos:
#   lb              balanceador, ^[a-z0-9][a-z0-9-]{0,31}$; debe estar encendido con IPv4 en puente
#   subnet          N de la red interna 10.10.N.0/24 del balanceador, 1-254
#   vm              VM que pasa a ser servidor, misma regla que lb y distinta de ella
#   server_name     nombre en el backend, server1 a server10
#   port            puerto del servidor, 1-65535
#   frontend_port   puerto del frontend del balanceador, 1-65535 y distinto de 22
#   bridge_adapter  interfaz del host a la que vuelve la VM si hay que revertir (APP_BRIDGE_ADAPTER)
#   spec            (cero a nueve) servidores que el balanceador ya tiene, cada uno
#                   "<server_name>,<vm>,<port>,<ip>,<on|off>"; con el nuevo no pasan de diez
#
# Variables de entorno:
#   APP_SSH_KEY_PATH  llave privada del backend; se exige <ruta>.pub
#
# Salida:
#   stdout  "Servidor <server_name> (<vm>) asignado a <lb>" y, en la última línea, "ip=<ipv4>"
#   stderr  diagnósticos
#
# Códigos de salida:
#   0   éxito: la VM queda encendida en la red interna y balanceada
#   2   argumentos inválidos o variables de entorno faltantes
#   3   VM o balanceador no encontrado en VirtualBox
#   4   fallo de VBoxManage
#   5   falló un paso (la VM se revirtió); también si no se pudo detener la VM
#   9   falló un paso y también la reversión: puede quedar la VM con la red interna o sus llaves en jump
#   10  configuración de HAProxy inválida; se mantuvo la anterior (la VM se revirtió)
#   11  no se pudo recargar HAProxy; se restauró la configuración anterior (la VM se revirtió)
#   12  el balanceador está apagado o sin IPv4 en puente; no se cambió nada
#   14  el servidor no obtuvo una IPv4 interna en 120 segundos (la VM se revirtió)
#   15  no se pudo actualizar la cuenta jump (la VM se revirtió)
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
# 1.1 Exigir al menos 7 argumentos; si no, imprimir el uso en stderr y salir con 2.
# 1.2 Validar cada valor contra su lista permitida (haproxy-common.sh); lb y vm deben ser distintos.
# 1.3 Validar las specs existentes con la subred (IP en 10.10.N.10-200, nombres únicos): como máximo
#     9, porque con la nueva el balanceador no puede pasar de 10. Ni el nombre ni la VM nuevos pueden
#     estar ya en ellas.
# 1.4 Exigir que exista "$APP_SSH_KEY_PATH.pub" (jump_sync la lee).
# Ninguna validación fallida debe llamar a VBoxManage ni a ssh.

if (($# < 7)); then
  fail 2 "Uso: assign-server.sh <lb> <subnet> <vm> <server_name> <port> <frontend_port> <bridge_adapter> [<spec>...]"
fi
lb="$1"
subnet="$2"
vm="$3"
server_name="$4"
port="$5"
frontend_port="$6"
bridge_adapter="$7"
shift 7
specs=("$@")

validate_lb_name "$lb" || exit 2
validate_subnet "$subnet" || exit 2
validate_vm_name "$vm" || exit 2
[[ "$lb" != "$vm" ]] || fail 2 "lb y vm deben ser distintos"
validate_server_name "$server_name" || exit 2
validate_port "$port" || exit 2
validate_frontend_port "$frontend_port" || exit 2
validate_bridge "$bridge_adapter" || exit 2
if ((${#specs[@]} > 9)); then
  fail 2 "El balanceador ya tiene 10 servidores"
fi
validate_specs "$subnet" "${specs[@]}" || exit 2
for spec in "${specs[@]}"; do
  IFS=, read -r spec_name spec_vm _ <<<"$spec"
  [[ "$spec_name" != "$server_name" ]] || fail 2 "server_name ya está en uso en el balanceador"
  [[ "$spec_vm" != "$vm" ]] || fail 2 "La VM ya es servidor de este balanceador"
done
if [[ -z "${APP_SSH_KEY_PATH:-}" || ! -r "$APP_SSH_KEY_PATH.pub" ]]; then
  fail 2 "No se encontró la llave pública del backend (APP_SSH_KEY_PATH.pub)"
fi

# == 2. Comprobar el estado actual ==
# 2.1 Comprobar con "VBoxManage showvminfo" que existan el balanceador y la VM; si no, salir con 3.
# 2.2 El balanceador debe estar encendido con IPv4 en puente (lb_reachable); si no, salir con 12
#     sin cambiar nada.
# 2.3 Leer el estado de energía de la VM (VMState de showvminfo --machinereadable): se restaura si algo
#     falla. Si está encendida, apagarla con "../prepare/stop-vm.sh <vm>" (ACPI y apagado forzado);
#     si falla, salir con 5.

for name in "$lb" "$vm"; do
  if ! VBoxManage showvminfo "$name" >/dev/null 2>&1; then
    fail 3 "VM no encontrada en VirtualBox: $name"
  fi
done

lb_reachable "$lb" || exit $?

was_running=no
state="$(VBoxManage showvminfo "$vm" --machinereadable 2>/dev/null | sed -n 's/^VMState="\(.*\)"$/\1/p' || true)"
[[ "$state" != running ]] || was_running=yes

if [[ "$was_running" == yes ]]; then
  "$scripts_dir/prepare/stop-vm.sh" "$vm" >&2 || fail 5 "No se pudo detener la VM $vm"
fi

# == 3. Reversión en caso de fallo ==
# Se ejecuta al salir con un código distinto de cero, una vez cambiados los adaptadores (sección 4).
# Conserva el código original; si la propia reversión falla, sale con 9. haproxy.cfg no se toca aquí:
# apply_cfg ya lo restauró, o nunca se cambió.
# 3.1 jump_drop <lb> <vm>: quita las llaves de la VM de jump (idempotente; no lee el servidor).
# 3.2 Si la VM está encendida, apagarla con "VBoxManage controlvm <vm> poweroff" y esperar hasta 10 s.
# 3.3 "VBoxManage modifyvm <vm> --nic1 bridged --bridgeadapter1 <bridge_adapter> --nic2 none".
# 3.4 Solo si la VM estaba encendida antes: "VBoxManage startvm <vm> --type headless".

adapters_changed=no

rollback() {
  local status=$? restore_failed=no now i
  trap - EXIT INT TERM
  if ((status == 0)) || [[ "$adapters_changed" != yes ]]; then
    return "$status"
  fi
  echo "Falló la asignación de $vm; revirtiendo" >&2
  jump_drop "$lb" "$vm" >&2 || restore_failed=yes
  now="$(VBoxManage showvminfo "$vm" --machinereadable 2>/dev/null | sed -n 's/^VMState="\(.*\)"$/\1/p' || true)"
  if [[ "$now" == running || "$now" == stopping ]]; then
    VBoxManage controlvm "$vm" poweroff >&2 || echo "No se pudo apagar la VM $vm" >&2
    for ((i = 0; i < 10; i++)); do
      now="$(VBoxManage showvminfo "$vm" --machinereadable 2>/dev/null | sed -n 's/^VMState="\(.*\)"$/\1/p' || true)"
      [[ "$now" == running || "$now" == stopping ]] || break
      sleep 1
    done
  fi
  VBoxManage modifyvm "$vm" --nic1 bridged --bridgeadapter1 "$bridge_adapter" --nic2 none >&2 || restore_failed=yes
  if [[ "$restore_failed" == no && "$was_running" == yes ]]; then
    VBoxManage startvm "$vm" --type headless >&2 || restore_failed=yes
  fi
  if [[ "$restore_failed" == yes ]]; then
    echo "La restauración falló; revise la VM $vm (adaptadores y estado) y las llaves de jump en $lb" >&2
    exit 9
  fi
  exit "$status"
}

# == 4. Pasar la VM a la red interna ==
# 4.1 Instalar el "trap" de la sección 3 y "VBoxManage modifyvm <vm> --nic1 intnet --intnet1
#     stratus-<lb> --nic2 nat": el adaptador 2 da salida a internet por NAT, sin reenvío de puertos.
# 4.2 Borrar las propiedades de IP de Guest Additions (/Net/0/V4/IP y /Net/1/V4/IP) para no leer una
#     IP del arranque anterior, y "VBoxManage startvm <vm> --type headless".
# 4.3 Esperar con wait_ips hasta 120 s (60 sondeos de 2 s) una IPv4 del adaptador 1 en el rango
#     10.10.N.10-200 que reparte el DHCP de la red. Si no llega, salir con 14.

trap rollback EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

adapters_changed=yes
VBoxManage modifyvm "$vm" --nic1 intnet --intnet1 "stratus-$lb" --nic2 nat >&2 ||
  fail 4 "No se pudo configurar la red de la VM $vm"
for prop in /VirtualBox/GuestInfo/Net/0/V4/IP /VirtualBox/GuestInfo/Net/1/V4/IP; do
  VBoxManage guestproperty delete "$vm" "$prop" >&2 || true
done
VBoxManage startvm "$vm" --type headless >&2 || fail 4 "No se pudo encender la VM $vm"

read -r _ vm_ip <<<"$(wait_ips "$subnet" "$vm")"
if [[ "$vm_ip" == "-" || -z "$vm_ip" ]]; then
  fail 14 "El servidor $vm no obtuvo una IPv4 interna en 120 segundos"
fi

# == 5. Llaves de jump y backend ==
# 5.1 Registrar la ruta de la VM por el balanceador (vm_route) y copiar sus llaves a jump con
#     jump_sync <lb> <vm>. Si falla, salir con 15.
# 5.2 Generar haproxy.cfg con las specs existentes más "<server_name>,<vm>,<port>,<ip>,on" y
#     aplicarlo con apply_cfg (revisión, recarga y verificación). Su código 10 u 11 sale tal cual.

vm_route "$vm" "$lb"
jump_sync "$lb" "$vm" || exit 15

new_spec="$server_name,$vm,$port,$vm_ip,on"
rc=0
render_cfg "$frontend_port" "${specs[@]}" "$new_spec" | apply_cfg "$lb" "$frontend_port" || rc=$?
if ((rc != 0)); then
  exit "$rc"
fi

# == 6. Resultado ==
# 6.1 Imprimir en stdout "Servidor <server_name> (<vm>) asignado a <lb>" y la línea "ip=<ipv4>": el
#     backend guarda esa IP como la última conocida.

trap - EXIT INT TERM
echo "Servidor $server_name ($vm) asignado a $lb"
echo "ip=$vm_ip"

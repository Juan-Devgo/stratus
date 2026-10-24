#!/usr/bin/env bash
# delete-vm.sh - Elimina una VM de VirtualBox junto con su disco diferencial; si está encendida,
# la apaga antes. El disco base nunca se cierra, se borra ni se modifica.
#
# Uso:
#   delete-vm.sh <vm_name>
#
# Argumentos:
#   vm_name  nombre de la VM, ^[a-z0-9][a-z0-9-]{0,31}$
#
# Variables de entorno:
#   ninguna
#
# Salida:
#   stdout  mensaje de resultado
#   stderr  diagnósticos
#
# Códigos de salida:
#   0   éxito: la VM ya no está registrada (también si no existía)
#   2   argumentos inválidos
#   4   fallo de VBoxManage al apagar o desregistrar la VM
#   99  operación no implementada

set -euo pipefail

# == 1. Validar argumentos ==
# 1.1 Exigir exactamente 1 argumento; si no, imprimir el uso en stderr y salir con 2.
# 1.2 Validar vm_name contra ^[a-z0-9][a-z0-9-]{0,31}$; si no coincide, salir con 2.

fail() {
  echo "$2" >&2
  exit "$1"
}

if (($# != 1)); then
  fail 2 "Uso: delete-vm.sh <vm_name>"
fi
vm_name="$1"
if [[ ! "$vm_name" =~ ^[a-z0-9][a-z0-9-]{0,31}$ ]]; then
  fail 2 'vm_name inválido: debe cumplir ^[a-z0-9][a-z0-9-]{0,31}$'
fi

# == 2. Comprobar el estado actual ==
# 2.1 Leer "VBoxManage showvminfo <vm_name> --machinereadable": la línea VMState=... y el UUID del
#     disco conectado (SATA-ImageUUID-0-0).
# 2.2 Si showvminfo falla, la VM ya no está en VirtualBox: imprimir
#     "La VM <vm_name> no existía en VirtualBox" y salir con 0 (idempotente: el backend puede
#     borrar su registro).

if ! info="$(VBoxManage showvminfo "$vm_name" --machinereadable 2>/dev/null)"; then
  echo "La VM $vm_name no existía en VirtualBox"
  exit 0
fi
state="$(sed -n 's/^VMState="\(.*\)"$/\1/p' <<<"$info")"
medium_uuid="$(sed -n 's/^"\{0,1\}SATA-ImageUUID-0-0"\{0,1\}="\(.*\)"$/\1/p' <<<"$info")"

# == 3. Apagar si está encendida ==
# 3.1 Si VMState es "running", "VBoxManage controlvm <vm_name> poweroff" (sin ACPI: la
#     confirmación ya se pidió en la UI).
# 3.2 Si falla, imprimir el motivo en stderr y salir con 4.
# 3.3 Esperar hasta 10 s a que la VM deje de estar "running" o "stopping", para que VirtualBox
#     libere su sesión antes de tocar el almacenamiento.

if [[ "$state" == running ]]; then
  VBoxManage controlvm "$vm_name" poweroff >&2 || fail 4 "No se pudo apagar la VM $vm_name"
  for ((i = 0; i < 10; i++)); do
    now="$(VBoxManage showvminfo "$vm_name" --machinereadable 2>/dev/null | sed -n 's/^VMState="\(.*\)"$/\1/p' || true)"
    [[ "$now" == running || "$now" == stopping ]] || break
    sleep 1
  done
fi

# == 4. Soltar y borrar el disco diferencial ==
# 4.1 Si hay un disco conectado, leer su "Parent UUID:" con "VBoxManage showmediuminfo disk <uuid>":
#     un disco sin padre (el disco base) muestra "base".
# 4.2 "VBoxManage storageattach <vm_name> --storagectl SATA --port 0 --device 0 --medium none".
# 4.3 Solo si el disco tiene padre (existe y no es "base"): "VBoxManage closemedium disk <uuid>
#     --delete". Un disco sin padre nunca se cierra ni se borra. Si closemedium falla (por
#     ejemplo, VirtualBox ya lo había quitado), se informa en stderr y se continúa.

if [[ -n "$medium_uuid" && "$medium_uuid" != none ]]; then
  parent="$(VBoxManage showmediuminfo disk "$medium_uuid" 2>/dev/null |
    sed -n 's/^Parent UUID:[[:space:]]*//p' || true)"
  VBoxManage storageattach "$vm_name" --storagectl SATA --port 0 --device 0 --medium none >&2 ||
    echo "No se pudo soltar el disco de $vm_name" >&2
  if [[ -n "$parent" && "$parent" != base ]]; then
    VBoxManage closemedium disk "$medium_uuid" --delete >&2 ||
      echo "No se pudo borrar el disco diferencial de $vm_name" >&2
  fi
fi

# == 5. Desregistrar ==
# 5.1 "VBoxManage unregistervm <vm_name> --delete" para borrar la VM y su carpeta.
# 5.2 Si falla, imprimir el motivo en stderr y salir con 4.

VBoxManage unregistervm "$vm_name" --delete >&2 || fail 4 "No se pudo eliminar la VM $vm_name"

# == 6. Resultado ==
# 6.1 Imprimir en stdout "VM <vm_name> eliminada junto con su disco".

echo "VM $vm_name eliminada junto con su disco"

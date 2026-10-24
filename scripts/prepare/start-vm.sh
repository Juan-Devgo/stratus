#!/usr/bin/env bash
# start-vm.sh - Enciende una VM registrada en VirtualBox sin ventana (headless).
#
# Uso:
#   start-vm.sh <vm_name>
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
#   0   éxito: la VM está encendida (también si ya lo estaba)
#   2   argumentos inválidos
#   3   VM no encontrada en VirtualBox
#   4   fallo de VBoxManage al encender la VM
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
  fail 2 "Uso: start-vm.sh <vm_name>"
fi
vm_name="$1"
if [[ ! "$vm_name" =~ ^[a-z0-9][a-z0-9-]{0,31}$ ]]; then
  fail 2 'vm_name inválido: debe cumplir ^[a-z0-9][a-z0-9-]{0,31}$'
fi

# == 2. Comprobar el estado actual ==
# 2.1 Leer el estado con "VBoxManage showvminfo <vm_name> --machinereadable" (línea VMState=...).
# 2.2 Si showvminfo falla, la VM no existe: imprimir "VM no encontrada" y salir con 3.
# 2.3 Si VMState es "running", imprimir "La VM <vm_name> ya estaba encendida" y salir con 0
#     (idempotente: no se vuelve a encender).

# read_state: deja el estado de la VM en "state"; devuelve 1 si VBoxManage no la conoce.
read_state() {
  local info
  info="$(VBoxManage showvminfo "$vm_name" --machinereadable 2>/dev/null)" || return 1
  state="$(sed -n 's/^VMState="\(.*\)"$/\1/p' <<<"$info")"
}

if ! read_state; then
  fail 3 "VM no encontrada"
fi
if [[ "$state" == running ]]; then
  echo "La VM $vm_name ya estaba encendida"
  exit 0
fi

# == 3. Encender ==
# 3.1 "VBoxManage startvm <vm_name> --type headless".
# 3.2 Si falla, imprimir el motivo en stderr y salir con 4.

VBoxManage startvm "$vm_name" --type headless >&2 || fail 4 "No se pudo encender la VM $vm_name"

# == 4. Resultado ==
# 4.1 Imprimir en stdout "VM <vm_name> encendida".

echo "VM $vm_name encendida"

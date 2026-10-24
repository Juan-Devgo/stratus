#!/usr/bin/env bash
# stop-vm.sh - Apaga una VM con la señal ACPI y fuerza el apagado si sigue encendida tras 60 s.
#
# Uso:
#   stop-vm.sh <vm_name>
#
# Argumentos:
#   vm_name  nombre de la VM, ^[a-z0-9][a-z0-9-]{0,31}$
#
# Variables de entorno:
#   ninguna
#
# Salida:
#   stdout  mensaje de resultado (indica si el apagado fue forzado)
#   stderr  diagnósticos
#
# Códigos de salida:
#   0   éxito: la VM está apagada (también si ya lo estaba)
#   2   argumentos inválidos
#   3   VM no encontrada en VirtualBox
#   4   fallo de VBoxManage al enviar ACPI o al forzar el apagado
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
  fail 2 "Uso: stop-vm.sh <vm_name>"
fi
vm_name="$1"
if [[ ! "$vm_name" =~ ^[a-z0-9][a-z0-9-]{0,31}$ ]]; then
  fail 2 'vm_name inválido: debe cumplir ^[a-z0-9][a-z0-9-]{0,31}$'
fi

# == 2. Comprobar el estado actual ==
# 2.1 Leer el estado con "VBoxManage showvminfo <vm_name> --machinereadable" (línea VMState=...).
# 2.2 Si showvminfo falla, la VM no existe: imprimir "VM no encontrada" y salir con 3.
# 2.3 Si VMState no es "running", imprimir "La VM <vm_name> ya estaba apagada" y salir con 0
#     (idempotente).

# read_state: deja el estado de la VM en "state"; devuelve 1 si VBoxManage no la conoce.
read_state() {
  local info
  info="$(VBoxManage showvminfo "$vm_name" --machinereadable 2>/dev/null)" || return 1
  state="$(sed -n 's/^VMState="\(.*\)"$/\1/p' <<<"$info")"
}

if ! read_state; then
  fail 3 "VM no encontrada"
fi
if [[ "$state" != running ]]; then
  echo "La VM $vm_name ya estaba apagada"
  exit 0
fi

# == 3. Apagado ordenado ==
# 3.1 "VBoxManage controlvm <vm_name> acpipowerbutton"; si falla, salir con 4.
# 3.2 Repetir 12 veces: "sleep 5" y volver a leer VMState como en 2.1 (60 s en total; contar
#     las esperas, no el reloj, para que la prueba con stubs sea determinista).
# 3.3 Si VMState deja de ser "running", imprimir "VM <vm_name> apagada" y salir con 0.

VBoxManage controlvm "$vm_name" acpipowerbutton >&2 || fail 4 "No se pudo enviar la señal ACPI a $vm_name"
for ((i = 0; i < 12; i++)); do
  sleep 5
  read_state || fail 4 "No se pudo leer el estado de la VM $vm_name"
  if [[ "$state" != running ]]; then
    echo "VM $vm_name apagada"
    exit 0
  fi
done

# == 4. Apagado forzado ==
# 4.1 Si tras las 12 esperas sigue en "running", "VBoxManage controlvm <vm_name> poweroff".
# 4.2 Si falla, imprimir el motivo en stderr y salir con 4.
# 4.3 Imprimir en stdout "VM <vm_name> apagada a la fuerza: no respondió a ACPI en 60 segundos".

VBoxManage controlvm "$vm_name" poweroff >&2 || fail 4 "No se pudo forzar el apagado de $vm_name"
echo "VM $vm_name apagada a la fuerza: no respondió a ACPI en 60 segundos"

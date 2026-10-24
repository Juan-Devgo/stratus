#!/usr/bin/env bash
# list-vms.sh - Lista las VMs registradas en VirtualBox con su estado y su dirección IPv4.
#
# Uso:
#   list-vms.sh
#
# Argumentos:
#   ninguno
#
# Variables de entorno:
#   ninguna
#
# Salida:
#   stdout  una línea "<nombre> <running|poweroff> <ipv4|->" por VM, en el orden de VirtualBox;
#           la IP es "-" si la VM está apagada o no reporta una IPv4
#   stderr  diagnósticos
#
# Códigos de salida:
#   0   éxito (también si no hay VMs: stdout queda vacío)
#   2   argumentos inválidos (se recibió alguno)
#   4   fallo de VBoxManage
#   99  operación no implementada

set -euo pipefail

# == 1. Validar argumentos ==
# 1.1 Si se recibe cualquier argumento, imprimir el uso en stderr y salir con 2.

fail() {
  echo "$2" >&2
  exit "$1"
}

if (($# != 0)); then
  fail 2 "Uso: list-vms.sh (no recibe argumentos)"
fi

# == 2. Consultar VirtualBox ==
# 2.1 Obtener los nombres con "VBoxManage list vms"; cada línea tiene la forma
#     "<nombre entre comillas> {uuid}" y el nombre se toma de entre las comillas.
# 2.2 Obtener las VMs encendidas con "VBoxManage list runningvms" (mismo formato).
# 2.3 Si alguna de las dos llamadas falla, imprimir el motivo en stderr y salir con 4.
# 2.4 Por cada VM encendida, leer la IPv4 de su adaptador 1 con vm_nic_ip (lib/vm-exec.sh). Una VM
#     de una sola NIC se lee en /Net/0 como siempre; un servidor de balanceador (dos NICs) o un
#     balanceador se busca por MAC, así que se muestra la IP interna del servidor y la IP en puente
#     del balanceador (REQ-13).

# shellcheck source=lib/vm-exec.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/vm-exec.sh"

all_vms="$(VBoxManage list vms)" || fail 4 "VBoxManage list vms falló"
running_vms="$(VBoxManage list runningvms)" || fail 4 "VBoxManage list runningvms falló"

# Cada línea es "<nombre entre comillas> {uuid}": el nombre es lo que queda entre las comillas.
vm_names="$(sed -n 's/^"\(.*\)" {[^}]*}$/\1/p' <<<"$all_vms")"
running_names="$(sed -n 's/^"\(.*\)" {[^}]*}$/\1/p' <<<"$running_vms")"

# == 3. Imprimir el resultado ==
# 3.1 Por cada nombre de 2.1, imprimir "<nombre> running <ip>" si aparece en 2.2 y
#     "<nombre> poweroff -" en cualquier otro caso (apagada, guardada o abortada).
# 3.2 La IP es el valor de 2.4 solo si es una IPv4; si la VM no reporta nada, el valor no es una
#     IPv4 o la consulta falla, se imprime "-" (la lectura de la IP nunca hace fallar el listado).
# 3.3 No imprimir nada más en stdout: el backend interpreta cada línea.

# vm_ipv4 <nombre>: imprime la IPv4 que reporta la VM, o "-".
vm_ipv4() {
  vm_nic_ip "$1" 1 2>/dev/null || echo "-"
}

while IFS= read -r name; do
  [[ -n "$name" ]] || continue
  if grep -qxF -- "$name" <<<"$running_names"; then
    echo "$name running $(vm_ipv4 "$name")"
  else
    echo "$name poweroff -"
  fi
done <<<"$vm_names"

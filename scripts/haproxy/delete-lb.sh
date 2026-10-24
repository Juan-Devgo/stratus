#!/usr/bin/env bash
# delete-lb.sh - Elimina un balanceador: su VM y su disco diferencial (delete-vm.sh de 001) y después
# su red interna "stratus-<lb>" con su servidor DHCP. El disco base nunca se toca. El backend ya
# comprobó que el balanceador no tiene servidores asignados.
#
# Uso:
#   delete-lb.sh <lb>
#
# Argumentos:
#   lb  nombre del balanceador, ^[a-z0-9][a-z0-9-]{0,31}$
#
# Variables de entorno:
#   ninguna
#
# Salida:
#   stdout  "Balanceador <lb> eliminado junto con su red interna"
#   stderr  diagnósticos
#
# Códigos de salida:
#   0   éxito: la VM y la red ya no existen (también si alguna no existía)
#   2   argumentos inválidos
#   4   fallo de VBoxManage al apagar o desregistrar la VM (la red no se toca)
#   99  operación no implementada

set -euo pipefail

scripts_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=lib/haproxy-common.sh
source "$scripts_dir/haproxy/lib/haproxy-common.sh"

# == 1. Validar argumentos ==
# 1.1 Exigir exactamente 1 argumento; si no, imprimir el uso en stderr y salir con 2.
# 1.2 Validar lb contra ^[a-z0-9][a-z0-9-]{0,31}$; si no coincide, salir con 2.
# Ninguna validación fallida debe llamar a VBoxManage.

if (($# != 1)); then
  fail 2 "Uso: delete-lb.sh <lb>"
fi
lb="$1"
validate_lb_name "$lb" || exit 2

# == 2. Borrar la VM ==
# 2.1 "../prepare/delete-vm.sh <lb>": apaga la VM si está encendida, borra su disco diferencial y la
#     desregistra. Es idempotente. Si falla, salir con 4 sin tocar la red.

"$scripts_dir/prepare/delete-vm.sh" "$lb" >&2 || fail 4 "No se pudo eliminar la VM $lb"

# == 3. Borrar la red interna ==
# 3.1 "VBoxManage dhcpserver remove --network=stratus-<lb>". Si el servidor DHCP ya no existe (una
#     eliminación anterior a medias) se avisa en stderr y se continúa: el resultado es el mismo.

VBoxManage dhcpserver remove --network="stratus-$lb" >&2 ||
  echo "El servidor DHCP de stratus-$lb no existía o no se pudo quitar" >&2

# == 4. Resultado ==
# 4.1 Imprimir en stdout "Balanceador <lb> eliminado junto con su red interna".

echo "Balanceador $lb eliminado junto con su red interna"

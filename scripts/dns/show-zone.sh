#!/usr/bin/env bash
# show-zone.sh - Muestra los registros que sirve el servidor DNS (005, enmienda del 2026-11-11):
# lee el archivo de zona /var/lib/bind/db.<dominio> con "named-checkzone -D" (forma canónica,
# nombres absolutos) e imprime un registro por línea. Solo lee: no cambia nada en la VM.
#
# Uso:
#   show-zone.sh <dns> <dominio>
#
# Argumentos:
#   dns      nombre del servidor DNS, ^[a-z0-9][a-z0-9-]{0,31}$ (nunca está detrás de un balanceador)
#   dominio  zona de APP_DNS_DOMAIN, de 1 a 4 etiquetas ^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$
#
# Variables de entorno:
#   APP_SSH_KEY_PATH  llave privada del backend
#
# Salida:
#   stdout  una línea por registro, separada por tabuladores: <tipo> <nombre> <ttl> <valor>;
#           el nombre va sin el punto final
#   stderr  diagnósticos
#
# Códigos de salida:
#   0   éxito
#   2   argumentos inválidos o variables de entorno faltantes
#   3   VM no encontrada en VirtualBox
#   4   fallo de comunicación con la VM (SSH)
#   5   no se pudo leer la zona en la VM
#   7   el servidor DNS no tiene dirección IP; verifique que esté encendido
#   99  operación no implementada

set -euo pipefail

scripts_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=../prepare/lib/vm-exec.sh
source "$scripts_dir/prepare/lib/vm-exec.sh"
# shellcheck source=lib/dns-common.sh
source "$scripts_dir/dns/lib/dns-common.sh"

# == 1. Validar argumentos ==
# 1.1 Exigir exactamente 2 argumentos; si no, imprimir el uso en stderr y salir con 2.
# 1.2 dns y dominio contra sus listas permitidas y APP_SSH_KEY_PATH definida; si no, salir con 2.
# Ninguna validación fallida llama a VBoxManage ni a ssh.

if (($# != 2)); then
  fail 2 "Uso: show-zone.sh <dns> <dominio>"
fi
dns="$1"
domain="$2"

validate_vm_name "$dns" || exit 2
validate_domain "$domain" || exit 2
if [[ -z "${APP_SSH_KEY_PATH:-}" ]]; then
  fail 2 "Falta la variable de entorno APP_SSH_KEY_PATH"
fi

# == 2. Comprobar la VM ==
# 2.1 Comprobar que la VM exista con "VBoxManage showvminfo <dns>"; si no, salir con 3.

if ! VBoxManage showvminfo "$dns" >/dev/null 2>&1; then
  fail 3 "VM no encontrada en VirtualBox"
fi

# == 3. Leer la zona ==
# 3.1 "named-checkzone -D -o - <dominio> <ruta>" en la VM imprime la zona en forma canónica.
# 3.2 Los códigos 2, 4 y 7 de vm_exec pasan tal cual; cualquier otro fallo sale con 5 y deja la
#     salida de la herramienta en stderr.

rc=0
dump="$(vm_exec "$dns" named-checkzone -D -o - "$domain" "/var/lib/bind/db.$domain" 2>&1 </dev/null)" || rc=$?
if ((rc != 0)); then
  case "$rc" in
    2 | 4 | 7) exit "$rc" ;;
  esac
  printf '%s\n' "$dump" >&2
  fail 5 "No se pudo leer la zona $domain en $dns"
fi

# == 4. Imprimir los registros ==
# 4.1 Quedarse solo con las líneas canónicas "<nombre> <ttl> IN <tipo> <datos...>": las líneas
#     "zone ...: loaded serial" y "OK" que algunas versiones imprimen en stdout se descartan.
# 4.2 Quitar el punto final del nombre e imprimir "<tipo>\t<nombre>\t<ttl>\t<valor>"; el valor
#     junta los datos con un espacio.

printf '%s\n' "$dump" | awk '
  $3 == "IN" && NF >= 5 && $2 ~ /^[0-9]+$/ {
    name = $1
    sub(/\.$/, "", name)
    value = $5
    for (i = 6; i <= NF; i++) value = value " " $i
    printf "%s\t%s\t%s\t%s\n", $4, name, $2, value
  }'

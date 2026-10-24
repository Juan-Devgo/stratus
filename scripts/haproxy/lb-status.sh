#!/usr/bin/env bash
# lb-status.sh - Lee el estado de cada servidor del backend de un balanceador desde el socket de
# estadísticas de HAProxy (127.0.0.1:9999 en el balanceador), con el comando "show stat". Es de solo
# lectura: no cambia nada en el balanceador.
#
# Uso:
#   lb-status.sh <lb>
#
# Argumentos:
#   lb  balanceador, ^[a-z0-9][a-z0-9-]{0,31}$; debe estar encendido con IPv4 en puente
#
# Variables de entorno:
#   APP_SSH_KEY_PATH  llave privada del backend
#
# Salida:
#   stdout  una línea "<server_name> <estado>" por servidor del backend stratus_back (UP, DOWN, MAINT,
#           NOLB...), sin las filas FRONTEND ni BACKEND; vacío si el balanceador no tiene servidores
#   stderr  diagnósticos
#
# Códigos de salida:
#   0   éxito
#   2   argumentos inválidos o variables de entorno faltantes
#   4   fallo de comunicación con el balanceador (SSH)
#   5   no se pudo leer el socket de estadísticas o su respuesta no tiene la columna status
#   12  el balanceador está apagado o sin IPv4 en puente
#   99  operación no implementada

set -euo pipefail

scripts_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=../prepare/lib/vm-exec.sh
source "$scripts_dir/prepare/lib/vm-exec.sh"
# shellcheck source=lib/haproxy-common.sh
source "$scripts_dir/haproxy/lib/haproxy-common.sh"

# == 1. Validar argumentos ==
# 1.1 Exigir exactamente 1 argumento; si no, imprimir el uso en stderr y salir con 2.
# 1.2 Validar lb contra ^[a-z0-9][a-z0-9-]{0,31}$; si no coincide, salir con 2.
# 1.3 Exigir APP_SSH_KEY_PATH; si falta, salir con 2.
# Ninguna validación fallida debe llamar a VBoxManage ni a ssh.

if (($# != 1)); then
  fail 2 "Uso: lb-status.sh <lb>"
fi
lb="$1"
validate_lb_name "$lb" || exit 2
if [[ -z "${APP_SSH_KEY_PATH:-}" ]]; then
  fail 2 "Falta la variable de entorno APP_SSH_KEY_PATH"
fi

# == 2. Comprobar el balanceador ==
# 2.1 Debe estar encendido con IPv4 en puente (lb_reachable); si no, salir con 12.

lb_reachable "$lb" || exit $?

# == 3. Leer el socket de estadísticas ==
# 3.1 Un solo comando remoto con un programa constante (nada del usuario se interpola): abre
#     /dev/tcp/127.0.0.1/9999, envía "show stat" y copia la respuesta CSV a stdout. "timeout 10" evita
#     que un HAProxy colgado deje la operación esperando. Un fallo de SSH sale con 4; cualquier otro,
#     con 5.

rc=0
raw="$(vm_exec "$lb" timeout 10 bash -c 'exec 3<>/dev/tcp/127.0.0.1/9999 && echo "show stat" >&3 && cat <&3' </dev/null)" || rc=$?
if ((rc == 4)); then
  exit 4
elif ((rc != 0)); then
  fail 5 "No se pudo leer el estado de HAProxy en $lb"
fi

# == 4. Interpretar la respuesta ==
# 4.1 La primera línea del CSV es "# pxname,svname,...": la posición de pxname, svname y status se
#     toma de ahí, no de números fijos. Si falta alguna columna, salir con 5.
# 4.2 De las filas del backend stratus_back, imprimir "<svname> <status>" salvo las de BACKEND y
#     FRONTEND. Del estado solo se conserva la primera palabra ("UP 1/3" -> UP, "DOWN 2/3" -> DOWN,
#     "MAINT (via ...)" -> MAINT).

parsed_ok=yes
parsed="$(awk -F, '
  /^#/ {
    for (i = 1; i <= NF; i++) {
      h = $i
      sub(/^# */, "", h)
      if (h == "pxname") px = i
      if (h == "svname") sv = i
      if (h == "status") st = i
    }
    found = (px && sv && st)
    next
  }
  found && $px == "stratus_back" && $sv != "BACKEND" && $sv != "FRONTEND" {
    split($st, word, " ")
    print $sv, word[1]
  }
  END { if (!found) exit 3 }
' <<<"$raw")" || parsed_ok=no
if [[ "$parsed_ok" == no ]]; then
  fail 5 "La respuesta de HAProxy no tiene la columna status"
fi

# == 5. Resultado ==
# 5.1 Imprimir las líneas de 4.2 (ninguna si el backend no tiene servidores).

if [[ -n "$parsed" ]]; then
  echo "$parsed"
fi

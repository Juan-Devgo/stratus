#!/usr/bin/env bash
# save-frontend.sh - Cambia el puerto del frontend de un balanceador: permite el puerto en ufw si el
# balanceador lo tiene instalado, reescribe haproxy.cfg completo con el puerto nuevo y los servidores
# guardados, lo revisa, recarga HAProxy y verifica que escuche en el puerto. Si algo falla, el
# haproxy.cfg y HAProxy quedan como estaban. Con 0 servidores HAProxy responde 503.
#
# Uso:
#   save-frontend.sh <lb> <frontend_port> [<spec>...]
#
# Argumentos:
#   lb             balanceador, ^[a-z0-9][a-z0-9-]{0,31}$; debe estar encendido con IPv4 en puente
#   frontend_port  puerto nuevo del frontend, 1-65535 y distinto de 22
#   spec           (cero a diez) servidores guardados, cada uno
#                  "<server_name>,<vm>,<port>,<ip>,<on|off>"
#
# Variables de entorno:
#   APP_SSH_KEY_PATH  llave privada del backend
#
# Salida:
#   stdout  "Frontend de <lb> en el puerto <port>"
#   stderr  diagnósticos
#
# Códigos de salida:
#   0   éxito: HAProxy escucha en el puerto nuevo
#   2   argumentos inválidos o variables de entorno faltantes
#   4   fallo de comunicación con el balanceador (SSH)
#   5   falló ufw
#   9   falló la recarga y también la restauración de haproxy.cfg
#   10  configuración de HAProxy inválida; se mantuvo la anterior
#   11  no se pudo recargar HAProxy (por ejemplo, el puerto está ocupado); se restauró la anterior
#   12  el balanceador está apagado o sin IPv4 en puente; no se cambió nada
#   99  operación no implementada

set -euo pipefail

scripts_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=../prepare/lib/vm-exec.sh
source "$scripts_dir/prepare/lib/vm-exec.sh"
# shellcheck source=lib/haproxy-common.sh
source "$scripts_dir/haproxy/lib/haproxy-common.sh"

# == 1. Validar argumentos ==
# 1.1 Exigir al menos 2 argumentos; si no, imprimir el uso en stderr y salir con 2.
# 1.2 Validar lb, frontend_port y las specs (hasta 10, nombres únicos, cualquier IPv4) contra sus
#     listas permitidas (haproxy-common.sh); si alguna falla, salir con 2.
# 1.3 Exigir APP_SSH_KEY_PATH; si falta, salir con 2.
# Ninguna validación fallida debe llamar a VBoxManage ni a ssh.

if (($# < 2)); then
  fail 2 "Uso: save-frontend.sh <lb> <frontend_port> [<spec>...]"
fi
lb="$1"
frontend_port="$2"
shift 2
specs=("$@")

validate_lb_name "$lb" || exit 2
validate_frontend_port "$frontend_port" || exit 2
validate_specs - "${specs[@]}" || exit 2
if [[ -z "${APP_SSH_KEY_PATH:-}" ]]; then
  fail 2 "Falta la variable de entorno APP_SSH_KEY_PATH"
fi

# == 2. Comprobar el balanceador ==
# 2.1 Debe estar encendido con IPv4 en puente (lb_reachable); si no, salir con 12 sin cambiar nada.

lb_reachable "$lb" || exit $?

# == 3. Permitir el puerto en ufw ==
# 3.1 Si "command -v ufw" encuentra ufw en el balanceador, "ufw allow <frontend_port>/tcp" (idempotente).
#     Si ufw no está instalado (el comando termina con 1), no se hace nada. Un fallo de la regla sale
#     con 5 y uno de SSH con 4.

ufw_rc=0
vm_exec "$lb" sh -c 'command -v ufw >/dev/null 2>&1' </dev/null || ufw_rc=$?
case "$ufw_rc" in
  0) vm_exec "$lb" ufw allow "$frontend_port/tcp" </dev/null || fail 5 "No se pudo permitir el puerto $frontend_port/tcp en ufw" ;;
  5) ;; # ufw no está instalado
  *) exit "$ufw_rc" ;; # fallo de SSH
esac

# == 4. Reescribir haproxy.cfg ==
# 4.1 Generar el archivo completo con el puerto nuevo y las specs guardadas, y aplicarlo con apply_cfg
#     (revisión, recarga y verificación del puerto). Su código 10 u 11 sale tal cual.

rc=0
render_cfg "$frontend_port" "${specs[@]}" | apply_cfg "$lb" "$frontend_port" || rc=$?
if ((rc != 0)); then
  exit "$rc"
fi

# == 5. Resultado ==
# 5.1 Imprimir en stdout "Frontend de <lb> en el puerto <frontend_port>".

echo "Frontend de $lb en el puerto $frontend_port"

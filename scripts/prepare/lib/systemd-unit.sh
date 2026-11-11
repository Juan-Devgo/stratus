#!/usr/bin/env bash
# systemd-unit.sh - Biblioteca compartida (005): acciones y estado de una unidad systemd de una
# VM (apache2 o nginx en un servidor web, named en el servidor DNS), por SSH con vm_exec.
#
# Uso: desde un script que ya cargó vm-exec.sh
#   source "$(dirname "${BASH_SOURCE[0]}")/../prepare/lib/systemd-unit.sh"
#   unit_action <vm_name> <unidad> <start|stop|restart>
#   unit_status <vm_name> <unidad>
#
# Argumentos:
#   vm_name  nombre de la VM (ya validado por el script que llama)
#   unidad   unidad systemd (ya validada por el script que llama: apache2, nginx o named)
#   acción   start, stop o restart
#
# Salida:
#   unit_action  stdout: "Servicio <unidad> <iniciado|detenido|reiniciado> en <vm_name>" y, tras
#                start o restart, "active_state=<estado>"
#   unit_status  stdout: la salida de "systemctl status --no-pager --full <unidad>"
#   stderr       diagnósticos
#
# Códigos de salida: valor de retorno de las funciones
#   0   éxito (también un servicio inactivo o fallido en unit_status)
#   2   argumentos inválidos o APP_SSH_KEY_PATH faltante
#   4   fallo de comunicación con la VM (SSH)
#   5   systemctl falló en la VM
#   7   la VM no tiene dirección IP
#   12  el balanceador de la VM (--jump) está apagado o sin IPv4 en puente
#   99  operación no implementada
#
# Se carga con "source" desde scripts que ya están en modo estricto; repetirlo aquí no cambia sus
# opciones.
set -euo pipefail

# == 1. unit_action <vm_name> <unidad> <acción> ==
# 1.1 Exigir 3 argumentos y una acción start, stop o restart; si no, devolver 2 sin llamar a ssh.
# 1.2 Ejecutar "systemctl <acción> <unidad>". 2, 4, 7 y 12 de vm_exec pasan tal cual; otro fallo
#     devuelve 5. Una acción que deja el estado igual (iniciar un servicio activo) termina bien.
# 1.3 Imprimir el mensaje.
# 1.4 Tras start o restart, esperar 3 segundos y leer ActiveState ("desconocido" si no se puede
#     leer): un servicio que cae justo después de iniciar no es un fallo de la operación (003).

unit_action() {
  if (($# != 3)) || [[ ! "$3" =~ ^(start|stop|restart)$ ]]; then
    echo "Uso: unit_action <vm_name> <unidad> <start|stop|restart>" >&2
    return 2
  fi
  local vm="$1" unit="$2" action="$3" rc=0 state done_msg
  vm_exec "$vm" systemctl "$action" "$unit" || rc=$?
  case "$rc" in
    0) ;;
    2 | 4 | 7 | 12) return "$rc" ;;
    *)
      echo "No se pudo ejecutar systemctl $action $unit en $vm" >&2
      return 5
      ;;
  esac
  case "$action" in
    start) done_msg=iniciado ;;
    stop) done_msg=detenido ;;
    restart) done_msg=reiniciado ;;
  esac
  echo "Servicio $unit $done_msg en $vm"
  [[ "$action" != stop ]] || return 0

  sleep 3
  state="$(vm_exec "$vm" systemctl show -p ActiveState --value "$unit" 2>/dev/null </dev/null)" || state=""
  echo "active_state=${state:-desconocido}"
}

# == 2. unit_status <vm_name> <unidad> ==
# 2.1 Exigir 2 argumentos; si no, devolver 2.
# 2.2 Ejecutar "systemctl status --no-pager --full <unidad>" en un programa remoto fijo que trata
#     el código 3 (inactivo o fallido) como éxito: Estado muestra la salida, no un fallo (REQ-12).
# 2.3 2, 4, 7 y 12 pasan tal cual; otro fallo devuelve 5.

unit_status() {
  if (($# != 2)); then
    echo "Uso: unit_status <vm_name> <unidad>" >&2
    return 2
  fi
  local rc=0
  vm_exec "$1" sh -c 'systemctl status --no-pager --full "$1"; rc=$?; [ "$rc" -eq 0 ] || [ "$rc" -eq 3 ]' sh "$2" || rc=$?
  case "$rc" in
    0 | 2 | 4 | 7 | 12) return "$rc" ;;
  esac
  echo "No se pudo leer el estado de $2 en $1" >&2
  return 5
}

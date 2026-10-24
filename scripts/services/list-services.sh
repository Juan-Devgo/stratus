#!/usr/bin/env bash
# list-services.sh - Lista los servicios systemd creados por Stratus en una VM (los que tienen
# la marca en su archivo de unidad) con su estado y su configuración. Solo lee: no cambia nada en
# la VM. Entra por SSH con la llave del backend contra la IP de la VM.
#
# Uso:
#   list-services.sh [--jump <lb>] <vm_name>
#
# Argumentos:
#   --jump <lb>  (opcional, siempre primero) balanceador por el que se llega a la VM cuando es un
#                servidor asignado a él (004); el backend lo agrega
#   vm_name     nombre de la VM, ^[a-z0-9][a-z0-9-]{0,31}$
#
# Variables de entorno:
#   APP_SSH_KEY_PATH  llave privada del backend
#
# Salida:
#   stdout  una línea por servicio, campos separados por tabulador:
#           name, active_state, enabled_state, restart, restart_sec, executable, args,
#           env_keys (claves separadas por coma, pueden faltar), description.
#           Del archivo de variables solo salen las claves, nunca los valores.
#   stderr  diagnósticos
#
# Códigos de salida:
#   0   éxito (también si no hay servicios: stdout queda vacío)
#   2   argumentos inválidos o variables de entorno faltantes
#   3   VM no encontrada en VirtualBox
#   4   fallo de comunicación con la VM (SSH)
#   5   fallo del comando remoto
#   7   VM sin dirección IP; verifique que esté encendida
#   12  el balanceador (--jump) está apagado o sin IPv4 en puente
#   99  operación no implementada

set -euo pipefail

# shellcheck source=../prepare/lib/vm-exec.sh
source "$(dirname "${BASH_SOURCE[0]}")/../prepare/lib/vm-exec.sh"
# shellcheck source=lib/service-common.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/service-common.sh"

# == 1. Validar argumentos ==
# 1.0 Opciones: "--jump <lb>" es opcional y va siempre primero (REQ-19). Con él, vm_name es un
#     servidor asignado al balanceador <lb> y se alcanza por SSH a través de su cuenta jump. El
#     backend lo agrega; el script no deduce el rol. parse_jump (service-common.sh) lo valida
#     (^[a-z0-9][a-z0-9-]{0,31}$) y sale con 2 si falta el valor o no cumple.
# 1.1 Exigir exactamente 1 argumento; si no, imprimir el uso en stderr y salir con 2.
# 1.2 Validar vm_name contra ^[a-z0-9][a-z0-9-]{0,31}$; si no coincide, salir con 2.
# 1.3 Exigir APP_SSH_KEY_PATH; si falta, salir con 2.
# Ninguna validación fallida debe llamar a VBoxManage ni a ssh.

parse_jump "$@"
shift "$jump_shift"

if (($# != 1)); then
  fail 2 "Uso: list-services.sh [--jump <lb>] <vm_name>"
fi
vm_name="$1"

if [[ ! "$vm_name" =~ ^[a-z0-9][a-z0-9-]{0,31}$ ]]; then
  fail 2 'vm_name inválido: debe cumplir ^[a-z0-9][a-z0-9-]{0,31}$'
fi

if [[ -z "${APP_SSH_KEY_PATH:-}" ]]; then
  fail 2 "Falta la variable de entorno APP_SSH_KEY_PATH"
fi

# == 2. Preparar la ejecución en la VM ==
# 2.1 Comprobar que la VM exista con "VBoxManage showvminfo <vm_name>"; si falla, salir con 3.
# 2.2 Comprobar con "vm_ip" que la VM tenga IPv4; si no, salir con 7 (vm_ip ya imprimió el motivo).

# Con --jump, registrar la ruta antes de cualquier vm_ip o vm_exec: la VM se alcanza por el balanceador.
[[ -z "$jump_lb" ]] || vm_route "$vm_name" "$jump_lb"

if ! VBoxManage showvminfo "$vm_name" >/dev/null 2>&1; then
  fail 3 "VM no encontrada en VirtualBox"
fi

vm_ip "$vm_name" >/dev/null || exit $?

# == 3. Leer los servicios (REQ-16, REQ-21) ==
# 3.1 Un solo comando remoto "sh -c <programa>" con un programa constante: nada del usuario se
#     interpola en él. Recibe la marca y los dos directorios como argumentos ($1, $2, $3).
# 3.2 Recorre los *.service del directorio de unidades y se queda con los que tienen la marca como
#     línea exacta y un nombre válido (^[a-z][a-z0-9-]{0,31}$).
# 3.3 De cada unidad lee Description, ExecStart (sin el prefijo /opt/<name>/, separado en
#     ejecutable y argumentos), Restart y RestartSec.
# 3.4 El estado activo es "systemctl show -p ActiveState --value"; el habilitado es "enabled" si
#     "systemctl is-enabled" lo dice y "disabled" en cualquier otro caso.
# 3.5 De /etc/<name>.env solo salen las claves, con sed: los valores nunca se leen en la salida.
# 3.6 Si el comando remoto falla, salir con 5.

LIST="$(
  cat <<'PROGRAM'
marker="$1"
units="$2"
etc="$3"
for file in "$units"/*.service; do
  [ -f "$file" ] || continue
  grep -qxF "$marker" "$file" || continue
  name="$(basename "$file" .service)"
  printf '%s\n' "$name" | grep -qE '^[a-z][a-z0-9-]{0,31}$' || continue

  description="$(sed -n 's/^Description=//p' "$file" | head -n 1)"
  exec_start="$(sed -n 's/^ExecStart=//p' "$file" | head -n 1)"
  exec_start="${exec_start#/opt/$name/}"
  executable="${exec_start%% *}"
  args="${exec_start#"$executable"}"
  args="${args# }"
  restart="$(sed -n 's/^Restart=//p' "$file" | head -n 1)"
  restart_sec="$(sed -n 's/^RestartSec=//p' "$file" | head -n 1)"

  active="$(systemctl show -p ActiveState --value "$name.service" 2>/dev/null)"
  enabled=disabled
  [ "$(systemctl is-enabled "$name.service" 2>/dev/null)" = enabled ] && enabled=enabled

  env_keys="$(sed -n 's/^\([A-Z_][A-Z0-9_]*\)=.*/\1/p' "$etc/$name.env" 2>/dev/null | paste -sd, -)"

  printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' "$name" "${active:-unknown}" "$enabled" \
    "$restart" "$restart_sec" "$executable" "$args" "$env_keys" "$description"
done
PROGRAM
)"

guest 5 sh -c "$LIST" sh "$MARKER" /etc/systemd/system /etc

# == 4. Resultado ==
# 4.1 La salida del paso 3 ya es el resultado (stdout); no se imprime nada más.

#!/usr/bin/env bash
# jump-keys.sh - Biblioteca compartida: mantiene en el balanceador las llaves públicas que pueden
# entrar al servidor a través de la cuenta "jump" (REQ-17). La cuenta jump no tiene contraseña ni
# shell; su archivo /var/lib/jump/.ssh/authorized_keys lleva una línea por llave:
#   restrict,port-forwarding <tipo> <llave> stratus-jump:<vm>
# más la línea de la llave del backend (etiqueta "stratus-backend"), que esta biblioteca nunca toca.
#
# Uso (desde otro script, que ya tiene "set -euo pipefail" y ya cargó vm-exec.sh):
#   source "$(dirname "${BASH_SOURCE[0]}")/../prepare/lib/vm-exec.sh"
#   source "$(dirname "${BASH_SOURCE[0]}")/lib/jump-keys.sh"
#   jump_sync <lb> <vm> [<usuario_excluido>]
#   jump_drop <lb> <vm>
#
# Argumentos:
#   lb                 balanceador, ^[a-z0-9][a-z0-9-]{0,31}$
#   vm                 servidor asignado a ese balanceador, misma regla
#   usuario_excluido   (opcional) usuario del servidor cuya llave no se copia, por ejemplo el que
#                      se está eliminando; root nunca se excluye por error porque se compara el
#                      nombre exacto
#
# Variables de entorno:
#   APP_SSH_KEY_PATH  llave privada del backend; se lee "<ruta>.pub" para no copiar esa llave
#
# Salida:
#   stdout  nada
#   stderr  diagnósticos
#
# Códigos de salida (valor de retorno de las funciones):
#   0   éxito: el conjunto de llaves de la VM en jump quedó igual al del servidor (o vacío)
#   2   argumentos inválidos
#   15  no se pudo leer el servidor o escribir en el balanceador; jump queda como estaba
#   99  operación no implementada
#
# Este archivo se carga con "source": no activa "set -euo pipefail" porque hereda el modo
# estricto del script que lo carga. Necesita vm-exec.sh ya cargado.

# Archivo de llaves de la cuenta jump en el balanceador.
JUMP_AUTHORIZED_KEYS=/var/lib/jump/.ssh/authorized_keys

# Nombre válido de VM o balanceador (la misma regla de haproxy-common.sh, sin depender de él).
_jump_valid_name() {
  [[ "${1-}" =~ ^[a-z0-9][a-z0-9-]{0,31}$ ]]
}

# == 1. jump_sync <lb> <vm> [<usuario_excluido>] ==
# 1.1 Validar los nombres (2 si no cumplen) y registrar la ruta del servidor con vm_route.
# 1.2 Leer, con un solo vm_exec enrutado al servidor, las llaves de /root/.ssh/authorized_keys y de
#     <home>/.ssh/authorized_keys de los usuarios con UID 1000-59999 (los usuarios de 002). Cada
#     línea sale como "<dueño> <línea original>"; el programa remoto es constante.
# 1.3 En el host: descartar las líneas del usuario excluido y las que no son una llave (comentarios,
#     líneas en blanco), la llave del backend (se compara tipo y llave con <APP_SSH_KEY_PATH>.pub)
#     y las repetidas (el mismo tipo y llave en root y en un usuario, o con otro comentario).
# 1.4 Dar a cada llave la forma "restrict,port-forwarding <tipo> <llave> stratus-jump:<vm>".
# 1.5 Escribir el resultado con _jump_write. Cualquier fallo de 1.2 o 1.5 imprime "No se pudo
#     actualizar la cuenta jump de <lb>" y devuelve 15.

jump_sync() {
  if (($# < 2 || $# > 3)) || ! _jump_valid_name "$1" || ! _jump_valid_name "$2"; then
    echo "Uso: jump_sync <lb> <vm> [<usuario_excluido>] (nombres válidos)" >&2
    return 2
  fi
  local lb="$1" vm="$2" exclude="${3-}" raw owner type blob rest backend_type backend_blob out="" key
  local -A seen=()
  vm_route "$vm" "$lb" || return 2

  if ! read -r backend_type backend_blob rest <"$APP_SSH_KEY_PATH.pub"; then
    echo "No se pudo leer la llave pública del backend ($APP_SSH_KEY_PATH.pub)" >&2
    return 15
  fi
  seen["$backend_type $backend_blob"]=1

  raw="$(vm_exec "$vm" sh -c '{ [ ! -r /root/.ssh/authorized_keys ] || awk '\''{ print "root " $0 }'\'' /root/.ssh/authorized_keys; getent passwd | awk -F: '\''$3 >= 1000 && $3 <= 59999 { print $1, $6 }'\'' | while read -r user home; do [ ! -r "$home/.ssh/authorized_keys" ] || awk -v u="$user" '\''{ print u " " $0 }'\'' "$home/.ssh/authorized_keys"; done; }; true' </dev/null)" || {
    echo "No se pudo actualizar la cuenta jump de $lb: no se pudieron leer las llaves de $vm" >&2
    return 15
  }

  while IFS=' ' read -r owner type blob rest; do
    [[ -n "$owner" && "$owner" != "$exclude" ]] || continue
    [[ "$type" =~ ^(ssh-[a-z0-9]+|ecdsa-sha2-nistp[0-9]+|sk-[a-z0-9@.-]+)$ ]] || continue
    [[ "$blob" =~ ^[A-Za-z0-9+/=]+$ ]] || continue
    key="$type $blob"
    [[ -z "${seen[$key]:-}" ]] || continue
    seen["$key"]=1
    out+="restrict,port-forwarding $type $blob stratus-jump:$vm"$'\n'
  done <<<"$raw"

  printf '%s' "$out" | _jump_write "$lb" "$vm"
}

# == 2. jump_drop <lb> <vm> ==
# 2.1 Validar los nombres (2 si no cumplen).
# 2.2 Escribir un conjunto vacío: quita todas las líneas "stratus-jump:<vm>" sin leer el servidor,
#     que puede estar apagado o sin red interna. Es idempotente. Falla con 15 como jump_sync.

jump_drop() {
  if (($# != 2)) || ! _jump_valid_name "$1" || ! _jump_valid_name "$2"; then
    echo "Uso: jump_drop <lb> <vm> (nombres válidos)" >&2
    return 2
  fi
  : | _jump_write "$1" "$2"
}

# == 3. _jump_write <lb> <vm> ==
# 3.1 Leer las líneas nuevas de stdin y mandarlas a un vm_exec DIRECTO al balanceador (no enrutado).
# 3.2 El programa remoto es constante: la VM y la ruta del archivo llegan como $1 y $2. Arma un
#     archivo nuevo con las líneas actuales que NO terminan en " stratus-jump:<vm>" (las de otras VMs
#     y la del backend quedan) más las nuevas, le da dueño jump y modo 600, y lo mueve sobre el
#     original con mv (reemplazo atómico: sshd nunca ve un archivo a medias).
# 3.3 Si algo falla, imprimir "No se pudo actualizar la cuenta jump de <lb>" y devolver 15.

_jump_write() {
  local lb="$1" vm="$2" rc=0
  vm_exec "$lb" sh -c 'f=$2; t=$f.new; { grep -v -- " stratus-jump:$1\$" "$f" 2>/dev/null || true; cat; } >"$t" && chown jump:jump "$t" && chmod 600 "$t" && mv "$t" "$f"' \
    sh "$vm" "$JUMP_AUTHORIZED_KEYS" || rc=$?
  if ((rc != 0)); then
    echo "No se pudo actualizar la cuenta jump de $lb" >&2
    return 15
  fi
}

#!/usr/bin/env bash
# create-backend-key.sh - Crea el par de llaves ed25519 propio del backend, sin frase de paso,
# con el que el backend entra por SSH como root a las VMs. Corre en el host al iniciar el
# backend si el archivo de la llave no existe. Esta llave nunca se ofrece para descargar.
#
# Uso:
#   create-backend-key.sh <key_path>
#
# Argumentos:
#   key_path  ruta absoluta de la llave privada (APP_SSH_KEY_PATH); la pública queda en
#             "<key_path>.pub"
#
# Variables de entorno:
#   ninguna
#
# Salida:
#   stdout  mensaje de resultado; nunca el contenido de la llave privada
#   stderr  diagnósticos
#
# Códigos de salida:
#   0   éxito: el par existe con permisos 600 (también si ya existía)
#   2   argumentos inválidos
#   5   fallo de ssh-keygen, de chmod o al crear el directorio de la llave
#   99  operación no implementada

set -euo pipefail

# == 1. Validar argumentos ==
# 1.1 Exigir exactamente 1 argumento; si no, imprimir el uso en stderr y salir con 2.
# 1.2 Exigir que key_path sea una ruta absoluta (empieza con "/"), sin saltos de línea ni
#     caracteres de control; si no, salir con 2.

fail() {
  echo "$2" >&2
  exit "$1"
}

if (($# != 1)); then
  fail 2 "Uso: create-backend-key.sh <key_path>"
fi
key_path="$1"
if [[ "$key_path" != /* || "$key_path" =~ [[:cntrl:]] ]]; then
  fail 2 "key_path debe ser una ruta absoluta sin caracteres de control"
fi

# == 2. Idempotencia ==
# 2.1 Si key_path y "<key_path>.pub" ya existen, no regenerarlos (otra llave dejaría al
#     backend sin acceso a las VMs ya configuradas): aplicar "chmod 600 <key_path>",
#     imprimir "La llave del backend ya existe en <key_path>" y salir con 0.
# 2.2 Si existe solo uno de los dos archivos, imprimir el motivo en stderr y salir con 5
#     sin tocarlos; el equipo decide a mano cuál conservar.

if [[ -f "$key_path" && -f "$key_path.pub" ]]; then
  chmod 600 "$key_path" || fail 5 "No se pudieron ajustar los permisos de $key_path"
  echo "La llave del backend ya existe en $key_path"
  exit 0
fi
if [[ -e "$key_path" || -e "$key_path.pub" ]]; then
  fail 5 "Solo existe uno de los dos archivos ($key_path, $key_path.pub); conserve uno a mano"
fi

# == 3. Crear el par ==
# 3.1 Crear el directorio padre con "mkdir -p" y permisos 700; si falla, salir con 5.
# 3.2 Fijar "umask 077" antes de generar, para que la llave nunca sea legible por otros
#     ni siquiera por un instante.
# 3.3 Ejecutar "ssh-keygen -q -t ed25519 -N "" -C stratus-backend -f <key_path>"; si falla,
#     salir con 5. No redirigir su salida a stdout: no debe imprimir la llave privada.
# 3.4 Ejecutar "chmod 600 <key_path>" y "chmod 644 <key_path>.pub"; si falla, salir con 5.

mkdir -p -m 700 "$(dirname "$key_path")" || fail 5 "No se pudo crear el directorio de la llave"
umask 077
ssh-keygen -q -t ed25519 -N "" -C stratus-backend -f "$key_path" >&2 || fail 5 "ssh-keygen falló"
chmod 600 "$key_path" && chmod 644 "$key_path.pub" || fail 5 "No se pudieron ajustar los permisos de la llave"

# == 4. Resultado ==
# 4.1 Imprimir en stdout "Llave del backend creada en <key_path>"; nunca el contenido de la
#     llave privada.

echo "Llave del backend creada en $key_path"

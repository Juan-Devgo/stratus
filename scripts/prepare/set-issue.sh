#!/usr/bin/env bash
# set-issue.sh - Reescribe /etc/issue y /etc/issue.net en la VM, por SSH con la llave del backend contra la IP de
# la VM, con un mensaje propio y los campos dinámicos elegidos, que agetty expande en la pantalla
# de inicio de sesión.
#
# Uso:
#   set-issue.sh [--jump <lb>] <vm_name> <mensaje> [campo...]
#
# Argumentos:
#   --jump <lb>  (opcional, siempre primero) balanceador por el que se llega a la VM cuando es un
#                servidor asignado a él (004); el backend lo agrega
#   vm_name     nombre de la VM, ^[a-z0-9][a-z0-9-]{0,31}$
#   mensaje     texto propio (puede ser vacío ""): hasta 500 caracteres UTF-8 imprimibles y
#               saltos de línea, sin barra invertida ni otros caracteres de control
#   campo       cero o más claves, en el orden en que deben aparecer (el backend ya las
#               envía en el orden fijo de REQ-26):
#                 hostname  -> "Host: \n"
#                 date      -> "Fecha: \d"
#                 time      -> "Hora: \t"
#                 ipv4      -> "IPv4: \4"
#                 os        -> "Sistema: \s"
#                 kernel    -> "Kernel: \r"
#                 arch      -> "Arquitectura: \m"
#                 tty       -> "Terminal: \l"
#                 users     -> "Usuarios: \U"
#
# Variables de entorno:
#   APP_SSH_KEY_PATH  llave privada del backend
#
# Salida:
#   stdout  mensaje de resultado
#   stderr  diagnósticos
#
# Códigos de salida:
#   0   éxito: /etc/issue y /etc/issue.net tienen el contenido pedido (también si ya lo tenía)
#   2   argumentos inválidos o variables de entorno faltantes
#   3   VM no encontrada en VirtualBox
#   4   fallo de comunicación con la VM (SSH)
#   5   fallo al escribir /etc/issue o /etc/issue.net en la VM
#   7   VM sin dirección IP; verifique que esté encendida
#   12  el balanceador (--jump) está apagado o sin IPv4 en puente
#   99  operación no implementada

set -euo pipefail

# == 1. Validar argumentos ==
# 1.0 Opciones: "--jump <lb>" es opcional y va siempre primero (REQ-19). Con él, vm_name es un
#     servidor asignado al balanceador <lb> y se alcanza por SSH a través de su cuenta jump. El
#     backend lo agrega; el script no deduce el rol. <lb> cumple ^[a-z0-9][a-z0-9-]{0,31}$;
#     si falta o no cumple, salir con 2.
# 1.1 Exigir al menos 2 argumentos (el mensaje puede ser ""); si no, imprimir el uso en
#     stderr y salir con 2.
# 1.2 Validar vm_name contra ^[a-z0-9][a-z0-9-]{0,31}$; si no coincide, salir con 2.
# 1.3 Con "LC_ALL=C.UTF-8" para contar caracteres y no bytes, rechazar con 2 un mensaje de
#     más de 500 caracteres ("${#mensaje}").
# 1.4 Rechazar con 2 un mensaje que contenga "\" (agetty lo interpretaría como un escape) o
#     cualquier carácter de control distinto del salto de línea: quitar los "\n" y comprobar
#     con "[[ ... =~ [[:cntrl:]] ]]" (cubre tabulación, retorno de carro, \x07, etc.).
# 1.5 Hacer "shift 2"; cada campo restante debe ser una de las 9 claves de la cabecera; una
#     clave desconocida sale con 2.
# 1.6 Si el mensaje está vacío y no hay campos, salir con 2 (no hay nada que escribir).
# Ninguna validación fallida debe llamar a VBoxManage ni a ssh.

fail() {
  echo "$2" >&2
  exit "$1"
}

jump_lb=""
if [[ "${1-}" == --jump ]]; then
  if [[ ! "${2-}" =~ ^[a-z0-9][a-z0-9-]{0,31}$ ]]; then
    fail 2 '--jump inválido: debe cumplir ^[a-z0-9][a-z0-9-]{0,31}$'
  fi
  jump_lb="$2"
  shift 2
fi

if (($# < 2)); then
  fail 2 "Uso: set-issue.sh [--jump <lb>] <vm_name> <mensaje> [campo...]"
fi
vm_name="$1"
message="$2"

if [[ ! "$vm_name" =~ ^[a-z0-9][a-z0-9-]{0,31}$ ]]; then
  fail 2 'vm_name inválido: debe cumplir ^[a-z0-9][a-z0-9-]{0,31}$'
fi

# Con UTF-8 "${#message}" cuenta caracteres y no bytes; también hace que [[:cntrl:]] vea
# caracteres de control Unicode.
LC_ALL=C.UTF-8
if ((${#message} > 500)); then
  fail 2 "mensaje demasiado largo: máximo 500 caracteres"
fi

if [[ "$message" == *\\* ]]; then
  fail 2 "mensaje inválido: no puede contener barra invertida"
fi
message_without_newlines="${message//$'\n'/}"
if [[ "$message_without_newlines" =~ [[:cntrl:]] ]]; then
  fail 2 "mensaje inválido: no puede contener caracteres de control salvo el salto de línea"
fi

shift 2
for field in "$@"; do
  case "$field" in
    hostname | date | time | ipv4 | os | kernel | arch | tty | users) ;;
    *) fail 2 "campo desconocido: $field" ;;
  esac
done

if [[ -z "$message" && $# -eq 0 ]]; then
  fail 2 "No hay nada que escribir: mensaje vacío y sin campos"
fi

# == 2. Preparar la ejecución en la VM ==
# 2.1 Cargar la biblioteca con: source "$(dirname "${BASH_SOURCE[0]}")/lib/vm-exec.sh".
# 2.2 Comprobar que la VM exista con "VBoxManage showvminfo <vm_name>"; si falla, salir con 3.
# 2.3 Comprobar con "vm_ip" que la VM tenga IPv4; si no, salir con 7 antes de cualquier comando
#     remoto (vm_ip ya imprimió el motivo).
# 2.4 Cada comando en la VM se ejecuta como root con "vm_exec <vm_name> <comando> <argumentos...>";
#     si vm_exec devuelve 4 (SSH), 2, 7 o 12, el script sale con ese mismo código.

# shellcheck source=lib/vm-exec.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/vm-exec.sh"

# Con --jump, registrar la ruta antes de cualquier vm_ip o vm_exec: la VM se alcanza por el balanceador.
[[ -z "$jump_lb" ]] || vm_route "$vm_name" "$jump_lb"

if ! VBoxManage showvminfo "$vm_name" >/dev/null 2>&1; then
  fail 3 "VM no encontrada en VirtualBox"
fi

vm_ip "$vm_name" >/dev/null || exit $?

# guest <código_de_fallo> <comando> [argumentos...]: ejecuta el comando como root en la VM. Sale con
# 2, 4 o 7 tal como los devuelve vm_exec, y con <código_de_fallo> si el comando termina con error.
guest() {
  local fail_code="$1" rc=0
  shift
  vm_exec "$vm_name" "$@" || rc=$?
  case "$rc" in
    0) ;;
    2 | 4 | 7 | 12) exit "$rc" ;;
    *) exit "$fail_code" ;;
  esac
}

# == 3. Componer el contenido (REQ-26) ==
# 3.1 Si el mensaje no está vacío, es la primera parte del contenido, con sus saltos de línea.
# 3.2 Después, una línea por campo en el orden recibido, con la etiqueta de la cabecera, por
#     ejemplo "Host: \n". El escape se escribe literal (barra invertida y letra), entre
#     comillas simples, para que agetty lo expanda y no bash.
# 3.3 Unir las partes con "\n" y terminar el archivo con un salto de línea.
# 3.4 Ejemplo: "Hola hostname users" produce las líneas "Hola", "Host: \n" y "Usuarios: \U".

# Los escapes van literales (entre comillas simples) para que los expanda agetty, no bash.
content=""
if [[ -n "$message" ]]; then
  content="$message"
fi
for field in "$@"; do
  case "$field" in
    hostname) line='Host: \n' ;;
    date) line='Fecha: \d' ;;
    time) line='Hora: \t' ;;
    ipv4) line='IPv4: \4' ;;
    os) line='Sistema: \s' ;;
    kernel) line='Kernel: \r' ;;
    arch) line='Arquitectura: \m' ;;
    tty) line='Terminal: \l' ;;
    users) line='Usuarios: \U' ;;
  esac
  content+="${content:+$'\n'}$line"
done

# == 4. Escribir /etc/issue ==
# 4.1 Enviar el contenido como un solo argumento, nunca dentro del texto del comando:
#     vm_exec ... sh -c 'printf "%s\n" "$1" | tee "$2" "$3" >/dev/null' sh "<contenido>" /etc/issue /etc/issue.net
#     Así el contenido no se interpreta en el shell remoto (P3). El mismo contenido se escribe
#     también en /etc/issue.net para que se vea al iniciar sesión por SSH.
# 4.2 Reescribir el archivo completo deja el mismo resultado si se repite (idempotente).
# 4.3 Si la escritura de cualquiera de los dos archivos falla, reenviar su stderr y salir con 5.

# El contenido viaja como un solo argumento ($1), nunca dentro del texto del comando.
guest 5 sh -c 'printf "%s\n" "$1" | tee "$2" "$3" >/dev/null' sh "$content" /etc/issue /etc/issue.net

# == 5. Resultado ==
# 5.1 Imprimir en stdout "/etc/issue de <vm_name> actualizado".

echo "/etc/issue de $vm_name actualizado"

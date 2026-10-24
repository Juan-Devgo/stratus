#!/usr/bin/env bash
# create-user.sh - Crea un usuario regular en la VM, con sudo sin contraseña opcional y con
# una contraseña o un par de llaves SSH ed25519. Entra por SSH con la llave del backend
# contra la IP de la VM. Si algo falla después de crear la cuenta, la limpieza quita la
# cuenta, su home y su regla sudo, para que no quede un usuario a medias.
#
# Uso:
#   create-user.sh [--jump <lb>] <vm_name> <username> <sudo> <ssh_key> [password_file]
#
# Argumentos:
#   --jump <lb>  (opcional, siempre primero) balanceador por el que se llega a la VM cuando es un
#                servidor asignado a él (004); el backend lo agrega
#   vm_name        nombre de la VM, ^[a-z0-9][a-z0-9-]{0,31}$
#   username       nombre del usuario, ^[a-z][a-z0-9_-]{0,31}$
#   sudo           yes o no: regla "NOPASSWD: ALL" en /etc/sudoers.d/stratus-<username>
#   ssh_key        yes o no: genera un par ed25519 sin passphrase, instala la llave pública y
#                  bloquea la contraseña de la cuenta
#   password_file  solo con ssh_key=no (y entonces obligatorio): archivo del host con la
#                  contraseña, de 8 a 72 caracteres ASCII imprimibles sin espacio al inicio ni
#                  al final. La contraseña nunca va en argv: llega a chpasswd por la entrada
#                  estándar de ssh. Con ssh_key=yes el script espera exactamente 4 argumentos.
#
# Variables de entorno:
#   APP_SSH_KEY_PATH  llave privada del backend
#
# Salida:
#   stdout  con ssh_key=yes: solo la llave privada del usuario (el backend la guarda en memoria)
#           con ssh_key=no: "Usuario <username> creado en <vm_name>"
#   stderr  diagnósticos; nunca la contraseña ni la llave privada
#
# Códigos de salida:
#   0   éxito
#   2   argumentos inválidos, archivo de contraseña inválido o variables de entorno faltantes
#   3   VM no encontrada en VirtualBox
#   4   fallo de comunicación con la VM (SSH)
#   5   fallo al crear el usuario (la limpieza terminó bien)
#   6   el usuario ya existe en la VM
#   7   VM sin dirección IP; verifique que esté encendida
#   12  el balanceador (--jump) está apagado o sin IPv4 en puente
#   15  con --jump y ssh_key=yes: no se pudo actualizar la cuenta jump; se deshizo la creación
#   8   ya existe un grupo con ese nombre en la VM
#   9   la creación falló y la limpieza también: puede quedar una cuenta parcial
#   99  operación no implementada

set -euo pipefail

# == 1. Validar argumentos ==
# 1.0 Opciones: "--jump <lb>" es opcional y va siempre primero (REQ-19). Con él, vm_name es un
#     servidor asignado al balanceador <lb> y se alcanza por SSH a través de su cuenta jump. El
#     backend lo agrega; el script no deduce el rol. <lb> cumple ^[a-z0-9][a-z0-9-]{0,31}$;
#     si falta o no cumple, salir con 2.
# 1.1 Exigir al menos 4 argumentos; con ssh_key=no exigir exactamente 5 (el quinto es el
#     archivo de contraseña) y con ssh_key=yes exactamente 4. Si no, imprimir el uso en stderr
#     y salir con 2.
# 1.2 Validar vm_name contra ^[a-z0-9][a-z0-9-]{0,31}$ y username contra ^[a-z][a-z0-9_-]{0,31}$.
# 1.3 Validar que sudo y ssh_key sean yes o no.
# 1.4 Exigir APP_SSH_KEY_PATH.
# 1.5 Con ssh_key=no: el archivo de contraseña debe ser legible, de una sola línea, y su
#     contenido (leído con LC_ALL=C) debe cumplir ^[!-~][ -~]{6,70}[!-~]$. El mensaje de error
#     nunca repite la contraseña.
# Ninguna validación fallida debe llamar a VBoxManage ni a ssh.

fail() {
  echo "$2" >&2
  exit "$1"
}

usage="Uso: create-user.sh [--jump <lb>] <vm_name> <username> <sudo yes|no> <ssh_key yes|no> [password_file]"
jump_lb=""
if [[ "${1-}" == --jump ]]; then
  if [[ ! "${2-}" =~ ^[a-z0-9][a-z0-9-]{0,31}$ ]]; then
    fail 2 '--jump inválido: debe cumplir ^[a-z0-9][a-z0-9-]{0,31}$'
  fi
  jump_lb="$2"
  shift 2
fi

if (($# < 4)); then
  fail 2 "$usage"
fi
vm_name="$1"
username="$2"
sudo_opt="$3"
ssh_key_opt="$4"

if [[ "$ssh_key_opt" == no ]]; then
  (($# == 5)) || fail 2 "$usage"
  password_file="$5"
else
  (($# == 4)) || fail 2 "$usage"
  password_file=""
fi

if [[ ! "$vm_name" =~ ^[a-z0-9][a-z0-9-]{0,31}$ ]]; then
  fail 2 'vm_name inválido: debe cumplir ^[a-z0-9][a-z0-9-]{0,31}$'
fi
if [[ ! "$username" =~ ^[a-z][a-z0-9_-]{0,31}$ ]]; then
  fail 2 'username inválido: debe cumplir ^[a-z][a-z0-9_-]{0,31}$'
fi
if [[ "$sudo_opt" != yes && "$sudo_opt" != no ]]; then
  fail 2 "sudo inválido: debe ser yes o no"
fi
if [[ "$ssh_key_opt" != yes && "$ssh_key_opt" != no ]]; then
  fail 2 "ssh_key inválido: debe ser yes o no"
fi
if [[ -z "${APP_SSH_KEY_PATH:-}" ]]; then
  fail 2 "Falta la variable de entorno APP_SSH_KEY_PATH"
fi

password=""
if [[ "$ssh_key_opt" == no ]]; then
  if [[ ! -f "$password_file" || ! -r "$password_file" ]]; then
    fail 2 "El archivo de contraseña no existe o no se puede leer"
  fi
  # Se lee solo la primera línea y se exige que no haya más contenido.
  extra=""
  {
    IFS= read -r password || true
    IFS= read -r extra || true
  } <"$password_file"
  # Con LC_ALL=C el rango [!-~] son exactamente los ASCII imprimibles 0x21-0x7E.
  export LC_ALL=C
  password_re='^[!-~][ -~]{6,70}[!-~]$'
  if [[ -n "$extra" || ! "$password" =~ $password_re ]]; then
    fail 2 "Contraseña inválida: debe tener de 8 a 72 caracteres ASCII imprimibles, sin espacio al inicio ni al final"
  fi
fi

# == 2. Preparar la ejecución en la VM ==
# 2.1 Cargar la biblioteca ../prepare/lib/vm-exec.sh (vm_ip y vm_exec).
# 2.2 Comprobar que la VM exista con "VBoxManage showvminfo <vm_name>"; si falla, salir con 3.
# 2.3 Comprobar con "vm_ip" que la VM tenga IPv4; si no, salir con 7 (vm_ip ya imprimió el motivo).
# 2.4 Cada comando en la VM va por "guest <código_de_fallo> <comando...>", que sale con 2, 4 o 7
#     tal como los devuelve vm_exec y con <código_de_fallo> si el comando termina con error.

# shellcheck source=../prepare/lib/vm-exec.sh
source "$(dirname "${BASH_SOURCE[0]}")/../prepare/lib/vm-exec.sh"
# shellcheck source=../haproxy/lib/jump-keys.sh
source "$(dirname "${BASH_SOURCE[0]}")/../haproxy/lib/jump-keys.sh"

# Con --jump, registrar la ruta antes de cualquier vm_ip o vm_exec: la VM se alcanza por el balanceador.
[[ -z "$jump_lb" ]] || vm_route "$vm_name" "$jump_lb"

if ! VBoxManage showvminfo "$vm_name" >/dev/null 2>&1; then
  fail 3 "VM no encontrada en VirtualBox"
fi

vm_ip "$vm_name" >/dev/null || exit $?

guest() {
  local fail_code="$1" rc=0
  shift
  vm_exec "$vm_name" "$@" || rc=$?
  case "$rc" in
    0) ;;
    2 | 4 | 7 | 12) exit "$rc" ;;
    *)
      # Solo se nombra el programa (nunca sus argumentos) para no imprimir datos del usuario.
      echo "Falló el paso $1 en la VM (código $rc)" >&2
      exit "$fail_code"
      ;;
  esac
}

# == 3. Comprobar que el nombre esté libre (REQ-4) ==
# 3.1 Un solo comando remoto responde "user" si getent passwd conoce el nombre (cualquier UID),
#     "group" si solo existe un grupo con ese nombre, o "free". El nombre llega como argumento.
# 3.2 "user" sale con 6 y "group" con 8; nada ha cambiado en la VM.

lookup="$(guest 5 sh -c 'if getent passwd "$1" >/dev/null; then echo user; elif getent group "$1" >/dev/null; then echo group; else echo free; fi' sh "$username")" || exit $?
case "$lookup" in
  free) ;;
  user) fail 6 "El usuario ya existe en la VM" ;;
  group) fail 8 "Ya existe un grupo con ese nombre en la VM" ;;
  *) fail 5 "Respuesta inesperada al buscar el usuario en la VM" ;;
esac

# == 4. Preparar sudo y la llave (REQ-6, REQ-8) ==
# 4.1 Solo con sudo=yes: instalar sudo si falta con
#     "command -v sudo || DEBIAN_FRONTEND=noninteractive apt-get install -y sudo". Es un respaldo,
#     porque el disco base ya trae sudo. Si falla, salir con 5: aún no se creó nada.
# 4.2 Solo con ssh_key=yes: crear un directorio temporal en el host (umask 077) que se borra al
#     salir, y generar ahí el par ed25519 sin passphrase con el comentario stratus-<vm>-<usuario>.
#     La salida de ssh-keygen va a stderr. Si falla, salir con 5.

tmp_dir=""
account_created=no

# on_exit corre al terminar el script, por cualquier camino (éxito, error o SIGTERM, que sale con
# 143). Siempre borra el directorio temporal con la llave privada. Si ya se creó la cuenta y el
# script termina con error, deshace la creación (REQ-12):
#   1. "userdel -r <u>": quita la cuenta, el home y el buzón de correo.
#   2. "rm -f" de la regla sudo definitiva y de la temporal.
#   3. "groupdel <u>" si quedó el grupo.
#   4. Verificación: no queda la cuenta, ni /home/<u>, ni la regla sudo.
# Todos los pasos se intentan aunque uno falle y su salida va a stderr. Si todo salió bien se
# conserva el código original; si algo falló, se imprime qué puede quedar y se sale con 9.
on_exit() {
  local rc=$?
  trap - EXIT
  rm -rf "$tmp_dir"
  if ((rc == 0)) || [[ "$account_created" != yes ]]; then
    exit "$rc"
  fi
  local cleanup_failed=no
  vm_exec "$vm_name" userdel -r "$username" >&2 || cleanup_failed=yes
  vm_exec "$vm_name" rm -f "/etc/sudoers.d/stratus-$username" "/etc/sudoers.d/.stratus-$username" >&2 ||
    cleanup_failed=yes
  vm_exec "$vm_name" sh -c '! getent group "$1" >/dev/null || groupdel "$1"' sh "$username" >&2 ||
    cleanup_failed=yes
  vm_exec "$vm_name" sh -c '! getent passwd "$1" >/dev/null && [ ! -e "/home/$1" ] && [ ! -e "/etc/sudoers.d/stratus-$1" ]' sh "$username" >&2 ||
    cleanup_failed=yes
  if [[ "$cleanup_failed" == yes ]]; then
    echo "Limpieza fallida; puede quedar en la VM: cuenta $username, /home/$username, /etc/sudoers.d/stratus-$username" >&2
    exit 9
  fi
  exit "$rc"
}
# La llave privada solo existe en tmp_dir y en la salida de este script. SIGTERM (el límite de
# tiempo del backend) e SIGINT salen con 143 y 130 para pasar por on_exit y limpiar.
trap on_exit EXIT
trap 'exit 143' TERM
trap 'exit 130' INT

if [[ "$sudo_opt" == yes ]]; then
  guest 5 sh -c 'command -v sudo >/dev/null || DEBIAN_FRONTEND=noninteractive apt-get install -y sudo'
fi

if [[ "$ssh_key_opt" == yes ]]; then
  umask 077
  tmp_dir="$(mktemp -d)"
  ssh-keygen -q -t ed25519 -N "" -C "stratus-$vm_name-$username" -f "$tmp_dir/id_ed25519" >&2 ||
    fail 5 "No se pudo generar el par de llaves"
  public_key="$(<"$tmp_dir/id_ed25519.pub")"
fi

# == 5. Crear la cuenta (REQ-5) ==
# 5.1 "useradd -m -s /bin/bash -U <username>": home /home/<username>, shell bash y un grupo
#     primario con el mismo nombre. Si falla, salir con 5.
# 5.2 Desde aquí cualquier fallo pasa por on_exit, que deshace la creación (REQ-12).

guest 5 useradd -m -s /bin/bash -U "$username"
account_created=yes

# == 6. Regla sudo (REQ-6) ==
# 6.1 Solo con sudo=yes: escribir la regla en /etc/sudoers.d/.stratus-<username> (el punto hace
#     que sudo la ignore mientras se valida), darle modo 440, validarla con "visudo -cf" y solo
#     entonces renombrarla a /etc/sudoers.d/stratus-<username>. Si falla, salir con 5.

if [[ "$sudo_opt" == yes ]]; then
  guest 5 sh -c 'f=/etc/sudoers.d/stratus-$1; t=/etc/sudoers.d/.stratus-$1; printf "%s ALL=(ALL:ALL) NOPASSWD: ALL\n" "$1" >"$t" && chmod 440 "$t" && visudo -cf "$t" && mv "$t" "$f"' sh "$username"
fi

# == 7. Acceso del usuario (REQ-7, REQ-8) ==
# 7.1 Con ssh_key=no: "usuario:contraseña" llega a chpasswd por la entrada estándar de ssh. printf
#     es un builtin de bash, así que la contraseña no aparece en el argv de ningún proceso.
# 7.2 Con ssh_key=yes: crear ~/.ssh (700), instalar la llave pública en authorized_keys (dueño el
#     usuario, modo 600) y bloquear la contraseña con "passwd -l". La llave pública va como
#     argumento del comando remoto, no dentro de su texto.
# 7.3 Con ssh_key=yes y --jump, copiar la llave a jump con jump_sync (la lee del servidor, donde ya
#     está instalada); si falla, salir con 15 y la limpieza deshace la cuenta.
# 7.4 Si algo falla, salir con 5.

if [[ "$ssh_key_opt" == no ]]; then
  printf '%s:%s\n' "$username" "$password" | guest 5 chpasswd
else
  guest 5 install -d -m 700 -o "$username" -g "$username" "/home/$username/.ssh"
  guest 5 sh -c 'printf "%s\n" "$2" >"/home/$1/.ssh/authorized_keys" && chown "$1:$1" "/home/$1/.ssh/authorized_keys" && chmod 600 "/home/$1/.ssh/authorized_keys"' sh "$username" "$public_key"
  guest 5 passwd -l "$username"
  # Con --jump, la llave nueva también debe poder entrar por jump (REQ-17). Si jump no se actualiza,
  # sale con 15 y on_exit deshace la cuenta (account_created=yes).
  if [[ -n "$jump_lb" ]]; then
    jump_sync "$jump_lb" "$vm_name" >&2 || fail 15 "No se pudo actualizar la cuenta jump de $jump_lb; se deshizo la creación de $username"
  fi
fi

# == 8. Resultado ==
# 8.1 Con ssh_key=yes imprimir solo la llave privada en stdout; con ssh_key=no imprimir
#     "Usuario <username> creado en <vm_name>".
# 8.2 Solo cuando la salida ya se entregó se desactiva la limpieza: si no se pudo imprimir, la
#     cuenta se deshace porque nadie podría usarla.

if [[ "$ssh_key_opt" == yes ]]; then
  cat "$tmp_dir/id_ed25519"
else
  echo "Usuario $username creado en $vm_name"
fi
account_created=no

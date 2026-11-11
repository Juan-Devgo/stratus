#!/usr/bin/env bash
# create-web.sh - Crea un servidor web Apache o Nginx (005): una VM como las de 001 (create-vm.sh)
# sobre el disco base del tipo, con un único sitio en el puerto 80 de nombre <vm>.<dominio> y raíz
# /var/www/html, y el servicio habilitado y activo. Si un paso falla, borra la VM y su disco
# diferencial; el disco base nunca se modifica.
#
# Uso:
#   create-web.sh <vm> <memory_mb> <cpus> <apache|nginx> <base_disk> <bridge_adapter> <domain>
#
# Argumentos:
#   vm              nombre del servidor web (y de su VM), ^[a-z0-9][a-z0-9-]{0,31}$
#   memory_mb       memoria en MB, entero entre 512 y 8192
#   cpus            número de CPUs, entero entre 1 y 4
#   apache|nginx    tipo de servidor web
#   base_disk       ruta absoluta del disco base del tipo (APP_APACHE_BASE_DISK o APP_NGINX_BASE_DISK)
#   bridge_adapter  interfaz del host para la red en puente (APP_BRIDGE_ADAPTER), ^[A-Za-z0-9._:-]{1,64}$
#   domain          zona DNS (APP_DNS_DOMAIN): 1-4 etiquetas ^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$
#
# Variables de entorno:
#   APP_GUEST_PASSWORD_FILE  archivo del host con la contraseña de root de la imagen base (lo lee
#                            create-vm.sh; nunca va en argv)
#   APP_SSH_KEY_PATH         llave privada del backend
#
# Salida:
#   stdout  "Servidor web <vm> (<Apache|Nginx>) creado; IP <ipv4>" y, en la última línea, "ip=<ipv4>"
#   stderr  diagnósticos
#
# Códigos de salida:
#   0   éxito
#   2   argumentos inválidos o variables de entorno faltantes
#   4   fallo de VBoxManage o de SSH
#   5   falló un paso (la VM ya se borró); también el código de create-vm.sh
#   6   disco base no encontrado, interfaz de puente no encontrada o VM ya registrada
#   7   la VM quedó sin dirección IP (la VM ya se borró)
#   9   falló un paso y también la limpieza: puede quedar la VM
#   99  operación no implementada
#
# Tiempo máximo: create-vm.sh puede tardar hasta 14 minutos; si el backend lo detiene por tiempo
# recibe SIGTERM y la limpieza de la sección 3 se ejecuta igual.

set -euo pipefail

scripts_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=../prepare/lib/vm-exec.sh
source "$scripts_dir/prepare/lib/vm-exec.sh"
# shellcheck source=lib/web-common.sh
source "$scripts_dir/web/lib/web-common.sh"

# == 1. Validar argumentos ==
# 1.1 Exigir exactamente 7 argumentos; si no, imprimir el uso en stderr y salir con 2.
# 1.2 Validar cada valor contra su lista permitida (web-common.sh); si alguno falla, salir con 2.
# 1.3 Exigir que APP_GUEST_PASSWORD_FILE sea un archivo legible y APP_SSH_KEY_PATH; si no, salir con 2.
# Ninguna validación fallida debe llamar a VBoxManage, a ssh ni a create-vm.sh.

if (($# != 7)); then
  fail 2 "Uso: create-web.sh <vm> <memory_mb> <cpus> <apache|nginx> <base_disk> <bridge_adapter> <domain>"
fi
vm="$1"
memory_mb="$2"
cpus="$3"
type="$4"
base_disk="$5"
bridge_adapter="$6"
domain="$7"

validate_vm_name "$vm" || exit 2
validate_memory "$memory_mb" || exit 2
validate_cpus "$cpus" || exit 2
validate_type "$type" || exit 2
validate_base_disk "$base_disk" || exit 2
validate_bridge "$bridge_adapter" || exit 2
validate_domain "$domain" || exit 2

if [[ -z "${APP_GUEST_PASSWORD_FILE:-}" || ! -f "$APP_GUEST_PASSWORD_FILE" || ! -r "$APP_GUEST_PASSWORD_FILE" ]]; then
  fail 2 "APP_GUEST_PASSWORD_FILE debe ser un archivo legible"
fi
if [[ -z "${APP_SSH_KEY_PATH:-}" ]]; then
  fail 2 "Falta la variable de entorno APP_SSH_KEY_PATH"
fi

unit="$(type_unit "$type")"
if [[ "$type" == apache ]]; then
  label=Apache
else
  label=Nginx
fi

# == 2. Comprobaciones antes de crear nada ==
# 2.1 Si base_disk no es un archivo legible, imprimir "Disco base de <Apache|Nginx> no encontrado" y
#     salir con 6 (REQ-3).
# 2.2 Si "VBoxManage list vms" ya tiene una VM <vm>, imprimir "VM ya registrada en VirtualBox" y
#     salir con 6: la limpieza borra la VM <vm>, así que nunca puede empezar con una VM ajena.

if [[ ! -f "$base_disk" || ! -r "$base_disk" ]]; then
  fail 6 "Disco base de $label no encontrado"
fi

registered="$(VBoxManage list vms)" || fail 4 "VBoxManage list vms falló"
if sed -n 's/^"\(.*\)" {[^}]*}$/\1/p' <<<"$registered" | grep -qxF -- "$vm"; then
  fail 6 "VM ya registrada en VirtualBox"
fi

# == 3. Limpieza en caso de fallo ==
# Se ejecuta al salir con un código distinto de cero, una vez lanzado create-vm.sh. Conserva el
# código original; si la propia limpieza falla, sale con 9.
# 3.1 "../prepare/delete-vm.sh <vm>" (apaga, suelta y borra el disco diferencial y desregistra la
#     VM; el disco base no se toca; es idempotente).

cleanup() {
  local status=$?
  trap - EXIT INT TERM
  if ((status == 0)); then
    return 0
  fi
  echo "Falló la creación de $vm; limpiando" >&2
  if ! "$scripts_dir/prepare/delete-vm.sh" "$vm" >&2; then
    echo "La limpieza falló; revise VirtualBox: puede quedar la VM $vm" >&2
    exit 9
  fi
  exit "$status"
}

# == 4. Crear la VM ==
# 4.1 Desde aquí, instalar el "trap" de la sección 3.
# 4.2 "../prepare/create-vm.sh <vm> <memory_mb> <cpus> <base_disk> <bridge_adapter>": red en puente
#     con DHCP y el bootstrap de 001 (llave del backend). Cualquier fallo conserva su código.

trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

"$scripts_dir/prepare/create-vm.sh" "$vm" "$memory_mb" "$cpus" "$base_disk" "$bridge_adapter" >&2

# == 5. Configurar el sitio por SSH (REQ-4) ==
# Cada paso usa vm_exec como root con la llave del backend; si falla, sale con 5 y la sección 3 limpia.
# 5.1 Escribir el sitio de render_site <tipo> <vm>.<dominio>:
#     - nginx: /etc/nginx/sites-available/stratus, enlazado en sites-enabled, sin sites-enabled/default;
#     - apache: /etc/apache2/sites-available/000-default.conf (habilitado de fábrica).
# 5.2 Revisar la configuración: "nginx -t" o "apache2ctl configtest".
# 5.3 "systemctl enable <unidad>", "systemctl reload-or-restart <unidad>" y "systemctl is-active
#     <unidad>" debe ser active.

# webx <comando...>: vm_exec en el servidor web; 2, 4 y 7 salen tal cual y otro fallo sale con 5.
webx() {
  local rc=0
  vm_exec "$vm" "$@" || rc=$?
  case "$rc" in
    0) ;;
    2 | 4 | 7) exit "$rc" ;;
    *) fail 5 "Falló un paso en el servidor web $vm (código $rc): $1" ;;
  esac
}

if [[ "$type" == nginx ]]; then
  render_site nginx "$vm.$domain" | webx sh -c 'cat >"$1"' sh /etc/nginx/sites-available/stratus
  webx ln -sf /etc/nginx/sites-available/stratus /etc/nginx/sites-enabled/stratus </dev/null
  webx rm -f /etc/nginx/sites-enabled/default </dev/null
  webx nginx -t </dev/null >&2
else
  render_site apache "$vm.$domain" | webx sh -c 'cat >"$1"' sh /etc/apache2/sites-available/000-default.conf
  webx apache2ctl configtest </dev/null >&2
fi
webx systemctl enable "$unit" </dev/null >&2
webx systemctl reload-or-restart "$unit" </dev/null >&2
webx systemctl is-active "$unit" </dev/null >/dev/null

# == 6. Resultado ==
# 6.1 Leer la IPv4 con vm_ip e imprimir el mensaje y, en la última línea, "ip=<ipv4>" (el backend
#     la usa para el registro DNS, REQ-5).

ip="$(vm_ip "$vm")" || exit $?
echo "Servidor web $vm ($label) creado; IP $ip"
echo "ip=$ip"

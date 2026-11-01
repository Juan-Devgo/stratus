#!/usr/bin/env bash
# web-common.sh - Biblioteca compartida de scripts/web (005): validadores (P3: la misma lista
# permitida que la UI y el backend), la unidad de cada tipo, el sitio de Apache o Nginx y la
# opción "--jump".
#
# Uso: desde un script de scripts/web
#   source "$(dirname "${BASH_SOURCE[0]}")/lib/web-common.sh"
#
# Argumentos: los de cada función
#   fail <código> <mensaje>              imprime el mensaje en stderr y sale con el código
#   validate_vm_name <vm>                ^[a-z0-9][a-z0-9-]{0,31}$
#   validate_memory <mb>                 512-8192
#   validate_cpus <n>                    1-4
#   validate_type <tipo>                 apache o nginx
#   validate_unit <unidad>               apache2 o nginx
#   validate_base_disk <ruta>            ruta absoluta
#   validate_bridge <interfaz>           ^[A-Za-z0-9._:-]{1,64}$
#   validate_domain <dominio>            1-4 etiquetas ^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$ con puntos
#   type_unit <tipo>                     imprime apache2 o nginx
#   render_site <tipo> <fqdn>            imprime el sitio de Apache o Nginx (REQ-4)
#   parse_jump "$@"                      lee "--jump <lb>" si va primero: deja jump_lb y jump_shift
#
# Salida:
#   stdout  type_unit y render_site
#   stderr  el motivo de un valor inválido; nunca repite el valor
#
# Códigos de salida: valor de retorno de los validadores; fail y parse_jump salen del script
#   0   éxito
#   1   valor inválido (el script que llama sale con 2)
#   2   argumentos inválidos (parse_jump)
#   99  operación no implementada
#
# Se carga con "source" desde scripts que ya están en modo estricto; repetirlo aquí no cambia sus
# opciones.
set -euo pipefail

# == 1. fail <código> <mensaje> ==
# 1.1 Imprimir el mensaje en stderr y salir con el código.

fail() {
  echo "$2" >&2
  exit "$1"
}

# == 2. Validadores ==
# Cada uno devuelve 0 si el valor es válido, o 1 con el motivo en stderr.

# 2.1 Nombre de VM o de balanceador (001 REQ-1).
validate_vm_name() {
  if [[ ! "${1-}" =~ ^[a-z0-9][a-z0-9-]{0,31}$ ]]; then
    echo 'vm inválido: debe cumplir ^[a-z0-9][a-z0-9-]{0,31}$' >&2
    return 1
  fi
}

# 2.2 memory_mb 512-8192 y cpus 1-4 (igual que 001); solo dígitos antes de comparar.
validate_memory() {
  if [[ ! "${1-}" =~ ^[0-9]{1,9}$ ]] || ((10#$1 < 512 || 10#$1 > 8192)); then
    echo "memory_mb inválido: entero entre 512 y 8192" >&2
    return 1
  fi
}

validate_cpus() {
  if [[ ! "${1-}" =~ ^[0-9]{1,9}$ ]] || ((10#$1 < 1 || 10#$1 > 4)); then
    echo "cpus inválido: entero entre 1 y 4" >&2
    return 1
  fi
}

# 2.3 Tipo de servidor web (REQ-1) y su unidad systemd (REQ-12).
validate_type() {
  if [[ "${1-}" != apache && "${1-}" != nginx ]]; then
    echo "Seleccione el tipo de servidor web" >&2
    return 1
  fi
}

validate_unit() {
  if [[ "${1-}" != apache2 && "${1-}" != nginx ]]; then
    echo "unidad inválida: debe ser apache2 o nginx" >&2
    return 1
  fi
}

# 2.4 base_disk: ruta absoluta sin caracteres de control; bridge_adapter: nombre de interfaz.
validate_base_disk() {
  if [[ ! "${1-}" =~ ^/[^[:cntrl:]]*$ ]]; then
    echo "base_disk inválido: debe ser una ruta absoluta" >&2
    return 1
  fi
}

validate_bridge() {
  if [[ ! "${1-}" =~ ^[A-Za-z0-9._:-]{1,64}$ ]]; then
    echo 'bridge_adapter inválido: debe cumplir ^[A-Za-z0-9._:-]{1,64}$' >&2
    return 1
  fi
}

# 2.5 Dominio: 1 a 4 etiquetas separadas por puntos (REQ-15).
validate_domain() {
  local domain="${1-}" label
  local -a labels
  IFS=. read -ra labels <<<"$domain"
  # Un punto al final o dos seguidos dejan una etiqueta vacía que read no cuenta.
  if [[ -z "$domain" || "$domain" == *. || "$domain" == *..* || ${#labels[@]} -gt 4 ]]; then
    echo "dominio inválido: de 1 a 4 etiquetas separadas por puntos" >&2
    return 1
  fi
  for label in "${labels[@]}"; do
    if [[ ! "$label" =~ ^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$ ]]; then
      echo 'dominio inválido: cada etiqueta debe cumplir ^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$' >&2
      return 1
    fi
  done
}

# == 3. type_unit <tipo> ==
# 3.1 apache usa la unidad apache2; nginx usa nginx.

type_unit() {
  case "$1" in
    apache) echo apache2 ;;
    nginx) echo nginx ;;
  esac
}

# == 4. render_site <tipo> <fqdn> ==
# 4.1 Un único sitio en el puerto 80 para toda petición, con nombre <fqdn> y raíz /var/www/html
#     (REQ-4). Sin HTTPS. El fqdn ya viene validado (nombre de VM y dominio).
# 4.2 nginx: /etc/nginx/sites-available/stratus, servidor por defecto.
# 4.3 apache: /etc/apache2/sites-available/000-default.conf.

render_site() {
  local type="$1" fqdn="$2"
  if [[ "$type" == nginx ]]; then
    printf 'server {\n'
    printf '  listen 80 default_server;\n'
    printf '  listen [::]:80 default_server;\n'
    printf '  server_name %s;\n' "$fqdn"
    printf '  root /var/www/html;\n'
    printf '  index index.html index.nginx-debian.html;\n'
    printf '  location / { try_files $uri $uri/ =404; }\n'
    printf '}\n'
  else
    printf '<VirtualHost *:80>\n'
    printf '  ServerName %s\n' "$fqdn"
    printf '  DocumentRoot /var/www/html\n'
    printf '</VirtualHost>\n'
  fi
}

# == 5. parse_jump "$@" (004 REQ-19) ==
# 5.1 Si el primer argumento es "--jump", el segundo debe cumplir ^[a-z0-9][a-z0-9-]{0,31}$; si
#     falta o no cumple, salir con 2. Se guarda en jump_lb y jump_shift queda en 2.
# 5.2 Sin "--jump", jump_lb queda vacío y jump_shift en 0. El backend lo agrega solo para un
#     servidor asignado a un balanceador; el script nunca deduce el rol.

parse_jump() {
  jump_lb=""
  jump_shift=0
  if [[ "${1-}" == --jump ]]; then
    if [[ ! "${2-}" =~ ^[a-z0-9][a-z0-9-]{0,31}$ ]]; then
      fail 2 '--jump inválido: debe cumplir ^[a-z0-9][a-z0-9-]{0,31}$'
    fi
    jump_lb="$2"
    jump_shift=2
  fi
}

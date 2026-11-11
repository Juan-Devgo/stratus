#!/usr/bin/env bash
# vm-exec.sh - Biblioteca compartida: resuelve la IPv4 de una VM y ejecuta comandos como root en
# ella por SSH con la llave del backend. Guest Additions ya no se usa aquí: solo create-vm.sh
# lo usa, una vez, al crear la VM.
#
# Los servidores asignados a un balanceador (004) no se alcanzan directo: el script que recibe
# "--jump <lb>" registra la ruta con vm_route y desde ahí vm_ip y vm_exec entran por la cuenta
# "jump" del balanceador, con la misma llave del backend.
#
# Uso (desde otro script de scripts/prepare, que ya tiene "set -euo pipefail"):
#   source "$(dirname "${BASH_SOURCE[0]}")/lib/vm-exec.sh"
#   vm_route <vm_name> <lb_name>
#   vm_nic_ip <vm_name> <nic>
#   vm_ip <vm_name>
#   vm_exec <vm_name> <comando> [argumentos...]
#   vm_scp <vm_name> <local> <remoto>
#
# Argumentos:
#   vm_name  nombre de la VM registrada en VirtualBox
#   lb_name  nombre del balanceador por el que se llega a la VM
#   nic      número del adaptador de VirtualBox, 1 a 4
#   comando  programa a ejecutar en la VM; cada argumento va como elemento separado
#   local    archivo del host que se copia (005)
#   remoto   ruta de destino en la VM
#
# Variables de entorno:
#   APP_SSH_KEY_PATH  llave privada del backend; las llaves de host fijadas van en
#                     "<ruta>.known_hosts"
#
# Variables globales:
#   VM_ROUTES  arreglo asociativo vm -> balanceador; lo llena vm_route
#
# Salida:
#   vm_nic_ip, vm_ip  stdout: la IPv4
#   vm_exec           stdout: la salida del comando remoto
#   vm_scp            nada en stdout
#   stderr            diagnósticos
#
# Códigos de salida (valor de retorno de las funciones):
#   0   éxito
#   2   argumentos inválidos o APP_SSH_KEY_PATH faltante
#   4   fallo de comunicación con la VM (SSH terminó con 255, o scp falló)
#   5   el comando remoto terminó con código distinto de cero
#   7   la VM no tiene dirección IP (apagada, sin Guest Additions o sin IPv4)
#   12  la VM tiene ruta y su balanceador está apagado o sin IPv4 en puente
#   99  operación no implementada
#
# Este archivo se carga con "source": no activa "set -euo pipefail" porque hereda el
# modo estricto del script que lo carga y no debe cambiar las opciones de quien lo use.

# Rutas registradas: vm -> balanceador. "-g" para que exista aunque el archivo se cargue dentro de
# una función. Es global del proceso: no se hereda a los scripts hijos, que reciben "--jump".
declare -gA VM_ROUTES
# Opciones de salto de la última llamada a _proxy_opts.
declare -ga _VM_PROXY

# Mensaje de VM sin IP, el mismo de 001 REQ-29.
_NO_IP_MSG="La VM no tiene dirección IP; verifique que esté encendida"

# == 1. vm_route <vm_name> <lb_name> ==
# 1.1 Exigir 2 argumentos distintos entre sí; si no, devolver 2.
# 1.2 Registrar VM_ROUTES[<vm_name>]=<lb_name>. Desde ahí vm_ip y vm_exec de esa VM pasan por el
#     balanceador. Las demás VMs, el balanceador incluido, siguen directas.

vm_route() {
  if (($# != 2)) || [[ "$1" == "$2" ]]; then
    echo "Uso: vm_route <vm_name> <lb_name> (distintos entre sí)" >&2
    return 2
  fi
  VM_ROUTES["$1"]="$2"
}

# == 2. vm_nic_ip <vm_name> <nic> ==
# 2.1 Exigir 2 argumentos y nic entre 1 y 4; si no, devolver 2.
# 2.2 Leer "VBoxManage showvminfo <vm_name> --machinereadable": si VMState no es "running",
#     imprimir el mensaje de VM sin IP en stderr y devolver 7.
# 2.3 Una VM de una sola NIC (nic2 ausente o "none") conserva la lectura de siempre: la IPv4 de
#     "/VirtualBox/GuestInfo/Net/0/V4/IP", solo para nic 1.
# 2.4 Con dos NICs, Guest Additions numera los adaptadores en el orden del invitado, que no tiene
#     por qué coincidir con el de VirtualBox. Se toma "macaddress<nic>" de showvminfo, se busca la
#     entrada /Net/<i>/MAC igual (sin importar mayúsculas ni ":") y se lee /Net/<i>/V4/IP.
# 2.5 Si no hay una IPv4, imprimir el mismo mensaje y devolver 7. La IP viene de DHCP y puede
#     cambiar entre arranques: nunca se guarda, cada llamada la resuelve de nuevo.

# _guest_prop <vm_name> <ruta>: valor de /VirtualBox/GuestInfo<ruta>, o vacío si no existe.
_guest_prop() {
  VBoxManage guestproperty get "$1" "/VirtualBox/GuestInfo$2" 2>/dev/null </dev/null |
    sed -n 's/^Value: //p' || true
}

# _norm_mac <mac>: la MAC en mayúsculas y sin ":" para comparar.
_norm_mac() {
  local mac="${1^^}"
  echo "${mac//:/}"
}

vm_nic_ip() {
  if (($# != 2)) || [[ ! "$2" =~ ^[1-4]$ ]]; then
    echo "Uso: vm_nic_ip <vm_name> <nic 1-4>" >&2
    return 2
  fi
  local vm_name="$1" nic="$2" info state nic2 want_mac i value=""
  info="$(VBoxManage showvminfo "$vm_name" --machinereadable 2>/dev/null </dev/null)" || info=""
  state="$(sed -n 's/^VMState="\(.*\)"$/\1/p' <<<"$info")"
  if [[ "$state" == running ]]; then
    nic2="$(sed -n 's/^nic2="\(.*\)"$/\1/p' <<<"$info")"
    if [[ -z "$nic2" || "$nic2" == none ]]; then
      # Una sola NIC: Net/0 es el adaptador 1.
      if [[ "$nic" == 1 ]]; then
        value="$(_guest_prop "$vm_name" /Net/0/V4/IP)"
      fi
    else
      want_mac="$(_norm_mac "$(sed -n "s/^macaddress${nic}=\"\(.*\)\"\$/\1/p" <<<"$info")")"
      if [[ -n "$want_mac" ]]; then
        # Una VM de este proyecto tiene como máximo 2 NICs; se revisan 4 entradas por holgura.
        for i in 0 1 2 3; do
          if [[ "$(_norm_mac "$(_guest_prop "$vm_name" "/Net/$i/MAC")")" == "$want_mac" ]]; then
            value="$(_guest_prop "$vm_name" "/Net/$i/V4/IP")"
            break
          fi
        done
      fi
    fi
  fi
  if [[ "$value" =~ ^[0-9]{1,3}(\.[0-9]{1,3}){3}$ ]]; then
    echo "$value"
    return 0
  fi
  echo "$_NO_IP_MSG" >&2
  return 7
}

# == 3. vm_ip <vm_name> ==
# 3.1 Exigir exactamente 1 argumento; si no, imprimir el uso en stderr y devolver 2.
# 3.2 Sin ruta: imprimir la IPv4 del adaptador 1 (vm_nic_ip <vm_name> 1) o devolver 7.
# 3.3 Con ruta (vm_route): primero resolver el adaptador 1 del balanceador; si falla, imprimir
#     "El balanceador <lb> está apagado; enciéndalo para llegar al servidor" y devolver 12 sin
#     seguir. Luego resolver el adaptador 1 del servidor (su IP interna); si falla, devolver 7.

# _lb_ip <lb_name>: IPv4 en puente del balanceador, o el mensaje de balanceador apagado y 12.
_lb_ip() {
  local lb_ip
  lb_ip="$(vm_nic_ip "$1" 1 2>/dev/null)" || {
    echo "El balanceador $1 está apagado; enciéndalo para llegar al servidor" >&2
    return 12
  }
  echo "$lb_ip"
}

vm_ip() {
  if (($# != 1)); then
    echo "Uso: vm_ip <vm_name>" >&2
    return 2
  fi
  local vm_name="$1"
  if [[ -n "${VM_ROUTES[$vm_name]:-}" ]]; then
    _lb_ip "${VM_ROUTES[$vm_name]}" >/dev/null || return $?
  fi
  vm_nic_ip "$vm_name" 1
}

# == 4. vm_exec <vm_name> <comando> [argumentos...] ==
# 4.1 Exigir al menos 2 argumentos y que APP_SSH_KEY_PATH exista; si no, devolver 2.
# 4.2 Resolver la IP con vm_ip en cada llamada; si falla (7 o 12), devolver ese código sin ejecutar ssh.
# 4.3 Ejecutar "ssh -i <APP_SSH_KEY_PATH> -o BatchMode=yes -o ConnectTimeout=10
#     -o StrictHostKeyChecking=accept-new -o UserKnownHostsFile=<APP_SSH_KEY_PATH>.known_hosts
#     -o HostKeyAlias=<vm_name> root@<ip> -- <comando> <argumentos...>". HostKeyAlias fija la
#     llave de host por nombre de VM, no por IP, porque la IP puede cambiar. Cada argumento se cita
#     con "printf '%q '" porque ssh une los argumentos en una sola línea para el shell remoto.
# 4.4 Con ruta, además agrega -o ProxyCommand="ssh -i <llave> -o BatchMode=yes -o ConnectTimeout=10
#     -o StrictHostKeyChecking=accept-new -o UserKnownHostsFile=<llave>.known_hosts
#     -o HostKeyAlias=<lb> -W %h:%p jump@<IP del balanceador>". Es lo que haría "ssh -J", pero "-J"
#     no pasa -i ni HostKeyAlias al salto.
# 4.5 Si ssh termina con 255 (no hubo conexión o autenticación), devolver 4.
# 4.6 Reenviar stdout y stderr del comando remoto sin modificarlos; si terminó con código distinto
#     de cero, devolver 5; si no, 0.

# _require_key: devuelve 2 si APP_SSH_KEY_PATH falta o no existe.
_require_key() {
  if [[ -z "${APP_SSH_KEY_PATH:-}" || ! -e "$APP_SSH_KEY_PATH" ]]; then
    echo "No se encontró la llave del backend (APP_SSH_KEY_PATH)" >&2
    return 2
  fi
}

# _proxy_opts <vm_name>: deja en _VM_PROXY las opciones "-o ProxyCommand=..." de una VM con ruta
# (paso 4.4), o vacío si es directa. Devuelve 12 si el balanceador está apagado.
_proxy_opts() {
  _VM_PROXY=()
  [[ -n "${VM_ROUTES[$1]:-}" ]] || return 0
  local lb="${VM_ROUTES[$1]}" lb_ip key
  lb_ip="$(_lb_ip "$lb")" || return $?
  # El comando del salto lo ejecuta un shell local: la ruta de la llave va citada.
  key="$(printf '%q' "$APP_SSH_KEY_PATH")"
  _VM_PROXY=(-o "ProxyCommand=ssh -i $key -o BatchMode=yes -o ConnectTimeout=10 -o StrictHostKeyChecking=accept-new -o UserKnownHostsFile=$key.known_hosts -o HostKeyAlias=$lb -W %h:%p jump@$lb_ip")
}

vm_exec() {
  if (($# < 2)); then
    echo "Uso: vm_exec <vm_name> <comando> [argumentos...]" >&2
    return 2
  fi
  _require_key || return $?
  local vm_name="$1" ip rc=0 remote
  shift
  ip="$(vm_ip "$vm_name")" || return $?
  remote="$(printf '%q ' "$@")"
  _proxy_opts "$vm_name" || return $?
  ssh -i "$APP_SSH_KEY_PATH" -o BatchMode=yes -o ConnectTimeout=10 \
    -o StrictHostKeyChecking=accept-new -o UserKnownHostsFile="$APP_SSH_KEY_PATH.known_hosts" \
    "${_VM_PROXY[@]}" -o HostKeyAlias="$vm_name" "root@$ip" -- "$remote" || rc=$?
  if ((rc == 255)); then
    echo "No se pudo conectar por SSH a $ip" >&2
    return 4
  fi
  if ((rc != 0)); then
    return 5
  fi
  return 0
}

# == 5. vm_scp <vm_name> <local> <remoto> (005) ==
# 5.1 Exigir 3 argumentos y APP_SSH_KEY_PATH; si no, devolver 2.
# 5.2 Resolver la IP con vm_ip (7 o 12 sin llamar a scp) y la ruta como vm_exec (pasos 4.3 y 4.4).
# 5.3 Ejecutar "scp <opciones de vm_exec> <local> root@<ip>:<remoto>". Si scp falla, devolver 4.

vm_scp() {
  if (($# != 3)); then
    echo "Uso: vm_scp <vm_name> <local> <remoto>" >&2
    return 2
  fi
  _require_key || return $?
  local vm_name="$1" local_file="$2" remote_path="$3" ip
  ip="$(vm_ip "$vm_name")" || return $?
  _proxy_opts "$vm_name" || return $?
  if ! scp -i "$APP_SSH_KEY_PATH" -o BatchMode=yes -o ConnectTimeout=10 \
    -o StrictHostKeyChecking=accept-new -o UserKnownHostsFile="$APP_SSH_KEY_PATH.known_hosts" \
    "${_VM_PROXY[@]}" -o HostKeyAlias="$vm_name" "$local_file" "root@$ip:$remote_path"; then
    echo "No se pudo copiar el archivo por scp a $ip" >&2
    return 4
  fi
}

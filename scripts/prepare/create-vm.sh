#!/usr/bin/env bash
# create-vm.sh - Crea una VM Debian 13 como hija de un disco base multiattach, con red en puente,
# la prepara por Guest Additions para que el backend entre por SSH y la deja encendida.
#
# Uso:
#   create-vm.sh <vm_name> <memory_mb> <cpus> <base_disk> <bridge_adapter> [<intnet_name>]
#
# Argumentos:
#   vm_name         nombre de la VM, ^[a-z0-9][a-z0-9-]{0,31}$
#   memory_mb       memoria en MB, entero entre 512 y 8192
#   cpus            número de CPUs, entero entre 1 y 4
#   base_disk       ruta absoluta del disco base (APP_BASE_DISK en el backend)
#   bridge_adapter  interfaz del host a la que se conecta la red en puente (APP_BRIDGE_ADAPTER),
#                   ^[A-Za-z0-9._:-]{1,64}$; debe aparecer en "VBoxManage list bridgedifs"
#   intnet_name     (opcional) red interna de VirtualBox a la que se conecta una segunda NIC,
#                   ^stratus-[a-z0-9][a-z0-9-]{0,31}$; la usa create-lb.sh (004). Sin él la VM
#                   tiene una sola NIC en puente, como siempre
#
# Variables de entorno:
#   APP_GUEST_PASSWORD_FILE  archivo del host (permisos 600) con la contraseña de root de la imagen
#                            base; solo lo lee VBoxManage con --passwordfile: nunca va en argv,
#                            en la salida ni en el historial
#   APP_SSH_KEY_PATH         llave privada del backend; se exige <ruta>.pub y los host keys
#                            fijados van en <ruta>.known_hosts
#
# Requisitos del disco base (los prepara el equipo una sola vez, a mano):
#   - Debian 13 instalado en una VM con el firmware BIOS por defecto de VirtualBox y una sola NIC.
#   - Guest Additions instaladas y arrancando con el sistema.
#   - openssh-server instalado.
#   - sudo instalado.
#   - La contraseña de root es el contenido de APP_GUEST_PASSWORD_FILE.
#   - El disco no está conectado como disco normal a otra VM; este script lo marca multiattach.
#
# Salida:
#   stdout  "VM <vm_name> creada y encendida; IP <ipv4>"
#   stderr  diagnósticos
#
# Códigos de salida:
#   0   éxito: la VM quedó creada, preparada, encendida y accesible por SSH en su IP
#   2   argumentos inválidos o variables de entorno faltantes
#   4   fallo de VBoxManage al crear o configurar la VM, o no se pudo marcar el disco multiattach
#   5   Guest Additions no respondió, el bootstrap falló, no hay IP tras reiniciar o falló SSH
#   6   disco base no encontrado, interfaz de puente no encontrada o VM ya registrada en VirtualBox
#   99  operación no implementada
#
# Tiempo máximo: cada espera tiene su cota y el peor caso suma 14 minutos, un minuto menos que el
# límite de 15 del backend, así el script termina casi siempre con su propio mensaje:
#   propiedad de Guest Additions   guestproperty wait --timeout 180000          180 s
#   disponibilidad de GA           8 intentos de ga_root 10000 /bin/true + 5 s   120 s
#   bootstrap                      ga_root 60000                                  60 s
#   apagado ordenado               12 sondeos de 5 s, luego poweroff              60 s
#   espera tras el poweroff        10 sondeos de 1 s                              10 s
#   propiedad de la IP             guestproperty wait --timeout 180000          180 s
#   comprobación SSH               8 intentos de ConnectTimeout=10 + 5 s         120 s
#   suma de las esperas                                                          730 s
#   llamadas a VBoxManage, ssh-keygen -R y limpieza (margen)                    110 s
# Si el backend lo detiene por tiempo, le envía SIGTERM: el script sale con 143 y la limpieza de la
# sección 3 quita la VM a medio crear (el backend espera 30 s antes de SIGKILL).

set -euo pipefail

# == 1. Validar argumentos ==
# 1.1 Exigir 5 argumentos, o 6 con intnet_name; si no, imprimir el uso en stderr y salir con 2.
# 1.2 Validar vm_name contra ^[a-z0-9][a-z0-9-]{0,31}$; si no coincide, salir con 2.
# 1.3 Validar que memory_mb (512-8192) y cpus (1-4) sean enteros en rango (solo dígitos antes de
#     comparar); si no, salir con 2.
# 1.4 Validar que base_disk sea una ruta absoluta sin caracteres de control y que bridge_adapter
#     cumpla ^[A-Za-z0-9._:-]{1,64}$; si no, salir con 2. Si hay intnet_name, debe cumplir
#     ^stratus-[a-z0-9][a-z0-9-]{0,31}$.
# 1.5 Exigir que APP_GUEST_PASSWORD_FILE sea un archivo legible y que exista
#     "$APP_SSH_KEY_PATH.pub"; si no, salir con 2.
# Ninguna validación fallida debe llamar a VBoxManage.

fail() {
  echo "$2" >&2
  exit "$1"
}

if (($# != 5 && $# != 6)); then
  fail 2 "Uso: create-vm.sh <vm_name> <memory_mb> <cpus> <base_disk> <bridge_adapter> [<intnet_name>]"
fi
vm_name="$1"
memory_mb="$2"
cpus="$3"
base_disk="$4"
bridge_adapter="$5"
intnet_name="${6-}"

if [[ ! "$vm_name" =~ ^[a-z0-9][a-z0-9-]{0,31}$ ]]; then
  fail 2 'vm_name inválido: debe cumplir ^[a-z0-9][a-z0-9-]{0,31}$'
fi

# in_range <valor> <mínimo> <máximo>: entero (solo dígitos) dentro del rango.
in_range() {
  [[ "$1" =~ ^[0-9]{1,9}$ ]] && ((10#$1 >= $2 && 10#$1 <= $3))
}
if ! in_range "$memory_mb" 512 8192; then
  fail 2 "memory_mb inválido: entero entre 512 y 8192"
fi
if ! in_range "$cpus" 1 4; then
  fail 2 "cpus inválido: entero entre 1 y 4"
fi

if [[ ! "$base_disk" =~ ^/[^[:cntrl:]]*$ ]]; then
  fail 2 "base_disk inválido: debe ser una ruta absoluta"
fi
if [[ ! "$bridge_adapter" =~ ^[A-Za-z0-9._:-]{1,64}$ ]]; then
  fail 2 'bridge_adapter inválido: debe cumplir ^[A-Za-z0-9._:-]{1,64}$'
fi

if (($# == 6)) && [[ ! "$intnet_name" =~ ^stratus-[a-z0-9][a-z0-9-]{0,31}$ ]]; then
  fail 2 'intnet_name inválido: debe cumplir ^stratus-[a-z0-9][a-z0-9-]{0,31}$'
fi

if [[ -z "${APP_GUEST_PASSWORD_FILE:-}" || ! -f "$APP_GUEST_PASSWORD_FILE" || ! -r "$APP_GUEST_PASSWORD_FILE" ]]; then
  fail 2 "APP_GUEST_PASSWORD_FILE debe ser un archivo legible"
fi
if [[ -z "${APP_SSH_KEY_PATH:-}" || ! -r "$APP_SSH_KEY_PATH.pub" ]]; then
  fail 2 "No se encontró la llave pública del backend (APP_SSH_KEY_PATH.pub)"
fi

# == 2. Comprobaciones antes de crear nada ==
# Ninguna de estas comprobaciones registra una VM ni toca el disco base, salvo marcarlo multiattach.
# 2.1 Si base_disk no es un archivo legible, imprimir "Disco base no encontrado: <ruta>" y salir
#     con 6.
# 2.2 Si bridge_adapter no está entre las líneas "Name:" de "VBoxManage list bridgedifs", imprimir
#     "Interfaz de puente no encontrada: <nombre>" y salir con 6.
# 2.3 Si "VBoxManage showmediuminfo disk <base_disk>" no informa "Type: multiattach", marcarlo con
#     "VBoxManage modifymedium disk <base_disk> --type multiattach" (idempotente: si ya lo es no se
#     llama). Si falla, imprimir "No se pudo marcar el disco base como multiattach; verifique que
#     no esté conectado a otra VM" y salir con 4.
# 2.4 Si "VBoxManage showvminfo <vm_name>" termina con 0, la VM ya está registrada: imprimir
#     "VM ya registrada en VirtualBox" y salir con 6 sin modificarla.

if [[ ! -f "$base_disk" || ! -r "$base_disk" ]]; then
  fail 6 "Disco base no encontrado: $base_disk"
fi

bridges="$(VBoxManage list bridgedifs)" || fail 4 "VBoxManage list bridgedifs falló"
if ! sed -n 's/^Name:[[:space:]]*//p' <<<"$bridges" | sed 's/[[:space:]]*$//' | grep -qxF -- "$bridge_adapter"; then
  fail 6 "Interfaz de puente no encontrada: $bridge_adapter"
fi

# Si showmediuminfo falla, el tipo queda vacío y se intenta marcar el disco igualmente.
medium_info="$(VBoxManage showmediuminfo disk "$base_disk" 2>/dev/null || true)"
if ! grep -qE '^Type:[[:space:]]+multiattach' <<<"$medium_info"; then
  VBoxManage modifymedium disk "$base_disk" --type multiattach >&2 ||
    fail 4 "No se pudo marcar el disco base como multiattach; verifique que no esté conectado a otra VM"
fi

if VBoxManage showvminfo "$vm_name" >/dev/null 2>&1; then
  fail 6 "VM ya registrada en VirtualBox"
fi

# == 3. Limpieza en caso de fallo ==
# Se ejecuta al salir con un código distinto de cero, una vez creada la VM (sección 4). Conserva el
# código original; sus errores solo se informan en stderr.
# 3.1 Si la VM está encendida, "VBoxManage controlvm <vm_name> poweroff" y esperar a que termine
#     de apagarse.
# 3.2 Leer el UUID del disco conectado (SATA-ImageUUID-0-0 de "showvminfo --machinereadable") y,
#     si es un disco diferencial (su "Parent UUID:" existe y no es "base"), anotarlo.
# 3.3 "VBoxManage storageattach <vm_name> ... --medium none" para soltar el disco.
# 3.4 Solo si el disco es diferencial: "VBoxManage closemedium disk <uuid> --delete". El disco base
#     (sin padre) nunca se cierra, se borra ni se modifica.
# 3.5 "VBoxManage unregistervm <vm_name> --delete".

# read_state: deja el estado de energía de la VM en "state" (vacío si VBoxManage no la conoce).
read_state() {
  local info
  info="$(VBoxManage showvminfo "$vm_name" --machinereadable 2>/dev/null)" || {
    state=""
    return 1
  }
  state="$(sed -n 's/^VMState="\(.*\)"$/\1/p' <<<"$info")"
}

# wait_stopped: espera hasta 10 s a que la VM deje de estar "running" o "stopping", para que
# VirtualBox libere su sesión antes de modificar su almacenamiento.
wait_stopped() {
  local i
  for ((i = 0; i < 10; i++)); do
    read_state || return 0
    [[ "$state" == running || "$state" == stopping ]] || return 0
    sleep 1
  done
}

cleanup() {
  local status=$? info medium_uuid parent=""
  trap - EXIT INT TERM
  if ((status == 0)); then
    return 0
  fi
  echo "Falló la creación de $vm_name; limpiando" >&2
  info="$(VBoxManage showvminfo "$vm_name" --machinereadable 2>/dev/null || true)"
  if [[ "$info" == *'VMState="running"'* ]]; then
    VBoxManage controlvm "$vm_name" poweroff >&2 || echo "No se pudo apagar la VM $vm_name" >&2
    wait_stopped
  fi
  medium_uuid="$(sed -n 's/^"\{0,1\}SATA-ImageUUID-0-0"\{0,1\}="\(.*\)"$/\1/p' <<<"$info")"
  if [[ -n "$medium_uuid" && "$medium_uuid" != none ]]; then
    parent="$(VBoxManage showmediuminfo disk "$medium_uuid" 2>/dev/null |
      sed -n 's/^Parent UUID:[[:space:]]*//p' || true)"
    VBoxManage storageattach "$vm_name" --storagectl SATA --port 0 --device 0 --medium none >&2 ||
      echo "No se pudo soltar el disco de $vm_name" >&2
    if [[ -n "$parent" && "$parent" != base ]]; then
      VBoxManage closemedium disk "$medium_uuid" --delete >&2 ||
        echo "No se pudo borrar el disco diferencial de $vm_name" >&2
    fi
  fi
  VBoxManage unregistervm "$vm_name" --delete >&2 || echo "No se pudo eliminar la VM $vm_name" >&2
  exit "$status"
}

# == 4. Crear y configurar la VM ==
# 4.1 "VBoxManage createvm --name <vm_name> --ostype Debian_64 --register". Si falla no hay nada
#     que limpiar (y no se debe borrar una VM ajena con ese nombre): salir con 4.
# 4.2 Desde aquí, instalar el "trap" de la sección 3. Cualquier fallo siguiente sale con 4.
# 4.3 "VBoxManage modifyvm <vm_name> --memory <memory_mb> --cpus <cpus> --ioapic on --nic1 bridged
#     --bridgeadapter1 <bridge_adapter>": sin reenvío de puertos; la IP viene del DHCP de la LAN.
#     Con intnet_name se agrega "--nic2 intnet --intnet2 <intnet_name>" en el mismo comando, para
#     que la NIC 2 exista desde el primer arranque.
# 4.4 "VBoxManage storagectl <vm_name> --name SATA --add sata --controller IntelAhci".
# 4.5 "VBoxManage storageattach <vm_name> --storagectl SATA --port 0 --device 0 --type hdd
#     --mtype multiattach --medium <base_disk>": VirtualBox crea el disco diferencial de la VM y
#     no se crea ningún disco nuevo.
# 4.6 "VBoxManage startvm <vm_name> --type headless".

VBoxManage createvm --name "$vm_name" --ostype Debian_64 --register >&2 ||
  fail 4 "No se pudo crear la VM $vm_name"

trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

nic2_args=()
if [[ -n "$intnet_name" ]]; then
  nic2_args=(--nic2 intnet --intnet2 "$intnet_name")
fi
VBoxManage modifyvm "$vm_name" --memory "$memory_mb" --cpus "$cpus" --ioapic on \
  --nic1 bridged --bridgeadapter1 "$bridge_adapter" "${nic2_args[@]}" >&2 || fail 4 "No se pudo configurar la VM $vm_name"
VBoxManage storagectl "$vm_name" --name SATA --add sata --controller IntelAhci >&2 ||
  fail 4 "No se pudo agregar el controlador SATA a $vm_name"
VBoxManage storageattach "$vm_name" --storagectl SATA --port 0 --device 0 --type hdd \
  --mtype multiattach --medium "$base_disk" >&2 || fail 4 "No se pudo conectar el disco base a $vm_name"
VBoxManage startvm "$vm_name" --type headless >&2 || fail 4 "No se pudo encender la VM $vm_name"

# == 5. Esperar a Guest Additions ==
# 5.1 Esperar a que la VM reporte su versión de Guest Additions: si la propiedad
#     /VirtualBox/GuestAdd/Version aún no existe, "VBoxManage guestproperty wait <vm_name>
#     /VirtualBox/GuestAdd/Version --timeout 180000" y comprobar con "guestproperty get" que exista
#     (la espera termina con 0 también al agotarse el tiempo). Si no existe, salir con 5.
# 5.2 Con la propiedad presente, el servicio de ejecución de procesos puede tardar un poco más:
#     repetir hasta 8 veces "ga_root 10000 /bin/true" (10 s cada intento) con "sleep 5" entre
#     intentos. Si ninguno funciona (por ejemplo, la contraseña de la imagen base es otra), salir
#     con 5.

# ga_root <ms> <ejecutable> [argumentos...]: ejecuta un programa como root dentro de la VM por Guest
# Additions, con un límite de <ms> milisegundos (--timeout). La contraseña la lee VBoxManage del
# archivo (--passwordfile): no va en argv. La salida del programa va a stderr para que stdout solo
# tenga el resultado. Con --exe, VirtualBox 7.2 ya agrega el nombre del programa como argv[0]; si se
# repitiera tras "--", /bin/sh lo tomaría como un archivo de script ("cannot open sh").
ga_root() {
  local timeout_ms="$1" exe="$2"
  shift 2
  VBoxManage guestcontrol "$vm_name" run --username root --passwordfile "$APP_GUEST_PASSWORD_FILE" \
    --timeout "$timeout_ms" --wait-stdout --wait-stderr --exe "$exe" -- "$@" >&2
}

# guest_property <propiedad>: imprime el valor de la propiedad de la VM, o nada si no existe.
guest_property() {
  VBoxManage guestproperty get "$vm_name" "$1" 2>/dev/null | sed -n 's/^Value: //p' || true
}

ga_version_prop=/VirtualBox/GuestAdd/Version
if [[ -z "$(guest_property "$ga_version_prop")" ]]; then
  VBoxManage guestproperty wait "$vm_name" "$ga_version_prop" --timeout 180000 >&2 || true
fi
if [[ -z "$(guest_property "$ga_version_prop")" ]]; then
  fail 5 "Guest Additions de $vm_name no respondió a tiempo"
fi

ga_ready=0
for ((i = 0; i < 8; i++)); do
  if ga_root 10000 /bin/true; then
    ga_ready=1
    break
  fi
  sleep 5
done
if ((ga_ready == 0)); then
  fail 5 "No se pudo ejecutar un comando como root en $vm_name por Guest Additions; verifique la contraseña de la imagen base"
fi

# == 6. Bootstrap por Guest Additions ==
# Una sola llamada como root; es la única vez que se usa Guest Additions para configurar la VM.
# El script es una constante: la llave pública del backend y el nombre de la VM entran como
# argumentos posicionales ($1 y $2) y nunca se interpolan en su texto. Corre con "set -e" y:
# 6.1 Crea /root/.ssh (700) y authorized_keys (600) y agrega la llave pública si no estaba.
# 6.2 Escribe /etc/ssh/sshd_config.d/10-stratus.conf con "PasswordAuthentication no" y
#     "PermitRootLogin prohibit-password".
# 6.3 Regenera /etc/machine-id (y el enlace de dbus) para que los clones no compartan identidad.
# 6.4 Borra las concesiones DHCP y el DUID del cliente para que cada clon pida su propia IP.
# 6.5 Borra las llaves de host SSH y las regenera con "ssh-keygen -A".
# 6.6 Fija el nombre de host con "hostnamectl set-hostname" y reemplaza (o agrega) la línea
#     127.0.1.1 de /etc/hosts.
# 6.7 Valida la configuración con "sshd -t".
# 6.8 La llamada tiene un límite de 60 s (ga_root 60000). Si falla o se agota, salir con 5.

BOOTSTRAP=""
read -r -d '' BOOTSTRAP <<'EOS' || true
set -e
pub_key="$1"
host_name="$2"

# 6.1 Acceso del backend por llave.
install -d -m 700 /root/.ssh
touch /root/.ssh/authorized_keys
chmod 600 /root/.ssh/authorized_keys
grep -qxF "$pub_key" /root/.ssh/authorized_keys || printf '%s\n' "$pub_key" >>/root/.ssh/authorized_keys

# 6.2 Sin contraseña por SSH; root solo con llave.
install -d -m 755 /etc/ssh/sshd_config.d
printf '%s\n' 'PasswordAuthentication no' 'PermitRootLogin prohibit-password' \
  >/etc/ssh/sshd_config.d/10-stratus.conf

# 6.3 Nuevo machine-id.
rm -f /etc/machine-id /var/lib/dbus/machine-id
systemd-machine-id-setup
ln -sf /etc/machine-id /var/lib/dbus/machine-id

# 6.4 Nueva identidad DHCP.
rm -f /var/lib/dhcp/*.leases /var/lib/dhcpcd/duid /var/lib/dhcpcd/*.lease*

# 6.5 Nuevas llaves de host SSH.
rm -f /etc/ssh/ssh_host_*
ssh-keygen -A

# 6.6 Nombre de host y línea 127.0.1.1.
hostnamectl set-hostname "$host_name"
if grep -q '^127\.0\.1\.1[[:space:]]' /etc/hosts; then
  sed -i "s/^127\.0\.1\.1[[:space:]].*/127.0.1.1 $host_name/" /etc/hosts
else
  printf '127.0.1.1 %s\n' "$host_name" >>/etc/hosts
fi

# 6.7 Validar la configuración de sshd.
sshd -t
EOS

backend_pub_key="$(<"$APP_SSH_KEY_PATH.pub")"
ga_root 60000 /bin/sh -c "$BOOTSTRAP" sh "$backend_pub_key" "$vm_name" ||
  fail 5 "Falló la preparación de $vm_name por Guest Additions"

# == 7. Reiniciar y esperar la IP ==
# El reinicio hace que la VM arranque con el nuevo machine-id y las nuevas llaves de host, y que
# pida su IP al DHCP con su propia identidad.
# 7.1 "VBoxManage controlvm <vm_name> acpipowerbutton" y repetir 12 veces: "sleep 5" y leer
#     VMState (60 s en total; se cuentan las esperas, no el reloj, para que la prueba con stubs sea
#     determinista). Si sigue "running", "VBoxManage controlvm <vm_name> poweroff".
# 7.2 Esperar a que la VM termine de apagarse y borrar la propiedad
#     /VirtualBox/GuestInfo/Net/0/V4/IP, para no leer la IP anterior.
# 7.3 "VBoxManage startvm <vm_name> --type headless"; si falla, salir con 4.
# 7.4 Esperar la propiedad de la IP (guestproperty wait con 180000 ms y luego get). El valor debe
#     ser una IPv4; si no, imprimir "La VM no obtuvo una dirección IP por DHCP" y salir con 5.

ip_prop=/VirtualBox/GuestInfo/Net/0/V4/IP

VBoxManage controlvm "$vm_name" acpipowerbutton >&2 || echo "No se pudo enviar la señal ACPI a $vm_name" >&2
for ((i = 0; i < 12; i++)); do
  sleep 5
  read_state || break
  [[ "$state" == running ]] || break
done
if [[ "$state" == running ]]; then
  VBoxManage controlvm "$vm_name" poweroff >&2 || fail 4 "No se pudo forzar el apagado de $vm_name"
fi
wait_stopped
VBoxManage guestproperty delete "$vm_name" "$ip_prop" >&2 || true
VBoxManage startvm "$vm_name" --type headless >&2 || fail 4 "No se pudo encender la VM $vm_name"

if [[ -z "$(guest_property "$ip_prop")" ]]; then
  VBoxManage guestproperty wait "$vm_name" "$ip_prop" --timeout 180000 >&2 || true
fi
vm_ip="$(guest_property "$ip_prop")"
if [[ ! "$vm_ip" =~ ^[0-9]{1,3}(\.[0-9]{1,3}){3}$ ]]; then
  fail 5 "La VM no obtuvo una dirección IP por DHCP"
fi

# == 8. Comprobar el acceso SSH del backend ==
# 8.1 "ssh-keygen -R <vm_name> -f <APP_SSH_KEY_PATH>.known_hosts": olvidar una llave de host
#     anterior con ese nombre (se tolera que falle si no había ninguna).
# 8.2 Hasta 8 intentos, con "sleep 5" entre ellos: "ssh -i <APP_SSH_KEY_PATH> -o BatchMode=yes
#     -o ConnectTimeout=10 -o StrictHostKeyChecking=accept-new
#     -o UserKnownHostsFile=<APP_SSH_KEY_PATH>.known_hosts -o HostKeyAlias=<vm_name>
#     root@<ip> true". Así queda fijada la llave de host de esta VM.
# 8.3 Si ningún intento funciona, imprimir "La llave del backend no abrió una sesión SSH en <ip>"
#     y salir con 5.

ssh-keygen -R "$vm_name" -f "$APP_SSH_KEY_PATH.known_hosts" >&2 || true
ssh_ok=0
for ((i = 0; i < 8; i++)); do
  if ssh -i "$APP_SSH_KEY_PATH" -o BatchMode=yes -o ConnectTimeout=10 -o StrictHostKeyChecking=accept-new \
    -o UserKnownHostsFile="$APP_SSH_KEY_PATH.known_hosts" -o HostKeyAlias="$vm_name" \
    "root@$vm_ip" true >&2; then
    ssh_ok=1
    break
  fi
  sleep 5
done
if ((ssh_ok == 0)); then
  fail 5 "La llave del backend no abrió una sesión SSH en $vm_ip"
fi

# == 9. Resultado ==
# 9.1 Quitar el "trap" de limpieza.
# 9.2 Imprimir en stdout "VM <vm_name> creada y encendida; IP <ip>".

trap - EXIT INT TERM
echo "VM $vm_name creada y encendida; IP $vm_ip"

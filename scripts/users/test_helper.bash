# Utilidades compartidas por las pruebas bats de scripts/users.
# Uso en cada archivo .bats:
#   load test_helper
#
# Carga el helper de scripts/prepare (assert_script_structure, setup_stubs, stub_command,
# stub_vm_ip). Los scripts bajo prueba se ejecutan con "</dev/null", salvo que la prueba
# entregue su propia entrada estándar.
#
# Funciones:
#   setup_users_stubs <vm> <ip|->   setup_stubs + stub_vm_ip, un stub de ssh que responde
#                                   según variables de entorno (ver abajo) y un ssh-keygen que
#                                   escribe un par de llaves falso en la ruta de "-f".
#   setup_password_file <clave>     escribe "$BATS_TEST_TMPDIR/password" (permisos 600, sin salto
#                                   de línea final) y exporta PASSWORD_FILE.
#   run_remote_locally              solo para list-users: el stub de ssh ejecuta el programa remoto
#                                   en el equipo local, con stubs de getent y sudo y los homes
#                                   bajo "$BATS_TEST_TMPDIR/home" (el programa solo lee).
#
# Archivos que deja el stub de ssh en "$BATS_TEST_TMPDIR":
#   calls.log       argv de cada llamada (lo escribe setup_stubs; los argumentos remotos vienen
#                   citados con "printf '%q '", igual que los envía vm_exec).
#   remote.log      el comando remoto de cada llamada ya descitado, una línea por llamada.
#   ssh-stdin.log   la entrada estándar de todas las llamadas, una detrás de otra.
#
# Variables de entorno que lee el stub de ssh:
#   LOOKUP=user|group|free   respuesta del paso de búsqueda de create-user.sh (por defecto free).
#   TARGET_UID=<n|none>      respuesta de "id -u" en delete-user.sh (por defecto 1002).
#   FAIL_REMOTE=<ERE>        termina con 1 cuando el comando remoto coincide.
#   FAIL_CLEANUP=<ERE>       termina con 1 cuando el comando es de limpieza (userdel -r, groupdel,
#                            borrado de la regla sudoers temporal o la verificación) y coincide.

load ../prepare/test_helper

# Directorio de los scripts del módulo de usuarios (donde vive este helper).
USERS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# setup_users_stubs <vm> <ipv4|->
# Deja la VM <vm> encendida con esa IP (o apagada sin IP si es "-") y reemplaza el cuerpo del
# stub de ssh por uno que registra, responde y falla según las variables del encabezado.
setup_users_stubs() {
  setup_stubs
  stub_vm_ip "$1" "$2"
  : >"$BATS_TEST_TMPDIR/remote.log"
  : >"$BATS_TEST_TMPDIR/ssh-stdin.log"

  # ssh-keygen falso: crea <ruta> (llave privada falsa) y <ruta>.pub (llave pública falsa).
  stub_command ssh-keygen 'while (($#)); do
  if [[ "$1" == -f ]]; then
    printf "%s\n" "-----BEGIN OPENSSH PRIVATE KEY-----" "FAKEKEYDATA" "-----END OPENSSH PRIVATE KEY-----" >"$2"
    printf "%s\n" "ssh-ed25519 AAAAFAKEPUBLICKEY stratus-test" >"$2.pub"
  fi
  shift
done'

  # El directorio de la prueba se fija al crear el stub, como hace setup_stubs con calls.log.
  cat >>"$BATS_TEST_TMPDIR/bin/ssh" <<EOF
tmp="$BATS_TEST_TMPDIR"
# La entrada estándar (por ejemplo "usuario:clave" hacia chpasswd) queda en un solo archivo.
cat >>"\$tmp/ssh-stdin.log"
# El comando remoto es el último argumento, citado con %q: eval lo separa en palabras.
remote="\${@: -1}"
eval "words=(\$remote)"
flat="\${words[*]}"
printf '%s\n' "\$flat" >>"\$tmp/remote.log"

# run_remote_locally: ejecuta el programa remoto aquí mismo.
if [[ -e "\$tmp/run-locally" ]]; then
  "\${words[@]}"
  exit \$?
fi

cleanup_re='userdel -r|groupdel|/etc/sudoers\.d/\.stratus|\[ ! -e'
if [[ -n "\${FAIL_CLEANUP:-}" && "\$flat" =~ \$cleanup_re && "\$flat" =~ \$FAIL_CLEANUP ]]; then
  exit 1
fi
if [[ -n "\${FAIL_REMOTE:-}" && "\$flat" =~ \$FAIL_REMOTE ]]; then
  exit 1
fi
# Como el sistema real: visudo y passwd escriben su aviso en la salida estándar.
if [[ "\$flat" == *'visudo -cf'* ]]; then
  echo "/etc/sudoers.d/.stratus-x: parsed OK"
elif [[ "\$flat" == passwd\ -l* ]]; then
  echo "passwd: contraseña cambiada."
fi
if [[ "\$flat" == *'echo user; elif'* ]]; then
  echo "\${LOOKUP:-free}"
  exit 0
fi
if [[ "\$flat" == *'id -u "\$1"'* ]]; then
  echo "\${TARGET_UID:-1002}"
  exit 0
fi
exit 0
EOF
}

# setup_password_file <clave>
# Simula el archivo secreto que entrega el backend: permisos 600 y sin salto de línea final.
setup_password_file() {
  PASSWORD_FILE="$BATS_TEST_TMPDIR/password"
  printf '%s' "$1" >"$PASSWORD_FILE"
  chmod 600 "$PASSWORD_FILE"
  export PASSWORD_FILE
}

# run_remote_locally
# Para list-users.bats. Datos: root, www-data, ana (1001, sudo y una llave), luis (1002, sin sudo
# y un authorized_keys que solo tiene comentarios y líneas en blanco) y nobody (65534).
run_remote_locally() {
  local home="$BATS_TEST_TMPDIR/home"
  mkdir -p "$home/ana/.ssh" "$home/luis/.ssh"
  printf '# llaves de ana\nssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFAKE ana@pc\n' >"$home/ana/.ssh/authorized_keys"
  printf '# sin llaves\n\n   \n' >"$home/luis/.ssh/authorized_keys"

  cat >"$BATS_TEST_TMPDIR/bin/getent" <<EOF
#!/usr/bin/env bash
[[ "\${1:-}" == passwd ]] || exit 2
data='root:x:0:0:root:/root:/bin/bash
www-data:x:33:33:www-data:/var/www:/usr/sbin/nologin
ana:x:1001:1001::$home/ana:/bin/bash
luis:x:1002:1002::$home/luis:/bin/bash
nobody:x:65534:65534:nobody:/nonexistent:/usr/sbin/nologin'
if [[ -n "\${2:-}" ]]; then
  grep -E "^\$2:" <<<"\$data" || exit 2
else
  echo "\$data"
fi
EOF
  cat >"$BATS_TEST_TMPDIR/bin/sudo" <<'EOF'
#!/usr/bin/env bash
# Solo ana puede usar sudo (grupo sudo o regla en sudoers.d).
if [[ "${4:-}" == ana ]]; then
  echo "User ana may run the following commands on debian:"
  echo "    (ALL : ALL) NOPASSWD: ALL"
else
  echo "User ${4:-?} is not allowed to run sudo on debian."
fi
EOF
  chmod +x "$BATS_TEST_TMPDIR/bin/getent" "$BATS_TEST_TMPDIR/bin/sudo"
  : >"$BATS_TEST_TMPDIR/run-locally"
}

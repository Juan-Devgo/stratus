#!/usr/bin/env bats
# Pruebas de service-common.sh: validadores, check_archive, render_unit, encode_env y los
# ayudantes remotos (guest, require_managed, read_state, write_remote_file, read_unit_fields)
# con los stubs de VBoxManage y ssh; no tocan VirtualBox ni VMs.

load ../test_helper

bats_require_minimum_version 1.5.0

setup() {
  setup_services_stubs vm1 192.168.1.50
  export APP_SSH_KEY_PATH="$BATS_TEST_TMPDIR/stratus_backend"
  : >"$APP_SSH_KEY_PATH"
  # shellcheck source=../../prepare/lib/vm-exec.sh
  source "$SERVICES_DIR/../prepare/lib/vm-exec.sh"
  # shellcheck source=service-common.sh
  source "$SERVICES_DIR/lib/service-common.sh"
  vm_name=vm1
  name=demian-lee
  ARCHIVE="$BATS_TEST_TMPDIR/app.tar.gz"
}

# --- Validadores (REQ-2, REQ-3, REQ-6, REQ-7, REQ-8) ---

@test "AC-3 validate_name acepta nombres válidos y rechaza el resto" {
  local long_name
  long_name="$(printf 'a%.0s' {1..33})"
  for value in demian-lee api a "$(printf 'a%.0s' {1..32})" app-1; do
    run validate_name "$value"
    [ "$status" -eq 0 ]
  done
  for value in Demian 1app 'app;rm' app_x '' "$long_name" $'api\n' -api 'a b'; do
    run --separate-stderr validate_name "$value"
    [ "$status" -eq 1 ]
    [ -n "$stderr" ]
  done
}

@test "AC-8 validate_description acepta acentos y rechaza % \\ \" y caracteres de control" {
  for value in "Servidor de producción ñ" "API" "$(printf 'a%.0s' {1..100})"; do
    run validate_description "$value"
    [ "$status" -eq 0 ]
  done
  for value in '' '50%' 'a\b' 'di"ce' $'dos\nlíneas' $'tab\taquí' "$(printf 'a%.0s' {1..101})"; do
    run validate_description "$value"
    [ "$status" -eq 1 ]
  done
}

@test "AC-7 validate_executable acepta rutas relativas y rechaza ../x, /bin/sh y segmentos . o .." {
  local long_exe
  long_exe="$(printf 'a%.0s' {1..256})"
  for value in demian-lee bin/server a.b/c_d-e "$(printf 'a%.0s' {1..255})"; do
    run validate_executable "$value"
    [ "$status" -eq 0 ]
  done
  for value in '' ../x /bin/sh a/./b a/../b . .. 'a b' 'a;b' 'a//b' 'bin/' "$long_exe"; do
    run validate_executable "$value"
    [ "$status" -eq 1 ]
  done
}

@test "AC-8 validate_args acepta vacío y hasta 16 tokens permitidos" {
  local tokens17
  tokens17="$(printf 'a %.0s' {1..16})a"
  for value in '' '--port 8080' '--listen=0.0.0.0:80 -v' 'a,b@c+d/e' "$(printf 'a %.0s' {1..15})a"; do
    run validate_args "$value"
    [ "$status" -eq 0 ]
  done
  # 'a;b', '$HOME' y '"x"' son los casos del AC-8; el resto prueba espacios y largo.
  for value in 'a;b' '$HOME' '"x"' "$tokens17" ' a' 'a ' 'a  b' $'a\nb' "$(printf 'x%.0s' {1..129})"; do
    run validate_args "$value"
    [ "$status" -eq 1 ]
  done
}

@test "AC-8 validate_restart y validate_restart_sec" {
  for value in no always on-failure; do
    run validate_restart "$value"
    [ "$status" -eq 0 ]
  done
  for value in on-abort '' Always 'no ' on-success; do
    run validate_restart "$value"
    [ "$status" -eq 1 ]
  done
  for value in 0 5 3600 10; do
    run validate_restart_sec "$value"
    [ "$status" -eq 0 ]
  done
  for value in -1 3601 '' 05 1.5 abc 99999; do
    run validate_restart_sec "$value"
    [ "$status" -eq 1 ]
  done
}

# --- validate_env_file (REQ-9) ---

@test "AC-9 validate_env_file acepta un archivo válido, vacío o de 50 filas" {
  setup_env_file PORT=8080 'SERVER_NAME=Servidor Produccion 1' 'ADMIN_PASSWORD=a b "c" $HOME \x' EMPTY=
  run validate_env_file "$ENV_FILE"
  [ "$status" -eq 0 ]

  : >"$ENV_FILE"
  run validate_env_file "$ENV_FILE"
  [ "$status" -eq 0 ]

  local rows=() i
  for i in {1..50}; do rows+=("K$i=v"); done
  setup_env_file "${rows[@]}"
  run validate_env_file "$ENV_FILE"
  [ "$status" -eq 0 ]

  # Un valor de 1024 caracteres es válido.
  setup_env_file "K=$(printf 'x%.0s' {1..1024})"
  run validate_env_file "$ENV_FILE"
  [ "$status" -eq 0 ]
}

@test "AC-9 validate_env_file rechaza claves y valores fuera de la lista permitida" {
  local rows=() i
  for i in {1..51}; do rows+=("K$i=v"); done

  # Cada caso es el contenido completo del archivo.
  setup_env_file port=1
  run validate_env_file "$ENV_FILE"
  [ "$status" -eq 1 ]
  setup_env_file 1PORT=1
  run validate_env_file "$ENV_FILE"
  [ "$status" -eq 1 ]
  setup_env_file PORT=1 PORT=2
  run validate_env_file "$ENV_FILE"
  [ "$status" -eq 1 ]
  setup_env_file "NOEQUALS"
  run validate_env_file "$ENV_FILE"
  [ "$status" -eq 1 ]
  setup_env_file "K=$(printf 'x%.0s' {1..1025})"
  run validate_env_file "$ENV_FILE"
  [ "$status" -eq 1 ]
  setup_env_file "${rows[@]}"
  run validate_env_file "$ENV_FILE"
  [ "$status" -eq 1 ]
  setup_env_file "K=café"
  run validate_env_file "$ENV_FILE"
  [ "$status" -eq 1 ]
  setup_env_file "K=con tab"$'\t'"aquí"
  run validate_env_file "$ENV_FILE"
  [ "$status" -eq 1 ]
  # Una fila vacía en medio y un archivo que no existe.
  printf 'A=1\n\nB=2\n' >"$ENV_FILE"
  run validate_env_file "$ENV_FILE"
  [ "$status" -eq 1 ]
  run validate_env_file "$BATS_TEST_TMPDIR/no-existe"
  [ "$status" -eq 1 ]
}

@test "AC-9 validate_env_file no repite los valores en el mensaje de error" {
  setup_env_file port=Secreta123
  run --separate-stderr validate_env_file "$ENV_FILE"
  [ "$status" -eq 1 ]
  [[ "$stderr" != *Secreta123* ]]
}

# --- check_archive (REQ-5, REQ-6) ---

@test "AC-6 check_archive acepta un archivo válido con el ejecutable" {
  make_archive "$ARCHIVE" demian-lee templates/ static/x data/x
  run check_archive "$ARCHIVE" demian-lee
  [ "$status" -eq 0 ]
  make_archive "$ARCHIVE" bin/server
  run check_archive "$ARCHIVE" bin/server
  [ "$status" -eq 0 ]
}

@test "AC-6 check_archive devuelve 10 para los archivos inválidos" {
  local kind
  for kind in dotdot absolute symlink hardlink fifo corrupt; do
    make_bad_archive "$kind" "$ARCHIVE"
    run --separate-stderr check_archive "$ARCHIVE" app
    [ "$status" -eq 10 ]
    [[ "$stderr" == *"Archivo comprimido inválido"* ]]
  done
  # Un archivo que no es gzip y uno que no existe.
  printf 'no es gzip' >"$ARCHIVE"
  run check_archive "$ARCHIVE" app
  [ "$status" -eq 10 ]
  run check_archive "$BATS_TEST_TMPDIR/no-existe" app
  [ "$status" -eq 10 ]
}

@test "AC-7 check_archive devuelve 11 si el ejecutable no es un archivo regular del archivo" {
  make_archive "$ARCHIVE" demian-lee templates/
  run --separate-stderr check_archive "$ARCHIVE" bin/app
  [ "$status" -eq 11 ]
  [[ "$stderr" == *"El ejecutable no existe en el directorio de la aplicación"* ]]
  # Un directorio no es un ejecutable.
  run check_archive "$ARCHIVE" templates
  [ "$status" -eq 11 ]
}

# --- render_unit (REQ-11) ---

@test "AC-2 render_unit imprime la marca y exactamente las líneas de REQ-11" {
  run render_unit demian-lee "Servidor de producción" demian-lee "" on-failure 5
  [ "$status" -eq 0 ]
  local expected
  expected="$SERVICE_MARKER
[Unit]
Description=Servidor de producción
After=network.target

[Service]
Type=simple
User=demian-lee
Group=demian-lee
WorkingDirectory=/opt/demian-lee
EnvironmentFile=/etc/demian-lee.env
ExecStart=/opt/demian-lee/demian-lee
Restart=on-failure
RestartSec=5
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ReadWritePaths=/opt/demian-lee

[Install]
WantedBy=multi-user.target"
  [ "$output" = "$expected" ]
}

@test "AC-2 render_unit agrega los argumentos a ExecStart y usa los valores recibidos" {
  run render_unit api "API interna" bin/server "--port 9090 -v" always 10
  [ "$status" -eq 0 ]
  [[ "$output" == *$'\nExecStart=/opt/api/bin/server --port 9090 -v\n'* ]]
  [[ "$output" == *$'\nRestart=always\nRestartSec=10\n'* ]]
  [[ "$output" == *$'\nUser=api\nGroup=api\nWorkingDirectory=/opt/api\n'* ]]
  [ "${lines[0]}" = "$SERVICE_MARKER" ]
}

# --- encode_env (REQ-9, REQ-13) ---

@test "AC-10 encode_env conserva el valor exacto de a b \"c\" \$HOME \\x" {
  setup_env_file 'K=a b "c" $HOME \x'
  run encode_env "$ENV_FILE"
  [ "$status" -eq 0 ]
  [ "$output" = 'K="a b \"c\" \$HOME \\x"' ]
}

@test "AC-10 encode_env escapa comillas invertidas, conserva el orden y los valores vacíos" {
  setup_env_file 'A=`id`' 'B=' 'C=x=y'
  run encode_env "$ENV_FILE"
  [ "$status" -eq 0 ]
  [ "$output" = 'A="\`id\`"'$'\n''B=""'$'\n''C="x=y"' ]
}

# --- Ayudantes (fail, is_running) ---

@test "fail imprime el mensaje en stderr y sale con el código" {
  run --separate-stderr fail 6 "Solo se pueden gestionar servicios creados por Stratus"
  [ "$status" -eq 6 ]
  [ "$stderr" = "Solo se pueden gestionar servicios creados por Stratus" ]
  [ -z "$output" ]
}

@test "is_running es verdadero para active y activating, falso para el resto" {
  for state in active activating "activating (auto-restart)"; do
    run is_running "$state"
    [ "$status" -eq 0 ]
  done
  for state in inactive failed deactivating desconocido ""; do
    run is_running "$state"
    [ "$status" -eq 1 ]
  done
}

# --- Ayudantes remotos ---

@test "guest deja pasar el éxito y la salida del comando remoto" {
  run guest 5 echo hola </dev/null
  [ "$status" -eq 0 ]
  grep -q '^echo hola$' "$BATS_TEST_TMPDIR/remote.log"
}

@test "guest sale con el código dado y un mensaje con el programa si el paso remoto falla" {
  FAIL_REMOTE='useradd' run --separate-stderr guest 5 useradd --system x </dev/null
  [ "$status" -eq 5 ]
  [ "$stderr" = "Falló el paso useradd en la VM (código 5)" ]
}

@test "guest usa fail_msg si está definido" {
  fail_msg="No se pudo eliminar el servicio api: falló el paso 4"
  FAIL_REMOTE='rm -rf' run --separate-stderr guest 5 rm -rf /opt/api </dev/null
  [ "$status" -eq 5 ]
  [ "$stderr" = "No se pudo eliminar el servicio api: falló el paso 4" ]
}

@test "guest deja pasar el 7 (VM sin IP) sin ejecutar ssh" {
  setup_services_stubs vm1 -
  : >"$BATS_TEST_TMPDIR/calls.log"
  run --separate-stderr guest 5 echo hola </dev/null
  [ "$status" -eq 7 ]
  [[ "$stderr" == *"La VM no tiene dirección IP; verifique que esté encendida"* ]]
  ! grep -q '^ssh ' "$BATS_TEST_TMPDIR/calls.log"
}

@test "guest deja pasar el 4 (fallo de SSH)" {
  SSH_RC=255 run guest 5 echo hola </dev/null
  [ "$status" -eq 4 ]
}

@test "require_managed deja pasar una unidad con la marca y revisa la marca exacta" {
  run require_managed </dev/null
  [ "$status" -eq 0 ]
  grep -q 'grep -qxF .*/etc/systemd/system/\$1.service' "$BATS_TEST_TMPDIR/remote.log"
  grep -qF -- "sh demian-lee $SERVICE_MARKER" "$BATS_TEST_TMPDIR/remote.log"
}

@test "AC-20 require_managed sale con 6 y el texto de la spec si la unidad no tiene la marca" {
  name=ssh
  MANAGED=no run --separate-stderr require_managed </dev/null
  [ "$status" -eq 6 ]
  [ "$stderr" = "Solo se pueden gestionar servicios creados por Stratus" ]
}

@test "read_state imprime el estado que informa systemd" {
  ACTIVE=failed run read_state </dev/null
  [ "$output" = failed ]
  ACTIVE="activating (auto-restart)" run read_state </dev/null
  [ "$output" = "activating (auto-restart)" ]
  grep -q 'systemctl show -p ActiveState --value' "$BATS_TEST_TMPDIR/remote.log"
  grep -q 'activating' "$BATS_TEST_TMPDIR/remote.log"
}

@test "read_state imprime desconocido si la lectura falla" {
  FAIL_REMOTE='systemctl show' run read_state </dev/null
  [ "$status" -eq 0 ]
  [ "$output" = desconocido ]
}

@test "write_remote_file escribe en un .tmp con dueño root, modo y mv -f, y envía el stdin" {
  run write_remote_file /etc/demian-lee.env 600 <<<'PORT="8080"'
  [ "$status" -eq 0 ]
  grep -q 'umask 077; cat >"\$1.tmp" && chown root:root "\$1.tmp" && chmod "\$2" "\$1.tmp" && mv -f "\$1.tmp" "\$1" sh /etc/demian-lee.env 600$' \
    "$BATS_TEST_TMPDIR/remote.log"
  [ "$(cat "$BATS_TEST_TMPDIR/stdin/1")" = 'PORT="8080"' ]
}

@test "read_unit_fields imprime los cinco campos separados por tabuladores" {
  UNIT_FIELDS=$'Servidor\tbin/server\t--port 9090\talways\t10' run read_unit_fields </dev/null
  [ "$status" -eq 0 ]
  [ "$output" = $'Servidor\tbin/server\t--port 9090\talways\t10' ]
  grep -q '/etc/systemd/system/\$1.service sh demian-lee$' "$BATS_TEST_TMPDIR/remote.log" ||
    grep -q 'sh demian-lee$' "$BATS_TEST_TMPDIR/remote.log"
}

@test "read_unit_fields interpreta una unidad real de render_unit (programa remoto ejecutado aquí)" {
  # El programa de awk corre aquí sobre una unidad escrita con render_unit.
  local dir="$BATS_TEST_TMPDIR/fake-root"
  mkdir -p "$dir"
  render_unit api "API interna" bin/server "--port 9090" always 10 >"$dir/api.service"
  name=api read_unit_fields </dev/null >/dev/null
  local program
  program="$(sed -n 's/^sh -c \(.*\) sh api$/\1/p' "$BATS_TEST_TMPDIR/remote.log" | tail -1)"
  [ -n "$program" ]
  # remote.log tiene "sh -c <programa> sh api"; se sustituye el directorio de unidades del programa por el de prueba y se ejecuta.
  program="${program//\/etc\/systemd\/system/$dir}"
  run sh -c "$program" sh api
  [ "$status" -eq 0 ]
  [ "$output" = $'API interna\tbin/server\t--port 9090\talways\t10' ]
}

# --- 004: servidores de un balanceador (REQ-19) ---

@test "AC-37 parse_jump sin --jump no consume argumentos" {
  parse_jump vm1 api

  [ -z "$jump_lb" ]
  [ "$jump_shift" -eq 0 ]
}

@test "AC-37 parse_jump toma el balanceador y consume dos argumentos" {
  parse_jump --jump lb1 vm1 api

  [ "$jump_lb" = "lb1" ]
  [ "$jump_shift" -eq 2 ]
}

@test "AC-37 parse_jump sale con 2 si falta el valor o no cumple la regla del nombre" {
  for args in "--jump" "--jump Lb1 vm1" "--jump lb_1 vm1" "--jump -lb vm1"; do
    # shellcheck disable=SC2086 # las palabras se separan a propósito
    run --separate-stderr parse_jump $args
    [ "$status" -eq 2 ]
    [[ "$stderr" == *"--jump inválido"* ]]
  done
  run parse_jump --jump "" vm1
  [ "$status" -eq 2 ]
  run parse_jump --jump 'lb1;x' vm1
  [ "$status" -eq 2 ]
}

@test "AC-29 guest deja pasar el 12 (balanceador apagado) sin ejecutar ssh" {
  stub_jump_topology lb1 poweroff web-a running
  vm_route web-a lb1
  vm_name=web-a

  run --separate-stderr guest 5 echo hola </dev/null

  [ "$status" -eq 12 ]
  [[ "$stderr" == *"El balanceador lb1 está apagado; enciéndalo para llegar al servidor"* ]]
  ! grep -qE '^ssh ' "$BATS_TEST_TMPDIR/calls.log" || return 1
}

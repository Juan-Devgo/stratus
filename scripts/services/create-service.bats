#!/usr/bin/env bats
# Pruebas de create-service.sh con stubs de VBoxManage y ssh: no tocan VirtualBox ni VMs.

load test_helper

# "run --separate-stderr" necesita bats 1.5 o superior.
bats_require_minimum_version 1.5.0

SCRIPT="$SERVICES_DIR/create-service.sh"

setup() {
  setup_services_stubs vm1 192.168.1.50
  CALLS="$BATS_TEST_TMPDIR/calls.log"
  REMOTE="$BATS_TEST_TMPDIR/remote.log"
  export APP_SSH_KEY_PATH="$BATS_TEST_TMPDIR/stratus_backend"
  : >"$APP_SSH_KEY_PATH"
  ARCHIVE="$BATS_TEST_TMPDIR/app.tar.gz"
  make_archive "$ARCHIVE" demian-lee templates/ static/x data/x
  setup_env_file PORT=8080 SERVER_NAME=Servidor-Produccion-1 ADMIN_PASSWORD=Secreta123
  # Argumentos del AC-1: vm name description archive executable args restart restart_sec env_file
  ARGS=(vm1 demian-lee "Servidor de producción" "$ARCHIVE" demian-lee "" on-failure 5 "$ENV_FILE")
}

# --- Estructura, validación y orden previo a la VM (T036, T037) ---

@test "AC-36 create-service.sh tiene cabecera, códigos de salida, modo estricto y secciones" {
  assert_script_structure "$SCRIPT"
}

@test "AC-30 create-service sale con 2 ante argumentos inválidos y no llama a VBoxManage ni ssh" {
  local args
  # Cantidad de argumentos.
  run "$SCRIPT" </dev/null
  [ "$status" -eq 2 ]
  run "$SCRIPT" "${ARGS[@]:0:8}" </dev/null
  [ "$status" -eq 2 ]
  run "$SCRIPT" "${ARGS[@]}" extra </dev/null
  [ "$status" -eq 2 ]
  # VM y nombre fuera de la lista permitida.
  args=("${ARGS[@]}")
  args[0]=VM_1
  run "$SCRIPT" "${args[@]}" </dev/null
  [ "$status" -eq 2 ]
  for value in Demian 1app 'app;rm' app_x '' "$(printf 'a%.0s' {1..33})"; do
    args=("${ARGS[@]}")
    args[1]="$value"
    run "$SCRIPT" "${args[@]}" </dev/null
    [ "$status" -eq 2 ]
  done
  # Archivo y variables inexistentes.
  args=("${ARGS[@]}")
  args[3]="$BATS_TEST_TMPDIR/no-existe.tar.gz"
  run "$SCRIPT" "${args[@]}" </dev/null
  [ "$status" -eq 2 ]
  args=("${ARGS[@]}")
  args[8]="$BATS_TEST_TMPDIR/no-existe"
  run "$SCRIPT" "${args[@]}" </dev/null
  [ "$status" -eq 2 ]
  # Sin la llave del backend tampoco se ejecuta nada.
  APP_SSH_KEY_PATH="" run "$SCRIPT" "${ARGS[@]}" </dev/null
  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

@test "AC-8 create-service sale con 2 ante descripción, argumentos, Restart o RestartSec inválidos" {
  local args case_ index value
  local tokens17
  tokens17="$(printf 'a %.0s' {1..16})a"
  # Cada caso: índice del argumento y valor inválido.
  local -a cases=(
    '2|50%' '2|a\b' '2|di"ce' $'2|dos\nlíneas' '2|'
    '5|a;b' '5|$HOME' '5|"x"' "5|$tokens17" '5| a' '5|a  b'
    '6|on-abort' '6|' '7|-1' '7|3601' '7|' '7|05'
    '4|../x' '4|/bin/sh'
  )
  for case_ in "${cases[@]}"; do
    index="${case_%%|*}"
    value="${case_#*|}"
    args=("${ARGS[@]}")
    args[index]="$value"
    run "$SCRIPT" "${args[@]}" </dev/null
    [ "$status" -eq 2 ]
  done
  [ ! -s "$CALLS" ]
}

@test "AC-9 create-service sale con 2 ante variables inválidas" {
  setup_env_file port=1
  run "$SCRIPT" "${ARGS[@]}" </dev/null
  [ "$status" -eq 2 ]
  setup_env_file PORT=1 PORT=2
  run "$SCRIPT" "${ARGS[@]}" </dev/null
  [ "$status" -eq 2 ]
  setup_env_file 'K=café'
  run "$SCRIPT" "${ARGS[@]}" </dev/null
  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

@test "AC-17 create-service sale con 7 si la VM no tiene IP y no ejecuta ssh" {
  setup_services_stubs vm1 -

  run --separate-stderr "$SCRIPT" "${ARGS[@]}" </dev/null

  [ "$status" -eq 7 ]
  [[ "$stderr" == *"La VM no tiene dirección IP; verifique que esté encendida"* ]]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

@test "create-service sale con 3 si VBoxManage no conoce la VM y no ejecuta ssh" {
  printf '#!/usr/bin/env bash\necho "VBoxManage $*" >>"%s"\nexit 1\n' "$CALLS" >"$BATS_TEST_TMPDIR/bin/VBoxManage"

  run --separate-stderr "$SCRIPT" "${ARGS[@]}" </dev/null

  [ "$status" -eq 3 ]
  [[ "$stderr" == *"VM no encontrada en VirtualBox"* ]]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

@test "AC-6 create-service sale con 10 ante un archivo inválido sin tocar la VM" {
  local kind args
  for kind in dotdot absolute symlink hardlink fifo corrupt; do
    make_bad_archive "$kind" "$BATS_TEST_TMPDIR/bad.tar.gz"
    args=("${ARGS[@]}")
    args[3]="$BATS_TEST_TMPDIR/bad.tar.gz"
    args[4]=app
    run --separate-stderr "$SCRIPT" "${args[@]}" </dev/null
    [ "$status" -eq 10 ]
    [[ "$stderr" == *"Archivo comprimido inválido"* ]]
  done
  [ ! -s "$CALLS" ]
}

@test "AC-7 create-service sale con 11 si el ejecutable no está en el archivo y no toca la VM" {
  local args=("${ARGS[@]}")
  args[4]=bin/app

  run --separate-stderr "$SCRIPT" "${args[@]}" </dev/null

  [ "$status" -eq 11 ]
  [[ "$stderr" == *"El ejecutable no existe en el directorio de la aplicación"* ]]
  [ ! -s "$CALLS" ]
}

@test "AC-4 create-service sale con 8 si el nombre está en uso y no cambia nada" {
  LOOKUP=exists run --separate-stderr "$SCRIPT" "${ARGS[@]}" </dev/null

  [ "$status" -eq 8 ]
  [ "$stderr" = "Ya existe un servicio o usuario con ese nombre en la VM" ]
  ! grep -qE 'useradd|install|tar|systemctl enable' "$REMOTE" || return 1
  # El programa de búsqueda revisa la unidad, el archivo de la unidad, el usuario, el grupo,
  # /opt/<name> y /etc/<name>.env, con el nombre como argumento.
  local program
  program="$(grep 'then echo exists' "$REMOTE")"
  for part in 'systemctl cat "$1.service"' '/etc/systemd/system/$1.service' 'getent passwd "$1"' \
    'getent group "$1"' '/opt/$1' '/etc/$1.env'; do
    [[ "$program" == *"$part"* ]]
  done
  [[ "$program" == *" sh demian-lee" ]]
}

# --- Creación (T039) ---

@test "AC-1 create-service ejecuta los pasos de REQ-10 en orden y entrega el archivo por stdin" {
  run --separate-stderr "$SCRIPT" "${ARGS[@]}" </dev/null

  [ "$status" -eq 0 ]
  assert_in_order \
    '^useradd --system --user-group --home-dir /opt/demian-lee --no-create-home --shell /usr/sbin/nologin demian-lee$' \
    '^install -d -m 755 -o demian-lee -g demian-lee /opt/demian-lee$' \
    '^tar -xzf - -C /opt/demian-lee --no-same-owner$' \
    '^chown -R demian-lee:demian-lee /opt/demian-lee$' \
    '^chmod u[+]x /opt/demian-lee/demian-lee$' \
    ' sh /etc/demian-lee.env 600$' \
    ' sh /etc/systemd/system/demian-lee.service 644$' \
    '^systemctl daemon-reload$' \
    '^systemctl enable --now demian-lee$'
  # El archivo llega byte a byte por la entrada estándar de la llamada de tar.
  cmp "$BATS_TEST_TMPDIR/stdin/$(line_of '^tar -xzf')" "$ARCHIVE"
  [ "$output" = $'Servicio demian-lee creado en vm1\nactive_state=active' ]
}

@test "AC-2 create-service escribe el archivo env 600 root:root con las variables codificadas" {
  run "$SCRIPT" "${ARGS[@]}" </dev/null

  [ "$status" -eq 0 ]
  local n
  n="$(line_of ' sh /etc/demian-lee.env 600$')"
  # El programa remoto deja el archivo root:root con el modo pedido y lo mueve con mv -f.
  sed -n "${n}p" "$REMOTE" | grep -qF 'chown root:root "$1.tmp" && chmod "$2" "$1.tmp" && mv -f "$1.tmp" "$1"'
  [ "$(cat "$BATS_TEST_TMPDIR/stdin/$n")" = $'PORT="8080"\nSERVER_NAME="Servidor-Produccion-1"\nADMIN_PASSWORD="Secreta123"' ]
}

@test "AC-2 create-service escribe la unidad con la marca y las líneas de REQ-11" {
  run "$SCRIPT" "${ARGS[@]}" </dev/null

  [ "$status" -eq 0 ]
  local unit
  unit="$(cat "$BATS_TEST_TMPDIR/stdin/$(line_of ' sh /etc/systemd/system/demian-lee.service 644$')")"
  [ "$(head -n 1 <<<"$unit")" = "$SERVICE_MARKER" ]
  for line in 'Description=Servidor de producción' 'After=network.target' 'Type=simple' 'User=demian-lee' \
    'Group=demian-lee' 'WorkingDirectory=/opt/demian-lee' 'EnvironmentFile=/etc/demian-lee.env' \
    'ExecStart=/opt/demian-lee/demian-lee' 'Restart=on-failure' 'RestartSec=5' 'NoNewPrivileges=true' \
    'ProtectSystem=strict' 'ProtectHome=true' 'PrivateTmp=true' 'ReadWritePaths=/opt/demian-lee' \
    'WantedBy=multi-user.target'; do
    grep -qxF "$line" <<<"$unit"
  done
}

@test "AC-2 create-service pasa los argumentos al ExecStart" {
  local args=("${ARGS[@]}")
  args[5]="--port 8080 -v"

  run "$SCRIPT" "${args[@]}" </dev/null

  [ "$status" -eq 0 ]
  grep -qxF 'ExecStart=/opt/demian-lee/demian-lee --port 8080 -v' \
    "$BATS_TEST_TMPDIR/stdin/$(line_of ' sh /etc/systemd/system/demian-lee.service 644$')"
}

@test "AC-11 Secreta123 no aparece en argv, remote.log, stdout ni stderr, solo en el stdin de ssh" {
  run --separate-stderr "$SCRIPT" "${ARGS[@]}" </dev/null
  [ "$status" -eq 0 ]
  [[ "$output" != *Secreta123* && "$stderr" != *Secreta123* ]]
  ! grep -q Secreta123 "$CALLS" "$REMOTE" || return 1
  grep -q Secreta123 "$BATS_TEST_TMPDIR/ssh-stdin.log"

  # También cuando la creación falla y se limpia.
  : >"$BATS_TEST_TMPDIR/ssh-stdin.log"
  rm -f "$BATS_TEST_TMPDIR/failed"
  FAIL_REMOTE='systemctl enable' run --separate-stderr "$SCRIPT" "${ARGS[@]}" </dev/null
  [ "$status" -eq 5 ]
  [[ "$output" != *Secreta123* && "$stderr" != *Secreta123* ]]
  ! grep -q Secreta123 "$CALLS" "$REMOTE" || return 1
}

@test "AC-14 create-service termina bien aunque la aplicación falle: espera 3 s y lee el estado" {
  local state
  for state in failed "activating (auto-restart)"; do
    : >"$CALLS"
    : >"$REMOTE"
    ACTIVE="$state" run "$SCRIPT" "${ARGS[@]}" </dev/null

    [ "$status" -eq 0 ]
    [ "$output" = "Servicio demian-lee creado en vm1"$'\n'"active_state=$state" ]
    # No hubo limpieza: ni rm, ni userdel, ni groupdel.
    ! grep -qE 'rm -rf|rm -f|userdel|groupdel|disable --now' "$REMOTE" || return 1
    # "sleep 3" va antes de la lectura del estado (la última llamada ssh).
    local sleep_line show_line
    sleep_line="$(grep -n '^sleep 3$' "$CALLS" | cut -d: -f1)"
    show_line="$(grep -n 'systemctl.show' "$CALLS" | tail -n 1 | cut -d: -f1)"
    [ -n "$sleep_line" ] && [ -n "$show_line" ] && [ "$sleep_line" -lt "$show_line" ]
    # Y después de enable --now.
    [ "$(grep -n 'enable' "$CALLS" | tail -n 1 | cut -d: -f1)" -lt "$sleep_line" ]
  done
}

@test "create-service imprime active_state=desconocido si no puede leer el estado" {
  FAIL_REMOTE='systemctl show' run "$SCRIPT" "${ARGS[@]}" </dev/null

  [ "$status" -eq 0 ]
  [ "$output" = $'Servicio demian-lee creado en vm1\nactive_state=desconocido' ]
}

# --- Limpieza (T041) ---

@test "AC-12 create-service limpia todo si falla tras crear el usuario y sale con 5" {
  FAIL_REMOTE='tar -xzf' run --separate-stderr "$SCRIPT" "${ARGS[@]}" </dev/null

  [ "$status" -eq 5 ]
  [[ "$stderr" == *"Falló el paso tar en la VM (código 5)"* ]]
  [[ "$stderr" != *"Limpieza fallida"* ]]
  # Después del paso que falló, la limpieza intenta cada cosa y verifica que no queda nada.
  assert_in_order \
    '^tar -xzf' \
    '^systemctl disable --now demian-lee$' \
    '^rm -f /etc/systemd/system/demian-lee.service .*/etc/demian-lee.env' \
    '^rm -rf /opt/demian-lee$' \
    'userdel "[$]1"' \
    'groupdel "[$]1"' \
    '^systemctl daemon-reload$' \
    '\[ ! -e "/etc/systemd/system/[$]1.service" \]'
  # No se siguió con la escritura de archivos ni con enable.
  ! grep -qE 'chown -R|enable --now|sh /etc/demian-lee.env' "$REMOTE" || return 1
}

@test "AC-12 create-service no limpia nada si falla useradd: aún no se creó nada" {
  FAIL_REMOTE='useradd' run --separate-stderr "$SCRIPT" "${ARGS[@]}" </dev/null

  [ "$status" -eq 5 ]
  ! grep -qE 'rm -rf|rm -f|userdel|groupdel|disable' "$REMOTE" || return 1
}

@test "AC-12 create-service limpia también si falla enable --now" {
  FAIL_REMOTE='enable --now' run --separate-stderr "$SCRIPT" "${ARGS[@]}" </dev/null

  [ "$status" -eq 5 ]
  assert_in_order '^systemctl enable --now' '^systemctl disable --now demian-lee$' '^rm -rf /opt/demian-lee$' 'userdel'
}

@test "AC-13 create-service sale con 9 si la limpieza también falla y deja los dos errores en stderr" {
  FAIL_REMOTE='tar -xzf' FAIL_RESTORE='userdel' run --separate-stderr "$SCRIPT" "${ARGS[@]}" </dev/null

  [ "$status" -eq 9 ]
  [[ "$stderr" == *"Falló el paso tar en la VM (código 5)"* ]]
  [[ "$stderr" == *"Limpieza fallida; puede quedar en la VM: /etc/systemd/system/demian-lee.service, /etc/demian-lee.env, /opt/demian-lee, usuario y grupo demian-lee"* ]]
  # Cada paso se intenta aunque uno falle: groupdel, daemon-reload y la verificación corren igual.
  assert_in_order 'userdel' 'groupdel' '^systemctl daemon-reload$' '\[ ! -e "/etc/systemd/system/[$]1.service" \]'
}

@test "AC-13 create-service sale con 9 si la verificación final encuentra restos" {
  FAIL_REMOTE='tar -xzf' FAIL_RESTORE='\[ ! -e' run --separate-stderr "$SCRIPT" "${ARGS[@]}" </dev/null

  [ "$status" -eq 9 ]
  [[ "$stderr" == *"Limpieza fallida; puede quedar en la VM:"* ]]
}

@test "AC-11 create-service no deja valores en stderr cuando la limpieza falla" {
  FAIL_REMOTE='tar -xzf' FAIL_RESTORE='userdel' run --separate-stderr "$SCRIPT" "${ARGS[@]}" </dev/null

  [ "$status" -eq 9 ]
  [[ "$output" != *Secreta123* && "$stderr" != *Secreta123* ]]
  ! grep -q Secreta123 "$CALLS" "$REMOTE" || return 1
}

@test "AC-37 create-service con --jump entra al servidor por el balanceador" {
  ARGS[0]=web-a
  stub_jump_topology lb1 running web-a running

  run --separate-stderr "$SCRIPT" --jump lb1 web-a "${ARGS[@]:1}" </dev/null

  [ "$status" -eq 0 ]
  assert_routed_ssh lb1 web-a
}

@test "AC-37 create-service sale con 2 si --jump no trae un balanceador válido y no llama a nada" {
  ARGS[0]=web-a
  stub_jump_topology lb1 running web-a running

  run "$SCRIPT" --jump </dev/null
  [ "$status" -eq 2 ]
  run "$SCRIPT" --jump Lb1 web-a "${ARGS[@]:1}" </dev/null
  [ "$status" -eq 2 ]
  run "$SCRIPT" --jump "" web-a "${ARGS[@]:1}" </dev/null
  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

@test "AC-29 create-service con --jump sale con 12 si el balanceador está apagado y no llama a ssh" {
  ARGS[0]=web-a
  stub_jump_topology lb1 poweroff web-a running

  run --separate-stderr "$SCRIPT" --jump lb1 web-a "${ARGS[@]:1}" </dev/null

  [ "$status" -eq 12 ]
  [[ "$stderr" == *"El balanceador lb1 está apagado; enciéndalo para llegar al servidor"* ]]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

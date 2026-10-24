#!/usr/bin/env bats
# Pruebas de redeploy-service.sh con stubs de VBoxManage y ssh: no tocan VirtualBox ni VMs.

load test_helper

# "run --separate-stderr" necesita bats 1.5 o superior.
bats_require_minimum_version 1.5.0

SCRIPT="$SERVICES_DIR/redeploy-service.sh"
BACKUP=/var/lib/stratus/backup/demian-lee
OLD=/opt/.stratus-backup-demian-lee
# Programa que devuelve el contenido anterior a /opt/<name> solo si existe la copia (ver la sección 4).
SWAP_BACK='rm -rf "[$]1" && mv "[$]2" "[$]1"'

setup() {
  setup_services_stubs vm1 192.168.1.50
  CALLS="$BATS_TEST_TMPDIR/calls.log"
  REMOTE="$BATS_TEST_TMPDIR/remote.log"
  export APP_SSH_KEY_PATH="$BATS_TEST_TMPDIR/stratus_backend"
  : >"$APP_SSH_KEY_PATH"
  ARCHIVE="$BATS_TEST_TMPDIR/nuevo.tar.gz"
  make_archive "$ARCHIVE" demian-lee bin/server static/x
  # Argumentos: vm name archive_file executable
  ARGS=(vm1 demian-lee "$ARCHIVE" demian-lee)
}

# --- Estructura, validación y orden previo a los cambios (T094) ---

@test "AC-36 redeploy-service.sh tiene cabecera, códigos de salida, modo estricto y secciones" {
  assert_script_structure "$SCRIPT"
}

@test "AC-30 redeploy-service sale con 2 ante argumentos inválidos y no llama a VBoxManage ni ssh" {
  local args
  run "$SCRIPT" </dev/null
  [ "$status" -eq 2 ]
  run "$SCRIPT" "${ARGS[@]:0:3}" </dev/null
  [ "$status" -eq 2 ]
  run "$SCRIPT" "${ARGS[@]}" extra </dev/null
  [ "$status" -eq 2 ]
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
  # Archivo inexistente y ejecutables fuera de la lista permitida.
  args=("${ARGS[@]}")
  args[2]="$BATS_TEST_TMPDIR/no-existe.tar.gz"
  run "$SCRIPT" "${args[@]}" </dev/null
  [ "$status" -eq 2 ]
  for value in ../x /bin/sh a/./b '' 'a b'; do
    args=("${ARGS[@]}")
    args[3]="$value"
    run "$SCRIPT" "${args[@]}" </dev/null
    [ "$status" -eq 2 ]
  done
  APP_SSH_KEY_PATH="" run "$SCRIPT" "${ARGS[@]}" </dev/null
  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

@test "AC-6 redeploy-service sale con 10 ante un archivo inválido sin tocar la VM" {
  local kind args
  for kind in dotdot absolute symlink hardlink fifo corrupt; do
    make_bad_archive "$kind" "$BATS_TEST_TMPDIR/bad.tar.gz"
    args=("${ARGS[@]}")
    args[2]="$BATS_TEST_TMPDIR/bad.tar.gz"
    args[3]=app
    run --separate-stderr "$SCRIPT" "${args[@]}" </dev/null
    [ "$status" -eq 10 ]
    [[ "$stderr" == *"Archivo comprimido inválido"* ]]
  done
  [ ! -s "$CALLS" ]
}

@test "AC-7 redeploy-service sale con 11 si el ejecutable no está en el archivo nuevo y no toca la VM" {
  local args=("${ARGS[@]}")
  args[3]=bin/otro

  run --separate-stderr "$SCRIPT" "${args[@]}" </dev/null

  [ "$status" -eq 11 ]
  [[ "$stderr" == *"El ejecutable no existe en el directorio de la aplicación"* ]]
  [ ! -s "$CALLS" ]
}

@test "AC-17 redeploy-service sale con 7 si la VM no tiene IP y no ejecuta ssh" {
  setup_services_stubs vm1 -

  run --separate-stderr "$SCRIPT" "${ARGS[@]}" </dev/null

  [ "$status" -eq 7 ]
  [[ "$stderr" == *"La VM no tiene dirección IP; verifique que esté encendida"* ]]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

@test "redeploy-service sale con 3 si VBoxManage no conoce la VM y no ejecuta ssh" {
  printf '#!/usr/bin/env bash\necho "VBoxManage $*" >>"%s"\nexit 1\n' "$CALLS" >"$BATS_TEST_TMPDIR/bin/VBoxManage"

  run --separate-stderr "$SCRIPT" "${ARGS[@]}" </dev/null

  [ "$status" -eq 3 ]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

@test "AC-20 redeploy-service rechaza una unidad sin la marca con 6 y no cambia nada" {
  local args=("${ARGS[@]}")
  args[1]=ssh

  MANAGED=no run --separate-stderr "$SCRIPT" "${args[@]}" </dev/null

  [ "$status" -eq 6 ]
  [ "$stderr" = "Solo se pueden gestionar servicios creados por Stratus" ]
  [ "$(wc -l <"$REMOTE")" -eq 1 ]
  grep -q 'grep -qxF' "$REMOTE"
}

# --- Redespliegue (T096) ---

@test "AC-26 redeploy-service reemplaza el contenido en orden y respeta el estado activo" {
  ACTIVE=active run --separate-stderr "$SCRIPT" "${ARGS[@]}" </dev/null

  [ "$status" -eq 0 ]
  assert_in_order \
    'grep -qxF' \
    'ExecStart=' \
    'systemctl show' \
    "^rm -rf $BACKUP $OLD\$" \
    "^mkdir -p -m 700 $BACKUP\$" \
    "^cp -p /etc/systemd/system/demian-lee.service $BACKUP/\$" \
    '^systemctl stop demian-lee$' \
    "^mv /opt/demian-lee $OLD\$" \
    '^install -d -m 755 -o demian-lee -g demian-lee /opt/demian-lee$' \
    '^tar -xzf - -C /opt/demian-lee --no-same-owner$' \
    '^chown -R demian-lee:demian-lee /opt/demian-lee$' \
    '^chmod u[+]x /opt/demian-lee/demian-lee$' \
    '^systemctl start demian-lee$' \
    "^rm -rf $BACKUP $OLD\$" \
    'systemctl show'
  cmp "$BATS_TEST_TMPDIR/stdin/$(line_of '^tar -xzf')" "$ARCHIVE"
  # El mismo ejecutable no reescribe la unidad ni recarga systemd.
  ! grep -qE 'daemon-reload| sh /etc/systemd/system/demian-lee.service 644' "$REMOTE" || return 1
  [ "$output" = $'Servicio demian-lee redesplegado en vm1\nactive_state=active' ]
}

@test "AC-26 redeploy-service sobre un servicio inactivo no lo detiene ni lo inicia" {
  ACTIVE=inactive run "$SCRIPT" "${ARGS[@]}" </dev/null

  [ "$status" -eq 0 ]
  assert_in_order "^mv /opt/demian-lee $OLD\$" '^tar -xzf' '^chown -R' "^rm -rf $BACKUP $OLD\$"
  ! grep -qE 'systemctl (stop|start|restart)' "$REMOTE" || return 1
  [ "$output" = "Servicio demian-lee redesplegado en vm1" ]
  ! grep -q '^sleep' "$CALLS" || return 1
}

@test "AC-43 redeploy-service detiene antes de reemplazar y vuelve a iniciar un servicio en activating (auto-restart)" {
  ACTIVE="activating (auto-restart)" run "$SCRIPT" "${ARGS[@]}" </dev/null

  [ "$status" -eq 0 ]
  assert_in_order '^systemctl stop demian-lee$' "^mv /opt/demian-lee $OLD\$" '^tar -xzf' '^systemctl start demian-lee$'
  [[ "$output" == *"active_state=activating (auto-restart)" ]]
}

@test "AC-38 redeploy-service cambia ExecStart conservando los argumentos y recarga systemd" {
  local args=("${ARGS[@]}")
  args[3]=bin/server

  UNIT_FIELDS=$'Servidor de producción\tdemian-lee\t--port 8080\ton-failure\t5' \
    run "$SCRIPT" "${args[@]}" </dev/null

  [ "$status" -eq 0 ]
  assert_in_order '^chown -R' '^chmod u[+]x /opt/demian-lee/bin/server$' ' sh /etc/systemd/system/demian-lee.service 644$' \
    '^systemctl daemon-reload$' '^systemctl start demian-lee$'
  local unit
  unit="$(cat "$BATS_TEST_TMPDIR/stdin/$(line_of ' sh /etc/systemd/system/demian-lee.service 644$')")"
  grep -qxF 'ExecStart=/opt/demian-lee/bin/server --port 8080' <<<"$unit"
  grep -qxF 'Description=Servidor de producción' <<<"$unit"
  grep -qxF 'Restart=on-failure' <<<"$unit"
  grep -qxF 'RestartSec=5' <<<"$unit"
}

@test "AC-38 redeploy-service con argumentos vacíos escribe ExecStart sin argumentos" {
  local args=("${ARGS[@]}")
  args[3]=bin/server

  run "$SCRIPT" "${args[@]}" </dev/null

  [ "$status" -eq 0 ]
  grep -qxF 'ExecStart=/opt/demian-lee/bin/server' \
    "$BATS_TEST_TMPDIR/stdin/$(line_of ' sh /etc/systemd/system/demian-lee.service 644$')"
}

@test "AC-26 redeploy-service espera 3 s tras iniciar y muestra el estado si la app falla" {
  ACTIVE=active ACTIVE_AFTER=failed run "$SCRIPT" "${ARGS[@]}" </dev/null

  [ "$status" -eq 0 ]
  [ "$output" = $'Servicio demian-lee redesplegado en vm1\nactive_state=failed' ]
  [ "$(grep -n '^sleep 3$' "$CALLS" | cut -d: -f1)" -gt "$(grep -n 'start' "$CALLS" | tail -n 1 | cut -d: -f1)" ]
}

# --- Restauración (T098) ---

@test "AC-27 redeploy-service restaura contenido, unidad y estado si falla chown tras detener" {
  FAIL_REMOTE='chown -R' ACTIVE=active run --separate-stderr "$SCRIPT" "${ARGS[@]}" </dev/null

  [ "$status" -eq 5 ]
  [[ "$stderr" == *"Falló el paso chown en la VM (código 5)"* ]]
  [[ "$stderr" != *"Restauración fallida"* ]]
  assert_in_order \
    '^chown -R' \
    '^systemctl stop demian-lee$' \
    "$SWAP_BACK" \
    "^cp -p $BACKUP/demian-lee.service /etc/systemd/system/\$" \
    '^systemctl daemon-reload$' \
    '^systemctl start demian-lee$' \
    "^rm -rf $BACKUP $OLD\$"
}

@test "AC-27 redeploy-service restaura también si falla systemctl start" {
  FAIL_REMOTE='systemctl start' ACTIVE=active run --separate-stderr "$SCRIPT" "${ARGS[@]}" </dev/null

  [ "$status" -eq 5 ]
  assert_in_order '^systemctl start demian-lee$' "$SWAP_BACK" '^systemctl daemon-reload$' '^systemctl start demian-lee$' "^rm -rf $BACKUP $OLD\$"
}

@test "AC-27 redeploy-service restaura un servicio inactivo sin iniciarlo" {
  FAIL_REMOTE='chown -R' ACTIVE=inactive run "$SCRIPT" "${ARGS[@]}" </dev/null

  [ "$status" -eq 5 ]
  assert_in_order '^chown -R' "$SWAP_BACK" '^systemctl daemon-reload$' "^rm -rf $BACKUP $OLD\$"
  ! grep -q 'systemctl start' "$REMOTE" || return 1
}

@test "AC-27 redeploy-service con una app nueva que falla al iniciar termina bien y conserva el contenido nuevo" {
  ACTIVE=active ACTIVE_AFTER=failed run "$SCRIPT" "${ARGS[@]}" </dev/null

  [ "$status" -eq 0 ]
  [[ "$output" == *"active_state=failed" ]]
  ! grep -q "$SWAP_BACK" "$REMOTE" || return 1
}

@test "AC-27 el programa de restauración solo reemplaza /opt/<name> si existe la copia" {
  FAIL_REMOTE='systemctl stop' ACTIVE=active run "$SCRIPT" "${ARGS[@]}" </dev/null
  [ "$status" -eq 5 ]
  local program
  program="$(sed -n 's/^sh -c \(.*\) sh \/opt\/demian-lee \/opt\/.stratus-backup-demian-lee$/\1/p' "$REMOTE")"
  [ -n "$program" ]

  # Sin copia (falló antes del mv): el contenido vivo se conserva.
  local live="$BATS_TEST_TMPDIR/live" old="$BATS_TEST_TMPDIR/old"
  mkdir -p "$live" && echo vivo >"$live/archivo"
  run sh -c "$program" sh "$live" "$old"
  [ "$status" -eq 0 ]
  [ "$(cat "$live/archivo")" = vivo ]

  # Con copia: vuelve el contenido anterior y desaparece el nuevo.
  mkdir -p "$old" && echo anterior >"$old/archivo"
  run sh -c "$program" sh "$live" "$old"
  [ "$status" -eq 0 ]
  [ "$(cat "$live/archivo")" = anterior ]
  [ ! -e "$old" ]
}

@test "AC-39 redeploy-service restaura la unidad anterior si el ejecutable cambió y el inicio falla" {
  local args=("${ARGS[@]}")
  args[3]=bin/server

  FAIL_REMOTE='systemctl start' ACTIVE=active UNIT_FIELDS=$'Servidor\tdemian-lee\t--port 8080\ton-failure\t5' \
    run --separate-stderr "$SCRIPT" "${args[@]}" </dev/null

  [ "$status" -eq 5 ]
  assert_in_order \
    ' sh /etc/systemd/system/demian-lee.service 644$' \
    '^systemctl start demian-lee$' \
    "$SWAP_BACK" \
    "^cp -p $BACKUP/demian-lee.service /etc/systemd/system/\$" \
    '^systemctl daemon-reload$' \
    '^systemctl start demian-lee$' \
    "^rm -rf $BACKUP $OLD\$"
}

@test "AC-27 redeploy-service sale con 9 si la restauración también falla y deja los dos errores" {
  FAIL_REMOTE='chown -R' FAIL_RESTORE='cp -p' ACTIVE=active run --separate-stderr "$SCRIPT" "${ARGS[@]}" </dev/null

  [ "$status" -eq 9 ]
  [[ "$stderr" == *"Falló el paso chown en la VM (código 5)"* ]]
  [[ "$stderr" == *"Restauración fallida; puede quedar en la VM: /opt/demian-lee, $OLD, /etc/systemd/system/demian-lee.service, $BACKUP"* ]]
  # Cada paso se intenta aunque uno falle.
  assert_in_order "$SWAP_BACK" '^systemctl daemon-reload$' '^systemctl start demian-lee$' "^rm -rf $BACKUP $OLD\$"
}

@test "redeploy-service sale con 5 sin restaurar si no puede borrar las copias" {
  # El segundo "rm -rf" de las copias es el del paso final; el primero es la limpieza previa.
  FAIL_REMOTE="^rm -rf $BACKUP" FAIL_SKIP=1 run --separate-stderr "$SCRIPT" "${ARGS[@]}" </dev/null

  [ "$status" -eq 5 ]
  [[ "$stderr" == *"No se pudieron borrar las copias de la VM"* ]]
  ! grep -q "$SWAP_BACK" "$REMOTE" || return 1
}

@test "redeploy-service no restaura si falla antes de terminar la copia de la unidad y la borra" {
  FAIL_REMOTE='^cp -p /etc/systemd' run --separate-stderr "$SCRIPT" "${ARGS[@]}" </dev/null

  [ "$status" -eq 5 ]
  assert_in_order "^cp -p /etc/systemd/system/demian-lee.service" "^rm -rf $BACKUP $OLD\$"
  ! grep -qE 'systemctl (stop|start)|^mv |tar -xzf' "$REMOTE" || return 1
}

@test "AC-37 redeploy-service con --jump entra al servidor por el balanceador" {
  ARGS[0]=web-a
  stub_jump_topology lb1 running web-a running

  run --separate-stderr "$SCRIPT" --jump lb1 web-a "${ARGS[@]:1}" </dev/null

  [ "$status" -eq 0 ]
  assert_routed_ssh lb1 web-a
}

@test "AC-37 redeploy-service sale con 2 si --jump no trae un balanceador válido y no llama a nada" {
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

@test "AC-29 redeploy-service con --jump sale con 12 si el balanceador está apagado y no llama a ssh" {
  ARGS[0]=web-a
  stub_jump_topology lb1 poweroff web-a running

  run --separate-stderr "$SCRIPT" --jump lb1 web-a "${ARGS[@]:1}" </dev/null

  [ "$status" -eq 12 ]
  [[ "$stderr" == *"El balanceador lb1 está apagado; enciéndalo para llegar al servidor"* ]]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

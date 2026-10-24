#!/usr/bin/env bats
# Pruebas de edit-service.sh con stubs de VBoxManage y ssh: no tocan VirtualBox ni VMs.

load test_helper

# "run --separate-stderr" necesita bats 1.5 o superior.
bats_require_minimum_version 1.5.0

SCRIPT="$SERVICES_DIR/edit-service.sh"
BACKUP=/var/lib/stratus/backup/demian-lee

setup() {
  setup_services_stubs vm1 192.168.1.50
  CALLS="$BATS_TEST_TMPDIR/calls.log"
  REMOTE="$BATS_TEST_TMPDIR/remote.log"
  export APP_SSH_KEY_PATH="$BATS_TEST_TMPDIR/stratus_backend"
  : >"$APP_SSH_KEY_PATH"
  # Argumentos: vm name description executable args restart restart_sec env_file ("-" conserva el env).
  ARGS=(vm1 demian-lee "Servidor de producción" demian-lee "" always 5 -)
}

# --- Estructura, validación y orden previo a los cambios (T080) ---

@test "AC-36 edit-service.sh tiene cabecera, códigos de salida, modo estricto y secciones" {
  assert_script_structure "$SCRIPT"
}

@test "AC-30 edit-service sale con 2 ante argumentos inválidos y no llama a VBoxManage ni ssh" {
  local args
  run "$SCRIPT" </dev/null
  [ "$status" -eq 2 ]
  run "$SCRIPT" "${ARGS[@]:0:7}" </dev/null
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
  # Un archivo de variables que no existe (y que no es "-").
  args=("${ARGS[@]}")
  args[7]="$BATS_TEST_TMPDIR/no-existe"
  run "$SCRIPT" "${args[@]}" </dev/null
  [ "$status" -eq 2 ]
  APP_SSH_KEY_PATH="" run "$SCRIPT" "${ARGS[@]}" </dev/null
  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

@test "AC-8 edit-service sale con 2 ante descripción, argumentos, Restart o RestartSec inválidos" {
  local args case_ index value tokens17
  tokens17="$(printf 'a %.0s' {1..16})a"
  local -a cases=(
    '2|50%' '2|a\b' '2|di"ce' $'2|dos\nlíneas' '2|'
    '4|a;b' '4|$HOME' '4|"x"' "4|$tokens17"
    '5|on-abort' '5|' '6|-1' '6|3601' '6|'
    '3|../x' '3|/bin/sh'
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

@test "AC-9 edit-service sale con 2 ante variables inválidas" {
  setup_env_file port=1
  local args=("${ARGS[@]}")
  args[7]="$ENV_FILE"

  run "$SCRIPT" "${args[@]}" </dev/null

  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

@test "AC-17 edit-service sale con 7 si la VM no tiene IP y no ejecuta ssh" {
  setup_services_stubs vm1 -

  run --separate-stderr "$SCRIPT" "${ARGS[@]}" </dev/null

  [ "$status" -eq 7 ]
  [[ "$stderr" == *"La VM no tiene dirección IP; verifique que esté encendida"* ]]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

@test "edit-service sale con 3 si VBoxManage no conoce la VM y no ejecuta ssh" {
  printf '#!/usr/bin/env bash\necho "VBoxManage $*" >>"%s"\nexit 1\n' "$CALLS" >"$BATS_TEST_TMPDIR/bin/VBoxManage"

  run --separate-stderr "$SCRIPT" "${ARGS[@]}" </dev/null

  [ "$status" -eq 3 ]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

@test "AC-20 edit-service rechaza una unidad sin la marca con 6 y no cambia nada" {
  local args=("${ARGS[@]}")
  args[1]=ssh

  MANAGED=no run --separate-stderr "$SCRIPT" "${args[@]}" </dev/null

  [ "$status" -eq 6 ]
  [ "$stderr" = "Solo se pueden gestionar servicios creados por Stratus" ]
  [ "$(wc -l <"$REMOTE")" -eq 1 ]
  grep -q 'grep -qxF' "$REMOTE"
}

@test "AC-7 edit-service sale con 11 si el ejecutable no está en /opt/<name> y no hace respaldo ni escribe" {
  local args=("${ARGS[@]}")
  args[3]=bin/app

  EXE_PRESENT=no run --separate-stderr "$SCRIPT" "${args[@]}" </dev/null

  [ "$status" -eq 11 ]
  [ "$stderr" = "El ejecutable no existe en el directorio de la aplicación" ]
  # Solo la marca y la comprobación del ejecutable (con el nombre y la ruta como argumentos).
  [ "$(wc -l <"$REMOTE")" -eq 2 ]
  sed -n 2p "$REMOTE" | grep -qF '[ -f "/opt/$1/$2" ]'
  sed -n 2p "$REMOTE" | grep -qF ' sh demian-lee bin/app'
  ! grep -qE 'backup|chmod|daemon-reload|restart' "$REMOTE" || return 1
}

# --- Edición (T082) ---

@test "AC-23 edit-service respalda, reescribe la unidad con Restart=always, recarga y reinicia si estaba activo" {
  ACTIVE=active run --separate-stderr "$SCRIPT" "${ARGS[@]}" </dev/null

  [ "$status" -eq 0 ]
  assert_in_order \
    'grep -qxF' \
    '\[ -f "/opt/[$]1/[$]2" \]' \
    'systemctl show' \
    "^rm -rf $BACKUP\$" \
    "^mkdir -p -m 700 $BACKUP\$" \
    "^cp -p /etc/systemd/system/demian-lee.service $BACKUP/\$" \
    'cp -p "[$]1" "[$]2" sh /etc/demian-lee.env '"$BACKUP"'/$' \
    '^chmod u[+]x /opt/demian-lee/demian-lee$' \
    ' sh /etc/systemd/system/demian-lee.service 644$' \
    '^systemctl daemon-reload$' \
    '^systemctl restart demian-lee$' \
    "^rm -rf $BACKUP\$" \
    'systemctl show'
  # La unidad nueva lleva Restart=always y la marca; el env y el estado habilitado no se tocan.
  local unit
  unit="$(cat "$BATS_TEST_TMPDIR/stdin/$(line_of ' sh /etc/systemd/system/demian-lee.service 644$')")"
  grep -qxF 'Restart=always' <<<"$unit"
  [ "$(head -n 1 <<<"$unit")" = "$SERVICE_MARKER" ]
  ! grep -qE 'sh /etc/demian-lee.env 600|enable|disable' "$REMOTE" || return 1
  [ "$output" = $'Servicio demian-lee actualizado en vm1\nactive_state=active' ]
}

@test "AC-24 edit-service sobre un servicio inactivo reescribe la unidad pero no lo inicia" {
  ACTIVE=inactive run --separate-stderr "$SCRIPT" "${ARGS[@]}" </dev/null

  [ "$status" -eq 0 ]
  assert_in_order ' sh /etc/systemd/system/demian-lee.service 644$' '^systemctl daemon-reload$' "^rm -rf $BACKUP\$"
  ! grep -qE 'systemctl (restart|start|stop)' "$REMOTE" || return 1
  [ "$output" = "Servicio demian-lee actualizado en vm1" ]
  ! grep -q '^sleep' "$CALLS" || return 1
}

@test "AC-43 edit-service trata activating (auto-restart) como en ejecución y reinicia" {
  ACTIVE="activating (auto-restart)" run "$SCRIPT" "${ARGS[@]}" </dev/null

  [ "$status" -eq 0 ]
  assert_in_order '^systemctl daemon-reload$' '^systemctl restart demian-lee$'
  [[ "$output" == *"active_state=activating (auto-restart)" ]]
}

@test "AC-23 edit-service espera 3 s tras reiniciar y muestra el estado si la app falla" {
  ACTIVE=active ACTIVE_AFTER=failed run "$SCRIPT" "${ARGS[@]}" </dev/null

  [ "$status" -eq 0 ]
  [ "$output" = $'Servicio demian-lee actualizado en vm1\nactive_state=failed' ]
  [ "$(grep -n '^sleep 3$' "$CALLS" | cut -d: -f1)" -gt "$(grep -n 'restart' "$CALLS" | tail -n 1 | cut -d: -f1)" ]
}

@test "AC-25 edit-service reescribe el env completo con 600 root:root y solo las variables nuevas" {
  setup_env_file PORT=9090
  local args=("${ARGS[@]}")
  args[7]="$ENV_FILE"

  run "$SCRIPT" "${args[@]}" </dev/null

  [ "$status" -eq 0 ]
  local n
  n="$(line_of ' sh /etc/demian-lee.env 600$')"
  sed -n "${n}p" "$REMOTE" | grep -qF 'chown root:root "$1.tmp" && chmod "$2" "$1.tmp" && mv -f "$1.tmp" "$1"'
  [ "$(cat "$BATS_TEST_TMPDIR/stdin/$n")" = 'PORT="9090"' ]
  # El env se escribe después de la unidad y antes de daemon-reload.
  assert_in_order ' sh /etc/systemd/system/demian-lee.service 644$' ' sh /etc/demian-lee.env 600$' '^systemctl daemon-reload$'
}

@test "AC-11 Secreta123 no aparece en argv, remote.log, stdout ni stderr, solo en el stdin de ssh" {
  setup_env_file ADMIN_PASSWORD=Secreta123
  local args=("${ARGS[@]}")
  args[7]="$ENV_FILE"

  run --separate-stderr "$SCRIPT" "${args[@]}" </dev/null
  [ "$status" -eq 0 ]
  [[ "$output" != *Secreta123* && "$stderr" != *Secreta123* ]]
  ! grep -q Secreta123 "$CALLS" "$REMOTE" || return 1
  grep -q Secreta123 "$BATS_TEST_TMPDIR/ssh-stdin.log"

  # También cuando la edición falla y se restaura.
  rm -f "$BATS_TEST_TMPDIR/failed"
  FAIL_REMOTE='systemctl restart' run --separate-stderr "$SCRIPT" "${args[@]}" </dev/null
  [ "$status" -eq 5 ]
  [[ "$output" != *Secreta123* && "$stderr" != *Secreta123* ]]
  ! grep -q Secreta123 "$CALLS" "$REMOTE" || return 1
}

# --- Restauración (T084) ---

@test "AC-40 edit-service restaura unidad y env, recarga, reinicia y borra el respaldo si el reinicio falla" {
  FAIL_REMOTE='systemctl restart' ACTIVE=active run --separate-stderr "$SCRIPT" "${ARGS[@]}" </dev/null

  [ "$status" -eq 5 ]
  [[ "$stderr" == *"Falló el paso systemctl en la VM (código 5)"* ]]
  [[ "$stderr" != *"Restauración fallida"* ]]
  # Después del reinicio que falló: copia de vuelta de la unidad y del env, recarga, reinicio previo
  # (estaba activo) y borrado del respaldo.
  assert_in_order \
    '^systemctl restart demian-lee$' \
    "^cp -p $BACKUP/demian-lee.service /etc/systemd/system/\$" \
    'cp -p "[$]1" "[$]2" sh '"$BACKUP"'/demian-lee.env /etc/$' \
    '^systemctl daemon-reload$' \
    '^systemctl restart demian-lee$' \
    "^rm -rf $BACKUP\$"
  [ "$(grep -c '^rm -rf' "$REMOTE")" -eq 2 ]
}

@test "AC-40 edit-service restaura también si daemon-reload falla (una vez)" {
  FAIL_REMOTE='daemon-reload' ACTIVE=active run --separate-stderr "$SCRIPT" "${ARGS[@]}" </dev/null

  [ "$status" -eq 5 ]
  assert_in_order '^systemctl daemon-reload$' "^cp -p $BACKUP/demian-lee.service" '^systemctl daemon-reload$' '^systemctl restart demian-lee$' "^rm -rf $BACKUP\$"
}

@test "AC-40 edit-service detiene el servicio al restaurar si estaba inactivo" {
  FAIL_REMOTE='daemon-reload' ACTIVE=inactive run "$SCRIPT" "${ARGS[@]}" </dev/null

  [ "$status" -eq 5 ]
  assert_in_order "^cp -p $BACKUP/demian-lee.service" '^systemctl daemon-reload$' '^systemctl stop demian-lee$' "^rm -rf $BACKUP\$"
  ! grep -q 'systemctl restart' "$REMOTE" || return 1
}

@test "AC-40 edit-service sale con 9 si la restauración también falla y deja los dos errores" {
  FAIL_REMOTE='systemctl restart' FAIL_RESTORE='cp -p' ACTIVE=active run --separate-stderr "$SCRIPT" "${ARGS[@]}" </dev/null

  [ "$status" -eq 9 ]
  [[ "$stderr" == *"Falló el paso systemctl en la VM (código 5)"* ]]
  [[ "$stderr" == *"Restauración fallida; puede quedar en la VM: /etc/systemd/system/demian-lee.service, /etc/demian-lee.env, $BACKUP"* ]]
  # Cada paso se intenta aunque uno falle: recarga, reinicio y borrado del respaldo corren igual.
  assert_in_order "^cp -p $BACKUP/demian-lee.service" '^systemctl daemon-reload$' '^systemctl restart demian-lee$' "^rm -rf $BACKUP\$"
}

@test "AC-40 edit-service con nuevos argumentos que hacen fallar la app termina bien con el estado" {
  ACTIVE=active ACTIVE_AFTER=failed run "$SCRIPT" "${ARGS[@]}" </dev/null

  [ "$status" -eq 0 ]
  [[ "$output" == *"active_state=failed" ]]
  ! grep -q 'cp -p /var/lib' "$REMOTE" || return 1
}

@test "edit-service sale con 5 sin restaurar si no puede borrar el respaldo" {
  # El segundo "rm -rf" del respaldo es el del paso final; el primero es la limpieza previa.
  FAIL_REMOTE='rm -rf /var/lib/stratus' FAIL_SKIP=1 run --separate-stderr "$SCRIPT" "${ARGS[@]}" </dev/null

  [ "$status" -eq 5 ]
  [[ "$stderr" == *"No se pudo borrar el respaldo $BACKUP"* ]]
  # La edición ya se aplicó: no hay copia de vuelta.
  ! grep -qE "^cp -p $BACKUP" "$REMOTE" || return 1
}

@test "edit-service no restaura si falla antes de terminar el respaldo y borra lo que alcanzó a crear" {
  FAIL_REMOTE='^cp -p /etc/systemd' run --separate-stderr "$SCRIPT" "${ARGS[@]}" </dev/null

  [ "$status" -eq 5 ]
  assert_in_order "^mkdir -p -m 700 $BACKUP\$" "^cp -p /etc/systemd/system/demian-lee.service" "^rm -rf $BACKUP\$"
  ! grep -qE 'daemon-reload|\.service 644' "$REMOTE" || return 1
}

@test "AC-37 edit-service con --jump entra al servidor por el balanceador" {
  ARGS[0]=web-a
  stub_jump_topology lb1 running web-a running

  run --separate-stderr "$SCRIPT" --jump lb1 web-a "${ARGS[@]:1}" </dev/null

  [ "$status" -eq 0 ]
  assert_routed_ssh lb1 web-a
}

@test "AC-37 edit-service sale con 2 si --jump no trae un balanceador válido y no llama a nada" {
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

@test "AC-29 edit-service con --jump sale con 12 si el balanceador está apagado y no llama a ssh" {
  ARGS[0]=web-a
  stub_jump_topology lb1 poweroff web-a running

  run --separate-stderr "$SCRIPT" --jump lb1 web-a "${ARGS[@]:1}" </dev/null

  [ "$status" -eq 12 ]
  [[ "$stderr" == *"El balanceador lb1 está apagado; enciéndalo para llegar al servidor"* ]]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

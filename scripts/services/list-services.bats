#!/usr/bin/env bats
# Pruebas de list-services.sh con stubs de VBoxManage, ssh y systemctl: no tocan VirtualBox ni VMs.

load test_helper

# "run --separate-stderr" necesita bats 1.5 o superior.
bats_require_minimum_version 1.5.0

SCRIPT="$SERVICES_DIR/list-services.sh"

setup() {
  setup_services_stubs vm1 192.168.1.50
  CALLS="$BATS_TEST_TMPDIR/calls.log"
  export APP_SSH_KEY_PATH="$BATS_TEST_TMPDIR/stratus_backend"
  : >"$APP_SSH_KEY_PATH"
}

@test "AC-36 list-services.sh tiene cabecera, códigos de salida, modo estricto y secciones" {
  assert_script_structure "$SCRIPT"
}

@test "AC-30 list-services sale con 2 ante argumentos inválidos y no llama a VBoxManage ni ssh" {
  run "$SCRIPT" </dev/null
  [ "$status" -eq 2 ]
  run "$SCRIPT" vm1 extra </dev/null
  [ "$status" -eq 2 ]
  run "$SCRIPT" VM_1 </dev/null
  [ "$status" -eq 2 ]
  run "$SCRIPT" "-vm" </dev/null
  [ "$status" -eq 2 ]
  # Sin la llave del backend tampoco se ejecuta nada.
  APP_SSH_KEY_PATH="" run "$SCRIPT" vm1 </dev/null
  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

@test "AC-17 list-services sale con 7 si la VM no tiene IP y no ejecuta ssh" {
  setup_services_stubs vm1 -

  run --separate-stderr "$SCRIPT" vm1 </dev/null

  [ "$status" -eq 7 ]
  [[ "$stderr" == *"La VM no tiene dirección IP; verifique que esté encendida"* ]]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

@test "list-services sale con 3 si VBoxManage no conoce la VM y no ejecuta ssh" {
  # VBoxManage falso que no conoce ninguna VM: se reemplaza el cuerpo del stub.
  printf '#!/usr/bin/env bash\necho "VBoxManage $*" >>"%s"\nexit 1\n' "$CALLS" >"$BATS_TEST_TMPDIR/bin/VBoxManage"

  run --separate-stderr "$SCRIPT" vm1 </dev/null

  [ "$status" -eq 3 ]
  [[ "$stderr" == *"VM no encontrada en VirtualBox"* ]]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

@test "AC-15 list-services lista solo unidades con la marca, con todos los campos" {
  run_remote_locally

  run --separate-stderr "$SCRIPT" vm1 </dev/null

  [ "$status" -eq 0 ]
  # name, active_state, enabled_state, restart, restart_sec, executable, args, env_keys, description
  # separados por tabuladores; api no tiene archivo env y demian-lee no tiene argumentos.
  expected=$'api\tinactive\tdisabled\talways\t10\tbin/server\t--port 9090\t\tAPI interna\n'
  expected+=$'demian-lee\tactive\tenabled\ton-failure\t5\tdemian-lee\t\tPORT,SERVER_NAME,ADMIN_PASSWORD\tServidor de producción'
  [ "$output" = "$expected" ]
  # Las unidades sin la marca no aparecen.
  [[ "$output" != *ssh* && "$output" != *cron* ]]
}

@test "AC-11 list-services imprime solo las claves del archivo env" {
  run_remote_locally

  run --separate-stderr "$SCRIPT" vm1 </dev/null

  [ "$status" -eq 0 ]
  [[ "$output" == *$'\tPORT,SERVER_NAME,ADMIN_PASSWORD\t'* ]]
  [[ "$output" != *Secreta123* && "$stderr" != *Secreta123* ]]
  # El programa remoto no contiene valores ni archivos del usuario, y el argv solo lleva la VM.
  ! grep -q Secreta123 "$BATS_TEST_TMPDIR/calls.log" "$BATS_TEST_TMPDIR/remote.log" || return 1
}

@test "list-services imprime nada y sale con 0 si no hay servicios de Stratus" {
  run_remote_locally
  rm "$BATS_TEST_TMPDIR"/units/{api,demian-lee}.service

  run --separate-stderr "$SCRIPT" vm1 </dev/null

  [ "$status" -eq 0 ]
  [ -z "$output" ]
}

@test "list-services lee la lista con un solo comando remoto con la marca y los directorios como argumentos" {
  run "$SCRIPT" vm1 </dev/null

  [ "$status" -eq 0 ]
  [ "$(wc -l <"$BATS_TEST_TMPDIR/remote.log")" -eq 1 ]
  grep -qF -- "sh $SERVICE_MARKER /etc/systemd/system /etc" "$BATS_TEST_TMPDIR/remote.log"
}

@test "list-services sale con 5 si el comando remoto falla" {
  FAIL_REMOTE='sh -c' run --separate-stderr "$SCRIPT" vm1 </dev/null

  [ "$status" -eq 5 ]
}

@test "AC-37 list-services con --jump entra al servidor por el balanceador" {
  stub_jump_topology lb1 running web-a running

  run --separate-stderr "$SCRIPT" --jump lb1 web-a </dev/null

  [ "$status" -eq 0 ]
  assert_routed_ssh lb1 web-a
}

@test "AC-37 list-services sale con 2 si --jump no trae un balanceador válido y no llama a nada" {
  stub_jump_topology lb1 running web-a running

  run "$SCRIPT" --jump </dev/null
  [ "$status" -eq 2 ]
  run "$SCRIPT" --jump Lb1 web-a </dev/null
  [ "$status" -eq 2 ]
  run "$SCRIPT" --jump "" web-a </dev/null
  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

@test "AC-29 list-services con --jump sale con 12 si el balanceador está apagado y no llama a ssh" {
  stub_jump_topology lb1 poweroff web-a running

  run --separate-stderr "$SCRIPT" --jump lb1 web-a </dev/null

  [ "$status" -eq 12 ]
  [[ "$stderr" == *"El balanceador lb1 está apagado; enciéndalo para llegar al servidor"* ]]
  ! grep -qE '^ssh ' "$CALLS" || return 1
}

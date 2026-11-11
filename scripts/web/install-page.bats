#!/usr/bin/env bats
# Pruebas de install-page.sh (005) con stubs de VBoxManage, ssh y scp: no tocan VMs.

load test_helper

bats_require_minimum_version 1.5.0

setup() {
  setup_web_stubs
  CALLS="$BATS_TEST_TMPDIR/calls.log"
  REMOTE="$BATS_TEST_TMPDIR/remote.log"
  export APP_SSH_KEY_PATH="$BATS_TEST_TMPDIR/stratus_backend"
  : >"$APP_SSH_KEY_PATH"
  SCRIPT="$WX/install-page.sh"
  ARCHIVE="$BATS_TEST_TMPDIR/pagina.tar.gz"
  make_archive "$ARCHIVE" index.html css/ css/site.css
  stub_vm_ip web-a 192.168.1.40
}

@test "AC-56 install-page.sh tiene la estructura exigida" {
  assert_script_structure "$WEB_DIR/install-page.sh"
}

@test "AC-11 install-page copia el archivo, guarda una copia, extrae con permisos y reemplaza" {
  run --separate-stderr "$SCRIPT" web-a "$ARCHIVE"

  [ "$status" -eq 0 ]
  [ "$output" = "Página instalada en web-a" ]
  grep -qE "^scp .* $ARCHIVE root@192.168.1.40:/var/tmp/stratus-page.tar.gz$" "$CALLS"
  grep -qx 'rm -rf /var/www/.stratus-page-new /var/www/.stratus-page-bak' "$REMOTE"
  grep -qx 'cp -a /var/www/html /var/www/.stratus-page-bak' "$REMOTE"
  grep -qx 'mkdir /var/www/.stratus-page-new' "$REMOTE"
  grep -qx 'tar -xzf /var/tmp/stratus-page.tar.gz --no-same-owner -C /var/www/.stratus-page-new' "$REMOTE"
  grep -qx 'chown -R root:root /var/www/.stratus-page-new' "$REMOTE"
  grep -qx 'find /var/www/.stratus-page-new -type d -exec chmod 755 {} +' "$REMOTE"
  grep -qx 'find /var/www/.stratus-page-new -type f -exec chmod 644 {} +' "$REMOTE"
  grep -qx 'rm -rf /var/www/.stratus-page-new /var/www/.stratus-page-bak /var/tmp/stratus-page.tar.gz' "$REMOTE"
  ! grep -q 'systemctl' "$REMOTE" || return 1
}

@test "AC-15 install-page reemplaza todo /var/www/html sin mezclar" {
  run --separate-stderr "$SCRIPT" web-a "$ARCHIVE"

  [ "$status" -eq 0 ]
  grep -qx 'sh -c rm -rf /var/www/html && mv /var/www/.stratus-page-new /var/www/html' "$REMOTE"
  # La copia se hace antes de extraer, y el reemplazo después de los permisos.
  backup="$(grep -nx 'cp -a /var/www/html /var/www/.stratus-page-bak' "$REMOTE" | cut -d: -f1)"
  extract="$(grep -n '^tar -xzf' "$REMOTE" | cut -d: -f1)"
  swap="$(grep -n '^sh -c rm -rf /var/www/html' "$REMOTE" | cut -d: -f1)"
  chmod_f="$(grep -n 'type f -exec chmod 644' "$REMOTE" | cut -d: -f1)"
  ((backup < extract && chmod_f < swap))
}

@test "AC-12 install-page sale con 11 sin index.html en la raíz y no toca la VM" {
  make_archive "$ARCHIVE" site/ site/index.html

  run --separate-stderr "$SCRIPT" web-a "$ARCHIVE"

  [ "$status" -eq 11 ]
  [ "$stderr" = "El archivo debe contener index.html en la raíz" ]
  ! grep -qE '^(scp|ssh) ' "$CALLS" || return 1
}

@test "AC-13 install-page sale con 10 ante archivos inválidos y no toca la VM" {
  # make_bad_archive es de las pruebas de 003: mismas reglas de archivo inválido (REQ-9).
  source "$WEB_DIR/../services/test_helper.bash"
  for kind in dotdot absolute symlink corrupt; do
    make_bad_archive "$kind" "$BATS_TEST_TMPDIR/bad.tar.gz"
    run --separate-stderr "$SCRIPT" web-a "$BATS_TEST_TMPDIR/bad.tar.gz"
    [ "$status" -eq 10 ]
    [ "$stderr" = "Archivo comprimido inválido" ]
  done
  ! grep -qE '^(scp|ssh) ' "$CALLS" || return 1
}

@test "AC-14 install-page restaura el contenido anterior si falla la extracción" {
  export FAIL_REMOTE='^tar -xzf'

  run --separate-stderr "$SCRIPT" web-a "$ARCHIVE"

  [ "$status" -eq 5 ]
  grep -qx 'sh -c rm -rf /var/www/html && mv /var/www/.stratus-page-bak /var/www/html' "$REMOTE"
  grep -qx 'rm -rf /var/www/.stratus-page-new /var/www/.stratus-page-bak /var/tmp/stratus-page.tar.gz' "$REMOTE"
  ! grep -q 'mv /var/www/.stratus-page-new /var/www/html' "$REMOTE" || return 1
}

@test "AC-14 install-page sale con 9 si también falla la restauración y conserva la copia" {
  export FAIL_REMOTE='^tar -xzf' FAIL_RESTORE='stratus-page-bak /var/www/html'

  run --separate-stderr "$SCRIPT" web-a "$ARCHIVE"

  [ "$status" -eq 9 ]
  [[ "$stderr" == *"/var/www/.stratus-page-bak"* ]]
  ! grep -qx 'rm -rf /var/www/.stratus-page-new /var/www/.stratus-page-bak /var/tmp/stratus-page.tar.gz' "$REMOTE" ||
    return 1
}

@test "AC-14 install-page borra el archivo remoto si falla la copia de seguridad, sin restaurar" {
  export FAIL_REMOTE='^cp -a /var/www/html'

  run --separate-stderr "$SCRIPT" web-a "$ARCHIVE"

  [ "$status" -eq 5 ]
  grep -qx 'rm -rf /var/www/.stratus-page-new /var/www/.stratus-page-bak /var/tmp/stratus-page.tar.gz' "$REMOTE"
  ! grep -q 'mv /var/www/.stratus-page-bak' "$REMOTE" || return 1
}

@test "AC-16 install-page con --jump copia y entra por el balanceador" {
  setup_web_stubs
  stub_jump_topology lb1 running web-a running

  run --separate-stderr "$SCRIPT" --jump lb1 web-a "$ARCHIVE"

  [ "$status" -eq 0 ]
  scp_line="$(grep -E '^scp ' "$CALLS")"
  [[ "$scp_line" == *"-o HostKeyAlias=lb1 -W %h:%p jump@192.168.1.60"* ]]
  [[ "$scp_line" == *"root@10.10.1.11:/var/tmp/stratus-page.tar.gz" ]]
  assert_routed_ssh lb1 web-a
}

@test "AC-16 install-page sale con 12 si el balanceador está apagado y no copia nada" {
  setup_web_stubs
  stub_jump_topology lb1 poweroff web-a running

  run --separate-stderr "$SCRIPT" --jump lb1 web-a "$ARCHIVE"

  [ "$status" -eq 12 ]
  [[ "$stderr" == *"El balanceador lb1 está apagado"* ]]
  ! grep -qE '^(scp|ssh) ' "$CALLS" || return 1
}

@test "AC-55 install-page sale con 3 si la VM no existe" {
  stub_command VBoxManage 'exit 1'

  run --separate-stderr "$SCRIPT" web-b "$ARCHIVE"

  [ "$status" -eq 3 ]
  ! grep -qE '^(scp|ssh) ' "$CALLS" || return 1
}

@test "AC-55 install-page sale con 2 ante argumentos inválidos sin llamar a nada" {
  run "$SCRIPT" Web_A "$ARCHIVE"
  [ "$status" -eq 2 ]
  run "$SCRIPT" web-a "$BATS_TEST_TMPDIR/no-existe.tar.gz"
  [ "$status" -eq 2 ]
  run "$SCRIPT" web-a
  [ "$status" -eq 2 ]
  run "$SCRIPT" --jump Lb1 web-a "$ARCHIVE"
  [ "$status" -eq 2 ]
  run env -u APP_SSH_KEY_PATH "$SCRIPT" web-a "$ARCHIVE"
  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}

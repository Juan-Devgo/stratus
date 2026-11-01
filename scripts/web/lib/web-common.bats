#!/usr/bin/env bats
# Pruebas de lib/web-common.sh (005): validadores, type_unit, render_site y parse_jump. La
# biblioteca se carga con "source", como hacen los scripts de scripts/web.

load ../test_helper

bats_require_minimum_version 1.5.0

setup() {
  # shellcheck source=web-common.sh
  source "$WEB_DIR/lib/web-common.sh"
}

@test "AC-56 web-common.sh tiene la estructura exigida" {
  assert_script_structure "$WEB_DIR/lib/web-common.sh"
}

@test "AC-55 validate_vm_name, validate_memory y validate_cpus" {
  local ok bad
  for ok in web-a w 0web a234567890123456789012345678901; do validate_vm_name "$ok"; done
  for bad in "" Web1 -web web_a "web a" a2345678901234567890123456789012x; do
    run validate_vm_name "$bad"
    [ "$status" -eq 1 ]
  done
  validate_memory 512
  validate_memory 8192
  for bad in 511 8193 "" 1k; do
    run validate_memory "$bad"
    [ "$status" -eq 1 ]
  done
  validate_cpus 1
  validate_cpus 4
  for bad in 0 5 "" x; do
    run validate_cpus "$bad"
    [ "$status" -eq 1 ]
  done
}

@test "AC-55 validate_type y validate_unit aceptan solo apache/nginx y apache2/nginx" {
  validate_type apache
  validate_type nginx
  local bad
  for bad in "" iis Apache apache2; do
    run --separate-stderr validate_type "$bad"
    [ "$status" -eq 1 ]
  done
  validate_unit apache2
  validate_unit nginx
  for bad in "" apache sshd named; do
    run validate_unit "$bad"
    [ "$status" -eq 1 ]
  done
}

@test "AC-55 validate_base_disk, validate_bridge y validate_domain" {
  validate_base_disk /vms/nginx.vdi
  run validate_base_disk vms/nginx.vdi
  [ "$status" -eq 1 ]
  validate_bridge enp3s0
  run validate_bridge "en p3s0"
  [ "$status" -eq 1 ]
  local ok bad
  for ok in stratus.lan lan a.b.c.lan lab-1.lan; do validate_domain "$ok"; done
  for bad in "" Stratus_Lan a.b.c.d.lan lab-.lan -lab.lan "stratus..lan" stratus.lan.; do
    run validate_domain "$bad"
    [ "$status" -eq 1 ]
  done
}

@test "AC-4 type_unit da la unidad de cada tipo" {
  [ "$(type_unit apache)" = apache2 ]
  [ "$(type_unit nginx)" = nginx ]
}

@test "AC-1 render_site nginx: server_name, default_server, raíz y página índice" {
  run render_site nginx web-a.stratus.lan

  [ "$status" -eq 0 ]
  [[ "$output" == *"listen 80 default_server;"* ]]
  [[ "$output" == *"listen [::]:80 default_server;"* ]]
  [[ "$output" == *"server_name web-a.stratus.lan;"* ]]
  [[ "$output" == *"root /var/www/html;"* ]]
  [[ "$output" == *"index index.html index.nginx-debian.html;"* ]]
  [[ "$output" == *'try_files $uri $uri/ =404;'* ]]
}

@test "AC-4 render_site apache: VirtualHost con ServerName y DocumentRoot" {
  run render_site apache web-b.stratus.lan

  [ "$status" -eq 0 ]
  [ "${lines[0]}" = "<VirtualHost *:80>" ]
  [[ "$output" == *"ServerName web-b.stratus.lan"* ]]
  [[ "$output" == *"DocumentRoot /var/www/html"* ]]
  [ "${lines[-1]}" = "</VirtualHost>" ]
}

@test "AC-55 parse_jump lee --jump <lb> al inicio y sale con 2 si es inválido" {
  parse_jump --jump lb1 web-a nginx
  [ "$jump_lb" = lb1 ]
  [ "$jump_shift" -eq 2 ]
  parse_jump web-a nginx
  [ -z "$jump_lb" ]
  [ "$jump_shift" -eq 0 ]
  run --separate-stderr parse_jump --jump LB web-a
  [ "$status" -eq 2 ]
  run parse_jump --jump
  [ "$status" -eq 2 ]
}

@test "fail imprime el mensaje en stderr y sale con el código" {
  run --separate-stderr fail 6 "Disco base de Nginx no encontrado"
  [ "$status" -eq 6 ]
  [ "$stderr" = "Disco base de Nginx no encontrado" ]
  [ -z "$output" ]
}

#!/usr/bin/env bats
# Pruebas de la configuración de despliegue de la UI; no crean ni modifican contenedores.

ROOT="$BATS_TEST_DIRNAME/.."

@test "Compose publica la UI solo en localhost y permite llegar al backend del host" {
  run docker compose --project-directory "$ROOT" config

  [ "$status" -eq 0 ]
  [[ "$output" != *"network_mode: host"* ]]
  [[ "$output" == *"host_ip: 127.0.0.1"* ]]
  [[ "$output" == *'published: "3000"'* ]]
  [[ "$output" == *"target: 3000"* ]]
  [[ "$output" == *"host.docker.internal=host-gateway"* ]]
}

@test "nginx escucha en el contenedor y reenvía la API al backend del host" {
  run grep -F 'listen 3000;' "$BATS_TEST_DIRNAME/nginx.conf"
  [ "$status" -eq 0 ]

  run grep -F 'proxy_pass http://host.docker.internal:8080;' "$BATS_TEST_DIRNAME/nginx.conf"
  [ "$status" -eq 0 ]
}

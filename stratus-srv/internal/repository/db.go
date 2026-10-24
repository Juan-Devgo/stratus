// Package repository guarda en SQLite las VMs registradas y el historial de operaciones.
package repository

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite" // controlador SQLite en Go puro, sin cgo
)

// schema crea las tablas si no existen; se ejecuta en cada arranque. Las tablas load_balancers y
// lb_servers (004) se agregan sin migración. Un stratus.db anterior al
// 2026-10-22 tiene otro esquema (ssh_port, ssh_enabled): se borra a mano, no hay migración.
const schema = `
CREATE TABLE IF NOT EXISTS vms (
  name TEXT PRIMARY KEY,
  user_key_generated INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS operations (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  module TEXT NOT NULL,
  operation TEXT NOT NULL,
  vm TEXT NOT NULL,
  command TEXT NOT NULL,
  args TEXT NOT NULL,
  exit_code INTEGER NOT NULL,
  stderr TEXT NOT NULL,
  result TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS load_balancers (
  name TEXT PRIMARY KEY,                -- también es una fila de vms
  subnet INTEGER NOT NULL UNIQUE,       -- N de 10.10.N.0/24, 1-254
  frontend_port INTEGER NOT NULL DEFAULT 80
);
CREATE TABLE IF NOT EXISTS lb_servers (
  lb TEXT NOT NULL,
  name TEXT NOT NULL,                   -- server<N>
  vm TEXT NOT NULL UNIQUE,              -- una VM solo puede ser servidor de un balanceador
  port INTEGER NOT NULL,
  last_ip TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  PRIMARY KEY (lb, name)
);
`

// Open abre (o crea) el archivo SQLite en path y crea el esquema.
func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("abrir base de datos %s: %w", path, err)
	}
	// Una sola conexión: SQLite admite un escritor a la vez y así se evitan errores SQLITE_BUSY.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("crear esquema: %w", err)
	}
	return db, nil
}

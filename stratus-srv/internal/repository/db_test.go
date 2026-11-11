package repository

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// TestOpenDropsDNSResolvers: una base creada antes del 2026-11-11 tiene la tabla dns_resolvers de
// US-7; al abrirla, el esquema la borra con sus filas (REQ-21, AC-29).
func TestOpenDropsDNSResolvers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if _, err := old.Exec(`CREATE TABLE dns_resolvers (vm TEXT PRIMARY KEY);
INSERT INTO dns_resolvers (vm) VALUES ('vm1');`); err != nil {
		t.Fatalf("crear tabla antigua: %v", err)
	}
	old.Close()

	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	var n int
	err = db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'dns_resolvers'`).Scan(&n)
	if err != nil || n != 0 {
		t.Errorf("dns_resolvers sigue en el esquema: n = %d, err = %v", n, err)
	}
}

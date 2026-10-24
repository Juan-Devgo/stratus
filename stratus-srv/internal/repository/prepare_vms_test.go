package repository

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

// openTestDB abre una base SQLite temporal con el esquema creado.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// mustInsert registra una VM y falla la prueba si hay error.
func mustInsert(t *testing.T, r *VMRepository, name string) VM {
	t.Helper()
	vm, err := r.Insert(context.Background(), name)
	if err != nil {
		t.Fatalf("Insert(%q): %v", name, err)
	}
	return vm
}

func TestAC29_InsertRejectsDuplicate(t *testing.T) {
	ctx := context.Background()
	repo := NewVMRepository(openTestDB(t))
	mustInsert(t, repo, "vm1")

	_, err := repo.Insert(ctx, "vm1")
	if !errors.Is(err, ErrDuplicateVM) {
		t.Fatalf("Insert duplicado: err = %v, se esperaba ErrDuplicateVM", err)
	}
	if _, err := repo.Insert(ctx, "vm2"); err != nil {
		t.Fatalf("Insert(vm2): %v", err)
	}
	vms, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(vms) != 2 {
		t.Errorf("List devolvió %d VMs, se esperaban 2", len(vms))
	}
}

func TestAC39_DeleteClearsFlag(t *testing.T) {
	ctx := context.Background()
	repo := NewVMRepository(openTestDB(t))
	mustInsert(t, repo, "vm1")
	if err := repo.SetUserKeyGenerated(ctx, "vm1"); err != nil {
		t.Fatalf("SetUserKeyGenerated: %v", err)
	}
	before, err := repo.Get(ctx, "vm1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !before.UserKeyGenerated {
		t.Fatalf("antes de eliminar: UserKeyGenerated = false, se esperaba true")
	}

	if err := repo.Delete(ctx, "vm1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := repo.Get(ctx, "vm1"); !errors.Is(err, ErrVMNotFound) {
		t.Fatalf("Get tras Delete: err = %v, se esperaba ErrVMNotFound", err)
	}

	after := mustInsert(t, repo, "vm1")
	if after.UserKeyGenerated {
		t.Errorf("tras eliminar e insertar de nuevo: UserKeyGenerated = true, se esperaba false")
	}
}

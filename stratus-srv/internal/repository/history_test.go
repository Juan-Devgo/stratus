package repository

import (
	"context"
	"fmt"
	"reflect"
	"testing"
)

func TestAC22_ListRecentReturns50NewestFirst(t *testing.T) {
	ctx := context.Background()
	repo := NewHistoryRepository(openTestDB(t))

	for i := 1; i <= 55; i++ {
		op := Operation{
			Module:    "prepare",
			Operation: "start_vm",
			VM:        fmt.Sprintf("vm%d", i),
			Command:   "prepare/start-vm.sh",
			Args:      []string{fmt.Sprintf("vm%d", i), "a b; c"},
			ExitCode:  99,
			Stderr:    "Operación no implementada",
			Result:    "error",
		}
		if err := repo.Insert(ctx, op); err != nil {
			t.Fatalf("Insert %d: %v", i, err)
		}
	}
	// Una operación de otro módulo no debe aparecer en la lista de prepare.
	if err := repo.Insert(ctx, Operation{Module: "users", Operation: "x", VM: "otra", Command: "c", Args: []string{}, Result: "ok"}); err != nil {
		t.Fatalf("Insert otro módulo: %v", err)
	}

	ops, err := repo.ListRecent(ctx, "prepare", 50)
	if err != nil {
		t.Fatalf("ListRecent: %v", err)
	}
	if len(ops) != 50 {
		t.Fatalf("ListRecent devolvió %d filas, se esperaban 50", len(ops))
	}
	first, last := ops[0], ops[49]
	if first.VM != "vm55" {
		t.Errorf("primera fila VM = %q, se esperaba vm55 (la última insertada)", first.VM)
	}
	if last.VM != "vm6" {
		t.Errorf("última fila VM = %q, se esperaba vm6", last.VM)
	}
	if !reflect.DeepEqual(first.Args, []string{"vm55", "a b; c"}) {
		t.Errorf("Args = %q, se esperaba [vm55 \"a b; c\"]", first.Args)
	}
	if first.ExitCode != 99 || first.Stderr != "Operación no implementada" || first.Result != "error" {
		t.Errorf("fila = %+v, no conserva código, stderr y resultado", first)
	}
	if first.CreatedAt.IsZero() {
		t.Error("CreatedAt está vacío")
	}
}

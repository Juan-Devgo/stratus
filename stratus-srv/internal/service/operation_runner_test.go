package service

import (
	"bytes"
	"context"
	"errors"
	"log"
	"path/filepath"
	"testing"

	"stratus-srv/internal/repository"
)

// AC-30: con recordSuccess=false, solo el éxito queda fuera del historial.
func TestAC30_OperationRunnerSkipsRecordOnlyOnSuccess(t *testing.T) {
	tests := []struct {
		name     string
		result   RunResult
		wantRows int
	}{
		{"éxito no se registra", RunResult{Stdout: []byte("ok\n")}, 0},
		{"error se registra", RunResult{ExitCode: 4, Stderr: "fallo de SSH"}, 1},
		{"timeout se registra", RunResult{TimedOut: true}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, err := repository.Open(filepath.Join(t.TempDir(), "test.db"))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			t.Cleanup(func() { db.Close() })
			history := repository.NewHistoryRepository(db)
			runner := &fakeRunner{results: map[string]RunResult{"users/list-users.sh": tt.result}}
			ops := newOperationRunner("users", runner, history, &VMLocks{}, log.New(&bytes.Buffer{}, "", 0))

			_, _ = ops.run(context.Background(), "list_users", "vm1", "users/list-users.sh",
				[]string{"vm1"}, false, nil)

			rows, err := history.ListRecent(context.Background(), "users", 50)
			if err != nil {
				t.Fatalf("ListRecent: %v", err)
			}
			if len(rows) != tt.wantRows {
				t.Fatalf("filas del historial = %d, se esperaban %d", len(rows), tt.wantRows)
			}
			if tt.wantRows == 1 && rows[0].Module != "users" {
				t.Errorf("module = %q, se esperaba users", rows[0].Module)
			}
		})
	}
}

// newTestOps arma un operationRunner de prueba con una base SQLite temporal.
func newTestOps(t *testing.T, runner ScriptRunner, locks *VMLocks) (*operationRunner, *repository.HistoryRepository) {
	t.Helper()
	db, err := repository.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	history := repository.NewHistoryRepository(db)
	return newOperationRunner("prepare", runner, history, locks, log.New(&bytes.Buffer{}, "", 0)), history
}

// AC-29: el código 12 de cualquier script se vuelve *LBOffError con la línea de stderr y queda en el historial.
func TestAC29_OperationRunnerMapsLBOff(t *testing.T) {
	const msg = "El balanceador lb1 está apagado; enciéndalo para llegar al servidor"
	runner := &fakeRunner{results: map[string]RunResult{
		"prepare/install-package.sh": {ExitCode: 12, Stderr: "diagnóstico previo\n" + msg + "\n"},
	}}
	ops, history := newTestOps(t, runner, &VMLocks{})

	_, err := ops.run(context.Background(), "install_package", "web-a", "prepare/install-package.sh",
		[]string{"--jump", "lb1", "web-a", "nginx"}, true, nil)

	var off *LBOffError
	if !errors.As(err, &off) {
		t.Fatalf("err = %v, se esperaba *LBOffError", err)
	}
	if off.Message != msg {
		t.Errorf("Message = %q, se esperaba %q", off.Message, msg)
	}
	if !errors.Is(err, ErrLBOff) {
		t.Error("errors.Is(err, ErrLBOff) = false")
	}
	rows, _ := history.ListRecent(context.Background(), "prepare", 50)
	if len(rows) != 1 || rows[0].ExitCode != 12 || rows[0].Result != "error" {
		t.Errorf("historial = %+v, se esperaba una fila de error con código 12", rows)
	}
}

// AC-34, REQ-21: runLocked toma todos los bloqueos durante el script y los suelta al terminar.
func TestAC34_RunLockedHoldsAllVMs(t *testing.T) {
	locks := &VMLocks{}
	runner := &fakeRunner{block: make(chan struct{}), started: make(chan struct{})}
	ops, _ := newTestOps(t, runner, locks)

	done := make(chan error, 1)
	go func() {
		_, err := ops.runLocked(context.Background(), "assign_server", "lb1", []string{"lb1", "web-a"},
			"haproxy/assign-server.sh", []string{"lb1"}, true, nil)
		done <- err
	}()
	<-runner.started

	if locks.TryLock("lb1") || locks.TryLock("web-a") {
		t.Error("lb1 y web-a debían estar bloqueadas mientras corre el script")
	}
	close(runner.block)
	if err := <-done; err != nil {
		t.Fatalf("runLocked: %v", err)
	}
	if !locks.TryLockAll("lb1", "web-a") {
		t.Error("tras terminar, lb1 y web-a debían estar libres")
	}
}

// AC-34: si alguna VM está ocupada se rechaza sin llamar al script ni tomar las demás.
func TestAC34_RunLockedRejectsWhenAnyBusy(t *testing.T) {
	locks := &VMLocks{}
	runner := &fakeRunner{}
	ops, _ := newTestOps(t, runner, locks)
	locks.TryLock("web-a")

	_, err := ops.runLocked(context.Background(), "assign_server", "lb1", []string{"lb1", "web-a"},
		"haproxy/assign-server.sh", []string{"lb1"}, true, nil)

	if !errors.Is(err, ErrOperationInProgress) {
		t.Fatalf("err = %v, se esperaba ErrOperationInProgress", err)
	}
	if len(runner.calls) != 0 {
		t.Errorf("se llamó al script %d veces, se esperaban 0", len(runner.calls))
	}
	if !locks.TryLock("lb1") {
		t.Error("lb1 debía seguir libre")
	}
}

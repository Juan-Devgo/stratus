package service

import (
	"context"
	"fmt"
	"io"
	"log"
	"testing"
)

// AC-8: queryVMStates devuelve la fila de historial de una lectura fallida de list-vms.sh para que
// el listado de servidores web la registre (REQ-35); una lectura correcta no devuelve fila.
func TestAC8_QueryVMStatesReturnsFailedRun(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	tests := []struct {
		name       string
		res        RunResult
		wantFailed bool
		wantCode   int
		wantResult string
	}{
		{"correcta", RunResult{Stdout: []byte("web-a running 192.168.1.40\n")}, false, 0, ""},
		{"código 4", RunResult{ExitCode: 4, Stderr: "error de VBoxManage\n"}, true, 4, "error"},
		{"tiempo límite", RunResult{TimedOut: true}, true, -1, "timeout"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeRunner{results: map[string]RunResult{"prepare/list-vms.sh": tt.res}}
			states, listErr, failed := queryVMStates(context.Background(), runner, logger)
			if !tt.wantFailed {
				if failed != nil || listErr != "" {
					t.Fatalf("failed = %+v, listErr = %q; se esperaba nil y vacío", failed, listErr)
				}
				if states["web-a"] != (vmStatus{state: "running", ip: "192.168.1.40"}) {
					t.Errorf("states = %+v", states)
				}
				return
			}
			if failed == nil || listErr == "" {
				t.Fatalf("failed = %+v, listErr = %q; se esperaba la fila fallida", failed, listErr)
			}
			if failed.Command != "prepare/list-vms.sh" || failed.ExitCode != tt.wantCode || failed.Result != tt.wantResult {
				t.Errorf("failed = %+v", failed)
			}
		})
	}
}

// cancelRunner imita a ExecRunner cuando el navegador cierra la conexión: con el contexto
// cancelado el script muere y Run devuelve el error del contexto o un código de salida.
type cancelRunner struct {
	res RunResult
	err error
}

func (c cancelRunner) Run(ctx context.Context, _ string, _ ...string) (RunResult, error) {
	if c.err != nil {
		return RunResult{}, fmt.Errorf("ejecutar list-vms.sh: %w", c.err)
	}
	return c.res, nil
}

// AC-71: una lectura de list-vms.sh cortada porque el navegador abandonó la petición no devuelve
// fila fallida (REQ-50); la misma falla con el contexto vivo sí la devuelve (REQ-35).
func TestAC71_CancelledListNotRecorded(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	tests := []struct {
		name       string
		ctx        context.Context
		runner     cancelRunner
		wantFailed bool
	}{
		{"cancelada con error", cancelled, cancelRunner{err: context.Canceled}, false},
		{"cancelada con código", cancelled, cancelRunner{res: RunResult{ExitCode: -1, Stderr: "signal: killed"}}, false},
		{"viva con código", context.Background(), cancelRunner{res: RunResult{ExitCode: 4, Stderr: "error de VBoxManage"}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, failed := queryVMStates(tt.ctx, tt.runner, logger)
			if got := failed != nil; got != tt.wantFailed {
				t.Errorf("failed = %+v; se esperaba fila = %v", failed, tt.wantFailed)
			}
		})
	}
}

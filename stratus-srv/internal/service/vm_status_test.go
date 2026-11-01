package service

import (
	"context"
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

package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestAC36_EnsureBackendKeyRunsScriptWhenMissing(t *testing.T) {
	tests := []struct {
		name      string
		existing  bool // si el archivo de la llave ya existe
		wantCalls int
	}{
		{"sin llave ejecuta el script", false, 1},
		{"con llave no ejecuta nada", true, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			keyPath := filepath.Join(t.TempDir(), "stratus_backend")
			if tc.existing {
				if err := os.WriteFile(keyPath, []byte("llave"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			runner := &fakeRunner{}

			if err := EnsureBackendKey(context.Background(), runner, keyPath); err != nil {
				t.Fatalf("EnsureBackendKey: %v", err)
			}

			calls := runner.callsTo("prepare/create-backend-key.sh")
			if len(calls) != tc.wantCalls || runner.callCount() != tc.wantCalls {
				t.Fatalf("llamadas = %+v, se esperaban %d a create-backend-key.sh", calls, tc.wantCalls)
			}
			if tc.wantCalls == 1 && !reflect.DeepEqual(calls[0].Args, []string{keyPath}) {
				t.Errorf("argv = %q, se esperaba %q", calls[0].Args, []string{keyPath})
			}
		})
	}

	t.Run("script fallido devuelve error", func(t *testing.T) {
		keyPath := filepath.Join(t.TempDir(), "stratus_backend")
		runner := &fakeRunner{results: map[string]RunResult{
			"prepare/create-backend-key.sh": {ExitCode: 99, Stderr: "Operación no implementada\n"},
		}}

		err := EnsureBackendKey(context.Background(), runner, keyPath)
		var opErr *OperationError
		if !errors.As(err, &opErr) || opErr.ExitCode != 99 || opErr.Detail != "Operación no implementada" {
			t.Errorf("err = %v, se esperaba OperationError con código 99", err)
		}
	})
}

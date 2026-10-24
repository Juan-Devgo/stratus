package service

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// EnsureBackendKey crea el par de llaves del backend con create-backend-key.sh si no existe
// el archivo keyPath (REQ-23). Solo comprueba si el archivo existe; crearlo y fijar sus
// permisos es trabajo del script (P1). No lee la llave ni registra su contenido.
func EnsureBackendKey(ctx context.Context, runner ScriptRunner, keyPath string) error {
	_, err := os.Stat(keyPath)
	if err == nil {
		return nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("comprobar la llave del backend: %w", err)
	}

	const operation = "create_backend_key"
	res, err := runner.Run(ctx, "prepare/create-backend-key.sh", keyPath)
	switch {
	case err != nil:
		return fmt.Errorf("crear la llave del backend: %w", err)
	case res.TimedOut:
		return fmt.Errorf("crear la llave del backend: %w", ErrTimeout)
	case res.ExitCode != 0:
		// El stderr del script no debe tener la llave, pero se enmascara por si acaso.
		stderr := strings.TrimSpace(Redact(res.Stderr))
		return &OperationError{Operation: operation, ExitCode: res.ExitCode, Detail: lastLine(stderr)}
	}
	return nil
}

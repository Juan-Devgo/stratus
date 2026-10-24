package service

import (
	"context"
	"fmt"
	"log"
	"strings"

	"stratus-srv/internal/repository"
)

// maxDetailRunes limita el detalle de stderr que se muestra en la UI.
const maxDetailRunes = 200

// exitVMNoIP es el código con que los scripts de invitado avisan que la VM no tiene IPv4 (REQ-29).
// create-vm.sh nunca lo usa.
const exitVMNoIP = 7

// exitLBOff es el código con que los scripts de invitado avisan que el balanceador por el que se
// llega a un servidor está apagado o sin IPv4 en puente (REQ-19). Se mapea para todos los módulos.
const exitLBOff = 12

// operationRunner es la única implementación de bloqueo, ejecución, historial y registro de
// las operaciones sobre una VM. Cada módulo (prepare, users) tiene el suyo, pero todos reciben el
// mismo *VMLocks: así una operación de cualquier módulo bloquea a las demás en esa VM (AC-24).
type operationRunner struct {
	runner  ScriptRunner
	history *repository.HistoryRepository
	logger  *log.Logger
	locks   *VMLocks
	module  string // valor de la columna module en el historial
}

func newOperationRunner(module string, runner ScriptRunner, history *repository.HistoryRepository,
	locks *VMLocks, logger *log.Logger) *operationRunner {
	return &operationRunner{runner: runner, history: history, logger: logger, locks: locks, module: module}
}

// run ejecuta un script que actúa sobre una VM: es runLocked con el bloqueo de esa sola VM.
func (o *operationRunner) run(ctx context.Context, operation, vm, script string, args []string,
	recordSuccess bool, onSuccess func(ctx context.Context) error) (RunResult, error) {
	var hook func(context.Context, RunResult) error
	if onSuccess != nil {
		hook = func(ctx context.Context, _ RunResult) error { return onSuccess(ctx) }
	}
	return o.runLocked(ctx, operation, vm, []string{vm}, script, args, recordSuccess, hook)
}

// runLocked ejecuta un script que puede cambiar varias VMs (por ejemplo un balanceador y su servidor):
//  1. toma el bloqueo de todas las lockVMs a la vez o devuelve ErrOperationInProgress sin llamar
//     al script (REQ-15, REQ-21); vm es la VM con que se registra la fila del historial;
//  2. ejecuta el script con los argumentos separados;
//  3. enmascara stderr y lo guarda en el historial con el comando, argv, código y resultado;
//     con recordSuccess=false un resultado "ok" no deja fila (listados, REQ-19);
//  4. registra un resumen en el log, nunca el stdout (puede tener la llave privada);
//  5. si el script terminó bien, ejecuta onSuccess (actualizar SQLite) con su resultado, antes de
//     soltar los bloqueos.
//
// Devuelve ErrTimeout, ErrVMNoIP, *LBOffError, *OperationError o el error de onSuccess; el RunResult sirve a
// quien necesite el stdout (por ejemplo la llave generada).
func (o *operationRunner) runLocked(ctx context.Context, operation, vm string, lockVMs []string, script string,
	args []string, recordSuccess bool, onSuccess func(ctx context.Context, res RunResult) error) (RunResult, error) {
	if !o.locks.TryLockAll(lockVMs...) {
		return RunResult{}, ErrOperationInProgress
	}
	defer o.locks.UnlockAll(lockVMs...)

	// Si el navegador cierra la conexión, el script sigue hasta terminar o hasta el
	// timeout de ExecRunner: cortarlo a la mitad dejaría la VM en un estado intermedio.
	ctx = context.WithoutCancel(ctx)

	res, runErr := o.runner.Run(ctx, script, args...)

	row := repository.Operation{
		Module:    o.module,
		Operation: operation,
		VM:        vm,
		Command:   script,
		Args:      redactAll(args),
		ExitCode:  res.ExitCode,
		Stderr:    strings.TrimSpace(Redact(res.Stderr)),
	}
	var err error
	switch {
	case runErr != nil: // el script no pudo iniciarse (no existe, sin permiso de ejecución)
		row.ExitCode = -1
		row.Stderr = Redact(runErr.Error())
		row.Result = "error"
		err = fmt.Errorf("%s en %s: %w", operation, vm, runErr)
	case res.TimedOut:
		row.ExitCode = -1
		row.Result = "timeout"
		err = ErrTimeout
	case res.ExitCode == exitVMNoIP:
		row.Result = "error"
		err = fmt.Errorf("%s en %s: %w", operation, vm, ErrVMNoIP)
	case res.ExitCode == exitLBOff:
		// La última línea de stderr ya nombra el balanceador y es el texto para el usuario.
		row.Result = "error"
		err = &LBOffError{Message: lastLine(row.Stderr)}
	case res.ExitCode != 0:
		row.Result = "error"
		err = &OperationError{Operation: operation, ExitCode: res.ExitCode, Detail: lastLine(row.Stderr)}
	default:
		row.Result = "ok"
	}

	o.logger.Printf("operación=%s vm=%s código=%d resultado=%s stderr=%q",
		operation, vm, row.ExitCode, row.Result, row.Stderr)
	if recordSuccess || row.Result != "ok" {
		if insertErr := o.history.Insert(ctx, row); insertErr != nil {
			o.logger.Printf("no se pudo guardar el historial de %s en %s: %v", operation, vm, insertErr)
		}
	}

	if err == nil && onSuccess != nil {
		if hookErr := onSuccess(ctx, res); hookErr != nil {
			err = fmt.Errorf("%s en %s: el script terminó bien pero no se actualizó la base de datos: %w",
				operation, vm, hookErr)
		}
	}
	return res, err
}

// redactAll enmascara cualquier llave privada en cada argumento (REQ-23). La contraseña de la
// imagen base no llega al backend: solo la lee VBoxManage desde el archivo, dentro de create-vm.sh.
func redactAll(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = Redact(a)
	}
	return out
}

// lastLine devuelve la última línea no vacía, recortada a maxDetailRunes caracteres.
func lastLine(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	line := strings.TrimSpace(lines[len(lines)-1])
	if line == "" {
		return "sin detalle en stderr"
	}
	if r := []rune(line); len(r) > maxDetailRunes {
		line = string(r[:maxDetailRunes])
	}
	return line
}

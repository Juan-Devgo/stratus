package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// RunResult es el resultado de ejecutar un script.
// Stdout nunca se guarda ni se registra: puede contener la llave privada del usuario.
type RunResult struct {
	ExitCode int
	Stdout   []byte
	Stderr   string
	TimedOut bool
}

// ScriptRunner ejecuta un script relativo al directorio de scripts, por ejemplo "prepare/stop-vm.sh".
// Es la única interfaz del servicio: las pruebas usan un falso que registra los argumentos.
type ScriptRunner interface {
	Run(ctx context.Context, script string, args ...string) (RunResult, error)
}

// ExecRunner ejecuta scripts reales con os/exec.
type ExecRunner struct {
	ScriptsDir string
	Timeout    time.Duration
	// KillGrace es lo que espera el script, tras recibir SIGTERM por el timeout, antes de que se
	// envíe SIGKILL al grupo. Le da tiempo de ejecutar su limpieza (por ejemplo, create-vm.sh
	// quita la VM a medio crear).
	KillGrace time.Duration
}

// Run ejecuta el script con los argumentos como elementos separados de argv (nunca "bash -c").
// Un código de salida distinto de cero no es un error; sólo lo es no poder iniciar el proceso.
// En timeout se envía SIGTERM a todo el grupo de procesos, SIGKILL tras KillGrace, y ExitCode vale -1.
func (r ExecRunner) Run(ctx context.Context, script string, args ...string) (RunResult, error) {
	if r.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.Timeout)
		defer cancel()
	}

	path := filepath.Join(r.ScriptsDir, script)
	cmd := exec.CommandContext(ctx, path, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// Grupo de procesos propio, para que el timeout también mate a los hijos (sleep, VBoxManage, ssh).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// Timeout: primero SIGTERM al grupo para que el script limpie; si sigue vivo tras KillGrace, SIGKILL.
	var mu sync.Mutex
	var killTimer *time.Timer
	defer func() {
		mu.Lock()
		defer mu.Unlock()
		if killTimer != nil {
			killTimer.Stop()
		}
	}()
	cmd.Cancel = func() error {
		pgid := cmd.Process.Pid
		mu.Lock()
		killTimer = time.AfterFunc(r.KillGrace, func() { _ = syscall.Kill(-pgid, syscall.SIGKILL) })
		mu.Unlock()
		return syscall.Kill(-pgid, syscall.SIGTERM)
	}
	// Si algún nieto sobrevive con las tuberías abiertas, no esperar indefinidamente.
	cmd.WaitDelay = r.KillGrace + time.Second

	err := cmd.Run()
	res := RunResult{Stdout: stdout.Bytes(), Stderr: stderr.String()}

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		res.TimedOut = true
		res.ExitCode = -1
		return res, nil
	}
	if err == nil {
		return res, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		res.ExitCode = exitErr.ExitCode()
		return res, nil
	}
	return res, fmt.Errorf("ejecutar %s: %w", script, err)
}

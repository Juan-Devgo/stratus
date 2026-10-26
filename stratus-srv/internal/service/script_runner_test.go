package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeScript crea un script ejecutable en dir/prepare/<name> y devuelve su ruta relativa.
func writeScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	rel := filepath.Join("prepare", name)
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("#!/usr/bin/env bash\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return rel
}

func TestAC1_ExecRunnerPassesDiscreteArgs(t *testing.T) {
	dir := t.TempDir()
	script := writeScript(t, dir, "args.sh", `for a in "$@"; do printf '%s\n' "$a"; done`)
	runner := ExecRunner{ScriptsDir: dir, Timeout: 5 * time.Second}

	res, err := runner.Run(context.Background(), script, "a b; c", "x")
	if err != nil {
		t.Fatalf("Run devolvió error: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, se esperaba 0", res.ExitCode)
	}
	lines := strings.Split(strings.TrimRight(string(res.Stdout), "\n"), "\n")
	want := []string{"a b; c", "x"}
	if len(lines) != len(want) {
		t.Fatalf("stdout tiene %d líneas %q, se esperaban %d", len(lines), lines, len(want))
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("línea %d = %q, se esperaba %q", i, lines[i], want[i])
		}
	}
}

func TestAC20_ExecRunnerKillsOnTimeout(t *testing.T) {
	dir := t.TempDir()
	script := writeScript(t, dir, "slow.sh", "sleep 30")
	runner := ExecRunner{ScriptsDir: dir, Timeout: 200 * time.Millisecond}

	start := time.Now()
	res, err := runner.Run(context.Background(), script)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Run devolvió error: %v", err)
	}
	if !res.TimedOut {
		t.Error("TimedOut = false, se esperaba true")
	}
	if res.ExitCode != -1 {
		t.Errorf("ExitCode = %d, se esperaba -1 en timeout", res.ExitCode)
	}
	if elapsed >= 2*time.Second {
		t.Errorf("Run tardó %v, se esperaba menos de 2 s", elapsed)
	}
}

func TestAC20_ExecRunnerSendsSIGTERMFirst(t *testing.T) {
	dir := t.TempDir()
	// El script limpia al recibir TERM, como create-vm.sh con su trap de limpieza.
	script := writeScript(t, dir, "term.sh", "trap 'echo limpieza >&2; exit 143' TERM\nsleep 30 &\nwait")
	// Bash instalado por Homebrew puede tardar más de 200 ms en su primer arranque en macOS. El
	// timeout debe vencer después de que el script haya alcanzado a instalar el trap que probamos.
	runner := ExecRunner{ScriptsDir: dir, Timeout: time.Second, KillGrace: 5 * time.Second}

	start := time.Now()
	res, err := runner.Run(context.Background(), script)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Run devolvió error: %v", err)
	}
	if !res.TimedOut || res.ExitCode != -1 {
		t.Errorf("TimedOut = %v, ExitCode = %d, se esperaba true y -1", res.TimedOut, res.ExitCode)
	}
	if !strings.Contains(res.Stderr, "limpieza") {
		t.Errorf("Stderr = %q, se esperaba que el script alcanzara a limpiar tras SIGTERM", res.Stderr)
	}
	if elapsed >= 2*time.Second {
		t.Errorf("Run tardó %v, se esperaba menos de 2 s", elapsed)
	}
}

func TestAC20_ExecRunnerKillsAfterGrace(t *testing.T) {
	dir := t.TempDir()
	// El script ignora TERM: solo SIGKILL tras el período de gracia lo detiene.
	script := writeScript(t, dir, "ignore-term.sh", "trap '' TERM\nwhile true; do sleep 1; done")
	runner := ExecRunner{ScriptsDir: dir, Timeout: 200 * time.Millisecond, KillGrace: 300 * time.Millisecond}

	start := time.Now()
	res, err := runner.Run(context.Background(), script)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Run devolvió error: %v", err)
	}
	if !res.TimedOut || res.ExitCode != -1 {
		t.Errorf("TimedOut = %v, ExitCode = %d, se esperaba true y -1", res.TimedOut, res.ExitCode)
	}
	if elapsed >= 2*time.Second {
		t.Errorf("Run tardó %v, se esperaba menos de 2 s", elapsed)
	}
}

func TestAC37_ExecRunnerCapturesPlaceholderExit(t *testing.T) {
	dir := t.TempDir()
	script := writeScript(t, dir, "placeholder.sh", "set -euo pipefail\necho \"Operación no implementada\" >&2\nexit 99")
	runner := ExecRunner{ScriptsDir: dir, Timeout: 5 * time.Second}

	res, err := runner.Run(context.Background(), script)
	if err != nil {
		t.Fatalf("Run devolvió error: %v", err)
	}
	if res.ExitCode != 99 {
		t.Errorf("ExitCode = %d, se esperaba 99", res.ExitCode)
	}
	if got := strings.TrimSpace(res.Stderr); got != "Operación no implementada" {
		t.Errorf("Stderr = %q, se esperaba %q", got, "Operación no implementada")
	}
	if res.TimedOut {
		t.Error("TimedOut = true, se esperaba false")
	}
}

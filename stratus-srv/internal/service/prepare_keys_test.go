package service

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// userKeyBody es el contenido secreto de la llave que "genera" el runner falso.
const userKeyBody = "b3BlbnNzaC1rZXktdjEAAAAALlaveDelUsuarioDePrueba"

func privateKey(body string) []byte {
	return []byte("-----BEGIN OPENSSH PRIVATE KEY-----\n" + body + "\n-----END OPENSSH PRIVATE KEY-----\n")
}

// keyRunner responde a generate-user-key.sh con la llave dada por stdout.
func keyRunner(key []byte) *fakeRunner {
	return &fakeRunner{results: map[string]RunResult{
		"prepare/generate-user-key.sh": {Stdout: key},
	}}
}

func TestAC9_GenerateUserKeyKeepsStdoutInMemory(t *testing.T) {
	key := privateKey(userKeyBody)
	env := newTestEnv(t, keyRunner(append([]byte(nil), key...)))
	env.register(t, "vm1")

	msg, err := env.svc.GenerateUserKey(context.Background(), "vm1")
	if err != nil {
		t.Fatalf("GenerateUserKey: %v", err)
	}
	if msg == "" {
		t.Error("se esperaba un mensaje de éxito")
	}

	got, err := env.svc.DownloadUserKey("vm1")
	if err != nil {
		t.Fatalf("DownloadUserKey: %v", err)
	}
	if !bytes.Equal(got, key) {
		t.Errorf("DownloadUserKey = %q, se esperaba la llave del stdout", got)
	}
	if _, err := env.svc.DownloadUserKey("vm1"); !errors.Is(err, ErrKeyUnavailable) {
		t.Errorf("segunda descarga: err = %v, se esperaba ErrKeyUnavailable", err)
	}
}

func TestAC13_PrivateKeyNotInHistoryOrLogs(t *testing.T) {
	key := privateKey(userKeyBody)
	tests := []struct {
		name   string
		result RunResult
	}{
		{"éxito", RunResult{Stdout: key, Stderr: "llave generada\n"}},
		// Un script defectuoso podría imprimir la llave también en stderr.
		{"fallo con la llave en stderr", RunResult{ExitCode: 5, Stdout: key, Stderr: string(key) + "falló sshd -t\n"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.result
			result.Stdout = append([]byte(nil), key...)
			runner := &fakeRunner{results: map[string]RunResult{"prepare/generate-user-key.sh": result}}
			env := newTestEnv(t, runner)
			env.register(t, "vm1")

			msg, err := env.svc.GenerateUserKey(context.Background(), "vm1")

			op := env.lastOperation(t)
			if op.Operation != "generate_user_key" || op.Command != "prepare/generate-user-key.sh" {
				t.Errorf("fila = %+v, se esperaba generate_user_key", op)
			}
			stored := strings.Join([]string{op.Operation, op.VM, op.Command, strings.Join(op.Args, " "), op.Stderr, op.Result}, " ")
			errText := ""
			if err != nil {
				errText = err.Error()
			}
			for where, text := range map[string]string{
				"historial": stored,
				"registro":  env.logs.String(),
				"mensaje":   msg,
				"error":     errText,
			} {
				if strings.Contains(text, userKeyBody) || strings.Contains(text, "BEGIN OPENSSH PRIVATE KEY") {
					t.Errorf("el %s contiene la llave privada: %q", where, text)
				}
			}
		})
	}
}

func TestAC35_GenerateUserKeySetsFlag(t *testing.T) {
	t.Run("éxito marca user_key_generated", func(t *testing.T) {
		runner := keyRunner(privateKey(userKeyBody))
		env := newTestEnv(t, runner)
		env.register(t, "vm1") // VM recién creada

		if _, err := env.svc.GenerateUserKey(context.Background(), "vm1"); err != nil {
			t.Fatalf("GenerateUserKey: %v", err)
		}
		calls := runner.callsTo("prepare/generate-user-key.sh")
		if len(calls) != 1 {
			t.Fatalf("generate-user-key.sh se llamó %d veces, se esperaba 1", len(calls))
		}
		if want := []string{"vm1"}; !reflect.DeepEqual(calls[0].Args, want) {
			t.Errorf("argv = %q, se esperaba %q", calls[0].Args, want)
		}
		vm, err := env.vms.Get(context.Background(), "vm1")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if !vm.UserKeyGenerated {
			t.Errorf("user_key_generated = false, se esperaba true")
		}
	})

	t.Run("fallo no marca la bandera ni guarda llave", func(t *testing.T) {
		runner := &fakeRunner{results: map[string]RunResult{
			"prepare/generate-user-key.sh": {ExitCode: 99, Stderr: "Operación no implementada\n"},
		}}
		env := newTestEnv(t, runner)
		env.register(t, "vm1")

		_, err := env.svc.GenerateUserKey(context.Background(), "vm1")
		if !errors.Is(err, ErrScriptFailed) {
			t.Fatalf("err = %v, se esperaba ErrScriptFailed", err)
		}
		vm, err := env.vms.Get(context.Background(), "vm1")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if vm.UserKeyGenerated {
			t.Errorf("user_key_generated = true, se esperaba false")
		}
		if _, err := env.svc.DownloadUserKey("vm1"); !errors.Is(err, ErrKeyUnavailable) {
			t.Errorf("DownloadUserKey: err = %v, se esperaba ErrKeyUnavailable", err)
		}
	})
}

func TestAC27_RegenerateReplacesKey(t *testing.T) {
	ctx := context.Background()
	runner := keyRunner(privateKey("llave-vieja"))
	env := newTestEnv(t, runner)
	env.register(t, "vm1")

	// Primera generación: deja la bandera y una llave pendiente de descarga.
	if _, err := env.svc.GenerateUserKey(ctx, "vm1"); err != nil {
		t.Fatalf("primera GenerateUserKey: %v", err)
	}

	newKey := privateKey("llave-nueva")
	runner.mu.Lock()
	runner.results["prepare/generate-user-key.sh"] = RunResult{Stdout: append([]byte(nil), newKey...)}
	runner.mu.Unlock()

	if _, err := env.svc.GenerateUserKey(ctx, "vm1"); err != nil {
		t.Fatalf("segunda GenerateUserKey: %v", err)
	}
	calls := runner.callsTo("prepare/generate-user-key.sh")
	if len(calls) != 2 {
		t.Fatalf("generate-user-key.sh se llamó %d veces, se esperaban 2", len(calls))
	}
	if want := []string{"vm1"}; !reflect.DeepEqual(calls[1].Args, want) {
		t.Errorf("argv al regenerar = %q, se esperaba %q", calls[1].Args, want)
	}
	got, err := env.svc.DownloadUserKey("vm1")
	if err != nil {
		t.Fatalf("DownloadUserKey: %v", err)
	}
	if !bytes.Equal(got, newKey) {
		t.Errorf("DownloadUserKey = %q, se esperaba la llave nueva", got)
	}
}

// AC-27, AC-42, REQ-21: en un servidor la llave se genera con --jump y bloquea al servidor y al balanceador.
func TestAC27_KeyOpsOnServerPassJump(t *testing.T) {
	runner := keyRunner(privateKey(userKeyBody))
	runner.block = make(chan struct{})
	runner.started = make(chan struct{}, 1)
	env := newHAProxyEnv(t, runner)
	env.registerLB(t, "lb1", 1, 80)
	env.registerServer(t, "lb1", "server1", "web-a", 80, "10.10.1.11")
	ctx := context.Background()

	done := make(chan error, 1)
	go func() {
		_, err := env.prepare.GenerateUserKey(ctx, "web-a")
		done <- err
	}()
	<-runner.started

	if env.locks.TryLock("web-a") {
		t.Error("web-a debía estar bloqueada mientras se genera la llave")
	}
	if env.locks.TryLock("lb1") {
		t.Error("lb1 debía estar bloqueado: jump_sync escribe en él")
	}
	if _, err := env.prepare.GenerateUserKey(ctx, "web-a"); !errors.Is(err, ErrOperationInProgress) {
		t.Errorf("segunda generación: err = %v, se esperaba ErrOperationInProgress", err)
	}
	close(runner.block)
	if err := <-done; err != nil {
		t.Fatalf("GenerateUserKey: %v", err)
	}

	calls := runner.callsTo("prepare/generate-user-key.sh")
	if want := []string{"--jump", "lb1", "web-a"}; len(calls) == 0 || !reflect.DeepEqual(calls[0].Args, want) {
		t.Errorf("argv = %+v, se esperaba %q", calls, want)
	}
	if !env.locks.TryLockAll("web-a", "lb1") {
		t.Error("tras terminar, web-a y lb1 debían quedar libres")
	}
}

// Una VM común o un balanceador no reciben --jump.
func TestGenerateUserKeyPlainVMAndLBHaveNoJump(t *testing.T) {
	runner := keyRunner(privateKey(userKeyBody))
	env := newHAProxyEnv(t, runner)
	env.registerLB(t, "lb1", 1, 80)
	env.register(t, "vm1")

	for _, vm := range []string{"vm1", "lb1"} {
		if _, err := env.prepare.GenerateUserKey(context.Background(), vm); err != nil {
			t.Fatalf("GenerateUserKey(%s): %v", vm, err)
		}
	}

	calls := runner.callsTo("prepare/generate-user-key.sh")
	if len(calls) != 2 || !reflect.DeepEqual(calls[0].Args, []string{"vm1"}) || !reflect.DeepEqual(calls[1].Args, []string{"lb1"}) {
		t.Errorf("llamadas = %+v, se esperaba [vm1] y [lb1]", calls)
	}
}

// AC-42: el código 15 (jump no se pudo actualizar) se traduce y no guarda ni la llave ni la bandera.
func TestAC42_GenerateUserKeyJumpFailure(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{
		"prepare/generate-user-key.sh": {ExitCode: 15, Stderr: "No se pudo actualizar la cuenta jump de lb1\n"},
	}}
	env := newHAProxyEnv(t, runner)
	env.registerLB(t, "lb1", 1, 80)
	env.registerServer(t, "lb1", "server1", "web-a", 80, "10.10.1.11")

	_, err := env.prepare.GenerateUserKey(context.Background(), "web-a")

	if !errors.Is(err, ErrJumpUpdateFailed) {
		t.Fatalf("err = %v, se esperaba ErrJumpUpdateFailed", err)
	}
	if _, err := env.prepare.DownloadUserKey("web-a"); !errors.Is(err, ErrKeyUnavailable) {
		t.Errorf("no debía quedar una llave: err = %v", err)
	}
	if vm, _ := env.vms.Get(context.Background(), "web-a"); vm.UserKeyGenerated {
		t.Error("la bandera user_key_generated no debía marcarse")
	}
}

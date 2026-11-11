package service

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// AC-18, AC-21: Iniciar, Detener y Reiniciar usan el script de la acción con la unidad del tipo,
// quedan siempre en el historial y devuelven el estado leído tras iniciar o reiniciar.
func TestAC18_WebActionUsesTypeUnit(t *testing.T) {
	tests := []struct {
		vm, typ, action, unit string
	}{
		{"web-a", WebTypeNginx, "start", "nginx"},
		{"web-a", WebTypeNginx, "stop", "nginx"},
		{"web-b", WebTypeApache, "restart", "apache2"},
	}
	for _, tt := range tests {
		t.Run(tt.action+" "+tt.typ, func(t *testing.T) {
			script := "web/" + tt.action + "-web.sh"
			runner := &fakeRunner{results: map[string]RunResult{
				script: {Stdout: []byte("Servicio " + tt.unit + " hecho en " + tt.vm + "\nactive_state=failed\n")},
			}}
			env := newWebEnv(t, runner)
			env.registerWeb(t, tt.vm, tt.typ)

			res, err := env.svc.WebAction(context.Background(), tt.vm, tt.action)

			if err != nil {
				t.Fatalf("WebAction: %v", err)
			}
			want := ServiceResult{Message: "Servicio " + tt.unit + " hecho en " + tt.vm, ActiveState: "failed"}
			if res != want {
				t.Errorf("resultado = %+v, se esperaba %+v", res, want)
			}
			calls := runner.callsTo(script)
			if len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, []string{tt.vm, tt.unit}) {
				t.Errorf("llamadas = %+v, se esperaba [%s %s]", calls, tt.vm, tt.unit)
			}
			if op := env.lastOperation(t); op.Operation != tt.action+"_web" || op.VM != tt.vm || op.Result != "ok" {
				t.Errorf("historial = %+v, se esperaba %s_web ok", op, tt.action)
			}
		})
	}
	t.Run("acción inválida o VM que no es servidor web", func(t *testing.T) {
		runner := &fakeRunner{}
		env := newWebEnv(t, runner)
		env.registerWeb(t, "web-a", WebTypeNginx)
		if _, err := env.vms.Insert(context.Background(), "vm1"); err != nil {
			t.Fatal(err)
		}

		if _, err := env.svc.WebAction(context.Background(), "web-a", "enable"); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("enable: err = %v, se esperaba ErrInvalidInput", err)
		}
		if _, err := env.svc.WebAction(context.Background(), "vm1", "start"); !errors.Is(err, ErrVMNotFound) {
			t.Errorf("vm1: err = %v, se esperaba ErrVMNotFound", err)
		}
		if n := runner.callCount(); n != 0 {
			t.Errorf("el runner se llamó %d veces, se esperaba 0", n)
		}
	})
}

// AC-18: en un servidor asignado a un balanceador la acción entra por él (--jump).
func TestAC18_WebActionOnServerPassesJump(t *testing.T) {
	runner := &fakeRunner{}
	env := newWebEnv(t, runner)
	env.registerWeb(t, "web-a", WebTypeApache)
	env.assignToLB(t, "lb1", "web-a")

	if _, err := env.svc.WebAction(context.Background(), "web-a", "stop"); err != nil {
		t.Fatalf("WebAction: %v", err)
	}
	want := []string{"--jump", "lb1", "web-a", "apache2"}
	if got := runner.callsTo("web/stop-web.sh")[0].Args; !reflect.DeepEqual(got, want) {
		t.Errorf("argv = %q, se esperaba %q", got, want)
	}
}

// AC-20: Estado devuelve la salida tal cual y solo queda en el historial si falla.
func TestAC20_WebStatusRecordsOnlyFailures(t *testing.T) {
	const out = "nginx.service - A high performance web server\n     Active: failed (Result: exit-code)\n"
	runner := &fakeRunner{results: map[string]RunResult{"web/status-web.sh": {Stdout: []byte(out)}}}
	env := newWebEnv(t, runner)
	env.registerWeb(t, "web-a", WebTypeNginx)
	ctx := context.Background()

	got, err := env.svc.WebStatus(ctx, "web-a")

	if err != nil || got != out {
		t.Fatalf("WebStatus = %q, %v", got, err)
	}
	if args := runner.callsTo("web/status-web.sh")[0].Args; !reflect.DeepEqual(args, []string{"web-a", "nginx"}) {
		t.Errorf("argv = %q, se esperaba [web-a nginx]", args)
	}
	if ops, _ := env.history.ListRecent(ctx, webModule, historyLimit); len(ops) != 0 {
		t.Errorf("una lectura correcta no deja fila: %+v", ops)
	}

	runner.results["web/status-web.sh"] = RunResult{ExitCode: 4, Stderr: "fallo de SSH\n"}
	if _, err := env.svc.WebStatus(ctx, "web-a"); err == nil {
		t.Fatal("se esperaba un error")
	}
	if op := env.lastOperation(t); op.Operation != "status_web" || op.Result != "error" {
		t.Errorf("historial = %+v, se esperaba status_web error", op)
	}
}

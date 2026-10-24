package service

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestAC18_ServiceActionRunsItsScript(t *testing.T) {
	for _, action := range []string{"start", "stop", "restart", "enable", "disable"} {
		t.Run(action, func(t *testing.T) {
			script := "services/" + action + "-service.sh"
			runner := &fakeRunner{results: map[string]RunResult{script: {Stdout: []byte("Servicio api " + action + " en vm1\n")}}}
			env := newServicesEnv(t, runner)
			env.register(t, "vm1")

			res, err := env.svc.ServiceAction(context.Background(), "vm1", "api", action)
			if err != nil {
				t.Fatalf("ServiceAction: %v", err)
			}

			if res.Message != "Servicio api "+action+" en vm1" || res.ActiveState != "" {
				t.Errorf("resultado = %+v", res)
			}
			calls := runner.callsTo(script)
			if len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, []string{"vm1", "api"}) {
				t.Errorf("llamadas = %+v, se esperaba una con argv [vm1 api]", calls)
			}
			// Las acciones siempre se registran, con éxito o con fallo (REQ-27).
			ops := env.operations(t)
			if len(ops) != 1 || ops[0].Operation != action+"_service" || ops[0].Result != "ok" {
				t.Errorf("historial = %+v, se esperaba una fila %s_service ok", ops, action)
			}
		})
	}
}

func TestAC18_ServiceActionRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name, service, action string
	}{
		{"acción desconocida", "api", "reload"},
		{"acción vacía", "api", ""},
		{"acción con ruta", "api", "../start"},
		{"nombre inválido", "Demian", "start"},
		{"nombre con punto y coma", "app;rm", "stop"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeRunner{}
			env := newServicesEnv(t, runner)
			env.register(t, "vm1")

			_, err := env.svc.ServiceAction(context.Background(), "vm1", tt.service, tt.action)

			if !errors.Is(err, ErrInvalidInput) {
				t.Errorf("err = %v, se esperaba ErrInvalidInput", err)
			}
			if n := runner.callCount(); n != 0 {
				t.Errorf("el runner se llamó %d veces, se esperaban 0", n)
			}
		})
	}
}

func TestAC42_StartReturnsNonActiveState(t *testing.T) {
	for _, action := range []string{"start", "restart"} {
		t.Run(action, func(t *testing.T) {
			script := "services/" + action + "-service.sh"
			runner := &fakeRunner{results: map[string]RunResult{script: {
				Stdout: []byte("Servicio api " + action + " en vm1\nactive_state=activating (auto-restart)\n")}}}
			env := newServicesEnv(t, runner)
			env.register(t, "vm1")

			res, err := env.svc.ServiceAction(context.Background(), "vm1", "api", action)

			if err != nil || res.ActiveState != "activating (auto-restart)" {
				t.Errorf("resultado = %+v, err = %v; se esperaba el estado sin error", res, err)
			}
		})
	}
}

func TestAC18_ServiceActionMapsScriptFailures(t *testing.T) {
	tests := []struct {
		name string
		exit int
		want error
	}{
		{"no administrado", 6, ErrNotManagedService},
		{"sin IP", 7, ErrVMNoIP},
		{"paso fallido", 5, ErrScriptFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeRunner{results: map[string]RunResult{"services/stop-service.sh": {ExitCode: tt.exit, Stderr: "x\n"}}}
			env := newServicesEnv(t, runner)
			env.register(t, "vm1")

			_, err := env.svc.ServiceAction(context.Background(), "vm1", "api", "stop")

			if !errors.Is(err, tt.want) {
				t.Errorf("err = %v, se esperaba %v", err, tt.want)
			}
			if ops := env.operations(t); len(ops) != 1 || ops[0].Result != "error" {
				t.Errorf("historial = %+v, se esperaba una fila con result error", ops)
			}
		})
	}
}

const statusOutput = "● api.service - API interna\n     Loaded: loaded (/etc/systemd/system/api.service; enabled)\n     Active: failed (Result: exit-code)\n"

func TestAC21_ServiceStatusReturnsOutput(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{"services/status-service.sh": {Stdout: []byte(statusOutput)}}}
	env := newServicesEnv(t, runner)
	env.register(t, "vm1")

	got, err := env.svc.ServiceStatus(context.Background(), "vm1", "api")
	if err != nil {
		t.Fatalf("ServiceStatus: %v", err)
	}

	if got != statusOutput {
		t.Errorf("salida = %q, se esperaba %q", got, statusOutput)
	}
	calls := runner.callsTo("services/status-service.sh")
	if len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, []string{"vm1", "api"}) {
		t.Errorf("llamadas = %+v, se esperaba una con argv [vm1 api]", calls)
	}
	// Una lectura correcta no deja fila en el historial (REQ-27).
	if ops := env.operations(t); len(ops) != 0 {
		t.Errorf("la lectura dejó %d filas en el historial, se esperaban 0", len(ops))
	}
}

func TestAC22_ServiceLogsReturnsOutput(t *testing.T) {
	const logs = "oct 22 10:00:00 vm1 api[1]: arrancó\noct 22 10:00:01 vm1 api[1]: escuchando\n"
	runner := &fakeRunner{results: map[string]RunResult{"services/logs-service.sh": {Stdout: []byte(logs)}}}
	env := newServicesEnv(t, runner)
	env.register(t, "vm1")

	got, err := env.svc.ServiceLogs(context.Background(), "vm1", "api")
	if err != nil {
		t.Fatalf("ServiceLogs: %v", err)
	}

	if got != logs {
		t.Errorf("salida = %q, se esperaba %q", got, logs)
	}
	calls := runner.callsTo("services/logs-service.sh")
	if len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, []string{"vm1", "api"}) {
		t.Errorf("llamadas = %+v, se esperaba una con argv [vm1 api]", calls)
	}
	if ops := env.operations(t); len(ops) != 0 {
		t.Errorf("la lectura dejó %d filas en el historial, se esperaban 0", len(ops))
	}
}

func TestAC21_ServiceStatusAndLogsRecordFailuresOnly(t *testing.T) {
	tests := []struct {
		name      string
		script    string
		operation string
		read      func(*ServicesService) (string, error)
	}{
		{"status", "services/status-service.sh", "service_status", func(s *ServicesService) (string, error) {
			return s.ServiceStatus(context.Background(), "vm1", "api")
		}},
		{"logs", "services/logs-service.sh", "service_logs", func(s *ServicesService) (string, error) {
			return s.ServiceLogs(context.Background(), "vm1", "api")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeRunner{results: map[string]RunResult{tt.script: {ExitCode: 6, Stderr: "Solo se pueden gestionar servicios creados por Stratus\n"}}}
			env := newServicesEnv(t, runner)
			env.register(t, "vm1")

			_, err := tt.read(env.svc)

			if !errors.Is(err, ErrNotManagedService) {
				t.Errorf("err = %v, se esperaba ErrNotManagedService", err)
			}
			ops := env.operations(t)
			if len(ops) != 1 || ops[0].Operation != tt.operation || ops[0].ExitCode != 6 {
				t.Errorf("historial = %+v, se esperaba una fila %s con código 6", ops, tt.operation)
			}
		})
	}
}

func TestAC21_ServiceStatusRejectsInvalidName(t *testing.T) {
	runner := &fakeRunner{}
	env := newServicesEnv(t, runner)
	env.register(t, "vm1")

	if _, err := env.svc.ServiceStatus(context.Background(), "vm1", "Demian"); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("ServiceStatus err = %v, se esperaba ErrInvalidInput", err)
	}
	if _, err := env.svc.ServiceLogs(context.Background(), "vm1", "app;rm"); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("ServiceLogs err = %v, se esperaba ErrInvalidInput", err)
	}
	if n := runner.callCount(); n != 0 {
		t.Errorf("el runner se llamó %d veces, se esperaban 0", n)
	}
}

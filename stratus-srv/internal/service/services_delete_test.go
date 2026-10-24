package service

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

const deleteServiceScript = "services/delete-service.sh"

func TestAC28_DeleteServicePassesArgs(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{deleteServiceScript: {Stdout: []byte("Servicio api eliminado de vm1\n")}}}
	env := newServicesEnv(t, runner)
	env.register(t, "vm1")

	msg, err := env.svc.DeleteService(context.Background(), "vm1", "api")
	if err != nil {
		t.Fatalf("DeleteService: %v", err)
	}

	if msg != "Servicio api eliminado de vm1" {
		t.Errorf("mensaje = %q", msg)
	}
	calls := runner.callsTo(deleteServiceScript)
	if len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, []string{"vm1", "api"}) {
		t.Errorf("llamadas = %+v, se esperaba una con argv [vm1 api]", calls)
	}
	ops := env.operations(t)
	if len(ops) != 1 || ops[0].Operation != "delete_service" || ops[0].Result != "ok" {
		t.Errorf("historial = %+v, se esperaba una fila delete_service ok", ops)
	}
}

func TestAC41_DeleteServiceFailureNamesService(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{deleteServiceScript: {ExitCode: 5,
		Stderr: "userdel: user api is currently used by process 123\nNo se pudo eliminar el servicio api: falló el paso 5\n"}}}
	env := newServicesEnv(t, runner)
	env.register(t, "vm1")

	_, err := env.svc.DeleteService(context.Background(), "vm1", "api")

	var opErr *OperationError
	if !errors.As(err, &opErr) || opErr.ExitCode != 5 {
		t.Fatalf("err = %v, se esperaba *OperationError con código 5", err)
	}
	if !strings.Contains(err.Error(), "No se pudo eliminar el servicio api") {
		t.Errorf("mensaje = %q, se esperaba que nombrara el servicio", err.Error())
	}
	ops := env.operations(t)
	if len(ops) != 1 || ops[0].Result != "error" || !strings.Contains(ops[0].Stderr, "falló el paso 5") {
		t.Errorf("historial = %+v, se esperaba una fila con result error y el paso", ops)
	}
}

func TestAC20_DeleteServiceMapsNotManagedAndRejectsInvalidName(t *testing.T) {
	t.Run("no administrado", func(t *testing.T) {
		runner := &fakeRunner{results: map[string]RunResult{deleteServiceScript: {ExitCode: 6}}}
		env := newServicesEnv(t, runner)
		env.register(t, "vm1")

		if _, err := env.svc.DeleteService(context.Background(), "vm1", "ssh"); !errors.Is(err, ErrNotManagedService) {
			t.Errorf("err = %v, se esperaba ErrNotManagedService", err)
		}
	})
	t.Run("nombre inválido", func(t *testing.T) {
		runner := &fakeRunner{}
		env := newServicesEnv(t, runner)
		env.register(t, "vm1")

		if _, err := env.svc.DeleteService(context.Background(), "vm1", "Demian"); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("err = %v, se esperaba ErrInvalidInput", err)
		}
		if n := runner.callCount(); n != 0 {
			t.Errorf("el runner se llamó %d veces, se esperaban 0", n)
		}
	})
}

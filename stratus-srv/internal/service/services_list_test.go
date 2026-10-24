package service

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"stratus-srv/internal/repository"
)

const servicesListScript = "services/list-services.sh"

// operations devuelve las filas del historial del módulo services, la más reciente primero.
func (e servicesEnv) operations(t *testing.T) []repository.Operation {
	t.Helper()
	ops, err := e.history.ListRecent(context.Background(), servicesModule, 100)
	if err != nil {
		t.Fatalf("ListRecent: %v", err)
	}
	return ops
}

// Salida de list-services.sh del AC-15: demian-lee activo y habilitado con 3 claves, api inactivo.
const listStdout = "demian-lee\tactive\tenabled\ton-failure\t5\tdemian-lee\t\tPORT,SERVER_NAME,ADMIN_PASSWORD\tServidor de producción\n" +
	"api\tinactive\tdisabled\talways\t10\tbin/server\t--port 9090\t\tAPI interna\n"

func TestAC15_ListServicesParsesRows(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{servicesListScript: {Stdout: []byte(listStdout)}}}
	env := newServicesEnv(t, runner)
	env.register(t, "vm1")

	got, err := env.svc.ListServices(context.Background(), "vm1")
	if err != nil {
		t.Fatalf("ListServices: %v", err)
	}

	want := []ServiceView{
		{Name: "demian-lee", Description: "Servidor de producción", ActiveState: "active", EnabledState: "enabled",
			Restart: "on-failure", RestartSec: 5, Executable: "demian-lee", Args: "",
			EnvKeys: []string{"PORT", "SERVER_NAME", "ADMIN_PASSWORD"}},
		{Name: "api", Description: "API interna", ActiveState: "inactive", EnabledState: "disabled",
			Restart: "always", RestartSec: 10, Executable: "bin/server", Args: "--port 9090", EnvKeys: []string{}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("servicios = %+v, se esperaba %+v", got, want)
	}
	calls := runner.callsTo(servicesListScript)
	if len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, []string{"vm1"}) {
		t.Errorf("llamadas = %+v, se esperaba una con argv [vm1]", calls)
	}
	if ops := env.operations(t); len(ops) != 0 {
		t.Errorf("un listado correcto dejó %d filas en el historial, se esperaban 0", len(ops))
	}
}

func TestAC15_ListServicesEmptyAndMalformedOutput(t *testing.T) {
	tests := []struct {
		name    string
		stdout  string
		wantErr string
	}{
		{"sin servicios", "", ""},
		{"campos de menos", "api\tactive\tenabled\n", "salida inesperada de list-services.sh"},
		{"RestartSec no numérico", "api\tactive\tenabled\talways\tx\tbin/app\t\t\tAPI\n", "salida inesperada de list-services.sh"},
		{"Restart inválido", "api\tactive\tenabled\ton-abort\t5\tbin/app\t\t\tAPI\n", "salida inesperada de list-services.sh"},
		{"campos de más", "api\tactive\tenabled\talways\t5\tbin/app\t\t\tAPI\tx\n", "salida inesperada de list-services.sh"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeRunner{results: map[string]RunResult{servicesListScript: {Stdout: []byte(tt.stdout)}}}
			env := newServicesEnv(t, runner)
			env.register(t, "vm1")

			got, err := env.svc.ListServices(context.Background(), "vm1")

			if tt.wantErr == "" {
				if err != nil || got == nil || len(got) != 0 {
					t.Fatalf("got %v, err %v; se esperaba una lista vacía no nula", got, err)
				}
				return
			}
			if err == nil || err.Error() != tt.wantErr {
				t.Errorf("err = %v, se esperaba %q", err, tt.wantErr)
			}
		})
	}
}

func TestAC15_ListServicesRecordsFailuresOnly(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{
		servicesListScript: {ExitCode: 7, Stderr: "La VM no tiene dirección IP; verifique que esté encendida\n"},
	}}
	env := newServicesEnv(t, runner)
	env.register(t, "vm1")

	_, err := env.svc.ListServices(context.Background(), "vm1")

	if !errors.Is(err, ErrVMNoIP) {
		t.Fatalf("err = %v, se esperaba ErrVMNoIP", err)
	}
	ops := env.operations(t)
	if len(ops) != 1 || ops[0].Operation != "list_services" || ops[0].ExitCode != 7 || ops[0].Result != "error" {
		t.Errorf("historial = %+v, se esperaba una fila list_services con código 7", ops)
	}
}

func TestAC15_ListServicesRejectsUnknownVM(t *testing.T) {
	runner := &fakeRunner{}
	env := newServicesEnv(t, runner)

	if _, err := env.svc.ListServices(context.Background(), "vm1"); !errors.Is(err, ErrVMNotFound) {
		t.Errorf("err = %v, se esperaba ErrVMNotFound", err)
	}
	if n := runner.callCount(); n != 0 {
		t.Errorf("el runner se llamó %d veces, se esperaban 0", n)
	}
}

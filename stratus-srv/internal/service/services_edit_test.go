package service

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

const editServiceScript = "services/edit-service.sh"

// validEditInput es la edición del AC-23: Restart always y el archivo de variables sin cambios.
func validEditInput() EditServiceInput {
	return EditServiceInput{Description: "Servidor de producción", Executable: "demian-lee", Restart: "always", RestartSec: 5}
}

// assertEditServiceRejected comprueba que EditService rechaza la entrada con ErrInvalidInput sin
// llamar al script y sin dejar archivos temporales.
func assertEditServiceRejected(t *testing.T, in EditServiceInput) {
	t.Helper()
	dir := useTempDir(t)
	runner := &fakeRunner{}
	env := newServicesEnv(t, runner)
	env.register(t, "vm1")

	_, err := env.svc.EditService(context.Background(), "vm1", "demian-lee", in)

	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("err = %v, se esperaba ErrInvalidInput", err)
	}
	if n := runner.callCount(); n != 0 {
		t.Errorf("el runner se llamó %d veces, se esperaban 0", n)
	}
	if left := leftovers(t, dir); len(left) != 0 {
		t.Errorf("quedaron archivos temporales: %v", left)
	}
}

func TestAC23_EditServicePassesArgs(t *testing.T) {
	useTempDir(t)
	runner := &fakeRunner{results: map[string]RunResult{editServiceScript: {
		Stdout: []byte("Servicio demian-lee actualizado en vm1\nactive_state=active\n")}}}
	env := newServicesEnv(t, runner)
	env.register(t, "vm1")

	res, err := env.svc.EditService(context.Background(), "vm1", "demian-lee", validEditInput())
	if err != nil {
		t.Fatalf("EditService: %v", err)
	}

	if res != (ServiceResult{Message: "Servicio demian-lee actualizado en vm1", ActiveState: "active"}) {
		t.Errorf("resultado = %+v", res)
	}
	calls := runner.callsTo(editServiceScript)
	want := []string{"vm1", "demian-lee", "Servidor de producción", "demian-lee", "", "always", "5", "-"}
	if len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, want) {
		t.Errorf("llamadas = %+v, se esperaba una con argv %q", calls, want)
	}
	ops := env.operations(t)
	if len(ops) != 1 || ops[0].Operation != "edit_service" || ops[0].Result != "ok" {
		t.Errorf("historial = %+v, se esperaba una fila edit_service ok", ops)
	}
}

func TestAC23_EditServiceRejectsValuesWithoutReplace(t *testing.T) {
	in := validEditInput()
	in.Env = []EnvVar{{"PORT", "1"}}
	assertEditServiceRejected(t, in)
}

func TestAC23_EditServiceRejectsInvalidName(t *testing.T) {
	runner := &fakeRunner{}
	env := newServicesEnv(t, runner)
	env.register(t, "vm1")

	_, err := env.svc.EditService(context.Background(), "vm1", "Demian", validEditInput())

	if !errors.Is(err, ErrInvalidInput) || runner.callCount() != 0 {
		t.Errorf("err = %v, llamadas = %d; se esperaba ErrInvalidInput y 0 llamadas", err, runner.callCount())
	}
}

func TestAC25_EditServiceReplaceEnvUsesSecretFile(t *testing.T) {
	useTempDir(t)
	var envPath, during string
	var mode os.FileMode
	runner := funcRunner(func(_ context.Context, _ string, args ...string) (RunResult, error) {
		envPath = args[len(args)-1]
		if data, err := os.ReadFile(envPath); err == nil {
			during = string(data)
		}
		if info, err := os.Stat(envPath); err == nil {
			mode = info.Mode().Perm()
		}
		return RunResult{Stdout: []byte("Servicio demian-lee actualizado en vm1\n")}, nil
	})
	env := newServicesEnv(t, runner)
	env.register(t, "vm1")
	in := validEditInput()
	in.ReplaceEnv = true
	in.Env = []EnvVar{{"PORT", "9090"}}

	if _, err := env.svc.EditService(context.Background(), "vm1", "demian-lee", in); err != nil {
		t.Fatalf("EditService: %v", err)
	}

	if during != "PORT=9090\n" || mode != 0o600 || !strings.Contains(envPath, "stratus-env-") {
		t.Errorf("durante Run el archivo %q tenía %q con modo %v", envPath, during, mode)
	}
	if _, err := os.Stat(envPath); !os.IsNotExist(err) {
		t.Errorf("%q sigue existiendo (%v)", envPath, err)
	}
}

func TestAC11_EditServiceEnvValuesOnlyInSecretFile(t *testing.T) {
	for _, exit := range []int{0, 5} {
		t.Run("código "+string(rune('0'+exit)), func(t *testing.T) {
			dir := useTempDir(t)
			runner := funcRunner(func(_ context.Context, _ string, args ...string) (RunResult, error) {
				for _, a := range args {
					if strings.Contains(a, "Secreta123") {
						t.Errorf("el valor secreto aparece en argv: %q", a)
					}
				}
				return RunResult{ExitCode: exit, Stdout: []byte("Servicio demian-lee actualizado en vm1\n"), Stderr: "falló\n"}, nil
			})
			env := newServicesEnv(t, runner)
			env.register(t, "vm1")
			in := validEditInput()
			in.ReplaceEnv = true
			in.Env = []EnvVar{{"ADMIN_PASSWORD", "Secreta123"}}

			_, _ = env.svc.EditService(context.Background(), "vm1", "demian-lee", in)

			if left := leftovers(t, dir); len(left) != 0 {
				t.Errorf("quedaron archivos temporales: %v", left)
			}
			for _, row := range env.operations(t) {
				if strings.Contains(row.Stderr, "Secreta123") || strings.Contains(strings.Join(row.Args, " "), "Secreta123") {
					t.Errorf("el historial contiene el valor: %+v", row)
				}
			}
			if strings.Contains(env.logs.String(), "Secreta123") {
				t.Errorf("el log contiene el valor: %s", env.logs.String())
			}
		})
	}
}

func TestAC7_EditServiceMapsExecutableMissingAndNotManaged(t *testing.T) {
	tests := []struct {
		name string
		exit int
		want error
	}{
		{"ejecutable ausente", 11, ErrExecutableMissing},
		{"no administrado", 6, ErrNotManagedService},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeRunner{results: map[string]RunResult{editServiceScript: {ExitCode: tt.exit, Stderr: "x\n"}}}
			env := newServicesEnv(t, runner)
			env.register(t, "vm1")

			_, err := env.svc.EditService(context.Background(), "vm1", "demian-lee", validEditInput())

			if !errors.Is(err, tt.want) {
				t.Errorf("err = %v, se esperaba %v", err, tt.want)
			}
		})
	}
}

func TestAC40_EditAndRedeployRestoreFailureMessages(t *testing.T) {
	const stderr = "Falló el paso systemctl en la VM (código 5)\n" +
		"Restauración fallida; puede quedar en la VM: /etc/systemd/system/demian-lee.service, /etc/demian-lee.env, /var/lib/stratus/backup/demian-lee\n"
	runner := &fakeRunner{results: map[string]RunResult{editServiceScript: {ExitCode: 9, Stderr: stderr}}}
	env := newServicesEnv(t, runner)
	env.register(t, "vm1")

	_, err := env.svc.EditService(context.Background(), "vm1", "demian-lee", validEditInput())

	var rollback *ServiceRollbackError
	if !errors.As(err, &rollback) {
		t.Fatalf("err = %v, se esperaba *ServiceRollbackError", err)
	}
	const want = "No se pudo editar el servicio demian-lee y la restauración falló; revise la VM. " +
		"Restauración fallida; puede quedar en la VM: /etc/systemd/system/demian-lee.service, /etc/demian-lee.env, /var/lib/stratus/backup/demian-lee"
	if err.Error() != want {
		t.Errorf("mensaje = %q, se esperaba %q", err.Error(), want)
	}
	ops := env.operations(t)
	if len(ops) != 1 || !strings.Contains(ops[0].Stderr, "Falló el paso systemctl") || !strings.Contains(ops[0].Stderr, "Restauración fallida") {
		t.Errorf("historial = %+v, se esperaba stderr con los dos errores", ops)
	}
}

func TestAC23_EditServiceReturnsNonActiveState(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{editServiceScript: {
		Stdout: []byte("Servicio demian-lee actualizado en vm1\nactive_state=failed\n")}}}
	env := newServicesEnv(t, runner)
	env.register(t, "vm1")

	res, err := env.svc.EditService(context.Background(), "vm1", "demian-lee", validEditInput())

	if err != nil || res.ActiveState != "failed" {
		t.Errorf("resultado = %+v, err = %v; se esperaba el estado failed sin error", res, err)
	}
}

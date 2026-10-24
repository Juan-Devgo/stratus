package service

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

const createServiceScript = "services/create-service.sh"

// validCreateInput es la entrada del AC-1: demian-lee con tres variables de entorno.
func validCreateInput() CreateServiceInput {
	return CreateServiceInput{
		Name: "demian-lee", Description: "Servidor de producción", Executable: "demian-lee",
		Restart: "on-failure", RestartSec: 5,
		Env: []EnvVar{{"PORT", "8080"}, {"SERVER_NAME", "Servidor-Produccion-1"}, {"ADMIN_PASSWORD", "Secreta123"}},
	}
}

func tarGzUpload(body string) ArchiveUpload {
	return ArchiveUpload{Filename: "demian-lee.tar.gz", Body: strings.NewReader(body)}
}

// assertCreateServiceRejected comprueba que CreateService rechaza la entrada con ErrInvalidInput
// sin llamar al script y sin dejar archivos temporales.
func assertCreateServiceRejected(t *testing.T, in CreateServiceInput) {
	t.Helper()
	dir := useTempDir(t)
	runner := &fakeRunner{}
	env := newServicesEnv(t, runner)
	env.register(t, "vm1")

	_, err := env.svc.CreateService(context.Background(), "vm1", in, tarGzUpload("contenido"))

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

func TestAC1_CreateServicePassesDiscreteArgs(t *testing.T) {
	useTempDir(t)
	var archiveContent, envContent string
	runner := funcRunner(func(_ context.Context, script string, args ...string) (RunResult, error) {
		if script != createServiceScript || len(args) != 9 {
			t.Errorf("script = %q, args = %v", script, args)
			return RunResult{}, nil
		}
		a, _ := os.ReadFile(args[3])
		e, _ := os.ReadFile(args[8])
		archiveContent, envContent = string(a), string(e)
		return RunResult{Stdout: []byte("Servicio demian-lee creado en vm1\nactive_state=active\n")}, nil
	})
	env := newServicesEnv(t, runner)
	env.register(t, "vm1")

	res, err := env.svc.CreateService(context.Background(), "vm1", validCreateInput(), tarGzUpload("contenido-del-archivo"))
	if err != nil {
		t.Fatalf("CreateService: %v", err)
	}

	if res != (ServiceResult{Message: "Servicio demian-lee creado en vm1", ActiveState: "active"}) {
		t.Errorf("resultado = %+v", res)
	}
	if archiveContent != "contenido-del-archivo" || envContent != "PORT=8080\nSERVER_NAME=Servidor-Produccion-1\nADMIN_PASSWORD=Secreta123\n" {
		t.Errorf("archivo = %q, variables = %q", archiveContent, envContent)
	}
}

func TestAC1_CreateServiceArgvIsDiscrete(t *testing.T) {
	useTempDir(t)
	runner := &fakeRunner{results: map[string]RunResult{createServiceScript: {Stdout: []byte("Servicio demian-lee creado en vm1\nactive_state=active\n")}}}
	env := newServicesEnv(t, runner)
	env.register(t, "vm1")
	in := validCreateInput()
	in.Args = "--port 8080 -v"

	if _, err := env.svc.CreateService(context.Background(), "vm1", in, tarGzUpload("x")); err != nil {
		t.Fatalf("CreateService: %v", err)
	}

	calls := runner.callsTo(createServiceScript)
	if len(calls) != 1 {
		t.Fatalf("llamadas = %+v, se esperaba una", calls)
	}
	a := calls[0].Args
	// vm, name, description, archive path, executable, args (un solo elemento), restart, restart_sec, env path.
	if len(a) != 9 || !reflect.DeepEqual([]string{a[0], a[1], a[2], a[4], a[5], a[6], a[7]},
		[]string{"vm1", "demian-lee", "Servidor de producción", "demian-lee", "--port 8080 -v", "on-failure", "5"}) {
		t.Errorf("argv = %q", a)
	}
	if !strings.HasPrefix(a[3], os.TempDir()) || !strings.HasSuffix(a[3], ".tar.gz") ||
		!strings.Contains(a[3], "stratus-archive-") || !strings.Contains(a[8], "stratus-env-") {
		t.Errorf("rutas temporales = %q y %q", a[3], a[8])
	}
}

func TestAC4_CreateServiceNameInUseMapsError(t *testing.T) {
	useTempDir(t)
	runner := &fakeRunner{results: map[string]RunResult{createServiceScript: {ExitCode: 8,
		Stderr: "Ya existe un servicio o usuario con ese nombre en la VM\n"}}}
	env := newServicesEnv(t, runner)
	env.register(t, "vm1")

	_, err := env.svc.CreateService(context.Background(), "vm1", validCreateInput(), tarGzUpload("x"))

	if !errors.Is(err, ErrServiceExists) {
		t.Errorf("err = %v, se esperaba ErrServiceExists", err)
	}
}

func TestAC6_InvalidArchiveMapsError(t *testing.T) {
	tests := []struct {
		name string
		exit int
		want error
	}{
		{"archivo inválido", 10, ErrInvalidArchive},
		{"ejecutable ausente", 11, ErrExecutableMissing},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useTempDir(t)
			runner := &fakeRunner{results: map[string]RunResult{createServiceScript: {ExitCode: tt.exit, Stderr: "x\n"}}}
			env := newServicesEnv(t, runner)
			env.register(t, "vm1")

			_, err := env.svc.CreateService(context.Background(), "vm1", validCreateInput(), tarGzUpload("x"))

			if !errors.Is(err, tt.want) {
				t.Errorf("err = %v, se esperaba %v", err, tt.want)
			}
		})
	}
}

func TestAC12_CreateServiceFailureReturnsOperationError(t *testing.T) {
	dir := useTempDir(t)
	runner := &fakeRunner{results: map[string]RunResult{createServiceScript: {ExitCode: 5,
		Stderr: "Falló el paso tar en la VM (código 5)\n"}}}
	env := newServicesEnv(t, runner)
	env.register(t, "vm1")

	_, err := env.svc.CreateService(context.Background(), "vm1", validCreateInput(), tarGzUpload("x"))

	var opErr *OperationError
	if !errors.As(err, &opErr) || opErr.ExitCode != 5 {
		t.Fatalf("err = %v, se esperaba *OperationError con código 5", err)
	}
	ops := env.operations(t)
	if len(ops) != 1 || ops[0].Operation != "create_service" || ops[0].Result != "error" || ops[0].ExitCode != 5 {
		t.Errorf("historial = %+v, se esperaba una fila create_service con result error", ops)
	}
	if left := leftovers(t, dir); len(left) != 0 {
		t.Errorf("quedaron archivos temporales: %v", left)
	}
}

func TestAC13_CreateServiceCleanupFailureMessage(t *testing.T) {
	useTempDir(t)
	const stderr = "Falló el paso tar en la VM (código 5)\n" +
		"Limpieza fallida; puede quedar en la VM: /etc/systemd/system/demian-lee.service, /etc/demian-lee.env, /opt/demian-lee, usuario y grupo demian-lee\n"
	runner := &fakeRunner{results: map[string]RunResult{createServiceScript: {ExitCode: 9, Stderr: stderr}}}
	env := newServicesEnv(t, runner)
	env.register(t, "vm1")

	_, err := env.svc.CreateService(context.Background(), "vm1", validCreateInput(), tarGzUpload("x"))

	var rollback *ServiceRollbackError
	if !errors.As(err, &rollback) {
		t.Fatalf("err = %v, se esperaba *ServiceRollbackError", err)
	}
	want := "No se pudo crear el servicio demian-lee y la limpieza falló; revise la VM. " +
		"Limpieza fallida; puede quedar en la VM: /etc/systemd/system/demian-lee.service, /etc/demian-lee.env, /opt/demian-lee, usuario y grupo demian-lee"
	if err.Error() != want {
		t.Errorf("mensaje = %q, se esperaba %q", err.Error(), want)
	}
	// El historial guarda los dos errores.
	ops := env.operations(t)
	if len(ops) != 1 || !strings.Contains(ops[0].Stderr, "Falló el paso tar") || !strings.Contains(ops[0].Stderr, "Limpieza fallida") {
		t.Errorf("historial = %+v, se esperaba stderr con los dos errores", ops)
	}
}

func TestAC14_CreateServiceReturnsNonActiveState(t *testing.T) {
	for _, state := range []string{"failed", "activating (auto-restart)"} {
		t.Run(state, func(t *testing.T) {
			useTempDir(t)
			runner := &fakeRunner{results: map[string]RunResult{createServiceScript: {
				Stdout: []byte("Servicio demian-lee creado en vm1\nactive_state=" + state + "\n")}}}
			env := newServicesEnv(t, runner)
			env.register(t, "vm1")

			res, err := env.svc.CreateService(context.Background(), "vm1", validCreateInput(), tarGzUpload("x"))

			if err != nil || res.ActiveState != state {
				t.Errorf("resultado = %+v, err = %v; se esperaba el estado %q sin error", res, err, state)
			}
		})
	}
}

func TestAC11_EnvValuesOnlyInSecretFile(t *testing.T) {
	for _, exit := range []int{0, 5} {
		t.Run("código "+string(rune('0'+exit)), func(t *testing.T) {
			dir := useTempDir(t)
			var archivePath, envPath string
			var duringEnv string
			var mode os.FileMode
			runner := funcRunner(func(_ context.Context, _ string, args ...string) (RunResult, error) {
				archivePath, envPath = args[3], args[8]
				if data, err := os.ReadFile(envPath); err == nil {
					duringEnv = string(data)
				}
				if info, err := os.Stat(envPath); err == nil {
					mode = info.Mode().Perm()
				}
				for _, a := range args {
					if strings.Contains(a, "Secreta123") {
						t.Errorf("el valor secreto aparece en argv: %q", a)
					}
				}
				return RunResult{ExitCode: exit, Stdout: []byte("Servicio demian-lee creado en vm1\n"), Stderr: "falló\n"}, nil
			})
			env := newServicesEnv(t, runner)
			env.register(t, "vm1")

			_, _ = env.svc.CreateService(context.Background(), "vm1", validCreateInput(), tarGzUpload("x"))

			if !strings.Contains(duringEnv, "ADMIN_PASSWORD=Secreta123\n") || mode != 0o600 {
				t.Errorf("durante Run el archivo tenía %q con modo %v", duringEnv, mode)
			}
			for _, path := range []string{archivePath, envPath} {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Errorf("%q sigue existiendo (%v)", path, err)
				}
			}
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

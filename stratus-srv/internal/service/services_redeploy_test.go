package service

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

const redeployServiceScript = "services/redeploy-service.sh"

func TestAC26_RedeployServicePassesArgs(t *testing.T) {
	dir := useTempDir(t)
	var archivePath, archiveContent string
	runner := funcRunner(func(_ context.Context, script string, args ...string) (RunResult, error) {
		if script != redeployServiceScript || len(args) != 4 {
			t.Errorf("script = %q, args = %v", script, args)
			return RunResult{}, nil
		}
		archivePath = args[2]
		data, _ := os.ReadFile(archivePath)
		archiveContent = string(data)
		if !reflect.DeepEqual([]string{args[0], args[1], args[3]}, []string{"vm1", "demian-lee", "bin/server"}) {
			t.Errorf("argv = %q", args)
		}
		return RunResult{Stdout: []byte("Servicio demian-lee redesplegado en vm1\nactive_state=active\n")}, nil
	})
	env := newServicesEnv(t, runner)
	env.register(t, "vm1")

	res, err := env.svc.RedeployService(context.Background(), "vm1", "demian-lee", "bin/server", tarGzUpload("nuevo-contenido"))
	if err != nil {
		t.Fatalf("RedeployService: %v", err)
	}

	if res != (ServiceResult{Message: "Servicio demian-lee redesplegado en vm1", ActiveState: "active"}) {
		t.Errorf("resultado = %+v", res)
	}
	if archiveContent != "nuevo-contenido" || !strings.Contains(archivePath, "stratus-archive-") || !strings.HasSuffix(archivePath, ".tar.gz") {
		t.Errorf("archivo = %q con contenido %q", archivePath, archiveContent)
	}
	// El archivo temporal se borra al terminar (REQ-12).
	if left := leftovers(t, dir); len(left) != 0 {
		t.Errorf("quedaron archivos temporales: %v", left)
	}
	ops := env.operations(t)
	if len(ops) != 1 || ops[0].Operation != "redeploy_service" || ops[0].Result != "ok" {
		t.Errorf("historial = %+v, se esperaba una fila redeploy_service ok", ops)
	}
}

func TestAC5_RedeployServiceRejectsArchive(t *testing.T) {
	tests := []struct {
		name string
		up   ArchiveUpload
		want string
	}{
		{"11 bytes con máximo 10", ArchiveUpload{Filename: "app.tar.gz", Body: strings.NewReader(strings.Repeat("x", 11))}, "El archivo supera 200 MB"},
		{"extensión .zip", ArchiveUpload{Filename: "app.zip", Body: strings.NewReader("x")}, "El archivo debe ser .tar.gz"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := useTempDir(t)
			runner := &fakeRunner{}
			env := newServicesEnv(t, runner)
			env.svc.cfg.MaxArchiveBytes = 10
			env.register(t, "vm1")

			_, err := env.svc.RedeployService(context.Background(), "vm1", "demian-lee", "demian-lee", tt.up)

			if !errors.Is(err, ErrInvalidInput) || err.Error() != tt.want {
				t.Errorf("err = %v, se esperaba ErrInvalidInput con %q", err, tt.want)
			}
			if n := runner.callCount(); n != 0 {
				t.Errorf("el runner se llamó %d veces, se esperaban 0", n)
			}
			if left := leftovers(t, dir); len(left) != 0 {
				t.Errorf("quedaron archivos temporales: %v", left)
			}
		})
	}
}

func TestAC7_RedeployServiceRejectsInvalidExecutableAndName(t *testing.T) {
	tests := []struct {
		name, service, executable string
	}{
		{"sube un nivel", "demian-lee", "../x"},
		{"ruta absoluta", "demian-lee", "/bin/sh"},
		{"segmento punto", "demian-lee", "a/./b"},
		{"ejecutable vacío", "demian-lee", ""},
		{"nombre inválido", "Demian", "demian-lee"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := useTempDir(t)
			runner := &fakeRunner{}
			env := newServicesEnv(t, runner)
			env.register(t, "vm1")

			_, err := env.svc.RedeployService(context.Background(), "vm1", tt.service, tt.executable, tarGzUpload("x"))

			if !errors.Is(err, ErrInvalidInput) {
				t.Errorf("err = %v, se esperaba ErrInvalidInput", err)
			}
			if n := runner.callCount(); n != 0 {
				t.Errorf("el runner se llamó %d veces, se esperaban 0", n)
			}
			if left := leftovers(t, dir); len(left) != 0 {
				t.Errorf("quedaron archivos temporales: %v", left)
			}
		})
	}
}

func TestAC6_RedeployInvalidArchiveMapsError(t *testing.T) {
	useTempDir(t)
	runner := &fakeRunner{results: map[string]RunResult{redeployServiceScript: {ExitCode: 10, Stderr: "Archivo comprimido inválido\n"}}}
	env := newServicesEnv(t, runner)
	env.register(t, "vm1")

	_, err := env.svc.RedeployService(context.Background(), "vm1", "demian-lee", "demian-lee", tarGzUpload("x"))

	if !errors.Is(err, ErrInvalidArchive) {
		t.Errorf("err = %v, se esperaba ErrInvalidArchive", err)
	}
}

func TestAC38_RedeployMapsExecutableMissing(t *testing.T) {
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
			useTempDir(t)
			runner := &fakeRunner{results: map[string]RunResult{redeployServiceScript: {ExitCode: tt.exit, Stderr: "x\n"}}}
			env := newServicesEnv(t, runner)
			env.register(t, "vm1")

			_, err := env.svc.RedeployService(context.Background(), "vm1", "demian-lee", "bin/server", tarGzUpload("x"))

			if !errors.Is(err, tt.want) {
				t.Errorf("err = %v, se esperaba %v", err, tt.want)
			}
		})
	}
}

func TestAC27_RedeployFailureReturnsOperationError(t *testing.T) {
	t.Run("fallo con exit 5", func(t *testing.T) {
		dir := useTempDir(t)
		runner := &fakeRunner{results: map[string]RunResult{redeployServiceScript: {ExitCode: 5,
			Stderr: "Falló el paso chown en la VM (código 5)\n"}}}
		env := newServicesEnv(t, runner)
		env.register(t, "vm1")

		_, err := env.svc.RedeployService(context.Background(), "vm1", "demian-lee", "demian-lee", tarGzUpload("x"))

		var opErr *OperationError
		if !errors.As(err, &opErr) || opErr.ExitCode != 5 {
			t.Fatalf("err = %v, se esperaba *OperationError con código 5", err)
		}
		ops := env.operations(t)
		if len(ops) != 1 || ops[0].Operation != "redeploy_service" || ops[0].Result != "error" {
			t.Errorf("historial = %+v, se esperaba una fila redeploy_service con result error", ops)
		}
		if left := leftovers(t, dir); len(left) != 0 {
			t.Errorf("quedaron archivos temporales: %v", left)
		}
	})

	t.Run("app nueva que falla al iniciar", func(t *testing.T) {
		useTempDir(t)
		runner := &fakeRunner{results: map[string]RunResult{redeployServiceScript: {
			Stdout: []byte("Servicio demian-lee redesplegado en vm1\nactive_state=failed\n")}}}
		env := newServicesEnv(t, runner)
		env.register(t, "vm1")

		res, err := env.svc.RedeployService(context.Background(), "vm1", "demian-lee", "demian-lee", tarGzUpload("x"))

		if err != nil || res.ActiveState != "failed" {
			t.Errorf("resultado = %+v, err = %v; se esperaba el estado failed sin error", res, err)
		}
	})
}

func TestAC40_RedeployRestoreFailureMessage(t *testing.T) {
	useTempDir(t)
	const stderr = "Falló el paso chown en la VM (código 5)\n" +
		"Restauración fallida; puede quedar en la VM: /opt/demian-lee, /opt/.stratus-backup-demian-lee, /etc/systemd/system/demian-lee.service, /var/lib/stratus/backup/demian-lee\n"
	runner := &fakeRunner{results: map[string]RunResult{redeployServiceScript: {ExitCode: 9, Stderr: stderr}}}
	env := newServicesEnv(t, runner)
	env.register(t, "vm1")

	_, err := env.svc.RedeployService(context.Background(), "vm1", "demian-lee", "demian-lee", tarGzUpload("x"))

	var rollback *ServiceRollbackError
	if !errors.As(err, &rollback) {
		t.Fatalf("err = %v, se esperaba *ServiceRollbackError", err)
	}
	const want = "No se pudo redesplegar el servicio demian-lee y la restauración falló; revise la VM. " +
		"Restauración fallida; puede quedar en la VM: /opt/demian-lee, /opt/.stratus-backup-demian-lee, /etc/systemd/system/demian-lee.service, /var/lib/stratus/backup/demian-lee"
	if err.Error() != want {
		t.Errorf("mensaje = %q, se esperaba %q", err.Error(), want)
	}
	ops := env.operations(t)
	if len(ops) != 1 || !strings.Contains(ops[0].Stderr, "Falló el paso chown") || !strings.Contains(ops[0].Stderr, "Restauración fallida") {
		t.Errorf("historial = %+v, se esperaba stderr con los dos errores", ops)
	}
}

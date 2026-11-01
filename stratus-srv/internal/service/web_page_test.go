package service

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"stratus-srv/internal/repository"
)

func pageUpload(body string) ArchiveUpload {
	return ArchiveUpload{Filename: "sitio.tar.gz", Body: strings.NewReader(body)}
}

// AC-11, AC-17: la página va como archivo temporal del host a install-page.sh, la operación queda
// como install_page y el archivo temporal se borra al terminar, con éxito o con fallo.
func TestAC11_InstallPageArgsAndCleanup(t *testing.T) {
	tests := []struct {
		name   string
		result RunResult
		wantOK bool
	}{
		{"éxito", RunResult{Stdout: []byte("Página instalada en web-a\n")}, true},
		{"fallo", RunResult{ExitCode: 5, Stderr: "Falló un paso en el servidor web web-a\n"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeRunner{results: map[string]RunResult{"web/install-page.sh": tt.result}}
			env := newWebEnv(t, runner)
			env.registerWeb(t, "web-a", WebTypeNginx)

			msg, err := env.svc.InstallPage(context.Background(), "web-a", pageUpload("contenido"))

			if tt.wantOK && (err != nil || msg != "Página instalada en web-a") {
				t.Fatalf("InstallPage = %q, %v", msg, err)
			}
			if !tt.wantOK && err == nil {
				t.Fatal("se esperaba un error")
			}
			calls := runner.callsTo("web/install-page.sh")
			if len(calls) != 1 || len(calls[0].Args) != 2 || calls[0].Args[0] != "web-a" {
				t.Fatalf("llamadas = %+v, se esperaba [web-a <tmp>]", calls)
			}
			if _, err := os.Stat(calls[0].Args[1]); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("el archivo temporal %s sigue en el host: %v", calls[0].Args[1], err)
			}
			op := env.lastOperation(t)
			if op.Operation != "install_page" || op.VM != "web-a" {
				t.Errorf("historial = %+v, se esperaba install_page en web-a", op)
			}
		})
	}
}

// AC-12: sin index.html en la raíz el script sale con 11 y el error es ErrIndexMissing.
func TestAC12_InstallPageMapsIndexMissing(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{
		"web/install-page.sh": {ExitCode: 11, Stderr: "El archivo debe contener index.html en la raíz\n"},
	}}
	env := newWebEnv(t, runner)
	env.registerWeb(t, "web-a", WebTypeApache)

	_, err := env.svc.InstallPage(context.Background(), "web-a", pageUpload("x"))

	if !errors.Is(err, ErrIndexMissing) {
		t.Errorf("err = %v, se esperaba ErrIndexMissing", err)
	}

	runner.results["web/install-page.sh"] = RunResult{ExitCode: 10, Stderr: "Archivo comprimido inválido\n"}
	if _, err := env.svc.InstallPage(context.Background(), "web-a", pageUpload("x")); !errors.Is(err, ErrInvalidArchive) {
		t.Errorf("err = %v, se esperaba ErrInvalidArchive", err)
	}
}

// AC-13: un nombre sin .tar.gz o un archivo de más de 200 MB se rechazan con los textos de 003 sin
// llamar al script; tampoco una VM que no es servidor web.
func TestAC13_InstallPageSizeAndExtension(t *testing.T) {
	runner := &fakeRunner{}
	env := newWebEnv(t, runner)
	env.registerWeb(t, "web-a", WebTypeNginx)
	if _, err := env.vms.Insert(context.Background(), "vm1"); err != nil {
		t.Fatal(err)
	}
	cfg := testWebConfig()
	cfg.MaxArchiveBytes = 4
	svc := NewWebService(cfg, runner, env.vms, env.lbs, env.web, env.dns, env.history, env.locks, env.svc.logger)
	ctx := context.Background()

	var vErr *ValidationError
	_, err := svc.InstallPage(ctx, "web-a", ArchiveUpload{Filename: "site.zip", Body: strings.NewReader("x")})
	if !errors.As(err, &vErr) || vErr.Message != "El archivo debe ser .tar.gz" {
		t.Errorf("site.zip: err = %v", err)
	}
	_, err = svc.InstallPage(ctx, "web-a", pageUpload("12345"))
	if !errors.As(err, &vErr) || vErr.Message != "El archivo supera 200 MB" {
		t.Errorf("demasiado grande: err = %v", err)
	}
	if _, err := svc.InstallPage(ctx, "vm1", pageUpload("x")); !errors.Is(err, ErrVMNotFound) {
		t.Errorf("vm1: err = %v, se esperaba ErrVMNotFound", err)
	}
	if n := runner.callCount(); n != 0 {
		t.Errorf("el runner se llamó %d veces, se esperaba 0", n)
	}
}

// AC-16: en un servidor asignado el argv empieza con --jump <lb>, y el balanceador apagado da
// *LBOffError.
func TestAC16_InstallPageOnServerPassesJump(t *testing.T) {
	runner := &fakeRunner{}
	env := newWebEnv(t, runner)
	env.registerWeb(t, "web-a", WebTypeNginx)
	ctx := context.Background()
	if _, err := env.vms.Insert(ctx, "lb1"); err != nil {
		t.Fatal(err)
	}
	if err := env.lbs.InsertLB(ctx, repository.LoadBalancer{Name: "lb1", Subnet: 1, FrontendPort: 80}); err != nil {
		t.Fatal(err)
	}
	if err := env.lbs.InsertServer(ctx, repository.LBServer{LB: "lb1", Name: "server1", VM: "web-a", Port: 80,
		Enabled: true}); err != nil {
		t.Fatal(err)
	}

	if _, err := env.svc.InstallPage(ctx, "web-a", pageUpload("x")); err != nil {
		t.Fatalf("InstallPage: %v", err)
	}
	args := runner.callsTo("web/install-page.sh")[0].Args
	if len(args) != 4 || args[0] != "--jump" || args[1] != "lb1" || args[2] != "web-a" {
		t.Errorf("argv = %q, se esperaba [--jump lb1 web-a <tmp>]", args)
	}

	runner.results = map[string]RunResult{"web/install-page.sh": {ExitCode: 12,
		Stderr: "El balanceador lb1 está apagado; enciéndalo para llegar al servidor\n"}}
	_, err := env.svc.InstallPage(ctx, "web-a", pageUpload("x"))
	var lbOff *LBOffError
	if !errors.As(err, &lbOff) {
		t.Errorf("err = %v, se esperaba *LBOffError", err)
	}
}

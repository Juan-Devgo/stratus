package service

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"stratus-srv/internal/repository"
)

// AC-1: crear un servidor Nginx llama a create-web.sh con cada valor como argumento separado y
// guarda la VM en vms y en web_servers.
func TestAC1_CreateWebPassesDiscreteArgs(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{
		"web/create-web.sh": {Stdout: []byte("Servidor web web-a (Nginx) creado; IP 192.168.1.40\nip=192.168.1.40\n")},
	}}
	env := newWebEnv(t, runner)
	ctx := context.Background()

	res, err := env.svc.CreateWeb(ctx, CreateWebInput{Name: "web-a", MemoryMB: 1024, CPUs: 1, Type: WebTypeNginx})
	if err != nil {
		t.Fatalf("CreateWeb: %v", err)
	}
	if want := (ActionResult{Message: "Servidor web web-a (Nginx) creado; IP 192.168.1.40"}); res != want {
		t.Errorf("resultado = %+v, se esperaba %+v", res, want)
	}
	calls := runner.callsTo("web/create-web.sh")
	if len(calls) != 1 {
		t.Fatalf("create-web.sh se llamó %d veces, se esperaba 1", len(calls))
	}
	want := []string{"web-a", "1024", "1", "nginx", testNginxDisk, testBridge, testDomain}
	if !reflect.DeepEqual(calls[0].Args, want) {
		t.Errorf("argv = %q, se esperaba %q", calls[0].Args, want)
	}
	if _, err := env.vms.Get(ctx, "web-a"); err != nil {
		t.Errorf("web-a no quedó en vms: %v", err)
	}
	if w, ok, err := env.web.Get(ctx, "web-a"); err != nil || !ok || w.Type != WebTypeNginx {
		t.Errorf("web_servers = %+v, %v, %v; se esperaba web-a nginx", w, ok, err)
	}
	if op := env.lastOperation(t); op.Operation != "create_web" || op.VM != "web-a" || op.Result != "ok" {
		t.Errorf("historial = %+v, se esperaba create_web ok en web-a", op)
	}
}

// AC-1: el tipo Apache usa su disco base.
func TestAC1_CreateWebApacheUsesApacheDisk(t *testing.T) {
	runner := &fakeRunner{}
	env := newWebEnv(t, runner)

	if _, err := env.svc.CreateWeb(context.Background(),
		CreateWebInput{Name: "web-b", MemoryMB: 2048, CPUs: 2, Type: WebTypeApache}); err != nil {
		t.Fatalf("CreateWeb: %v", err)
	}
	calls := runner.callsTo("web/create-web.sh")
	if len(calls) != 1 || calls[0].Args[3] != "apache" || calls[0].Args[4] != testApacheDisk {
		t.Errorf("llamadas = %+v, se esperaba apache con %s", calls, testApacheDisk)
	}
}

// AC-2: una entrada inválida, un nombre repetido o un tipo vacío o fuera de la lista no llega al script.
func TestAC2_CreateWebRejectsInvalid(t *testing.T) {
	tests := []struct {
		name string
		in   CreateWebInput
		want error
	}{
		{"nombre inválido", CreateWebInput{Name: "Web_A", MemoryMB: 1024, CPUs: 1, Type: WebTypeNginx}, ErrInvalidInput},
		{"memoria fuera de rango", CreateWebInput{Name: "web-a", MemoryMB: 256, CPUs: 1, Type: WebTypeNginx}, ErrInvalidInput},
		{"CPUs fuera de rango", CreateWebInput{Name: "web-a", MemoryMB: 1024, CPUs: 5, Type: WebTypeNginx}, ErrInvalidInput},
		{"tipo iis", CreateWebInput{Name: "web-a", MemoryMB: 1024, CPUs: 1, Type: "iis"}, ErrInvalidInput},
		{"sin tipo", CreateWebInput{Name: "web-a", MemoryMB: 1024, CPUs: 1}, ErrInvalidInput},
		{"nombre ya registrado", CreateWebInput{Name: "vm1", MemoryMB: 1024, CPUs: 1, Type: WebTypeApache}, ErrDuplicateVM},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeRunner{}
			env := newWebEnv(t, runner)
			if _, err := env.vms.Insert(context.Background(), "vm1"); err != nil {
				t.Fatal(err)
			}

			_, err := env.svc.CreateWeb(context.Background(), tt.in)

			if !errors.Is(err, tt.want) {
				t.Errorf("err = %v, se esperaba %v", err, tt.want)
			}
			if n := runner.callCount(); n != 0 {
				t.Errorf("el runner se llamó %d veces, se esperaba 0", n)
			}
			if !env.locks.TryLockCreate() {
				t.Error("el bloqueo de creación quedó tomado")
			}
		})
	}
}

// AC-3: sin disco base el error trae el texto del tipo y no queda ningún registro.
func TestAC3_CreateWebMissingDiskRegistersNothing(t *testing.T) {
	t.Run("el script sale con 6", func(t *testing.T) {
		runner := &fakeRunner{results: map[string]RunResult{
			"web/create-web.sh": {ExitCode: 6, Stderr: "Disco base de Apache no encontrado\n"},
		}}
		env := newWebEnv(t, runner)
		ctx := context.Background()

		_, err := env.svc.CreateWeb(ctx, CreateWebInput{Name: "web-a", MemoryMB: 1024, CPUs: 1, Type: WebTypeApache})

		if !errors.Is(err, ErrApacheBaseDiskMissing) || err.Error() != "Disco base de Apache no encontrado" {
			t.Fatalf("err = %v, se esperaba ErrApacheBaseDiskMissing", err)
		}
		if _, err := env.vms.Get(ctx, "web-a"); !errors.Is(err, repository.ErrVMNotFound) {
			t.Errorf("web-a no debía quedar en vms: err = %v", err)
		}
		if _, ok, _ := env.web.Get(ctx, "web-a"); ok {
			t.Error("web-a no debía quedar en web_servers")
		}
	})
	t.Run("otro fallo con 6 conserva su texto", func(t *testing.T) {
		runner := &fakeRunner{results: map[string]RunResult{
			"web/create-web.sh": {ExitCode: 6, Stderr: "VM ya registrada en VirtualBox\n"},
		}}
		env := newWebEnv(t, runner)

		_, err := env.svc.CreateWeb(context.Background(),
			CreateWebInput{Name: "web-a", MemoryMB: 1024, CPUs: 1, Type: WebTypeNginx})

		var opErr *OperationError
		if !errors.As(err, &opErr) || !strings.Contains(err.Error(), "VM ya registrada en VirtualBox") {
			t.Errorf("err = %v, se esperaba un OperationError con el texto del script", err)
		}
	})
	t.Run("disco base sin configurar", func(t *testing.T) {
		runner := &fakeRunner{}
		env := newWebEnv(t, runner)
		cfg := testWebConfig()
		cfg.ApacheBaseDisk = ""
		svc := NewWebService(cfg, runner, env.vms, env.lbs, env.web, env.dns, env.history, env.locks, env.svc.logger)
		ctx := context.Background()

		_, err := svc.CreateWeb(ctx, CreateWebInput{Name: "web-a", MemoryMB: 1024, CPUs: 1, Type: WebTypeApache})

		if !errors.Is(err, ErrApacheBaseDiskMissing) {
			t.Errorf("err = %v, se esperaba ErrApacheBaseDiskMissing", err)
		}
		if n := runner.callCount(); n != 0 {
			t.Errorf("el runner se llamó %d veces, se esperaba 0", n)
		}
		if _, err := env.vms.Get(ctx, "web-a"); !errors.Is(err, repository.ErrVMNotFound) {
			t.Errorf("web-a no debía quedar en vms: err = %v", err)
		}
	})
}

// AC-8: la lista trae solo los servidores web, con su tipo, estado, IP, FQDN y balanceador.
func TestAC8_ListWebServersOnly(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{
		"prepare/list-vms.sh": {Stdout: []byte(
			"web-a running 192.168.1.40\nweb-b running 10.10.1.11\nvm1 running 192.168.1.50\nlb1 running 192.168.1.60\n")},
	}}
	env := newWebEnv(t, runner)
	ctx := context.Background()
	env.registerWeb(t, "web-a", WebTypeNginx)
	env.registerWeb(t, "web-b", WebTypeApache)
	if _, err := env.vms.Insert(ctx, "vm1"); err != nil {
		t.Fatal(err)
	}
	if _, err := env.vms.Insert(ctx, "lb1"); err != nil {
		t.Fatal(err)
	}
	if err := env.lbs.InsertLB(ctx, repository.LoadBalancer{Name: "lb1", Subnet: 1, FrontendPort: 80}); err != nil {
		t.Fatal(err)
	}
	if err := env.lbs.InsertServer(ctx, repository.LBServer{LB: "lb1", Name: "server1", VM: "web-b", Port: 80,
		LastIP: "10.10.1.11", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := env.dns.InsertRecords(ctx, []repository.DNSRecord{{VM: "web-a", IP: "192.168.1.40"}}); err != nil {
		t.Fatal(err)
	}

	servers, listErr, err := env.svc.ListWebServers(ctx)

	if err != nil || listErr != "" {
		t.Fatalf("ListWebServers: %v, listErr %q", err, listErr)
	}
	want := []WebServerView{
		{Name: "web-a", Type: WebTypeNginx, State: "running", IP: "192.168.1.40", FQDN: "web-a.stratus.lan"},
		{Name: "web-b", Type: WebTypeApache, State: "running", IP: "10.10.1.11", LB: "lb1"},
	}
	if !reflect.DeepEqual(servers, want) {
		t.Errorf("servidores = %+v, se esperaba %+v", servers, want)
	}
	if ops, _ := env.history.ListRecent(ctx, webModule, historyLimit); len(ops) != 0 {
		t.Errorf("una lectura correcta no deja fila: %+v", ops)
	}
}

// AC-8 / REQ-35: una lectura fallida deja una fila list_web en el historial web.
func TestAC8_ListWebRecordsFailedRead(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{
		"prepare/list-vms.sh": {ExitCode: 4, Stderr: "VBoxManage list vms falló\n"},
	}}
	env := newWebEnv(t, runner)
	env.registerWeb(t, "web-a", WebTypeNginx)

	servers, listErr, err := env.svc.ListWebServers(context.Background())

	if err != nil {
		t.Fatalf("ListWebServers: %v", err)
	}
	if !strings.Contains(listErr, "VBoxManage list vms falló") {
		t.Errorf("listErr = %q, se esperaba el motivo", listErr)
	}
	if len(servers) != 1 || servers[0].State != "unknown" {
		t.Errorf("servidores = %+v, se esperaba web-a en unknown", servers)
	}
	op := env.lastOperation(t)
	if op.Operation != "list_web" || op.VM != "-" || op.Command != "prepare/list-vms.sh" || op.ExitCode != 4 ||
		op.Result != "error" {
		t.Errorf("historial = %+v, se esperaba list_web error con código 4", op)
	}
}

// AC-10: eliminar un servidor web borra su VM con delete-vm.sh como delete_web y sus filas; uno
// asignado a un balanceador se rechaza sin llamar a nada.
func TestAC10_DeleteWebRemovesRecord(t *testing.T) {
	t.Run("sin asignar", func(t *testing.T) {
		runner := &fakeRunner{}
		env := newWebEnv(t, runner)
		ctx := context.Background()
		env.registerWeb(t, "web-a", WebTypeNginx)

		res, err := env.svc.DeleteWeb(ctx, "web-a")

		if err != nil {
			t.Fatalf("DeleteWeb: %v", err)
		}
		if res.Message != "Servidor web web-a eliminado junto con su disco" || res.Warning != "" {
			t.Errorf("resultado = %+v", res)
		}
		calls := runner.callsTo("prepare/delete-vm.sh")
		if len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, []string{"web-a"}) {
			t.Errorf("llamadas = %+v, se esperaba delete-vm.sh [web-a]", calls)
		}
		if op := env.lastOperation(t); op.Operation != "delete_web" || op.VM != "web-a" || op.Result != "ok" {
			t.Errorf("historial = %+v, se esperaba delete_web ok", op)
		}
		if _, err := env.vms.Get(ctx, "web-a"); !errors.Is(err, repository.ErrVMNotFound) {
			t.Errorf("web-a debía salir de vms: err = %v", err)
		}
		if _, ok, _ := env.web.Get(ctx, "web-a"); ok {
			t.Error("web-a debía salir de web_servers")
		}
	})
	t.Run("asignado a un balanceador", func(t *testing.T) {
		runner := &fakeRunner{}
		env := newWebEnv(t, runner)
		ctx := context.Background()
		env.registerWeb(t, "web-a", WebTypeNginx)
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

		_, err := env.svc.DeleteWeb(ctx, "web-a")

		if !errors.Is(err, ErrVMIsServer) {
			t.Errorf("err = %v, se esperaba ErrVMIsServer", err)
		}
		if n := runner.callCount(); n != 0 {
			t.Errorf("el runner se llamó %d veces, se esperaba 0", n)
		}
		if _, ok, _ := env.web.Get(ctx, "web-a"); !ok {
			t.Error("web-a no debía salir de web_servers")
		}
	})
	t.Run("no es servidor web", func(t *testing.T) {
		runner := &fakeRunner{}
		env := newWebEnv(t, runner)
		if _, err := env.vms.Insert(context.Background(), "vm1"); err != nil {
			t.Fatal(err)
		}

		_, err := env.svc.DeleteWeb(context.Background(), "vm1")

		if !errors.Is(err, ErrVMNotFound) {
			t.Errorf("err = %v, se esperaba ErrVMNotFound", err)
		}
		if n := runner.callCount(); n != 0 {
			t.Errorf("el runner se llamó %d veces, se esperaba 0", n)
		}
	})
}

package service

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"stratus-srv/internal/repository"
)

const createDNSLine = "Servidor DNS dns1 creado en 192.168.1.53; zona stratus.lan"

func createDNSRunner(listVMs string) *fakeRunner {
	return &fakeRunner{results: map[string]RunResult{
		"dns/create-dns.sh":   {Stdout: []byte(createDNSLine + "\n")},
		"prepare/list-vms.sh": {Stdout: []byte(listVMs)},
	}}
}

// AC-22: con una configuración válida, create-dns.sh recibe la VM, el disco de DNS, la red y la
// configuración de bind9, y al terminar bien quedan la fila de vms y la del servidor DNS.
func TestAC22_CreateDNSArgsAndRows(t *testing.T) {
	runner := createDNSRunner("")
	env := newDNSEnv(t, runner)
	ctx := context.Background()

	msg, err := env.svc.CreateDNS(ctx, CreateVMInput{Name: "dns1", MemoryMB: 1024, CPUs: 1})

	if err != nil {
		t.Fatalf("CreateDNS: %v", err)
	}
	if msg != createDNSLine {
		t.Errorf("mensaje = %q, se esperaba %q", msg, createDNSLine)
	}
	calls := runner.callsTo("dns/create-dns.sh")
	want := []string{"dns1", "1024", "1", testDNSDisk, testBridge, "192.168.1.53/24", "192.168.1.1", testDomain,
		"1.1.1.1,8.8.8.8"}
	if len(calls) != 1 || !slices.Equal(calls[0].Args, want) {
		t.Errorf("create-dns.sh = %+v, se esperaba argv %v", calls, want)
	}
	if ok, err := env.svc.IsDNSServer(ctx, "dns1"); err != nil || !ok {
		t.Errorf("dns1 no quedó como servidor DNS: %v, %v", ok, err)
	}
	if _, err := env.vms.Get(ctx, "dns1"); err != nil {
		t.Errorf("dns1 no quedó en vms: %v", err)
	}
	if op := env.lastDNSOperation(t); op.Operation != "create_dns" || op.VM != "dns1" || op.Result != "ok" {
		t.Errorf("historial = %+v, se esperaba create_dns ok", op)
	}
}

// AC-23: con un servidor DNS registrado, crear otro se rechaza sin llamar a ningún script.
func TestAC23_SecondDNSRejected(t *testing.T) {
	runner := createDNSRunner("")
	env := newDNSEnv(t, runner)
	env.registerDNS(t, "dns1")

	_, err := env.svc.CreateDNS(context.Background(), validCreate("dns2"))

	if !errors.Is(err, ErrDNSExists) {
		t.Errorf("err = %v, se esperaba ErrDNSExists", err)
	}
	if n := runner.callCount(); n != 0 {
		t.Errorf("el runner se llamó %d veces, se esperaba 0", n)
	}
}

// AC-24: con la configuración de DNS inválida o sin disco base, la creación se rechaza sin llamar a
// ningún script ni registrar la VM; un disco que no existe en el host lo informa el script (exit 6).
func TestAC24_CreateDNSIncompleteConfig(t *testing.T) {
	tests := []struct {
		name   string
		change func(*DNSConfig)
		want   error
	}{
		{"IP vacía", func(c *DNSConfig) { c.IP = "" }, ErrDNSConfigIncomplete},
		{"IP sin prefijo", func(c *DNSConfig) { c.IP = "192.168.1.53" }, ErrDNSConfigIncomplete},
		{"gateway fuera de la subred", func(c *DNSConfig) { c.Gateway = "10.0.0.1" }, ErrDNSConfigIncomplete},
		{"dominio inválido", func(c *DNSConfig) { c.Domain = "Stratus_Lan" }, ErrDNSConfigIncomplete},
		{"reenviador inválido", func(c *DNSConfig) { c.Forwarders = "1.1.1" }, ErrDNSConfigIncomplete},
		{"sin disco base", func(c *DNSConfig) { c.BaseDisk = "" }, ErrDNSBaseDiskMissing},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := createDNSRunner("")
			env := newDNSEnv(t, runner)
			tt.change(&env.svc.cfg)

			_, err := env.svc.CreateDNS(context.Background(), validCreate("dns1"))

			if !errors.Is(err, tt.want) {
				t.Errorf("err = %v, se esperaba %v", err, tt.want)
			}
			if n := runner.callCount(); n != 0 {
				t.Errorf("el runner se llamó %d veces, se esperaba 0", n)
			}
			if _, err := env.vms.Get(context.Background(), "dns1"); !errors.Is(err, repository.ErrVMNotFound) {
				t.Errorf("dns1 no debía quedar en vms: %v", err)
			}
		})
	}

	t.Run("disco inexistente en el host", func(t *testing.T) {
		runner := createDNSRunner("")
		runner.results["dns/create-dns.sh"] = RunResult{ExitCode: 6, Stderr: "Disco base de DNS no encontrado\n"}
		env := newDNSEnv(t, runner)

		_, err := env.svc.CreateDNS(context.Background(), validCreate("dns1"))

		if !errors.Is(err, ErrDNSBaseDiskMissing) {
			t.Errorf("err = %v, se esperaba ErrDNSBaseDiskMissing", err)
		}
		if _, err := env.vms.Get(context.Background(), "dns1"); !errors.Is(err, repository.ErrVMNotFound) {
			t.Errorf("dns1 no debía quedar en vms: %v", err)
		}
	})
}

// AC-25: si la IP del servidor DNS responde en la LAN (exit 13), el error es ErrDNSIPInUse y no
// queda ninguna VM ni servidor registrado.
func TestAC25_IPInUseMapsError(t *testing.T) {
	runner := createDNSRunner("")
	runner.results["dns/create-dns.sh"] = RunResult{ExitCode: 13, Stderr: "La IP del servidor DNS ya está en uso\n"}
	env := newDNSEnv(t, runner)
	ctx := context.Background()

	_, err := env.svc.CreateDNS(ctx, validCreate("dns1"))

	if !errors.Is(err, ErrDNSIPInUse) {
		t.Errorf("err = %v, se esperaba ErrDNSIPInUse", err)
	}
	if _, err := env.vms.Get(ctx, "dns1"); !errors.Is(err, repository.ErrVMNotFound) {
		t.Errorf("dns1 no debía quedar en vms: %v", err)
	}
	if ok, _ := env.svc.IsDNSServer(ctx, "dns1"); ok {
		t.Error("dns1 no debía quedar como servidor DNS")
	}
}

// AC-27: solo los servidores web sin balanceador y con una IPv4 de la LAN entran como registros
// iniciales; el que no tiene IPv4, el asignado a un balanceador y el que informa una dirección fuera
// de la LAN se omiten, se nombran en el resultado y la creación termina bien. Ni el balanceador ni
// el servidor asignado quedan con registro.
func TestAC27_CreateDNSSkipsWebWithoutLANIP(t *testing.T) {
	runner := createDNSRunner("web-a running 192.168.1.40\nweb-b running 192.168.1.41\nweb-c poweroff -\n" +
		"web-d running 10.10.1.11\nweb-e running 10.0.2.15\nlb1 running 192.168.1.60\n")
	env := newDNSEnv(t, runner)
	ctx := context.Background()
	for _, name := range []string{"web-a", "web-b", "web-c", "web-d", "web-e"} {
		env.registerWeb(t, name, WebTypeNginx)
	}
	env.assignToLB(t, "lb1", "web-d")

	msg, err := env.svc.CreateDNS(ctx, validCreate("dns1"))

	if err != nil {
		t.Fatalf("CreateDNS: %v", err)
	}
	calls := runner.callsTo("dns/create-dns.sh")
	if len(calls) != 1 {
		t.Fatalf("create-dns.sh se llamó %d veces", len(calls))
	}
	if tail := calls[0].Args[9:]; !slices.Equal(tail, []string{"web-a,192.168.1.40", "web-b,192.168.1.41"}) {
		t.Errorf("registros iniciales = %v", tail)
	}
	recs, err := env.dns.Records(ctx)
	if err != nil || len(recs) != 2 || recs[0] != (repository.DNSRecord{VM: "web-a", IP: "192.168.1.40"}) {
		t.Errorf("dns_records = %+v, %v", recs, err)
	}
	if !strings.HasPrefix(msg, createDNSLine) {
		t.Errorf("mensaje = %q", msg)
	}
	for _, name := range []string{"web-c", "web-d", "web-e"} {
		if !strings.Contains(msg, name) {
			t.Errorf("mensaje = %q, se esperaba que nombrara %s", msg, name)
		}
	}
	for _, name := range []string{"web-a", "web-b", "lb1"} {
		if strings.Contains(msg[len(createDNSLine):], name) {
			t.Errorf("mensaje = %q, no debía nombrar %s", msg, name)
		}
	}
}

// REQ-17, REQ-23: la vista de la pantalla DNS trae el servidor con su estado, IPv4 y zona, y cada
// otra VM con su tipo, su rol, su registro y su resolvedor.
func TestAC23_DNSOverview(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{"prepare/list-vms.sh": {Stdout: []byte(
		"dns1 running 192.168.1.53\nweb-a running 192.168.1.40\nvm1 poweroff -\n")}}}
	env := newDNSEnv(t, runner)
	ctx := context.Background()
	env.registerWeb(t, "web-a", WebTypeNginx)
	env.registerDNS(t, "dns1", repository.DNSRecord{VM: "web-a", IP: "192.168.1.40"})
	if _, err := env.vms.Insert(ctx, "vm1"); err != nil {
		t.Fatal(err)
	}
	if err := env.dns.SetResolver(ctx, "vm1", true); err != nil {
		t.Fatal(err)
	}

	view, err := env.svc.Overview(ctx)

	if err != nil {
		t.Fatalf("Overview: %v", err)
	}
	wantServer := DNSServerView{Name: "dns1", State: "running", IP: "192.168.1.53", Zone: testDomain}
	if view.Server == nil || *view.Server != wantServer {
		t.Errorf("Server = %+v, se esperaba %+v", view.Server, wantServer)
	}
	want := []DNSVMView{
		{Name: "vm1", Role: "vm", State: "poweroff", Resolver: true},
		{Name: "web-a", Kind: WebTypeNginx, Role: "vm", State: "running", IP: "192.168.1.40",
			FQDN: "web-a." + testDomain, RecordIP: "192.168.1.40"},
	}
	if !slices.Equal(view.VMs, want) {
		t.Errorf("VMs = %+v, se esperaba %+v", view.VMs, want)
	}

	t.Run("sin servidor", func(t *testing.T) {
		env := newDNSEnv(t, &fakeRunner{})
		view, err := env.svc.Overview(ctx)
		if err != nil || view.Server != nil {
			t.Errorf("Overview = %+v, %v; se esperaba Server nil", view, err)
		}
	})
}

// AC-58, REQ-23: una VM servidor de un balanceador no muestra FQDN ni IP de registro aunque conserve
// una fila guardada; el balanceador sí muestra el suyo.
func TestAC58_OverviewHidesServerRecord(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{"prepare/list-vms.sh": {Stdout: []byte(
		"dns1 running 192.168.1.53\nlb1 running 192.168.1.41\nweb-b running 10.10.1.11\n")}}}
	env := newDNSEnv(t, runner)
	ctx := context.Background()
	env.registerWeb(t, "web-b", WebTypeApache)
	env.registerDNS(t, "dns1", repository.DNSRecord{VM: "web-b", IP: "10.10.1.11"}, repository.DNSRecord{VM: "lb1", IP: "192.168.1.41"})
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

	view, err := env.svc.Overview(ctx)

	if err != nil {
		t.Fatalf("Overview: %v", err)
	}
	want := []DNSVMView{
		{Name: "lb1", Role: "lb", State: "running", IP: "192.168.1.41", FQDN: "lb1." + testDomain, RecordIP: "192.168.1.41"},
		{Name: "web-b", Kind: WebTypeApache, Role: "server", State: "running", IP: "10.10.1.11"},
	}
	if !slices.Equal(view.VMs, want) {
		t.Errorf("VMs = %+v, se esperaba %+v", view.VMs, want)
	}
}

// AC-29: con una VM que usa el servidor DNS como resolvedor (aunque esté apagada), eliminarlo se
// rechaza sin llamar a ningún script; sin resolvedores, delete-vm.sh lo borra como delete_dns y no
// quedan filas de DNS.
func TestAC29_DeleteDNSRejectsWithResolvers(t *testing.T) {
	t.Run("con resolvedor", func(t *testing.T) {
		runner := &fakeRunner{}
		env := newDNSEnv(t, runner)
		ctx := context.Background()
		env.registerDNS(t, "dns1")
		if err := env.dns.SetResolver(ctx, "vm1", true); err != nil {
			t.Fatal(err)
		}

		_, err := env.svc.DeleteDNS(ctx)

		var resErr *ResolversSetError
		if !errors.As(err, &resErr) || err.Error() != "Quite primero el resolvedor DNS de las VMs: vm1" {
			t.Errorf("err = %v, se esperaba ResolversSetError con vm1", err)
		}
		if n := runner.callCount(); n != 0 {
			t.Errorf("el runner se llamó %d veces, se esperaba 0", n)
		}
		if ok, _ := env.svc.IsDNSServer(ctx, "dns1"); !ok {
			t.Error("dns1 debía seguir como servidor DNS")
		}
	})
	t.Run("sin resolvedores", func(t *testing.T) {
		runner := &fakeRunner{}
		env := newDNSEnv(t, runner)
		ctx := context.Background()
		env.registerDNS(t, "dns1", repository.DNSRecord{VM: "web-a", IP: "192.168.1.40"})

		msg, err := env.svc.DeleteDNS(ctx)

		if err != nil {
			t.Fatalf("DeleteDNS: %v", err)
		}
		if msg != "Servidor DNS dns1 eliminado junto con su disco" {
			t.Errorf("mensaje = %q", msg)
		}
		calls := runner.callsTo("prepare/delete-vm.sh")
		if len(calls) != 1 || !slices.Equal(calls[0].Args, []string{"dns1"}) {
			t.Errorf("delete-vm.sh = %+v, se esperaba [dns1]", calls)
		}
		if op := env.lastDNSOperation(t); op.Operation != "delete_dns" || op.Module != dnsModule {
			t.Errorf("historial = %+v, se esperaba delete_dns", op)
		}
		if _, ok, _ := env.dns.Server(ctx); ok {
			t.Error("dns_server no quedó vacío")
		}
		if recs, _ := env.dns.Records(ctx); len(recs) != 0 {
			t.Errorf("dns_records = %+v, se esperaba vacío", recs)
		}
		if _, err := env.vms.Get(ctx, "dns1"); !errors.Is(err, repository.ErrVMNotFound) {
			t.Errorf("dns1 no debía quedar en vms: %v", err)
		}
	})
	t.Run("sin servidor", func(t *testing.T) {
		env := newDNSEnv(t, &fakeRunner{})
		if _, err := env.svc.DeleteDNS(context.Background()); !errors.Is(err, ErrNoDNSServer) {
			t.Errorf("err = %v, se esperaba ErrNoDNSServer", err)
		}
	})
}

// AC-19: las acciones de bind9 llaman a dns/<acción>-bind.sh con el servidor DNS, se registran como
// <acción>_bind y toman su bloqueo; sin servidor DNS se rechazan.
func TestAC19_DNSActionLocksDNSServer(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{
		"dns/stop-bind.sh":  {Stdout: []byte("Servicio named detenido en dns1\n")},
		"dns/start-bind.sh": {Stdout: []byte("Servicio named iniciado en dns1\nactive_state=active\n")},
	}}
	env := newDNSEnv(t, runner)
	ctx := context.Background()

	if _, err := env.svc.DNSAction(ctx, "stop"); !errors.Is(err, ErrNoDNSServer) {
		t.Errorf("sin servidor: err = %v, se esperaba ErrNoDNSServer", err)
	}
	env.registerDNS(t, "dns1")

	res, err := env.svc.DNSAction(ctx, "stop")

	if err != nil || res.Message != "Servicio named detenido en dns1" {
		t.Errorf("DNSAction = %+v, %v", res, err)
	}
	calls := runner.callsTo("dns/stop-bind.sh")
	if len(calls) != 1 || !slices.Equal(calls[0].Args, []string{"dns1"}) {
		t.Errorf("stop-bind.sh = %+v, se esperaba [dns1]", calls)
	}
	if op := env.lastDNSOperation(t); op.Operation != "stop_bind" || op.VM != "dns1" {
		t.Errorf("historial = %+v, se esperaba stop_bind", op)
	}
	if res, err := env.svc.DNSAction(ctx, "start"); err != nil || res.ActiveState != "active" {
		t.Errorf("start = %+v, %v; se esperaba active_state active", res, err)
	}
	if _, err := env.svc.DNSAction(ctx, "enable"); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("enable: err = %v, se esperaba ErrInvalidInput", err)
	}

	if !env.locks.TryLock("dns1") {
		t.Fatal("no se pudo tomar el bloqueo de dns1")
	}
	defer env.locks.Unlock("dns1")
	if _, err := env.svc.DNSAction(ctx, "restart"); !errors.Is(err, ErrOperationInProgress) {
		t.Errorf("con dns1 bloqueado: err = %v, se esperaba ErrOperationInProgress", err)
	}
}

// AC-20: Estado devuelve la salida de systemctl status de named; una lectura correcta no deja fila.
func TestAC20_DNSStatusRecordsOnlyFailures(t *testing.T) {
	const out = "named.service\n     Active: failed\n"
	runner := &fakeRunner{results: map[string]RunResult{"dns/status-bind.sh": {Stdout: []byte(out)}}}
	env := newDNSEnv(t, runner)
	env.registerDNS(t, "dns1")

	got, err := env.svc.DNSStatus(context.Background())

	if err != nil || got != out {
		t.Errorf("DNSStatus = %q, %v", got, err)
	}
	if ops, _ := env.svc.History(context.Background()); len(ops) != 0 {
		t.Errorf("historial = %+v, se esperaba vacío", ops)
	}
}

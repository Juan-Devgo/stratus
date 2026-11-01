package service

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"stratus-srv/internal/repository"
)

const testDNSDisk = "/vms/dns-base.vdi"

// dnsEnv es el entorno de webEnv con el servicio DNS como svc: comparte SQLite, bloqueos y runner con
// los módulos web, prepare y haproxy (env.webEnv.svc sigue siendo el WebService).
type dnsEnv struct {
	webEnv
	svc *DNSService
}

func newDNSEnv(t *testing.T, runner *fakeRunner) dnsEnv {
	t.Helper()
	env := newWebEnv(t, runner)
	return dnsEnv{webEnv: env, svc: env.dnsSvc}
}

func testDNSConfig() DNSConfig {
	return DNSConfig{BaseDisk: testDNSDisk, BridgeAdapter: testBridge, IP: "192.168.1.53/24", Gateway: "192.168.1.1",
		Domain: testDomain, Forwarders: "1.1.1.1,8.8.8.8"}
}

// registerDNS registra el servidor DNS directamente en SQLite (su fila en vms y en dns_server).
func (e dnsEnv) registerDNS(t *testing.T, name string, initial ...repository.DNSRecord) {
	t.Helper()
	ctx := context.Background()
	if _, err := e.vms.Insert(ctx, name); err != nil {
		t.Fatalf("Insert(%q): %v", name, err)
	}
	if err := e.dns.InsertServer(ctx, name, initial); err != nil {
		t.Fatalf("InsertServer(%q): %v", name, err)
	}
}

// lastDNSOperation devuelve la fila más reciente del historial del módulo dns.
func (e dnsEnv) lastDNSOperation(t *testing.T) repository.Operation {
	t.Helper()
	ops, err := e.dnsSvc.History(context.Background())
	if err != nil || len(ops) == 0 {
		t.Fatalf("historial dns vacío: %v", err)
	}
	return ops[0]
}

// REQ-36: el historial del módulo dns solo trae sus propias filas; IsDNSServer reconoce al servidor.
func TestAC53_DNSHistoryOnlyDNSModule(t *testing.T) {
	env := newDNSEnv(t, &fakeRunner{})
	ctx := context.Background()
	env.registerDNS(t, "dns1")
	for _, op := range []repository.Operation{
		{Module: dnsModule, Operation: "create_dns", VM: "dns1", Command: "dns/create-dns.sh", Result: "ok"},
		{Module: webModule, Operation: "create_web", VM: "web-a", Command: "web/create-web.sh", Result: "ok"},
	} {
		if err := env.history.Insert(ctx, op); err != nil {
			t.Fatal(err)
		}
	}

	ops, err := env.svc.History(ctx)

	if err != nil || len(ops) != 1 || ops[0].Operation != "create_dns" {
		t.Errorf("History = %+v, %v; se esperaba solo create_dns", ops, err)
	}
	for vm, want := range map[string]bool{"dns1": true, "vm1": false} {
		if got, err := env.svc.IsDNSServer(ctx, vm); err != nil || got != want {
			t.Errorf("IsDNSServer(%s) = %v, %v; se esperaba %v", vm, got, err, want)
		}
	}
}

func TestRecordSpecs(t *testing.T) {
	got := recordSpecs([]repository.DNSRecord{{VM: "vm1", IP: "192.168.1.40"}, {VM: "lb1", IP: "192.168.1.41"}})
	if len(got) != 2 || got[0] != "vm1,192.168.1.40" || got[1] != "lb1,192.168.1.41" {
		t.Errorf("recordSpecs = %v", got)
	}
}

// REQ-25, REQ-28: los códigos de los scripts de registros se traducen a sus errores.
func TestMapDNSError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want error
	}{
		{"sin IP", ErrVMNoIP, ErrDNSOff},
		{"zona inválida", &OperationError{ExitCode: 10}, ErrZoneInvalid},
		{"IP en uso", &OperationError{ExitCode: 13}, ErrDNSIPInUse},
		{"otro código", &OperationError{ExitCode: 5}, ErrScriptFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mapDNSError(tt.err); !errors.Is(got, tt.want) {
				t.Errorf("mapDNSError = %v, se esperaba %v", got, tt.want)
			}
		})
	}
}

// AC-50, REQ-34: una instalación de página en curso bloquea web-a para los demás módulos, y un
// agregado de registros en curso bloquea el servidor DNS para bind9 y el refresco.
func TestAC50_CrossModuleLocks(t *testing.T) {
	t.Run("página en web-a", func(t *testing.T) {
		env, runner := newRecordsEnv(t)
		runner.block, runner.started, runner.blockOnly = make(chan struct{}), make(chan struct{}, 1), "web/install-page.sh"
		env.registerWeb(t, "web-a", WebTypeNginx)
		ctx := context.Background()

		done := make(chan error, 1)
		go func() {
			_, err := env.webEnv.svc.InstallPage(ctx, "web-a", pageUpload("x"))
			done <- err
		}()
		<-runner.started

		attempts := map[string]func() error{
			"WebAction":      func() error { _, err := env.webEnv.svc.WebAction(ctx, "web-a", "restart"); return err },
			"InstallPackage": func() error { _, err := env.prepare.InstallPackage(ctx, "web-a", "htop"); return err },
			"SetResolver":    func() error { _, err := env.svc.SetResolver(ctx, "web-a"); return err },
		}
		for name, attempt := range attempts {
			if err := attempt(); !errors.Is(err, ErrOperationInProgress) {
				t.Errorf("%s: err = %v, se esperaba ErrOperationInProgress", name, err)
			}
		}
		close(runner.block)
		if err := <-done; err != nil {
			t.Fatalf("InstallPage: %v", err)
		}
	})

	t.Run("registros en dns1", func(t *testing.T) {
		env, runner := newRecordsEnv(t)
		runner.block, runner.started, runner.blockOnly = make(chan struct{}), make(chan struct{}, 1), "dns/add-records.sh"
		ctx := context.Background()

		done := make(chan error, 1)
		go func() {
			_, _, err := env.svc.AddRecords(ctx, []string{"vm1"})
			done <- err
		}()
		<-runner.started

		attempts := map[string]func() error{
			"DNSAction": func() error { _, err := env.svc.DNSAction(ctx, "restart"); return err },
			"DNSStatus": func() error { _, err := env.svc.DNSStatus(ctx); return err },
			"Refresh":   func() error { _, err := env.svc.Refresh(ctx); return err },
		}
		for name, attempt := range attempts {
			if err := attempt(); !errors.Is(err, ErrOperationInProgress) {
				t.Errorf("%s: err = %v, se esperaba ErrOperationInProgress", name, err)
			}
		}
		if n := len(runner.callsTo("dns/add-records.sh")); n != 1 {
			t.Errorf("add-records.sh se llamó %d veces, se esperaba 1", n)
		}
		close(runner.block)
		if err := <-done; err != nil {
			t.Fatalf("AddRecords: %v", err)
		}
	})
}

// countOps cuenta las filas del historial por operación.
func countOps(ops []repository.Operation) map[string]int {
	n := map[string]int{}
	for _, op := range ops {
		n[op.Operation]++
	}
	return n
}

// AC-51, REQ-35: cada operación de dns deja una fila; Estado y la lectura de la lista solo cuando
// fallan.
func TestAC51_DNSRecordPolicy(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{"prepare/list-vms.sh": {Stdout: []byte(recordsListVMs)}}}
	env := newDNSEnv(t, runner)
	ctx := context.Background()
	if _, err := env.svc.CreateDNS(ctx, CreateVMInput{Name: "dns1", MemoryMB: 1024, CPUs: 1}); err != nil {
		t.Fatalf("CreateDNS: %v", err)
	}
	if _, err := env.vms.Insert(ctx, "vm1"); err != nil {
		t.Fatal(err)
	}
	steps := []func() error{
		func() error { _, _, err := env.svc.AddRecords(ctx, []string{"vm1"}); return err },
		func() error { _, err := env.svc.Refresh(ctx); return err },
		func() error { _, err := env.svc.RemoveRecord(ctx, "vm1"); return err },
		func() error { _, err := env.svc.SetResolver(ctx, "vm1"); return err },
		func() error { _, err := env.svc.UnsetResolver(ctx, "vm1"); return err },
		func() error { _, err := env.svc.DNSAction(ctx, "start"); return err },
		func() error { _, err := env.svc.DNSAction(ctx, "stop"); return err },
		func() error { _, err := env.svc.DNSAction(ctx, "restart"); return err },
	}
	for i := 0; i < 20; i++ {
		steps = append(steps, func() error { _, err := env.svc.DNSStatus(ctx); return err })
		steps = append(steps, func() error { _, err := env.svc.Overview(ctx); return err })
	}
	for i, step := range steps {
		if err := step(); err != nil {
			t.Fatalf("paso %d: %v", i, err)
		}
	}
	runner.results["dns/status-bind.sh"] = RunResult{ExitCode: 4}
	if _, err := env.svc.DNSStatus(ctx); err == nil {
		t.Fatal("DNSStatus fallido debía devolver error")
	}
	if _, err := env.svc.DeleteDNS(ctx); err != nil {
		t.Fatalf("DeleteDNS: %v", err)
	}

	ops, err := env.svc.History(ctx)
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]int{"create_dns": 1, "add_records": 1, "refresh_records": 1, "remove_record": 1,
		"set_resolver": 1, "unset_resolver": 1, "start_bind": 1, "stop_bind": 1, "restart_bind": 1,
		"status_bind": 1, "delete_dns": 1}
	if got := countOps(ops); !reflect.DeepEqual(got, want) {
		t.Errorf("filas por operación = %v, se esperaba %v", got, want)
	}
	if ops[0].Operation != "delete_dns" {
		t.Errorf("la más reciente = %s, se esperaba delete_dns", ops[0].Operation)
	}
}

// AC-53, REQ-36: con 55 filas de dns, History trae las 50 más recientes, la más nueva primero.
func TestAC53_DNSHistoryLimit(t *testing.T) {
	env := newDNSEnv(t, &fakeRunner{})
	ctx := context.Background()
	for i := 0; i < 55; i++ {
		op := repository.Operation{Module: dnsModule, Operation: fmt.Sprintf("op%02d", i), VM: "dns1",
			Command: "dns/x.sh", Result: "ok"}
		if err := env.history.Insert(ctx, op); err != nil {
			t.Fatal(err)
		}
	}

	ops, err := env.svc.History(ctx)

	if err != nil || len(ops) != 50 {
		t.Fatalf("History = %d filas, %v; se esperaban 50", len(ops), err)
	}
	if ops[0].Operation != "op54" || ops[49].Operation != "op05" {
		t.Errorf("primera = %s, última = %s; se esperaba op54 y op05", ops[0].Operation, ops[49].Operation)
	}
}

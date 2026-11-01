package service

import (
	"context"
	"errors"
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

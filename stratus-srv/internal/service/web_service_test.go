package service

import (
	"bytes"
	"context"
	"errors"
	"log"
	"path/filepath"
	"testing"
	"time"

	"stratus-srv/internal/repository"
)

const (
	testApacheDisk = "/vms/apache-base.vdi"
	testNginxDisk  = "/vms/nginx-base.vdi"
	testDomain     = "stratus.lan"
)

// webEnv agrupa el servicio de servidores web con un PrepareService y un HAProxyService que
// comparten sus bloqueos, su SQLite temporal y su runner: así se prueban las reglas cruzadas.
type webEnv struct {
	svc     *WebService
	dnsSvc  *DNSService
	prepare *PrepareService
	haproxy *HAProxyService
	vms     *repository.VMRepository
	lbs     *repository.LBRepository
	web     *repository.WebRepository
	dns     *repository.DNSRepository
	history *repository.HistoryRepository
	locks   *VMLocks
	runner  *fakeRunner
	logs    *bytes.Buffer
}

func newWebEnv(t *testing.T, runner *fakeRunner) webEnv {
	t.Helper()
	db, err := repository.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	env := webEnv{
		vms:     repository.NewVMRepository(db),
		lbs:     repository.NewLBRepository(db),
		web:     repository.NewWebRepository(db),
		dns:     repository.NewDNSRepository(db),
		history: repository.NewHistoryRepository(db),
		locks:   &VMLocks{},
		runner:  runner,
		logs:    &bytes.Buffer{},
	}
	logger := log.New(env.logs, "", 0)
	env.dnsSvc = NewDNSService(testDNSConfig(), runner, env.vms, env.lbs, env.web, env.dns, env.history,
		env.locks, logger)
	env.svc = NewWebService(testWebConfig(), runner, env.vms, env.lbs, env.web, env.dns, env.history,
		env.locks, env.dnsSvc, logger)
	env.haproxy = NewHAProxyService(HAProxyConfig{BaseDisk: testHAProxyDisk, BridgeAdapter: testBridge},
		runner, env.vms, env.lbs, env.dns, env.history, env.locks, env.dnsSvc, logger)
	env.prepare = NewPrepareService(Config{BaseDisk: testBaseDisk, BridgeAdapter: testBridge, KeyTTL: 10 * time.Minute},
		runner, env.vms, env.lbs, env.history, env.locks, env.haproxy, env.svc, env.dnsSvc, logger)
	return env
}

func testWebConfig() WebConfig {
	return WebConfig{ApacheBaseDisk: testApacheDisk, NginxBaseDisk: testNginxDisk, BridgeAdapter: testBridge,
		Domain: testDomain, MaxArchiveBytes: 10 << 20}
}

// registerWeb registra un servidor web directamente en SQLite (su fila en vms y en web_servers).
func (e webEnv) registerWeb(t *testing.T, name, typ string) {
	t.Helper()
	ctx := context.Background()
	if _, err := e.vms.Insert(ctx, name); err != nil {
		t.Fatalf("Insert(%q): %v", name, err)
	}
	if err := e.web.Insert(ctx, repository.WebServer{Name: name, Type: typ}); err != nil {
		t.Fatalf("web.Insert(%q): %v", name, err)
	}
}

// lastOperation devuelve la fila más reciente del historial del módulo web.
func (e webEnv) lastOperation(t *testing.T) repository.Operation {
	t.Helper()
	ops, err := e.history.ListRecent(context.Background(), webModule, historyLimit)
	if err != nil || len(ops) == 0 {
		t.Fatalf("historial web vacío: %v", err)
	}
	return ops[0]
}

// AC-7: con una creación de VM de 001 en curso, crear un servidor web se rechaza sin llamar a ningún
// script, porque ambas comparten el bloqueo de creación.
func TestAC7_CreateWebSharesCreateLock(t *testing.T) {
	runner := &fakeRunner{
		results:   map[string]RunResult{"prepare/create-vm.sh": {Stdout: []byte("VM vm1 creada y encendida\n")}},
		block:     make(chan struct{}),
		started:   make(chan struct{}),
		blockOnly: "prepare/create-vm.sh",
	}
	env := newWebEnv(t, runner)
	ctx := context.Background()

	done := make(chan error)
	go func() {
		_, err := env.prepare.CreateVM(ctx, validCreate("vm1"))
		done <- err
	}()
	<-runner.started

	_, err := env.svc.CreateWeb(ctx, CreateWebInput{Name: "web-a", MemoryMB: 1024, CPUs: 1, Type: WebTypeNginx})

	close(runner.block)
	if createErr := <-done; createErr != nil {
		t.Fatalf("CreateVM: %v", createErr)
	}
	if !errors.Is(err, ErrCreateInProgress) {
		t.Errorf("err = %v, se esperaba ErrCreateInProgress", err)
	}
	if n := len(runner.callsTo("web/create-web.sh")); n != 0 {
		t.Errorf("create-web.sh se llamó %d veces, se esperaba 0", n)
	}
	if _, err := env.vms.Get(ctx, "web-a"); !errors.Is(err, repository.ErrVMNotFound) {
		t.Errorf("web-a no debía quedar en vms: err = %v", err)
	}
}

// REQ-36: el historial del módulo web solo trae sus propias filas.
func TestAC53_WebHistoryOnlyWebModule(t *testing.T) {
	env := newWebEnv(t, &fakeRunner{})
	ctx := context.Background()
	for _, op := range []repository.Operation{
		{Module: webModule, Operation: "create_web", VM: "web-a", Command: "web/create-web.sh", Result: "ok"},
		{Module: prepareModule, Operation: "create_vm", VM: "vm1", Command: "prepare/create-vm.sh", Result: "ok"},
	} {
		if err := env.history.Insert(ctx, op); err != nil {
			t.Fatal(err)
		}
	}

	ops, err := env.svc.History(ctx)

	if err != nil || len(ops) != 1 || ops[0].Operation != "create_web" {
		t.Errorf("History = %+v, %v; se esperaba solo create_web", ops, err)
	}
	if got := env.svc.MaxArchiveBytes(); got != 10<<20 {
		t.Errorf("MaxArchiveBytes = %d, se esperaba %d", got, 10<<20)
	}
}

// assignToLB registra un balanceador con subred 1 y le asigna la VM como server1.
func (e webEnv) assignToLB(t *testing.T, lb, vm string) {
	t.Helper()
	ctx := context.Background()
	if _, err := e.vms.Insert(ctx, lb); err != nil {
		t.Fatal(err)
	}
	if err := e.lbs.InsertLB(ctx, repository.LoadBalancer{Name: lb, Subnet: 1, FrontendPort: 80}); err != nil {
		t.Fatal(err)
	}
	if err := e.lbs.InsertServer(ctx, repository.LBServer{LB: lb, Name: "server1", VM: vm, Port: 80,
		LastIP: "10.10.1.11", Enabled: true}); err != nil {
		t.Fatal(err)
	}
}

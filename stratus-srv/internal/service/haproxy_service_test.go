package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"stratus-srv/internal/repository"
)

const testHAProxyDisk = "/vms/haproxy-base.vdi"

// haproxyEnv agrupa el servicio de HAProxy con un PrepareService que comparte sus bloqueos, su
// SQLite temporal y su runner: así se prueban las reglas cruzadas entre módulos.
type haproxyEnv struct {
	svc     *HAProxyService
	prepare *PrepareService
	vms     *repository.VMRepository
	lbs     *repository.LBRepository
	history *repository.HistoryRepository
	locks   *VMLocks
	runner  *fakeRunner
	logs    *bytes.Buffer
}

func newHAProxyEnv(t *testing.T, runner *fakeRunner) haproxyEnv {
	t.Helper()
	db, err := repository.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	env := haproxyEnv{
		vms:     repository.NewVMRepository(db),
		lbs:     repository.NewLBRepository(db),
		history: repository.NewHistoryRepository(db),
		locks:   &VMLocks{},
		runner:  runner,
		logs:    &bytes.Buffer{},
	}
	logger := log.New(env.logs, "", 0)
	env.svc = NewHAProxyService(HAProxyConfig{BaseDisk: testHAProxyDisk, BridgeAdapter: testBridge},
		runner, env.vms, env.lbs, env.history, env.locks, logger)
	env.prepare = NewPrepareService(Config{BaseDisk: testBaseDisk, BridgeAdapter: testBridge, KeyTTL: 10 * time.Minute},
		runner, env.vms, env.lbs, env.history, env.locks, env.svc, logger)
	return env
}

// registerLB registra un balanceador directamente en SQLite (su fila en vms y en load_balancers).
func (e haproxyEnv) registerLB(t *testing.T, name string, subnet, port int) {
	t.Helper()
	ctx := context.Background()
	if _, err := e.vms.Insert(ctx, name); err != nil {
		t.Fatalf("Insert(%q): %v", name, err)
	}
	if err := e.lbs.InsertLB(ctx, repository.LoadBalancer{Name: name, Subnet: subnet, FrontendPort: port}); err != nil {
		t.Fatalf("InsertLB(%q): %v", name, err)
	}
}

// register registra una VM común directamente en SQLite.
func (e haproxyEnv) register(t *testing.T, name string) {
	t.Helper()
	if _, err := e.vms.Insert(context.Background(), name); err != nil {
		t.Fatalf("Insert(%q): %v", name, err)
	}
}

// registerServer registra la VM y la asigna al balanceador, sin pasar por el script.
func (e haproxyEnv) registerServer(t *testing.T, lb, name, vm string, port int, ip string) {
	t.Helper()
	ctx := context.Background()
	if _, err := e.vms.Insert(ctx, vm); err != nil {
		t.Fatalf("Insert(%q): %v", vm, err)
	}
	s := repository.LBServer{LB: lb, Name: name, VM: vm, Port: port, LastIP: ip, Enabled: true}
	if err := e.lbs.InsertServer(ctx, s); err != nil {
		t.Fatalf("InsertServer(%v): %v", s, err)
	}
}

// lastOperation devuelve la fila más reciente del historial del módulo haproxy.
func (e haproxyEnv) lastOperation(t *testing.T) repository.Operation {
	t.Helper()
	ops, err := e.history.ListRecent(context.Background(), "haproxy", 50)
	if err != nil {
		t.Fatalf("ListRecent: %v", err)
	}
	if len(ops) == 0 {
		t.Fatal("el historial de haproxy está vacío, se esperaba una operación")
	}
	return ops[0]
}

// haproxyRows cuenta las filas del historial del módulo haproxy.
func (e haproxyEnv) haproxyRows(t *testing.T) int {
	t.Helper()
	ops, err := e.history.ListRecent(context.Background(), "haproxy", 50)
	if err != nil {
		t.Fatalf("ListRecent: %v", err)
	}
	return len(ops)
}

// AC-8: crear una VM y crear un balanceador se excluyen mutuamente (el bloqueo de creación es uno solo).
func TestAC8_CreateLockSharedWithCreateVM(t *testing.T) {
	t.Run("create_vm en curso rechaza create_lb", func(t *testing.T) {
		runner := &fakeRunner{block: make(chan struct{}), started: make(chan struct{}, 1), blockOnly: "prepare/create-vm.sh"}
		env := newHAProxyEnv(t, runner)

		done := make(chan error, 1)
		go func() {
			_, err := env.prepare.CreateVM(context.Background(), validCreate("vm1"))
			done <- err
		}()
		<-runner.started

		_, err := env.svc.CreateLB(context.Background(), validCreate("lb1"))
		if !errors.Is(err, ErrCreateInProgress) {
			t.Errorf("CreateLB: err = %v, se esperaba ErrCreateInProgress", err)
		}
		if n := len(runner.callsTo("haproxy/create-lb.sh")); n != 0 {
			t.Errorf("create-lb.sh se llamó %d veces, se esperaban 0", n)
		}
		if _, err := env.vms.Get(context.Background(), "lb1"); !errors.Is(err, repository.ErrVMNotFound) {
			t.Errorf("lb1 no debía quedar registrado: err = %v", err)
		}
		close(runner.block)
		if err := <-done; err != nil {
			t.Errorf("CreateVM: %v", err)
		}
	})

	t.Run("create_lb en curso rechaza create_vm", func(t *testing.T) {
		runner := &fakeRunner{block: make(chan struct{}), started: make(chan struct{}, 1), blockOnly: "haproxy/create-lb.sh",
			results: map[string]RunResult{"haproxy/create-lb.sh": {Stdout: []byte("Balanceador lb1 creado\nsubnet=1\n")}}}
		env := newHAProxyEnv(t, runner)

		done := make(chan error, 1)
		go func() {
			_, err := env.svc.CreateLB(context.Background(), validCreate("lb1"))
			done <- err
		}()
		<-runner.started

		_, err := env.prepare.CreateVM(context.Background(), validCreate("vm1"))
		if !errors.Is(err, ErrCreateInProgress) {
			t.Errorf("CreateVM: err = %v, se esperaba ErrCreateInProgress", err)
		}
		if n := len(runner.callsTo("prepare/create-vm.sh")); n != 0 {
			t.Errorf("create-vm.sh se llamó %d veces, se esperaban 0", n)
		}
		close(runner.block)
		if err := <-done; err != nil {
			t.Errorf("CreateLB: %v", err)
		}
	})
}

// mapHAProxyError traduce solo los códigos propios del módulo.
func TestAC50_MapHAProxyError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want error
	}{
		{"8 sin subredes", &OperationError{Operation: "create_lb", ExitCode: 8}, ErrNoFreeSubnet},
		{"10 configuración inválida", &OperationError{Operation: "assign_server", ExitCode: 10}, ErrHAProxyConfigInvalid},
		{"11 recarga fallida", &OperationError{Operation: "save_frontend", ExitCode: 11}, ErrHAProxyReloadFailed},
		{"13 red interna registrada", &OperationError{Operation: "create_lb", ExitCode: 13}, ErrIntnetExists},
		{"envuelto", errors.Join(errors.New("x"), &OperationError{Operation: "create_lb", ExitCode: 13}), ErrIntnetExists},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mapHAProxyError(tt.err); !errors.Is(got, tt.want) {
				t.Errorf("mapHAProxyError = %v, se esperaba %v", got, tt.want)
			}
		})
	}
	other := &OperationError{Operation: "create_lb", ExitCode: 6, Detail: "Disco base de HAProxy no encontrado"}
	if got := mapHAProxyError(other); got != error(other) {
		t.Errorf("el código 6 debe quedar igual: %v", got)
	}
	if got := mapHAProxyError(ErrTimeout); !errors.Is(got, ErrTimeout) {
		t.Errorf("un error que no es OperationError debe quedar igual: %v", got)
	}
	if mapHAProxyError(nil) != nil {
		t.Error("mapHAProxyError(nil) debe ser nil")
	}
}

// AC-24: el código 10 de cualquier operación que reescribe haproxy.cfg se traduce y no cambia SQLite.
func TestAC24_ConfigInvalidMapsError(t *testing.T) {
	operations := []struct {
		name   string
		script string
		run    func(env haproxyEnv) error
	}{
		{"AssignServer", "haproxy/assign-server.sh", func(env haproxyEnv) error {
			_, err := env.svc.AssignServer(context.Background(), "lb1", "web-a", 80)
			return err
		}},
		{"RemoveServer", "haproxy/remove-server.sh", func(env haproxyEnv) error {
			_, err := env.svc.RemoveServer(context.Background(), "lb1", "web-b")
			return err
		}},
		{"SaveFrontend", "haproxy/save-frontend.sh", func(env haproxyEnv) error {
			_, err := env.svc.SaveFrontend(context.Background(), "lb1", 8080)
			return err
		}},
		{"Refresh", "haproxy/refresh-lb.sh", func(env haproxyEnv) error {
			_, err := env.svc.Refresh(context.Background(), "lb1")
			return err
		}},
	}
	for _, op := range operations {
		t.Run(op.name, func(t *testing.T) {
			const check = "[ALERT] (1) : config inválida"
			runner := &fakeRunner{results: map[string]RunResult{
				op.script: {ExitCode: 10, Stderr: check + "\nConfiguración de HAProxy inválida; se mantuvo la anterior\n"},
			}}
			env := newHAProxyEnv(t, runner)
			env.registerLB(t, "lb1", 1, 80)
			env.register(t, "web-a")
			env.registerServer(t, "lb1", "server1", "web-b", 80, "10.10.1.12")
			ctx := context.Background()

			err := op.run(env)

			if !errors.Is(err, ErrHAProxyConfigInvalid) {
				t.Fatalf("err = %v, se esperaba ErrHAProxyConfigInvalid", err)
			}
			rows, _ := env.lbs.Servers(ctx, "lb1")
			if len(rows) != 1 || rows[0].VM != "web-b" {
				t.Errorf("lb_servers = %+v, no debía cambiar", rows)
			}
			if lb, _ := env.lbs.GetLB(ctx, "lb1"); lb.FrontendPort != 80 {
				t.Errorf("frontend_port = %d, no debía cambiar", lb.FrontendPort)
			}
			if last := env.lastOperation(t); last.Result != "error" || last.ExitCode != 10 || !strings.Contains(last.Stderr, check) {
				t.Errorf("historial = %+v, se esperaba error 10 con la salida de la revisión", last)
			}
		})
	}
}

// AC-34: una asignación en curso bloquea el balanceador y la VM; las demás operaciones sobre
// cualquiera de los dos se rechazan sin llamar a ningún script.
func TestAC34_AssignLocksLBAndServer(t *testing.T) {
	runner := &fakeRunner{block: make(chan struct{}), started: make(chan struct{}, 1), blockOnly: "haproxy/assign-server.sh",
		results: map[string]RunResult{"haproxy/assign-server.sh": {Stdout: []byte(assignStdout)}}}
	env := newHAProxyEnv(t, runner)
	env.registerLB(t, "lb1", 1, 80)
	env.register(t, "web-a")
	ctx := context.Background()

	done := make(chan error, 1)
	go func() {
		_, err := env.svc.AssignServer(ctx, "lb1", "web-a", 80)
		done <- err
	}()
	<-runner.started

	attempts := map[string]func() error{
		"InstallPackage en web-a": func() error { _, err := env.prepare.InstallPackage(ctx, "web-a", "htop"); return err },
		"SaveFrontend en lb1":     func() error { _, err := env.svc.SaveFrontend(ctx, "lb1", 8080); return err },
		"Refresh en lb1":          func() error { _, err := env.svc.Refresh(ctx, "lb1"); return err },
	}
	for name, attempt := range attempts {
		if err := attempt(); !errors.Is(err, ErrOperationInProgress) {
			t.Errorf("%s: err = %v, se esperaba ErrOperationInProgress", name, err)
		}
	}
	if n := runner.callCount(); n != 1 {
		t.Errorf("el runner se llamó %d veces, se esperaba 1 (solo la asignación)", n)
	}
	close(runner.block)
	if err := <-done; err != nil {
		t.Fatalf("AssignServer: %v", err)
	}
}

// AC-35: el historial del módulo trae las últimas 50 filas de haproxy, la más reciente primero, sin
// mezclar otros módulos; la lectura de estado solo deja fila cuando falla.
func TestAC35_HAProxyHistoryAndRecordPolicy(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{
		"haproxy/lb-status.sh": {Stdout: []byte("server1 UP\n")},
	}}
	env := newHAProxyEnv(t, runner)
	env.registerLB(t, "lb1", 1, 80)
	ctx := context.Background()
	for i := 0; i < 55; i++ {
		row := repository.Operation{Module: "haproxy", Operation: fmt.Sprintf("op-%d", i), VM: "lb1", Command: "haproxy/x.sh", Result: "ok"}
		if err := env.history.Insert(ctx, row); err != nil {
			t.Fatal(err)
		}
		if i%10 == 0 {
			other := repository.Operation{Module: "prepare", Operation: "start_vm", VM: "vm1", Command: "prepare/start-vm.sh", Result: "ok"}
			if err := env.history.Insert(ctx, other); err != nil {
				t.Fatal(err)
			}
		}
	}

	ops, err := env.svc.History(ctx)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(ops) != 50 {
		t.Fatalf("History trae %d filas, se esperaban 50", len(ops))
	}
	if ops[0].Operation != "op-54" || ops[49].Operation != "op-5" {
		t.Errorf("orden = %s ... %s, se esperaba op-54 ... op-5", ops[0].Operation, ops[49].Operation)
	}
	for _, op := range ops {
		if op.Module != "haproxy" {
			t.Fatalf("fila de otro módulo: %+v", op)
		}
	}

	// Cinco lecturas exitosas no agregan filas; una que falla agrega una.
	for i := 0; i < 5; i++ {
		if _, err := env.svc.ServerStatus(ctx, "lb1"); err != nil {
			t.Fatalf("ServerStatus: %v", err)
		}
	}
	if n := env.haproxyRows(t); n != 50 { // ListRecent limita a 50: miramos la fila más reciente
		t.Errorf("filas = %d", n)
	}
	if last := env.lastOperation(t); last.Operation != "op-54" {
		t.Errorf("las lecturas exitosas agregaron la fila %+v", last)
	}
	runner.results["haproxy/lb-status.sh"] = RunResult{ExitCode: 5, Stderr: "No se pudo leer el estado de HAProxy en lb1\n"}
	if _, err := env.svc.ServerStatus(ctx, "lb1"); err == nil {
		t.Fatal("ServerStatus: se esperaba un error")
	}
	if last := env.lastOperation(t); last.Operation != "lb_status" || last.Result != "error" || last.VM != "lb1" {
		t.Errorf("fila tras el fallo = %+v, se esperaba lb_status con error en lb1", last)
	}
}

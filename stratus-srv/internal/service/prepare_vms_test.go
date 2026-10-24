package service

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"stratus-srv/internal/repository"
)

const (
	testBaseDisk = "/vms/base.vdi"
	testBridge   = "eth0"
)

// fakeCall es una invocación registrada por fakeRunner.
type fakeCall struct {
	Script string
	Args   []string
}

// fakeRunner reemplaza a ExecRunner en las pruebas: registra cada llamada y responde con
// el resultado preparado para ese script (o éxito vacío si no hay ninguno).
type fakeRunner struct {
	mu      sync.Mutex
	calls   []fakeCall
	results map[string]RunResult
	// Si block no es nil, Run avisa por started y espera a que se cierre block. Si blockOnly no
	// está vacío, solo se bloquea ese script y los demás responden de inmediato.
	block     chan struct{}
	started   chan struct{}
	blockOnly string
}

func (f *fakeRunner) Run(_ context.Context, script string, args ...string) (RunResult, error) {
	f.mu.Lock()
	f.calls = append(f.calls, fakeCall{Script: script, Args: append([]string(nil), args...)})
	res := f.results[script]
	block, started := f.block, f.started
	if f.blockOnly != "" && f.blockOnly != script {
		block = nil
	}
	f.mu.Unlock()

	if block != nil {
		started <- struct{}{}
		<-block
	}
	return res, nil
}

// callsTo devuelve las llamadas registradas a un script.
func (f *fakeRunner) callsTo(script string) []fakeCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []fakeCall
	for _, c := range f.calls {
		if c.Script == script {
			out = append(out, c)
		}
	}
	return out
}

func (f *fakeRunner) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// testEnv agrupa el servicio con sus dependencias reales (SQLite temporal) y el registro.
type testEnv struct {
	svc     *PrepareService
	vms     *repository.VMRepository
	lbs     *repository.LBRepository
	history *repository.HistoryRepository
	logs    *bytes.Buffer
}

func newTestEnv(t *testing.T, runner ScriptRunner) testEnv {
	t.Helper()
	db, err := repository.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	env := testEnv{
		vms:     repository.NewVMRepository(db),
		lbs:     repository.NewLBRepository(db),
		history: repository.NewHistoryRepository(db),
		logs:    &bytes.Buffer{},
	}
	cfg := Config{
		BaseDisk:      testBaseDisk,
		BridgeAdapter: testBridge,
		SSHKeyPath:    "/keys/stratus_backend",
		KeyTTL:        10 * time.Minute,
	}
	locks := &VMLocks{}
	logger := log.New(env.logs, "", 0)
	haproxy := NewHAProxyService(HAProxyConfig{BaseDisk: testHAProxyDisk, BridgeAdapter: testBridge},
		runner, env.vms, env.lbs, env.history, locks, logger)
	env.svc = NewPrepareService(cfg, runner, env.vms, env.lbs, env.history, locks, haproxy, logger)
	return env
}

// newPrepareForTest arma un PrepareService para las pruebas de bloqueo de otros módulos, que
// comparten solo los bloqueos y el runner. Su repositorio de balanceadores usa una base temporal
// propia: esas pruebas no tienen balanceadores ni servidores.
func newPrepareForTest(t *testing.T, runner ScriptRunner, vms *repository.VMRepository,
	history *repository.HistoryRepository, locks *VMLocks, logger *log.Logger) *PrepareService {
	t.Helper()
	db, err := repository.Open(filepath.Join(t.TempDir(), "lbs.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	lbs := repository.NewLBRepository(db)
	haproxy := NewHAProxyService(HAProxyConfig{}, runner, vms, lbs, history, locks, logger)
	return NewPrepareService(Config{KeyTTL: 1}, runner, vms, lbs, history, locks, haproxy, logger)
}

// register registra una VM directamente en SQLite, sin pasar por el script.
func (e testEnv) register(t *testing.T, name string) repository.VM {
	t.Helper()
	vm, err := e.vms.Insert(context.Background(), name)
	if err != nil {
		t.Fatalf("Insert(%q): %v", name, err)
	}
	return vm
}

// lastOperation devuelve la fila más reciente del historial de prepare.
func (e testEnv) lastOperation(t *testing.T) repository.Operation {
	t.Helper()
	ops, err := e.history.ListRecent(context.Background(), "prepare", 50)
	if err != nil {
		t.Fatalf("ListRecent: %v", err)
	}
	if len(ops) == 0 {
		t.Fatal("el historial está vacío, se esperaba una operación")
	}
	return ops[0]
}

func validCreate(name string) CreateVMInput {
	return CreateVMInput{Name: name, MemoryMB: 2048, CPUs: 2}
}

func TestAC1_CreateVMInvokesScriptWithArgs(t *testing.T) {
	const line = "VM vm1 creada y encendida; IP 192.168.1.50"
	runner := &fakeRunner{results: map[string]RunResult{
		"prepare/create-vm.sh": {Stdout: []byte(line + "\n")},
	}}
	env := newTestEnv(t, runner)

	msg, err := env.svc.CreateVM(context.Background(), validCreate("vm1"))
	if err != nil {
		t.Fatalf("CreateVM: %v", err)
	}
	if msg != line {
		t.Errorf("mensaje = %q, se esperaba %q", msg, line)
	}
	calls := runner.callsTo("prepare/create-vm.sh")
	if len(calls) != 1 {
		t.Fatalf("create-vm.sh se llamó %d veces, se esperaba 1", len(calls))
	}
	want := []string{"vm1", "2048", "2", testBaseDisk, testBridge}
	if !reflect.DeepEqual(calls[0].Args, want) {
		t.Errorf("argv = %q, se esperaba %q", calls[0].Args, want)
	}
	if _, err := env.vms.Get(context.Background(), "vm1"); err != nil {
		t.Errorf("la VM no quedó registrada: %v", err)
	}
}

func TestAC2_CreateVMInvalidSkipsScript(t *testing.T) {
	runner := &fakeRunner{}
	env := newTestEnv(t, runner)

	in := validCreate("vm1")
	in.CPUs = 5
	_, err := env.svc.CreateVM(context.Background(), in)
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("CreateVM con 5 CPUs: err = %v, se esperaba ErrInvalidInput", err)
	}
	if n := runner.callCount(); n != 0 {
		t.Errorf("el runner se llamó %d veces, se esperaba 0", n)
	}
	if _, err := env.vms.Get(context.Background(), "vm1"); !errors.Is(err, repository.ErrVMNotFound) {
		t.Errorf("Get tras entrada inválida: err = %v, se esperaba ErrVMNotFound", err)
	}
}

func TestAC41_CreateVMFailureReleasesName(t *testing.T) {
	tests := []struct {
		name   string
		result RunResult
		want   string
	}{
		{"disco base inexistente", RunResult{ExitCode: 6, Stderr: "Disco base no encontrado: /vms/base.vdi\n"},
			"Disco base no encontrado: /vms/base.vdi"},
		{"bootstrap fallido", RunResult{ExitCode: 5, Stderr: "Falló la preparación de vm1 por Guest Additions\n"},
			"Falló la preparación de vm1 por Guest Additions"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runner := &fakeRunner{results: map[string]RunResult{"prepare/create-vm.sh": tc.result}}
			env := newTestEnv(t, runner)
			ctx := context.Background()

			_, err := env.svc.CreateVM(ctx, validCreate("vm1"))
			var opErr *OperationError
			if !errors.As(err, &opErr) {
				t.Fatalf("err = %v, se esperaba OperationError", err)
			}
			if !strings.Contains(opErr.Error(), tc.want) {
				t.Errorf("mensaje = %q, se esperaba que contuviera %q", opErr.Error(), tc.want)
			}
			if _, err := env.vms.Get(ctx, "vm1"); !errors.Is(err, repository.ErrVMNotFound) {
				t.Errorf("Get tras fallar: err = %v, se esperaba ErrVMNotFound", err)
			}
			// El nombre quedó libre: un segundo intento llega al script.
			_, _ = env.svc.CreateVM(ctx, validCreate("vm1"))
			if n := len(runner.callsTo("prepare/create-vm.sh")); n != 2 {
				t.Errorf("create-vm.sh se llamó %d veces, se esperaban 2", n)
			}
		})
	}
}

func TestAC45_CreateVMNeverSeesGuestPassword(t *testing.T) {
	const password = "Pr0ebaClave+/xyz"
	passwordFile := filepath.Join(t.TempDir(), "guest-password")
	if err := os.WriteFile(passwordFile, []byte(password), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APP_GUEST_PASSWORD_FILE", passwordFile)
	runner := &fakeRunner{}
	env := newTestEnv(t, runner)

	if _, err := env.svc.CreateVM(context.Background(), validCreate("vm1")); err != nil {
		t.Fatalf("CreateVM: %v", err)
	}
	calls := runner.callsTo("prepare/create-vm.sh")
	if len(calls) != 1 || len(calls[0].Args) != 5 {
		t.Fatalf("llamadas = %+v, se esperaba una con exactamente 5 argumentos", calls)
	}
	op := env.lastOperation(t)
	seen := strings.Join(calls[0].Args, " ") + " " + strings.Join(op.Args, " ") + " " + op.Stderr + " " + env.logs.String()
	for _, secret := range []string{password, passwordFile} {
		if strings.Contains(seen, secret) {
			t.Errorf("el argv, el historial o el registro contienen %q", secret)
		}
	}
}

// startBlockedCreate inicia CreateVM(vm1) con create-vm.sh bloqueado y espera a que esté dentro del script.
func startBlockedCreate(t *testing.T, env testEnv, runner *fakeRunner) <-chan error {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		_, err := env.svc.CreateVM(context.Background(), validCreate("vm1"))
		done <- err
	}()
	<-runner.started
	return done
}

func TestAC46_SecondCreateRejected(t *testing.T) {
	runner := &fakeRunner{block: make(chan struct{}), started: make(chan struct{}, 2), blockOnly: "prepare/create-vm.sh"}
	env := newTestEnv(t, runner)
	ctx := context.Background()
	env.register(t, "vm3")
	first := startBlockedCreate(t, env, runner)

	_, err := env.svc.CreateVM(ctx, validCreate("vm2"))
	if !errors.Is(err, ErrCreateInProgress) {
		t.Fatalf("segunda creación: err = %v, se esperaba ErrCreateInProgress", err)
	}
	if ErrCreateInProgress.Error() != "Ya hay una creación de VM en curso" {
		t.Errorf("mensaje = %q", ErrCreateInProgress.Error())
	}
	if n := len(runner.callsTo("prepare/create-vm.sh")); n != 1 {
		t.Errorf("create-vm.sh se llamó %d veces, se esperaba 1", n)
	}
	if _, err := env.vms.Get(ctx, "vm2"); !errors.Is(err, repository.ErrVMNotFound) {
		t.Errorf("Get(vm2) tras el rechazo: err = %v, se esperaba ErrVMNotFound", err)
	}
	// Las demás operaciones no se bloquean por la creación en curso.
	if _, err := env.svc.StartVM(ctx, "vm3"); err != nil {
		t.Errorf("StartVM(vm3) durante la creación: %v", err)
	}

	close(runner.block)
	if err := <-first; err != nil {
		t.Fatalf("primera creación: %v", err)
	}
	if _, err := env.svc.CreateVM(ctx, validCreate("vm2")); err != nil {
		t.Errorf("creación tras terminar la primera: %v", err)
	}
	if n := len(runner.callsTo("prepare/create-vm.sh")); n != 2 {
		t.Errorf("create-vm.sh se llamó %d veces, se esperaban 2", n)
	}
}

func TestAC46_InvalidCreateDoesNotTakeLock(t *testing.T) {
	runner := &fakeRunner{block: make(chan struct{}), started: make(chan struct{}, 2), blockOnly: "prepare/create-vm.sh"}
	env := newTestEnv(t, runner)
	first := startBlockedCreate(t, env, runner)

	in := validCreate("vm2")
	in.CPUs = 5
	_, err := env.svc.CreateVM(context.Background(), in)
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("err = %v, se esperaba ErrInvalidInput (la validación va antes del bloqueo)", err)
	}

	close(runner.block)
	if err := <-first; err != nil {
		t.Errorf("primera creación: %v", err)
	}
}

func TestAC29_CreateVMDuplicateSkipsScript(t *testing.T) {
	runner := &fakeRunner{}
	env := newTestEnv(t, runner)
	env.register(t, "vm1")

	_, err := env.svc.CreateVM(context.Background(), validCreate("vm1"))
	if !errors.Is(err, ErrDuplicateVM) {
		t.Fatalf("CreateVM duplicada: err = %v, se esperaba ErrDuplicateVM", err)
	}
	if ErrDuplicateVM.Error() != "Ya existe una VM con ese nombre" {
		t.Errorf("mensaje = %q", ErrDuplicateVM.Error())
	}
	if n := runner.callCount(); n != 0 {
		t.Errorf("el runner se llamó %d veces, se esperaba 0", n)
	}
}

func TestAC3_ListVMsMergesStates(t *testing.T) {
	tests := []struct {
		name          string
		listResult    RunResult
		wantStates    map[string]string
		wantIPs       map[string]string
		wantListError string // texto que debe contener list_error; vacío si no debe haber error
	}{
		{
			name:          "vm2 no aparece en VirtualBox",
			listResult:    RunResult{Stdout: []byte("vm1 running 192.168.1.50\n")},
			wantStates:    map[string]string{"vm1": "running", "vm2": "unknown"},
			wantIPs:       map[string]string{"vm1": "192.168.1.50", "vm2": ""},
			wantListError: "vm2",
		},
		{
			name:       "ambas aparecen",
			listResult: RunResult{Stdout: []byte("vm1 running 192.168.1.50\nvm2 poweroff -\notra running -\nMint Clone poweroff -\n")},
			wantStates: map[string]string{"vm1": "running", "vm2": "poweroff"},
			wantIPs:    map[string]string{"vm1": "192.168.1.50", "vm2": ""},
		},
		{
			name:          "el script de listado es el placeholder",
			listResult:    RunResult{ExitCode: 99, Stderr: "Operación no implementada\n"},
			wantStates:    map[string]string{"vm1": "unknown", "vm2": "unknown"},
			wantIPs:       map[string]string{"vm1": "", "vm2": ""},
			wantListError: "Operación no implementada",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runner := &fakeRunner{results: map[string]RunResult{"prepare/list-vms.sh": tc.listResult}}
			env := newTestEnv(t, runner)
			env.register(t, "vm1")
			env.register(t, "vm2")

			vms, listErr, err := env.svc.ListVMs(context.Background())
			if err != nil {
				t.Fatalf("ListVMs: %v", err)
			}
			if len(vms) != 2 {
				t.Fatalf("ListVMs devolvió %d VMs, se esperaban 2", len(vms))
			}
			for _, vm := range vms {
				if want := tc.wantStates[vm.Name]; vm.State != want {
					t.Errorf("estado de %s = %q, se esperaba %q", vm.Name, vm.State, want)
				}
				if want := tc.wantIPs[vm.Name]; vm.IP != want {
					t.Errorf("IP de %s = %q, se esperaba %q", vm.Name, vm.IP, want)
				}
			}
			if tc.wantListError == "" && listErr != "" {
				t.Errorf("list_error = %q, se esperaba vacío", listErr)
			}
			if tc.wantListError != "" && !strings.Contains(listErr, tc.wantListError) {
				t.Errorf("list_error = %q, se esperaba que contuviera %q", listErr, tc.wantListError)
			}
			if ops, _ := env.history.ListRecent(context.Background(), "prepare", 50); len(ops) != 0 {
				t.Errorf("el listado se guardó en el historial (%d filas), no debe guardarse", len(ops))
			}
		})
	}
}

func TestAC42_ListVMsParsesIP(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{
		"prepare/list-vms.sh": {Stdout: []byte("vm1 running 192.168.1.50\nvm2 poweroff -\nvm3 running 999.1.1.1\nvm4 running fe80::1\n")},
	}}
	env := newTestEnv(t, runner)
	for _, name := range []string{"vm1", "vm2", "vm3", "vm4"} {
		env.register(t, name)
	}

	vms, _, err := env.svc.ListVMs(context.Background())
	if err != nil {
		t.Fatalf("ListVMs: %v", err)
	}
	want := map[string]string{"vm1": "192.168.1.50", "vm2": "", "vm3": "", "vm4": ""}
	for _, vm := range vms {
		if vm.IP != want[vm.Name] {
			t.Errorf("IP de %s = %q, se esperaba %q", vm.Name, vm.IP, want[vm.Name])
		}
		if vm.State == "unknown" {
			t.Errorf("estado de %s = unknown, se esperaba el que informa el script", vm.Name)
		}
	}
}

func TestAC30_StopVMInvokesStopScript(t *testing.T) {
	runner := &fakeRunner{}
	env := newTestEnv(t, runner)
	env.register(t, "vm1")

	msg, err := env.svc.StopVM(context.Background(), "vm1")
	if err != nil {
		t.Fatalf("StopVM: %v", err)
	}
	if msg == "" {
		t.Error("StopVM devolvió un mensaje vacío")
	}
	calls := runner.callsTo("prepare/stop-vm.sh")
	if len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, []string{"vm1"}) {
		t.Errorf("llamadas a stop-vm.sh = %+v, se esperaba una con [vm1]", calls)
	}
}

func TestAC47_StartVMInvokesStartScript(t *testing.T) {
	runner := &fakeRunner{}
	env := newTestEnv(t, runner)
	env.register(t, "vm1")

	res, err := env.svc.StartVM(context.Background(), "vm1")
	if err != nil {
		t.Fatalf("StartVM: %v", err)
	}
	if res.Message != "VM vm1 encendida" || res.Warning != "" {
		t.Errorf("resultado = %+v, se esperaba \"VM vm1 encendida\" sin advertencia", res)
	}
	if n := len(runner.callsTo("haproxy/refresh-lb.sh")); n != 0 {
		t.Errorf("una VM común no refresca ningún balanceador: %d llamadas", n)
	}
	calls := runner.callsTo("prepare/start-vm.sh")
	if len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, []string{"vm1"}) {
		t.Errorf("llamadas a start-vm.sh = %+v, se esperaba una con [vm1]", calls)
	}
	if op := env.lastOperation(t); op.Operation != "start_vm" || op.Result != "ok" {
		t.Errorf("historial = %+v, se esperaba start_vm con resultado ok", op)
	}

	// Una VM que la app no conoce se rechaza sin llamar al script.
	if _, err := env.svc.StartVM(context.Background(), "vm9"); !errors.Is(err, ErrVMNotFound) {
		t.Errorf("StartVM de VM inexistente: err = %v, se esperaba ErrVMNotFound", err)
	}
	if n := len(runner.callsTo("prepare/start-vm.sh")); n != 1 {
		t.Errorf("start-vm.sh se llamó %d veces, se esperaba 1", n)
	}
}

func TestAC31_DeleteVMRemovesRow(t *testing.T) {
	runner := &fakeRunner{}
	env := newTestEnv(t, runner)
	ctx := context.Background()
	env.register(t, "vm1")

	if _, err := env.svc.DeleteVM(ctx, "vm1"); err != nil {
		t.Fatalf("DeleteVM: %v", err)
	}
	calls := runner.callsTo("prepare/delete-vm.sh")
	if len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, []string{"vm1"}) {
		t.Errorf("llamadas a delete-vm.sh = %+v, se esperaba una con [vm1]", calls)
	}
	if _, err := env.vms.Get(ctx, "vm1"); !errors.Is(err, repository.ErrVMNotFound) {
		t.Errorf("Get tras DeleteVM: err = %v, se esperaba ErrVMNotFound", err)
	}

	// Una VM que la app no conoce se rechaza sin llamar al script.
	if _, err := env.svc.DeleteVM(ctx, "vm1"); !errors.Is(err, ErrVMNotFound) {
		t.Errorf("DeleteVM de VM inexistente: err = %v, se esperaba ErrVMNotFound", err)
	}
	if n := len(runner.callsTo("prepare/delete-vm.sh")); n != 1 {
		t.Errorf("delete-vm.sh se llamó %d veces, se esperaba 1", n)
	}
}

func TestAC39_DeleteVMClearsFlags(t *testing.T) {
	runner := &fakeRunner{}
	env := newTestEnv(t, runner)
	ctx := context.Background()
	env.register(t, "vm1")
	if err := env.vms.SetUserKeyGenerated(ctx, "vm1"); err != nil {
		t.Fatal(err)
	}
	// Llave del usuario generada y aún sin descargar.
	env.svc.keys.Put("vm1", []byte("-----BEGIN OPENSSH PRIVATE KEY-----\nclave\n-----END OPENSSH PRIVATE KEY-----\n"))

	if _, err := env.svc.DeleteVM(ctx, "vm1"); err != nil {
		t.Fatalf("DeleteVM: %v", err)
	}
	if _, err := env.svc.DownloadUserKey("vm1"); !errors.Is(err, ErrKeyUnavailable) {
		t.Errorf("DownloadUserKey tras eliminar: err = %v, se esperaba ErrKeyUnavailable", err)
	}
	if _, err := env.svc.CreateVM(ctx, validCreate("vm1")); err != nil {
		t.Fatalf("CreateVM tras eliminar: %v", err)
	}
	vm, err := env.vms.Get(ctx, "vm1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if vm.UserKeyGenerated {
		t.Errorf("user_key_generated tras eliminar y crear de nuevo = true, se esperaba false")
	}
}

func TestAC19_SameVMOperationRejected(t *testing.T) {
	runner := &fakeRunner{block: make(chan struct{}), started: make(chan struct{}, 1)}
	env := newTestEnv(t, runner)
	ctx := context.Background()
	env.register(t, "vm1")

	firstErr := make(chan error, 1)
	go func() {
		_, err := env.svc.StartVM(ctx, "vm1")
		firstErr <- err
	}()
	<-runner.started // la primera operación ya tiene el bloqueo y está "ejecutando" el script

	_, err := env.svc.StartVM(ctx, "vm1")
	if !errors.Is(err, ErrOperationInProgress) {
		t.Errorf("segunda operación: err = %v, se esperaba ErrOperationInProgress", err)
	}
	if ErrOperationInProgress.Error() != "Operación en curso en esta VM" {
		t.Errorf("mensaje = %q", ErrOperationInProgress.Error())
	}

	close(runner.block)
	if err := <-firstErr; err != nil {
		t.Errorf("primera operación: %v", err)
	}
	if n := len(runner.callsTo("prepare/start-vm.sh")); n != 1 {
		t.Errorf("start-vm.sh se llamó %d veces, se esperaba 1", n)
	}
}

func TestAC8_FailedScriptRecordsHistory(t *testing.T) {
	failed := RunResult{ExitCode: 5, Stderr: "boom\n"}
	tests := []struct {
		name      string
		script    string
		operation string
		run       func(env testEnv) error
		wantArgs  []string
		// wantRowGone indica que la VM reservada para crearla debe liberarse al fallar.
		wantRowGone bool
	}{
		{
			name:      "iniciar",
			script:    "prepare/start-vm.sh",
			operation: "start_vm",
			run: func(env testEnv) error {
				_, err := env.svc.StartVM(context.Background(), "vm1")
				return err
			},
			wantArgs: []string{"vm1"},
		},
		{
			name:      "crear",
			script:    "prepare/create-vm.sh",
			operation: "create_vm",
			run: func(env testEnv) error {
				_, err := env.svc.CreateVM(context.Background(), validCreate("vm2"))
				return err
			},
			wantArgs:    []string{"vm2", "2048", "2", testBaseDisk, testBridge},
			wantRowGone: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runner := &fakeRunner{results: map[string]RunResult{tc.script: failed}}
			env := newTestEnv(t, runner)
			env.register(t, "vm1")

			err := tc.run(env)
			var opErr *OperationError
			if !errors.As(err, &opErr) {
				t.Fatalf("err = %v, se esperaba OperationError", err)
			}
			if opErr.ExitCode != 5 || opErr.Detail != "boom" {
				t.Errorf("OperationError = %+v, se esperaba código 5 y detalle boom", opErr)
			}

			op := env.lastOperation(t)
			if op.Module != "prepare" || op.Operation != tc.operation || op.Command != tc.script {
				t.Errorf("fila = %+v, se esperaba módulo prepare, operación %s y comando %s", op, tc.operation, tc.script)
			}
			if !reflect.DeepEqual(op.Args, tc.wantArgs) {
				t.Errorf("args = %q, se esperaba %q", op.Args, tc.wantArgs)
			}
			if op.ExitCode != 5 || op.Stderr != "boom" || op.Result != "error" {
				t.Errorf("fila = %+v, se esperaba código 5, stderr boom y resultado error", op)
			}
			if tc.wantRowGone {
				if _, err := env.vms.Get(context.Background(), "vm2"); !errors.Is(err, repository.ErrVMNotFound) {
					t.Errorf("Get tras crear con fallo: err = %v, se esperaba ErrVMNotFound", err)
				}
			}
		})
	}
}

func TestAC20_TimeoutRecordedInHistory(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{
		"prepare/start-vm.sh": {ExitCode: -1, TimedOut: true},
	}}
	env := newTestEnv(t, runner)
	env.register(t, "vm1")

	_, err := env.svc.StartVM(context.Background(), "vm1")
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v, se esperaba ErrTimeout", err)
	}
	op := env.lastOperation(t)
	if op.Result != "timeout" || op.ExitCode != -1 {
		t.Errorf("fila = %+v, se esperaba resultado timeout y código -1", op)
	}
}

func TestAC37_PlaceholderRecordedInHistory(t *testing.T) {
	dir := t.TempDir()
	writeScript(t, dir, "start-vm.sh", "set -euo pipefail\necho \"Operación no implementada\" >&2\nexit 99")
	env := newTestEnv(t, ExecRunner{ScriptsDir: dir, Timeout: 5 * time.Second})
	env.register(t, "vm1")

	_, err := env.svc.StartVM(context.Background(), "vm1")
	var opErr *OperationError
	if !errors.As(err, &opErr) || opErr.ExitCode != 99 {
		t.Fatalf("err = %v, se esperaba OperationError con código 99", err)
	}
	op := env.lastOperation(t)
	if op.ExitCode != 99 || op.Stderr != "Operación no implementada" || op.Result != "error" {
		t.Errorf("fila = %+v, se esperaba código 99, stderr \"Operación no implementada\" y resultado error", op)
	}
}

func TestAC28_HistoryAndLogsHaveNoSecrets(t *testing.T) {
	privateKey := "-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAAA\n-----END OPENSSH PRIVATE KEY-----"
	runner := &fakeRunner{results: map[string]RunResult{
		"prepare/start-vm.sh": {
			ExitCode: 4,
			Stdout:   []byte("stdout con " + privateKey),
			Stderr:   "falló la conexión\n" + privateKey + "\n",
		},
	}}
	env := newTestEnv(t, runner)
	env.register(t, "vm1")

	if _, err := env.svc.StartVM(context.Background(), "vm1"); err == nil {
		t.Fatal("se esperaba un error del script")
	}

	op := env.lastOperation(t)
	stored := op.Stderr + " " + strings.Join(op.Args, " ")
	logs := env.logs.String()
	// La contraseña del invitado no llega al backend (REQ-22): solo queda la llave privada.
	const keyBody = "b3BlbnNzaC1rZXktdjEAAAAA"
	if strings.Contains(stored, keyBody) {
		t.Errorf("el historial contiene la llave privada: %q", stored)
	}
	if strings.Contains(logs, keyBody) {
		t.Errorf("el registro contiene la llave privada: %q", logs)
	}
	if !strings.Contains(logs, "start_vm") {
		t.Errorf("el registro no menciona la operación: %q", logs)
	}
}

func TestAC43_NoIPExitMapsToErrVMNoIP(t *testing.T) {
	noIP := RunResult{ExitCode: 7, Stderr: "La VM no tiene dirección IP; verifique que esté encendida\n"}
	scripts := map[string]RunResult{
		"prepare/install-package.sh":   noIP,
		"prepare/set-hostname.sh":      noIP,
		"prepare/set-issue.sh":         noIP,
		"prepare/generate-user-key.sh": noIP,
	}
	tests := []struct {
		name string
		run  func(env testEnv) error
	}{
		{"instalar paquete", func(env testEnv) error {
			_, err := env.svc.InstallPackage(context.Background(), "vm1", "nginx")
			return err
		}},
		{"hostname", func(env testEnv) error {
			_, err := env.svc.SetHostname(context.Background(), "vm1", "web-01")
			return err
		}},
		{"issue", func(env testEnv) error {
			_, err := env.svc.SetIssue(context.Background(), "vm1", IssueInput{Message: "Hola"})
			return err
		}},
		{"llave de usuario", func(env testEnv) error {
			_, err := env.svc.GenerateUserKey(context.Background(), "vm1")
			return err
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := newTestEnv(t, &fakeRunner{results: scripts})
			env.register(t, "vm1")

			err := tc.run(env)
			if !errors.Is(err, ErrVMNoIP) {
				t.Fatalf("err = %v, se esperaba ErrVMNoIP", err)
			}
			if ErrVMNoIP.Error() != "La VM no tiene dirección IP; verifique que esté encendida" {
				t.Errorf("mensaje = %q", ErrVMNoIP.Error())
			}
			op := env.lastOperation(t)
			if op.ExitCode != 7 || op.Result != "error" {
				t.Errorf("fila = %+v, se esperaba código 7 y resultado error", op)
			}
			if _, err := env.svc.DownloadUserKey("vm1"); !errors.Is(err, ErrKeyUnavailable) {
				t.Errorf("DownloadUserKey: err = %v, se esperaba ErrKeyUnavailable", err)
			}
		})
	}
}

// AC-9: ListVMs marca el rol de cada VM y, de un servidor, su balanceador y la IP en puente de este.
func TestAC9_ListVMsMarksRoles(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{
		"prepare/list-vms.sh": {Stdout: []byte("lb1 running 192.168.1.60\nweb-a running 10.10.1.11\nvm1 poweroff -\n")},
	}}
	env := newTestEnv(t, runner)
	ctx := context.Background()
	env.register(t, "lb1")
	env.register(t, "web-a")
	env.register(t, "vm1")
	if err := env.lbs.InsertLB(ctx, repository.LoadBalancer{Name: "lb1", Subnet: 1, FrontendPort: 80}); err != nil {
		t.Fatal(err)
	}
	if err := env.lbs.InsertServer(ctx, repository.LBServer{LB: "lb1", Name: "server1", VM: "web-a", Port: 80,
		LastIP: "10.10.1.11", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	vms, listErr, err := env.svc.ListVMs(ctx)
	if err != nil || listErr != "" {
		t.Fatalf("ListVMs: %v, %q", err, listErr)
	}

	want := []VMView{
		{Name: "lb1", State: "running", IP: "192.168.1.60", Role: "lb"},
		{Name: "vm1", State: "poweroff", Role: "vm"},
		{Name: "web-a", State: "running", IP: "10.10.1.11", Role: "server", LB: "lb1", LBIP: "192.168.1.60"},
	}
	if !reflect.DeepEqual(vms, want) {
		t.Errorf("ListVMs = %+v, se esperaba %+v", vms, want)
	}
}

// AC-19: una VM asignada a un balanceador no se puede eliminar desde 001.
func TestAC19_DeleteAssignedVMRejected(t *testing.T) {
	runner := &fakeRunner{}
	env := newTestEnv(t, runner)
	ctx := context.Background()
	env.register(t, "lb1")
	env.register(t, "web-a")
	if err := env.lbs.InsertLB(ctx, repository.LoadBalancer{Name: "lb1", Subnet: 1, FrontendPort: 80}); err != nil {
		t.Fatal(err)
	}
	if err := env.lbs.InsertServer(ctx, repository.LBServer{LB: "lb1", Name: "server1", VM: "web-a", Port: 80,
		LastIP: "10.10.1.11", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	_, err := env.svc.DeleteVM(ctx, "web-a")

	if !errors.Is(err, ErrVMIsServer) {
		t.Fatalf("err = %v, se esperaba ErrVMIsServer", err)
	}
	if n := runner.callCount(); n != 0 {
		t.Errorf("el runner se llamó %d veces, se esperaban 0", n)
	}
	if _, err := env.vms.Get(ctx, "web-a"); err != nil {
		t.Errorf("web-a debía seguir registrada: %v", err)
	}
}

// AC-43: eliminar un balanceador desde 001 usa delete-lb.sh y queda en el historial de haproxy.
func TestAC43_DeleteVMOnLBUsesDeleteLB(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{
		"haproxy/delete-lb.sh": {Stdout: []byte("Balanceador lb1 eliminado junto con su red interna\n")},
	}}
	env := newTestEnv(t, runner)
	ctx := context.Background()
	env.register(t, "lb1")
	env.register(t, "web-a")
	if err := env.lbs.InsertLB(ctx, repository.LoadBalancer{Name: "lb1", Subnet: 1, FrontendPort: 80}); err != nil {
		t.Fatal(err)
	}
	if err := env.lbs.InsertServer(ctx, repository.LBServer{LB: "lb1", Name: "server1", VM: "web-a", Port: 80,
		LastIP: "10.10.1.11", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	if _, err := env.svc.DeleteVM(ctx, "lb1"); !errors.Is(err, ErrLBHasServers) {
		t.Fatalf("DeleteVM con un servidor: err = %v, se esperaba ErrLBHasServers", err)
	}
	if n := runner.callCount(); n != 0 {
		t.Errorf("el runner se llamó %d veces, se esperaban 0", n)
	}

	if err := env.lbs.DeleteServer(ctx, "lb1", "web-a"); err != nil {
		t.Fatal(err)
	}
	msg, err := env.svc.DeleteVM(ctx, "lb1")
	if err != nil {
		t.Fatalf("DeleteVM sin servidores: %v", err)
	}
	if msg != "Balanceador lb1 eliminado junto con su red interna" {
		t.Errorf("mensaje = %q", msg)
	}
	calls := runner.callsTo("haproxy/delete-lb.sh")
	if len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, []string{"lb1"}) {
		t.Errorf("llamadas a delete-lb.sh = %+v, se esperaba una con [lb1]", calls)
	}
	if n := len(runner.callsTo("prepare/delete-vm.sh")); n != 0 {
		t.Errorf("delete-vm.sh se llamó %d veces, se esperaban 0", n)
	}
	if _, err := env.vms.Get(ctx, "lb1"); !errors.Is(err, repository.ErrVMNotFound) {
		t.Errorf("lb1 debía borrarse de vms: err = %v", err)
	}
	if _, err := env.lbs.GetLB(ctx, "lb1"); !errors.Is(err, repository.ErrLBNotFound) {
		t.Errorf("lb1 debía borrarse de load_balancers: err = %v", err)
	}
	ops, _ := env.history.ListRecent(ctx, "haproxy", 50)
	if len(ops) != 1 || ops[0].Operation != "delete_lb" {
		t.Errorf("historial de haproxy = %+v, se esperaba una fila delete_lb", ops)
	}
	if ops, _ := env.history.ListRecent(ctx, "prepare", 50); len(ops) != 0 {
		t.Errorf("historial de prepare = %+v, se esperaba vacío", ops)
	}
}

// registerAssigned deja lb1 (subred 1, puerto 80) con web-a como server1.
func (e testEnv) registerAssigned(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	e.register(t, "lb1")
	e.register(t, "web-a")
	if err := e.lbs.InsertLB(ctx, repository.LoadBalancer{Name: "lb1", Subnet: 1, FrontendPort: 80}); err != nil {
		t.Fatal(err)
	}
	if err := e.lbs.InsertServer(ctx, repository.LBServer{LB: "lb1", Name: "server1", VM: "web-a", Port: 80,
		LastIP: "10.10.1.11", Enabled: true}); err != nil {
		t.Fatal(err)
	}
}

// AC-21: tras encender un servidor, StartVM refresca las IPs de su balanceador.
func TestAC21_StartServerTriggersRefresh(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{
		"haproxy/refresh-lb.sh": {Stdout: []byte("server1 10.10.1.15 on\nreloaded=yes\n")},
	}}
	env := newTestEnv(t, runner)
	env.registerAssigned(t)
	ctx := context.Background()

	res, err := env.svc.StartVM(ctx, "web-a")
	if err != nil {
		t.Fatalf("StartVM: %v", err)
	}
	if res.Message != "VM web-a encendida" || res.Warning != "" {
		t.Errorf("resultado = %+v, se esperaba sin advertencia", res)
	}
	if len(runner.calls) != 2 || runner.calls[0].Script != "prepare/start-vm.sh" || runner.calls[1].Script != "haproxy/refresh-lb.sh" {
		t.Fatalf("llamadas = %+v, se esperaba start-vm.sh y luego refresh-lb.sh", runner.calls)
	}
	want := []string{"lb1", "1", "80", "server1,web-a,80,10.10.1.11,on"}
	if got := runner.calls[1].Args; !reflect.DeepEqual(got, want) {
		t.Errorf("argv de refresh-lb.sh = %q, se esperaba %q", got, want)
	}
	if rows, _ := env.lbs.Servers(ctx, "lb1"); rows[0].LastIP != "10.10.1.15" {
		t.Errorf("last_ip = %q, se esperaba 10.10.1.15", rows[0].LastIP)
	}
}

// Encender el balanceador también refresca sus servidores.
func TestStartLBTriggersRefresh(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{
		"haproxy/refresh-lb.sh": {Stdout: []byte("server1 10.10.1.11 on\nreloaded=no\n")},
	}}
	env := newTestEnv(t, runner)
	env.registerAssigned(t)

	res, err := env.svc.StartVM(context.Background(), "lb1")

	if err != nil || res.Warning != "" {
		t.Fatalf("StartVM(lb1) = %+v, %v", res, err)
	}
	calls := runner.callsTo("haproxy/refresh-lb.sh")
	if len(calls) != 1 || calls[0].Args[0] != "lb1" {
		t.Errorf("llamadas a refresh-lb.sh = %+v, se esperaba una para lb1", calls)
	}
}

// AC-41: si el refresco falla, encender la VM sigue siendo un éxito, con una advertencia.
func TestAC41_StartWithFailedRefreshWarns(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{
		"haproxy/refresh-lb.sh": {ExitCode: 12, Stderr: "El balanceador lb1 está apagado; enciéndalo para llegar al servidor\n"},
	}}
	env := newTestEnv(t, runner)
	env.registerAssigned(t)
	ctx := context.Background()

	res, err := env.svc.StartVM(ctx, "web-a")

	if err != nil {
		t.Fatalf("StartVM: %v", err)
	}
	if res.Message != "VM web-a encendida" {
		t.Errorf("Message = %q", res.Message)
	}
	if want := "VM iniciada; no se pudieron actualizar las IPs del balanceador lb1, use Actualizar IPs"; res.Warning != want {
		t.Errorf("Warning = %q, se esperaba %q", res.Warning, want)
	}
	ops, _ := env.history.ListRecent(ctx, "haproxy", 50)
	if len(ops) != 1 || ops[0].Operation != "refresh_lb" || ops[0].Result != "error" || ops[0].ExitCode != 12 {
		t.Errorf("historial de haproxy = %+v, se esperaba una fila refresh_lb con error 12", ops)
	}
}

// Si encender la VM falla no se refresca nada.
func TestStartFailureSkipsRefresh(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{
		"prepare/start-vm.sh": {ExitCode: 4, Stderr: "No se pudo encender la VM\n"},
	}}
	env := newTestEnv(t, runner)
	env.registerAssigned(t)

	if _, err := env.svc.StartVM(context.Background(), "web-a"); err == nil {
		t.Fatal("StartVM: se esperaba un error")
	}
	if n := len(runner.callsTo("haproxy/refresh-lb.sh")); n != 0 {
		t.Errorf("refresh-lb.sh se llamó %d veces, se esperaban 0", n)
	}
}

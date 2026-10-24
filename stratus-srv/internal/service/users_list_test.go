package service

import (
	"bytes"
	"context"
	"errors"
	"log"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"stratus-srv/internal/repository"
)

const listScript = "users/list-users.sh"

// usersEnv agrupa el servicio de usuarios con sus dependencias reales (SQLite temporal).
type usersEnv struct {
	svc     *UsersService
	vms     *repository.VMRepository
	lbs     *repository.LBRepository
	history *repository.HistoryRepository
	logs    *bytes.Buffer
}

// newUsersEnv crea un UsersService con su propio VMLocks. Una prueba de integración con el
// módulo prepare usa newUsersEnvWithLocks para compartir el bloqueo.
func newUsersEnv(t *testing.T, runner ScriptRunner) usersEnv {
	t.Helper()
	return newUsersEnvWithLocks(t, runner, &VMLocks{})
}

func newUsersEnvWithLocks(t *testing.T, runner ScriptRunner, locks *VMLocks) usersEnv {
	t.Helper()
	db, err := repository.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	env := usersEnv{
		vms:     repository.NewVMRepository(db),
		lbs:     repository.NewLBRepository(db),
		history: repository.NewHistoryRepository(db),
		logs:    &bytes.Buffer{},
	}
	env.svc = NewUsersService(UsersConfig{KeyTTL: 10 * time.Minute}, runner, env.vms, env.lbs, env.history,
		locks, log.New(env.logs, "", 0))
	return env
}

// register registra una VM directamente en SQLite, sin pasar por el script.
func (e usersEnv) register(t *testing.T, name string) {
	t.Helper()
	if _, err := e.vms.Insert(context.Background(), name); err != nil {
		t.Fatalf("Insert(%q): %v", name, err)
	}
}

// operations devuelve las filas del historial del módulo users, la más reciente primero.
func (e usersEnv) operations(t *testing.T) []repository.Operation {
	t.Helper()
	ops, err := e.history.ListRecent(context.Background(), "users", 50)
	if err != nil {
		t.Fatalf("ListRecent: %v", err)
	}
	return ops
}

func TestAC1_ListUsersParsesScriptOutput(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{
		listScript: {Stdout: []byte("ana 1001 yes yes\nluis 1002 no no\n")},
	}}
	env := newUsersEnv(t, runner)
	env.register(t, "vm1")

	got, err := env.svc.ListUsers(context.Background(), "vm1")
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}

	want := []UserView{
		{Username: "ana", UID: 1001, Sudo: true, SSHKey: true},
		{Username: "luis", UID: 1002, Sudo: false, SSHKey: false},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("usuarios = %+v, se esperaba %+v", got, want)
	}
	calls := runner.callsTo(listScript)
	if len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, []string{"vm1"}) {
		t.Errorf("llamadas = %+v, se esperaba una con argv [vm1]", calls)
	}
}

func TestAC1_ListUsersEmptyAndMalformedOutput(t *testing.T) {
	tests := []struct {
		name    string
		stdout  string
		wantLen int
		wantErr string
	}{
		{"sin usuarios", "", 0, ""},
		{"campos de menos", "ana 1001 yes\n", 0, "salida inesperada de list-users.sh"},
		{"uid no numérico", "ana x yes yes\n", 0, "salida inesperada de list-users.sh"},
		{"bandera inválida", "ana 1001 si yes\n", 0, "salida inesperada de list-users.sh"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeRunner{results: map[string]RunResult{listScript: {Stdout: []byte(tt.stdout)}}}
			env := newUsersEnv(t, runner)
			env.register(t, "vm1")

			got, err := env.svc.ListUsers(context.Background(), "vm1")

			if tt.wantErr == "" {
				if err != nil || got == nil || len(got) != tt.wantLen {
					t.Fatalf("got %v, err %v; se esperaba una lista vacía no nula", got, err)
				}
				return
			}
			if err == nil || err.Error() != tt.wantErr {
				t.Errorf("err = %v, se esperaba %q", err, tt.wantErr)
			}
		})
	}
}

func TestAC3_ListUsersMapsNoIP(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{
		listScript: {ExitCode: 7, Stderr: "La VM no tiene dirección IP; verifique que esté encendida\n"},
	}}
	env := newUsersEnv(t, runner)
	env.register(t, "vm1")

	_, err := env.svc.ListUsers(context.Background(), "vm1")

	if !errors.Is(err, ErrVMNoIP) {
		t.Fatalf("err = %v, se esperaba ErrVMNoIP", err)
	}
	ops := env.operations(t)
	if len(ops) != 1 || ops[0].Operation != "list_users" || ops[0].ExitCode != 7 {
		t.Errorf("historial = %+v, se esperaba una fila list_users con código 7", ops)
	}
}

func TestAC30_SuccessfulListsAreNotRecorded(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{
		listScript: {Stdout: []byte("ana 1001 yes yes\n")},
	}}
	env := newUsersEnv(t, runner)
	env.register(t, "vm1")

	for range 20 {
		if _, err := env.svc.ListUsers(context.Background(), "vm1"); err != nil {
			t.Fatalf("ListUsers: %v", err)
		}
	}
	if ops := env.operations(t); len(ops) != 0 {
		t.Fatalf("filas tras 20 listados correctos = %d, se esperaban 0", len(ops))
	}

	// Un fallo sí se registra, con su código y su stderr.
	runner.mu.Lock()
	runner.results[listScript] = RunResult{ExitCode: 4, Stderr: "No se pudo conectar por SSH a 192.168.1.50\n"}
	runner.mu.Unlock()
	var opErr *OperationError
	if _, err := env.svc.ListUsers(context.Background(), "vm1"); !errors.As(err, &opErr) {
		t.Fatalf("err = %v, se esperaba *OperationError", err)
	}
	ops := env.operations(t)
	if len(ops) != 1 || ops[0].ExitCode != 4 || ops[0].Stderr != "No se pudo conectar por SSH a 192.168.1.50" {
		t.Errorf("historial = %+v, se esperaba una fila con código 4 y su stderr", ops)
	}
}

// registerServer deja lb1 (subred 1) con web-a asignada como server1.
func (e usersEnv) registerServer(t *testing.T) {
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

// AC-27, AC-29: en un servidor asignado, el listado lleva --jump <lb> antes de la VM; en un
// balanceador o una VM común, no.
func TestAC29_ListUsersOnServerPassesJump(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{listScript: {Stdout: []byte("ana 1001 yes yes\n")}}}
	env := newUsersEnv(t, runner)
	env.registerServer(t)
	env.register(t, "vm1")

	for _, vm := range []string{"web-a", "lb1", "vm1"} {
		if _, err := env.svc.ListUsers(context.Background(), vm); err != nil {
			t.Fatalf("ListUsers(%s): %v", vm, err)
		}
	}

	calls := runner.callsTo(listScript)
	want := [][]string{{"--jump", "lb1", "web-a"}, {"lb1"}, {"vm1"}}
	for i, w := range want {
		if !reflect.DeepEqual(calls[i].Args, w) {
			t.Errorf("llamada %d: argv = %q, se esperaba %q", i, calls[i].Args, w)
		}
	}
}

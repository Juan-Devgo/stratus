package service

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"stratus-srv/internal/repository"
)

const assignStdout = "Servidor server1 (web-a) asignado a lb1\nip=10.10.1.11\n"

// newAssignEnv deja lb1 (subred 1, puerto 80) y la VM común web-a registrados, con el script de
// asignación respondiendo bien.
func newAssignEnv(t *testing.T) (haproxyEnv, *fakeRunner) {
	t.Helper()
	runner := &fakeRunner{results: map[string]RunResult{
		"haproxy/assign-server.sh": {Stdout: []byte(assignStdout)},
		"haproxy/remove-server.sh": {Stdout: []byte("Servidor web-a quitado de lb1\n")},
	}}
	env := newHAProxyEnv(t, runner)
	env.registerLB(t, "lb1", 1, 80)
	env.register(t, "web-a")
	return env, runner
}

// AC-13: AssignServer pasa los argumentos en el orden de assign-server.sh y guarda la fila.
func TestAC13_AssignServerArgsAndRow(t *testing.T) {
	env, runner := newAssignEnv(t)
	ctx := context.Background()

	msg, err := env.svc.AssignServer(ctx, "lb1", "web-a", 80)
	if err != nil {
		t.Fatalf("AssignServer: %v", err)
	}
	if msg != "Servidor server1 (web-a) asignado a lb1" {
		t.Errorf("mensaje = %q", msg)
	}
	calls := runner.callsTo("haproxy/assign-server.sh")
	if len(calls) != 1 {
		t.Fatalf("assign-server.sh se llamó %d veces, se esperaba 1", len(calls))
	}
	want := []string{"lb1", "1", "web-a", "server1", "80", "80", testBridge}
	if !reflect.DeepEqual(calls[0].Args, want) {
		t.Errorf("argv = %q, se esperaba %q", calls[0].Args, want)
	}
	rows, err := env.lbs.Servers(ctx, "lb1")
	if err != nil {
		t.Fatal(err)
	}
	wantRows := []repository.LBServer{{LB: "lb1", Name: "server1", VM: "web-a", Port: 80, LastIP: "10.10.1.11", Enabled: true}}
	if !reflect.DeepEqual(rows, wantRows) {
		t.Errorf("lb_servers = %+v, se esperaba %+v", rows, wantRows)
	}
}

// AC-13, AC-14: con servidores previos usa el nombre libre más bajo, el puerto del frontend guardado
// y las specs existentes al final del argv.
func TestAC13_AssignServerWithExistingServers(t *testing.T) {
	env, runner := newAssignEnv(t)
	ctx := context.Background()
	if err := env.lbs.SetFrontendPort(ctx, "lb1", 8080); err != nil {
		t.Fatal(err)
	}
	env.registerServer(t, "lb1", "server1", "web-b", 9000, "10.10.1.12")
	if err := env.lbs.UpdateServerIPs(ctx, "lb1", []repository.LBServer{{Name: "server1", LastIP: "10.10.1.12", Enabled: false}}); err != nil {
		t.Fatal(err)
	}
	runner.results["haproxy/assign-server.sh"] = RunResult{Stdout: []byte("Servidor server2 (web-a) asignado a lb1\nip=10.10.1.15\n")}

	if _, err := env.svc.AssignServer(ctx, "lb1", "web-a", 80); err != nil {
		t.Fatalf("AssignServer: %v", err)
	}

	want := []string{"lb1", "1", "web-a", "server2", "80", "8080", testBridge, "server1,web-b,9000,10.10.1.12,off"}
	if got := runner.callsTo("haproxy/assign-server.sh")[0].Args; !reflect.DeepEqual(got, want) {
		t.Errorf("argv = %q, se esperaba %q", got, want)
	}
	rows, _ := env.lbs.Servers(ctx, "lb1")
	if len(rows) != 2 || rows[1].Name != "server2" || rows[1].LastIP != "10.10.1.15" {
		t.Errorf("lb_servers = %+v", rows)
	}
}

// AC-15: los rechazos no llaman al script ni cambian nada.
func TestAC15_AssignRejectionsChangeNothing(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, env haproxyEnv)
		vm    string
		want  error
	}{
		{"la VM es un balanceador", func(t *testing.T, env haproxyEnv) { env.registerLB(t, "lb2", 2, 80) }, "lb2", ErrVMNotAssignable},
		{"la VM es servidor de otro balanceador", func(t *testing.T, env haproxyEnv) {
			env.registerLB(t, "lb2", 2, 80)
			env.registerServer(t, "lb2", "server1", "web-z", 80, "10.10.2.11")
		}, "web-z", ErrVMNotAssignable},
		{"el balanceador ya tiene 10 servidores", func(t *testing.T, env haproxyEnv) {
			for i := 1; i <= 10; i++ {
				env.registerServer(t, "lb1", fmt.Sprintf("server%d", i), fmt.Sprintf("vm-%d", i), 80, fmt.Sprintf("10.10.1.%d", 10+i))
			}
		}, "web-a", ErrLBFull},
		{"la VM no existe", func(*testing.T, haproxyEnv) {}, "web-q", ErrVMNotFound},
		{"nombre de VM inválido", func(*testing.T, haproxyEnv) {}, "WEB;a", ErrInvalidInput},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env, runner := newAssignEnv(t)
			tt.setup(t, env)
			before, _ := env.lbs.Servers(context.Background(), "lb1")

			_, err := env.svc.AssignServer(context.Background(), "lb1", tt.vm, 80)

			if !errors.Is(err, tt.want) {
				t.Errorf("err = %v, se esperaba %v", err, tt.want)
			}
			if n := runner.callCount(); n != 0 {
				t.Errorf("el runner se llamó %d veces, se esperaban 0", n)
			}
			after, _ := env.lbs.Servers(context.Background(), "lb1")
			if len(after) != len(before) {
				t.Errorf("lb_servers cambió: %d -> %d filas", len(before), len(after))
			}
			if n := env.haproxyRows(t); n != 0 {
				t.Errorf("el rechazo dejó %d filas en el historial", n)
			}
		})
	}
}

func TestAssignServerUnknownLB(t *testing.T) {
	env, runner := newAssignEnv(t)

	_, err := env.svc.AssignServer(context.Background(), "nada", "web-a", 80)

	if !errors.Is(err, ErrLBNotFound) {
		t.Errorf("err = %v, se esperaba ErrLBNotFound", err)
	}
	if n := runner.callCount(); n != 0 {
		t.Errorf("el runner se llamó %d veces, se esperaban 0", n)
	}
}

// AC-16: puertos fuera de 1-65535 no llegan al script.
func TestAC16_AssignServerInvalidPort(t *testing.T) {
	for _, port := range []int{0, -1, 65536} {
		t.Run(fmt.Sprint(port), func(t *testing.T) {
			env, runner := newAssignEnv(t)

			_, err := env.svc.AssignServer(context.Background(), "lb1", "web-a", port)

			if !errors.Is(err, ErrInvalidInput) {
				t.Errorf("err = %v, se esperaba ErrInvalidInput", err)
			}
			if n := runner.callCount(); n != 0 {
				t.Errorf("el runner se llamó %d veces, se esperaban 0", n)
			}
		})
	}
}

// AC-17: si el script falla no se guarda ninguna fila y el historial lo registra.
func TestAC17_AssignFailureStoresNothing(t *testing.T) {
	env, runner := newAssignEnv(t)
	runner.results["haproxy/assign-server.sh"] = RunResult{ExitCode: 14,
		Stderr: "El servidor web-a no obtuvo una IPv4 interna en 120 segundos\n"}

	_, err := env.svc.AssignServer(context.Background(), "lb1", "web-a", 80)

	var opErr *OperationError
	if !errors.As(err, &opErr) || opErr.ExitCode != 14 || !strings.Contains(err.Error(), "120 segundos") {
		t.Fatalf("err = %v, se esperaba *OperationError con código 14", err)
	}
	if rows, _ := env.lbs.Servers(context.Background(), "lb1"); len(rows) != 0 {
		t.Errorf("lb_servers = %+v, se esperaba vacío", rows)
	}
	if op := env.lastOperation(t); op.Operation != "assign_server" || op.Result != "error" || op.ExitCode != 14 {
		t.Errorf("historial = %+v, se esperaba assign_server error con código 14", op)
	}
}

// AC-24, AC-42: los códigos propios del módulo se traducen y no dejan filas.
func TestAssignServerMapsScriptCodes(t *testing.T) {
	tests := []struct {
		code int
		want error
	}{
		{10, ErrHAProxyConfigInvalid},
		{11, ErrHAProxyReloadFailed},
		{15, ErrJumpUpdateFailed},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.code), func(t *testing.T) {
			env, runner := newAssignEnv(t)
			runner.results["haproxy/assign-server.sh"] = RunResult{ExitCode: tt.code, Stderr: "falló\n"}

			_, err := env.svc.AssignServer(context.Background(), "lb1", "web-a", 80)

			if !errors.Is(err, tt.want) {
				t.Errorf("err = %v, se esperaba %v", err, tt.want)
			}
			if rows, _ := env.lbs.Servers(context.Background(), "lb1"); len(rows) != 0 {
				t.Errorf("lb_servers = %+v, se esperaba vacío", rows)
			}
		})
	}
}

// AC-48: asignar de nuevo una VM que ya es servidor del mismo balanceador se rechaza.
func TestAC48_ReassignSameLBRejected(t *testing.T) {
	env, runner := newAssignEnv(t)
	env.registerServer(t, "lb1", "server1", "web-b", 80, "10.10.1.12")

	_, err := env.svc.AssignServer(context.Background(), "lb1", "web-b", 80)

	if !errors.Is(err, ErrVMNotAssignable) {
		t.Errorf("err = %v, se esperaba ErrVMNotAssignable", err)
	}
	if n := runner.callCount(); n != 0 {
		t.Errorf("el runner se llamó %d veces, se esperaban 0", n)
	}
}

// AC-51: la fila del historial de asignar guarda el balanceador como VM y la VM y el puerto en los argumentos.
func TestAC51_AssignHistoryRow(t *testing.T) {
	env, _ := newAssignEnv(t)

	if _, err := env.svc.AssignServer(context.Background(), "lb1", "web-a", 80); err != nil {
		t.Fatal(err)
	}

	op := env.lastOperation(t)
	if op.Operation != "assign_server" || op.VM != "lb1" || op.Command != "haproxy/assign-server.sh" || op.Result != "ok" {
		t.Errorf("fila = %+v", op)
	}
	if joined := strings.Join(op.Args, " "); !strings.Contains(joined, "web-a") || !strings.Contains(joined, "80") {
		t.Errorf("args = %q, se esperaba que incluyera web-a y 80", op.Args)
	}
}

// AC-18: RemoveServer pasa los servidores que quedan y borra solo la fila quitada.
func TestAC18_RemoveServerDeletesRow(t *testing.T) {
	env, runner := newAssignEnv(t)
	ctx := context.Background()
	env.registerServer(t, "lb1", "server1", "web-b", 8080, "10.10.1.12")
	env.registerServer(t, "lb1", "server2", "vm-c", 80, "10.10.1.13")
	if err := env.lbs.DeleteServer(ctx, "lb1", "vm-c"); err != nil {
		t.Fatal(err)
	}
	if err := env.lbs.InsertServer(ctx, repository.LBServer{LB: "lb1", Name: "server2", VM: "web-a", Port: 80,
		LastIP: "10.10.1.11", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	msg, err := env.svc.RemoveServer(ctx, "lb1", "web-a")
	if err != nil {
		t.Fatalf("RemoveServer: %v", err)
	}
	if msg != "Servidor web-a quitado de lb1" {
		t.Errorf("mensaje = %q", msg)
	}
	want := []string{"lb1", "web-a", "80", testBridge, "server1,web-b,8080,10.10.1.12,on"}
	if got := runner.callsTo("haproxy/remove-server.sh")[0].Args; !reflect.DeepEqual(got, want) {
		t.Errorf("argv = %q, se esperaba %q", got, want)
	}
	rows, _ := env.lbs.Servers(ctx, "lb1")
	if len(rows) != 1 || rows[0].VM != "web-b" {
		t.Errorf("lb_servers = %+v, se esperaba solo web-b", rows)
	}
	if op := env.lastOperation(t); op.Operation != "remove_server" || op.VM != "lb1" {
		t.Errorf("historial = %+v", op)
	}
}

// AC-40: con el balanceador apagado el script sale con 12 y la fila se conserva.
func TestAC40_RemoveServerLBOffKeepsRow(t *testing.T) {
	env, runner := newAssignEnv(t)
	env.registerServer(t, "lb1", "server1", "web-b", 80, "10.10.1.12")
	runner.results["haproxy/remove-server.sh"] = RunResult{ExitCode: 12,
		Stderr: "El balanceador lb1 está apagado; enciéndalo para llegar al servidor\n"}

	_, err := env.svc.RemoveServer(context.Background(), "lb1", "web-b")

	var off *LBOffError
	if !errors.As(err, &off) || !errors.Is(err, ErrLBOff) {
		t.Fatalf("err = %v, se esperaba *LBOffError", err)
	}
	if rows, _ := env.lbs.Servers(context.Background(), "lb1"); len(rows) != 1 {
		t.Errorf("lb_servers = %+v, la fila debía quedar", rows)
	}
}

func TestRemoveServerRejections(t *testing.T) {
	env, runner := newAssignEnv(t)
	env.registerLB(t, "lb2", 2, 80)
	env.registerServer(t, "lb2", "server1", "web-z", 80, "10.10.2.11")
	ctx := context.Background()

	// web-a no es servidor de ningún balanceador; web-z lo es de lb2, no de lb1.
	for _, vm := range []string{"web-a", "web-z"} {
		if _, err := env.svc.RemoveServer(ctx, "lb1", vm); !errors.Is(err, ErrVMNotFound) {
			t.Errorf("RemoveServer(lb1, %s): err = %v, se esperaba ErrVMNotFound", vm, err)
		}
	}
	if _, err := env.svc.RemoveServer(ctx, "nada", "web-a"); !errors.Is(err, ErrLBNotFound) {
		t.Errorf("balanceador inexistente: err = %v", err)
	}
	if _, err := env.svc.RemoveServer(ctx, "lb1", "WEB;a"); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("nombre inválido: err = %v", err)
	}
	if n := runner.callCount(); n != 0 {
		t.Errorf("el runner se llamó %d veces, se esperaban 0", n)
	}
}

// AC-46: la lista de servidores sale de SQLite, sin llamar a ningún script, con el estado "-".
func TestAC46_ListServersFromSQLite(t *testing.T) {
	env, runner := newAssignEnv(t)
	env.registerServer(t, "lb1", "server2", "web-b", 8080, "10.10.1.12")
	env.registerServer(t, "lb1", "server1", "vm-c", 80, "10.10.1.13")

	servers, err := env.svc.ListServers(context.Background(), "lb1")
	if err != nil {
		t.Fatalf("ListServers: %v", err)
	}

	want := []ServerView{
		{Name: "server1", VM: "vm-c", IP: "10.10.1.13", Port: 80, State: "-"},
		{Name: "server2", VM: "web-b", IP: "10.10.1.12", Port: 8080, State: "-"},
	}
	if !reflect.DeepEqual(servers, want) {
		t.Errorf("ListServers = %+v, se esperaba %+v", servers, want)
	}
	if n := runner.callCount(); n != 0 {
		t.Errorf("el runner se llamó %d veces, se esperaban 0", n)
	}
	if _, err := env.svc.ListServers(context.Background(), "nada"); !errors.Is(err, ErrLBNotFound) {
		t.Errorf("balanceador inexistente: err = %v", err)
	}
}

// AC-25: ServerStatus convierte la salida de lb-status.sh en un mapa servidor -> estado y, al
// terminar bien, no deja fila en el historial.
func TestAC25_ServerStatusParses(t *testing.T) {
	env, runner := newAssignEnv(t)
	runner.results["haproxy/lb-status.sh"] = RunResult{Stdout: []byte("server1 UP\nserver2 DOWN\n")}

	states, err := env.svc.ServerStatus(context.Background(), "lb1")
	if err != nil {
		t.Fatalf("ServerStatus: %v", err)
	}

	want := map[string]string{"server1": "UP", "server2": "DOWN"}
	if !reflect.DeepEqual(states, want) {
		t.Errorf("ServerStatus = %v, se esperaba %v", states, want)
	}
	calls := runner.callsTo("haproxy/lb-status.sh")
	if len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, []string{"lb1"}) {
		t.Errorf("llamadas = %+v, se esperaba una con [lb1]", calls)
	}
	if n := env.haproxyRows(t); n != 0 {
		t.Errorf("la lectura exitosa dejó %d filas en el historial, se esperaban 0", n)
	}
}

// AC-25: sin servidores el mapa queda vacío (no nil), y un balanceador inexistente no llama al script.
func TestServerStatusEmptyAndUnknown(t *testing.T) {
	env, runner := newAssignEnv(t)
	runner.results["haproxy/lb-status.sh"] = RunResult{}

	states, err := env.svc.ServerStatus(context.Background(), "lb1")
	if err != nil || states == nil || len(states) != 0 {
		t.Errorf("ServerStatus = %#v, %v; se esperaba un mapa vacío", states, err)
	}
	before := runner.callCount()
	if _, err := env.svc.ServerStatus(context.Background(), "nada"); !errors.Is(err, ErrLBNotFound) {
		t.Errorf("balanceador inexistente: err = %v", err)
	}
	if runner.callCount() != before {
		t.Error("un balanceador inexistente no debe llamar al script")
	}
}

// REQ-21: la lectura toma el bloqueo del balanceador.
func TestServerStatusTakesLBLock(t *testing.T) {
	env, runner := newAssignEnv(t)
	env.locks.TryLock("lb1")

	_, err := env.svc.ServerStatus(context.Background(), "lb1")

	if !errors.Is(err, ErrOperationInProgress) {
		t.Errorf("err = %v, se esperaba ErrOperationInProgress", err)
	}
	if n := runner.callCount(); n != 0 {
		t.Errorf("el runner se llamó %d veces, se esperaban 0", n)
	}
}

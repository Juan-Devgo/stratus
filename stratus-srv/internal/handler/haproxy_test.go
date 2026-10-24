package handler

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"stratus-srv/internal/repository"
	"stratus-srv/internal/service"
)

// AC-1: POST /api/haproxy/lbs crea el balanceador y responde 201 con el mensaje del script.
func TestAC1_CreateLBRoute(t *testing.T) {
	const line = "Balanceador lb1 creado y encendido; IP 192.168.1.60; subred 10.10.1.0/24"
	runner := &fakeRunner{results: map[string]service.RunResult{
		"haproxy/create-lb.sh": {Stdout: []byte(line + "\nsubnet=1\n")},
	}}
	ts := newTestServerWithLBs(t, runner)

	status, body := do(t, http.MethodPost, ts.URL+"/api/haproxy/lbs", `{"name":"lb1","memory_mb":1024,"cpus":1}`)

	if status != http.StatusCreated {
		t.Errorf("status = %d, se esperaba 201", status)
	}
	if msg, _ := body["message"].(string); msg != line {
		t.Errorf("message = %q, se esperaba %q", msg, line)
	}
	if lb, err := ts.lbs.GetLB(context.Background(), "lb1"); err != nil || lb.Subnet != 1 || lb.FrontendPort != 80 {
		t.Errorf("load_balancers = %+v, %v", lb, err)
	}
}

func TestCreateLBRouteMapsErrors(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		result     service.RunResult
		wantStatus int
		wantError  string
	}{
		{"nombre inválido", `{"name":"LB_1","memory_mb":1024,"cpus":1}`, service.RunResult{},
			http.StatusBadRequest, "El nombre de la VM debe tener de 1 a 32 caracteres: minúsculas, números y guiones, sin empezar con guion"},
		{"JSON inválido", `{`, service.RunResult{}, http.StatusBadRequest, "El cuerpo de la petición no es JSON válido"},
		{"red interna registrada", `{"name":"lb1","memory_mb":1024,"cpus":1}`, service.RunResult{ExitCode: 13},
			http.StatusConflict, "Red interna ya registrada en VirtualBox"},
		{"sin subredes", `{"name":"lb1","memory_mb":1024,"cpus":1}`, service.RunResult{ExitCode: 8},
			http.StatusConflict, "No hay subredes internas libres"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeRunner{results: map[string]service.RunResult{"haproxy/create-lb.sh": tt.result}}
			ts := newTestServerWithLBs(t, runner)

			status, body := do(t, http.MethodPost, ts.URL+"/api/haproxy/lbs", tt.body)

			if status != tt.wantStatus {
				t.Errorf("status = %d, se esperaba %d", status, tt.wantStatus)
			}
			if msg, _ := body["error"].(string); msg != tt.wantError {
				t.Errorf("error = %q, se esperaba %q", msg, tt.wantError)
			}
		})
	}
}

// GET /api/haproxy/lbs devuelve los balanceadores con su estado y list_error.
func TestListLBsRoute(t *testing.T) {
	runner := &fakeRunner{results: map[string]service.RunResult{
		"prepare/list-vms.sh": {Stdout: []byte("lb1 running 192.168.1.60\nweb-a running 10.10.1.11\n")},
	}}
	ts := newTestServerWithLBs(t, runner)
	ctx := context.Background()
	reserve(t, ts.vms, "lb1")
	reserve(t, ts.vms, "web-a")
	if err := ts.lbs.InsertLB(ctx, repository.LoadBalancer{Name: "lb1", Subnet: 1, FrontendPort: 8080}); err != nil {
		t.Fatal(err)
	}
	if err := ts.lbs.InsertServer(ctx, repository.LBServer{LB: "lb1", Name: "server1", VM: "web-a", Port: 80,
		LastIP: "10.10.1.11", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	status, body := do(t, http.MethodGet, ts.URL+"/api/haproxy/lbs", "")

	if status != http.StatusOK {
		t.Fatalf("status = %d, se esperaba 200", status)
	}
	if listErr, ok := body["list_error"].(string); !ok || listErr != "" {
		t.Errorf("list_error = %v, se esperaba una cadena vacía", body["list_error"])
	}
	list, _ := body["lbs"].([]any)
	if len(list) != 1 {
		t.Fatalf("lbs = %v, se esperaba 1", body["lbs"])
	}
	lb, _ := list[0].(map[string]any)
	want := map[string]any{"name": "lb1", "state": "running", "ip": "192.168.1.60",
		"subnet": "10.10.1.0/24", "frontend_port": float64(8080), "server_count": float64(1)}
	for k, v := range want {
		if lb[k] != v {
			t.Errorf("lbs[0][%q] = %v, se esperaba %v", k, lb[k], v)
		}
	}
}

func TestListLBsRouteWithListError(t *testing.T) {
	runner := &fakeRunner{results: map[string]service.RunResult{
		"prepare/list-vms.sh": {ExitCode: 4, Stderr: "VBoxManage list vms falló\n"},
	}}
	ts := newTestServerWithLBs(t, runner)
	reserve(t, ts.vms, "lb1")
	if err := ts.lbs.InsertLB(context.Background(), repository.LoadBalancer{Name: "lb1", Subnet: 1, FrontendPort: 80}); err != nil {
		t.Fatal(err)
	}

	status, body := do(t, http.MethodGet, ts.URL+"/api/haproxy/lbs", "")

	if status != http.StatusOK {
		t.Fatalf("status = %d, se esperaba 200", status)
	}
	if listErr, _ := body["list_error"].(string); listErr == "" {
		t.Error("list_error vacío, se esperaba el motivo")
	}
	list, _ := body["lbs"].([]any)
	if lb, _ := list[0].(map[string]any); lb["state"] != "unknown" {
		t.Errorf("state = %v, se esperaba unknown", lb["state"])
	}
}

// AC-10: DELETE responde 200 sin servidores y 409 con ellos.
func TestAC10_DeleteLBRoute(t *testing.T) {
	runner := &fakeRunner{results: map[string]service.RunResult{
		"haproxy/delete-lb.sh": {Stdout: []byte("Balanceador lb1 eliminado junto con su red interna\n")},
	}}
	ts := newTestServerWithLBs(t, runner)
	ctx := context.Background()
	reserve(t, ts.vms, "lb1")
	reserve(t, ts.vms, "web-a")
	if err := ts.lbs.InsertLB(ctx, repository.LoadBalancer{Name: "lb1", Subnet: 1, FrontendPort: 80}); err != nil {
		t.Fatal(err)
	}
	if err := ts.lbs.InsertServer(ctx, repository.LBServer{LB: "lb1", Name: "server1", VM: "web-a", Port: 80,
		LastIP: "10.10.1.11", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	status, body := do(t, http.MethodDelete, ts.URL+"/api/haproxy/lbs/lb1", "")
	if status != http.StatusConflict {
		t.Errorf("con servidores: status = %d, se esperaba 409", status)
	}
	if msg, _ := body["error"].(string); msg != "Quite primero los servidores del balanceador" {
		t.Errorf("error = %q", msg)
	}
	if n := runner.callCount(); n != 0 {
		t.Errorf("el runner se llamó %d veces, se esperaban 0", n)
	}

	if err := ts.lbs.DeleteServer(ctx, "lb1", "web-a"); err != nil {
		t.Fatal(err)
	}
	status, body = do(t, http.MethodDelete, ts.URL+"/api/haproxy/lbs/lb1", "")
	if status != http.StatusOK {
		t.Errorf("sin servidores: status = %d, se esperaba 200", status)
	}
	if msg, _ := body["message"].(string); msg != "Balanceador lb1 eliminado junto con su red interna" {
		t.Errorf("message = %q", msg)
	}

	status, body = do(t, http.MethodDelete, ts.URL+"/api/haproxy/lbs/nada", "")
	if status != http.StatusNotFound {
		t.Errorf("inexistente: status = %d, se esperaba 404", status)
	}
	if msg, _ := body["error"].(string); msg != "El balanceador no existe" {
		t.Errorf("error = %q", msg)
	}
}

// newLBServer deja lb1 (subred 1) con una VM común web-a registrada.
func newLBServer(t *testing.T, results map[string]service.RunResult) (testServer, *fakeRunner) {
	t.Helper()
	runner := &fakeRunner{results: results}
	ts := newTestServerWithLBs(t, runner)
	reserve(t, ts.vms, "lb1")
	reserve(t, ts.vms, "web-a")
	if err := ts.lbs.InsertLB(context.Background(), repository.LoadBalancer{Name: "lb1", Subnet: 1, FrontendPort: 80}); err != nil {
		t.Fatal(err)
	}
	return ts, runner
}

// AC-13: POST .../servers asigna la VM y responde 201 con el mensaje del script.
func TestAC13_AssignServerRoute(t *testing.T) {
	ts, runner := newLBServer(t, map[string]service.RunResult{
		"haproxy/assign-server.sh": {Stdout: []byte("Servidor server1 (web-a) asignado a lb1\nip=10.10.1.11\n")},
	})

	status, body := do(t, http.MethodPost, ts.URL+"/api/haproxy/lbs/lb1/servers", `{"vm":"web-a","port":80}`)

	if status != http.StatusCreated {
		t.Errorf("status = %d, se esperaba 201", status)
	}
	if msg, _ := body["message"].(string); msg != "Servidor server1 (web-a) asignado a lb1" {
		t.Errorf("message = %q", msg)
	}
	if n := runner.callCount(); n != 1 {
		t.Errorf("el runner se llamó %d veces, se esperaba 1", n)
	}
	if rows, _ := ts.lbs.Servers(context.Background(), "lb1"); len(rows) != 1 || rows[0].LastIP != "10.10.1.11" {
		t.Errorf("lb_servers = %+v", rows)
	}
}

// AC-15, AC-16: los rechazos de asignar se traducen a su código y texto.
func TestAC15_HandlerMapsAssignErrors(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(t *testing.T, ts testServer)
		body       string
		wantStatus int
		wantError  string
	}{
		{"la VM es un balanceador", func(t *testing.T, ts testServer) {
			reserve(t, ts.vms, "lb2")
			if err := ts.lbs.InsertLB(context.Background(), repository.LoadBalancer{Name: "lb2", Subnet: 2, FrontendPort: 80}); err != nil {
				t.Fatal(err)
			}
		}, `{"vm":"lb2","port":80}`, http.StatusConflict, "La VM no se puede asignar a este balanceador"},
		{"la VM ya es servidor", func(t *testing.T, ts testServer) {
			if err := ts.lbs.InsertServer(context.Background(), repository.LBServer{LB: "lb1", Name: "server1", VM: "web-a",
				Port: 80, LastIP: "10.10.1.11", Enabled: true}); err != nil {
				t.Fatal(err)
			}
		}, `{"vm":"web-a","port":80}`, http.StatusConflict, "La VM no se puede asignar a este balanceador"},
		{"10 servidores", func(t *testing.T, ts testServer) {
			for i := 1; i <= 10; i++ {
				vm := fmt.Sprintf("vm-%d", i)
				reserve(t, ts.vms, vm)
				if err := ts.lbs.InsertServer(context.Background(), repository.LBServer{LB: "lb1", Name: fmt.Sprintf("server%d", i),
					VM: vm, Port: 80, LastIP: fmt.Sprintf("10.10.1.%d", 10+i), Enabled: true}); err != nil {
					t.Fatal(err)
				}
			}
		}, `{"vm":"web-a","port":80}`, http.StatusConflict, "El balanceador ya tiene 10 servidores"},
		{"puerto inválido", func(*testing.T, testServer) {}, `{"vm":"web-a","port":0}`, http.StatusBadRequest,
			"El puerto debe ser un número entre 1 y 65535"},
		{"VM inexistente", func(*testing.T, testServer) {}, `{"vm":"web-q","port":80}`, http.StatusNotFound, "La VM no existe"},
		{"JSON inválido", func(*testing.T, testServer) {}, `{`, http.StatusBadRequest, "El cuerpo de la petición no es JSON válido"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts, runner := newLBServer(t, nil)
			tt.setup(t, ts)

			status, body := do(t, http.MethodPost, ts.URL+"/api/haproxy/lbs/lb1/servers", tt.body)

			if status != tt.wantStatus {
				t.Errorf("status = %d, se esperaba %d", status, tt.wantStatus)
			}
			if msg, _ := body["error"].(string); msg != tt.wantError {
				t.Errorf("error = %q, se esperaba %q", msg, tt.wantError)
			}
			if n := runner.callCount(); n != 0 {
				t.Errorf("el runner se llamó %d veces, se esperaban 0", n)
			}
		})
	}
}

// AC-46: GET .../servers devuelve lo guardado en SQLite con el estado "-".
func TestAC46_ListServersRoute(t *testing.T) {
	ts, runner := newLBServer(t, nil)
	if err := ts.lbs.InsertServer(context.Background(), repository.LBServer{LB: "lb1", Name: "server1", VM: "web-a",
		Port: 8080, LastIP: "10.10.1.11", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	status, body := do(t, http.MethodGet, ts.URL+"/api/haproxy/lbs/lb1/servers", "")

	if status != http.StatusOK {
		t.Fatalf("status = %d, se esperaba 200", status)
	}
	list, _ := body["servers"].([]any)
	if len(list) != 1 {
		t.Fatalf("servers = %v, se esperaba 1", body["servers"])
	}
	row, _ := list[0].(map[string]any)
	want := map[string]any{"name": "server1", "vm": "web-a", "ip": "10.10.1.11", "port": float64(8080), "state": "-"}
	for k, v := range want {
		if row[k] != v {
			t.Errorf("servers[0][%q] = %v, se esperaba %v", k, row[k], v)
		}
	}
	if n := runner.callCount(); n != 0 {
		t.Errorf("el runner se llamó %d veces, se esperaban 0", n)
	}
}

// Sin servidores la lista es un arreglo vacío, no null: la UI la recorre sin comprobar nada.
func TestListServersRouteEmpty(t *testing.T) {
	ts, _ := newLBServer(t, nil)

	status, body := do(t, http.MethodGet, ts.URL+"/api/haproxy/lbs/lb1/servers", "")

	list, ok := body["servers"].([]any)
	if status != http.StatusOK || !ok || len(list) != 0 {
		t.Errorf("status = %d, servers = %#v; se esperaba 200 y []", status, body["servers"])
	}
	if status, _ := do(t, http.MethodGet, ts.URL+"/api/haproxy/lbs/nada/servers", ""); status != http.StatusNotFound {
		t.Errorf("balanceador inexistente: status = %d, se esperaba 404", status)
	}
}

// AC-18, AC-40: DELETE .../servers/{vm} responde 200, o 409 con el texto del balanceador apagado.
func TestAC18_AC40_RemoveServerRoute(t *testing.T) {
	ts, runner := newLBServer(t, map[string]service.RunResult{
		"haproxy/remove-server.sh": {Stdout: []byte("Servidor web-a quitado de lb1\n")},
	})
	if err := ts.lbs.InsertServer(context.Background(), repository.LBServer{LB: "lb1", Name: "server1", VM: "web-a",
		Port: 80, LastIP: "10.10.1.11", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	status, body := do(t, http.MethodDelete, ts.URL+"/api/haproxy/lbs/lb1/servers/web-a", "")

	if status != http.StatusOK {
		t.Errorf("status = %d, se esperaba 200", status)
	}
	if msg, _ := body["message"].(string); msg != "Servidor web-a quitado de lb1" {
		t.Errorf("message = %q", msg)
	}
	if rows, _ := ts.lbs.Servers(context.Background(), "lb1"); len(rows) != 0 {
		t.Errorf("lb_servers = %+v, se esperaba vacío", rows)
	}
	if n := runner.callCount(); n != 1 {
		t.Errorf("el runner se llamó %d veces, se esperaba 1", n)
	}

	// Un VM que ya no es servidor: 404.
	status, _ = do(t, http.MethodDelete, ts.URL+"/api/haproxy/lbs/lb1/servers/web-a", "")
	if status != http.StatusNotFound {
		t.Errorf("segunda baja: status = %d, se esperaba 404", status)
	}
}

func TestAC40_RemoveServerRouteLBOff(t *testing.T) {
	const msg = "El balanceador lb1 está apagado; enciéndalo para llegar al servidor"
	ts, _ := newLBServer(t, map[string]service.RunResult{
		"haproxy/remove-server.sh": {ExitCode: 12, Stderr: msg + "\n"},
	})
	if err := ts.lbs.InsertServer(context.Background(), repository.LBServer{LB: "lb1", Name: "server1", VM: "web-a",
		Port: 80, LastIP: "10.10.1.11", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	status, body := do(t, http.MethodDelete, ts.URL+"/api/haproxy/lbs/lb1/servers/web-a", "")

	if status != http.StatusConflict {
		t.Errorf("status = %d, se esperaba 409", status)
	}
	if got, _ := body["error"].(string); got != msg {
		t.Errorf("error = %q, se esperaba %q", got, msg)
	}
	if rows, _ := ts.lbs.Servers(context.Background(), "lb1"); len(rows) != 1 {
		t.Errorf("lb_servers = %+v, la fila debía quedar", rows)
	}
}

// AC-11: PUT .../frontend guarda el puerto y responde 200; el 22 se rechaza con 400.
func TestAC11_SaveFrontendRoute(t *testing.T) {
	ts, runner := newLBServer(t, map[string]service.RunResult{
		"haproxy/save-frontend.sh": {Stdout: []byte("Frontend de lb1 en el puerto 8080\n")},
	})

	status, body := do(t, http.MethodPut, ts.URL+"/api/haproxy/lbs/lb1/frontend", `{"port":8080}`)

	if status != http.StatusOK {
		t.Errorf("status = %d, se esperaba 200", status)
	}
	if msg, _ := body["message"].(string); msg != "Frontend de lb1 en el puerto 8080" {
		t.Errorf("message = %q", msg)
	}
	if lb, _ := ts.lbs.GetLB(context.Background(), "lb1"); lb.FrontendPort != 8080 {
		t.Errorf("frontend_port = %d, se esperaba 8080", lb.FrontendPort)
	}

	before := runner.callCount()
	status, body = do(t, http.MethodPut, ts.URL+"/api/haproxy/lbs/lb1/frontend", `{"port":22}`)
	if status != http.StatusBadRequest {
		t.Errorf("puerto 22: status = %d, se esperaba 400", status)
	}
	if msg, _ := body["error"].(string); msg != "El puerto 22 está reservado para SSH" {
		t.Errorf("error = %q", msg)
	}
	if runner.callCount() != before {
		t.Error("el puerto 22 no debía llegar al script")
	}
	if status, _ := do(t, http.MethodPut, ts.URL+"/api/haproxy/lbs/lb1/frontend", `{`); status != http.StatusBadRequest {
		t.Errorf("JSON inválido: status = %d, se esperaba 400", status)
	}
	if status, _ := do(t, http.MethodPut, ts.URL+"/api/haproxy/lbs/nada/frontend", `{"port":8080}`); status != http.StatusNotFound {
		t.Errorf("balanceador inexistente: status = %d, se esperaba 404", status)
	}
}

// AC-45: una recarga fallida responde 500 con el texto de la spec y no cambia el puerto.
func TestAC45_SaveFrontendRouteReloadFailed(t *testing.T) {
	ts, _ := newLBServer(t, map[string]service.RunResult{
		"haproxy/save-frontend.sh": {ExitCode: 11, Stderr: "No se pudo recargar HAProxy\n"},
	})

	status, body := do(t, http.MethodPut, ts.URL+"/api/haproxy/lbs/lb1/frontend", `{"port":8080}`)

	if status != http.StatusInternalServerError {
		t.Errorf("status = %d, se esperaba 500", status)
	}
	if msg, _ := body["error"].(string); msg != "No se pudo recargar HAProxy; se mantuvo la configuración anterior" {
		t.Errorf("error = %q", msg)
	}
	if lb, _ := ts.lbs.GetLB(context.Background(), "lb1"); lb.FrontendPort != 80 {
		t.Errorf("frontend_port = %d, se esperaba 80", lb.FrontendPort)
	}
}

// AC-22: POST .../refresh responde 200 con el mensaje y deja una fila en el historial.
func TestAC22_RefreshRoute(t *testing.T) {
	ts, _ := newLBServer(t, map[string]service.RunResult{
		"haproxy/refresh-lb.sh": {Stdout: []byte("server1 10.10.1.15 on\nreloaded=yes\n")},
	})
	if err := ts.lbs.InsertServer(context.Background(), repository.LBServer{LB: "lb1", Name: "server1", VM: "web-a",
		Port: 80, LastIP: "10.10.1.11", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	status, body := do(t, http.MethodPost, ts.URL+"/api/haproxy/lbs/lb1/refresh", "")

	if status != http.StatusOK {
		t.Fatalf("status = %d, se esperaba 200", status)
	}
	if msg, _ := body["message"].(string); msg == "" {
		t.Errorf("message vacío: %v", body)
	}
	if rows, _ := ts.lbs.Servers(context.Background(), "lb1"); rows[0].LastIP != "10.10.1.15" {
		t.Errorf("last_ip = %q, se esperaba 10.10.1.15", rows[0].LastIP)
	}

	if status, _ := do(t, http.MethodPost, ts.URL+"/api/haproxy/lbs/nada/refresh", ""); status != http.StatusNotFound {
		t.Errorf("balanceador inexistente: status = %d, se esperaba 404", status)
	}
}

func TestRefreshRouteLBOff(t *testing.T) {
	const msg = "El balanceador lb1 está apagado; enciéndalo para llegar al servidor"
	ts, _ := newLBServer(t, map[string]service.RunResult{
		"haproxy/refresh-lb.sh": {ExitCode: 12, Stderr: msg + "\n"},
	})

	status, body := do(t, http.MethodPost, ts.URL+"/api/haproxy/lbs/lb1/refresh", "")

	if status != http.StatusConflict {
		t.Errorf("status = %d, se esperaba 409", status)
	}
	if got, _ := body["error"].(string); got != msg {
		t.Errorf("error = %q, se esperaba %q", got, msg)
	}
}

// AC-25: GET .../status devuelve {"states": {servidor: estado}}.
func TestAC25_StatusRoute(t *testing.T) {
	ts, _ := newLBServer(t, map[string]service.RunResult{
		"haproxy/lb-status.sh": {Stdout: []byte("server1 UP\nserver2 DOWN\n")},
	})

	status, body := do(t, http.MethodGet, ts.URL+"/api/haproxy/lbs/lb1/status", "")

	if status != http.StatusOK {
		t.Fatalf("status = %d, se esperaba 200", status)
	}
	states, _ := body["states"].(map[string]any)
	if len(states) != 2 || states["server1"] != "UP" || states["server2"] != "DOWN" {
		t.Errorf("states = %v, se esperaba {server1: UP, server2: DOWN}", body["states"])
	}
	if status, _ := do(t, http.MethodGet, ts.URL+"/api/haproxy/lbs/nada/status", ""); status != http.StatusNotFound {
		t.Errorf("balanceador inexistente: status = %d, se esperaba 404", status)
	}
}

// Sin servidores "states" es un objeto vacío, no null.
func TestStatusRouteWithoutServers(t *testing.T) {
	ts, _ := newLBServer(t, map[string]service.RunResult{"haproxy/lb-status.sh": {}})

	status, body := do(t, http.MethodGet, ts.URL+"/api/haproxy/lbs/lb1/status", "")

	states, ok := body["states"].(map[string]any)
	if status != http.StatusOK || !ok || len(states) != 0 {
		t.Errorf("status = %d, states = %#v; se esperaba 200 y {}", status, body["states"])
	}
}

// AC-40: con el balanceador apagado el estado responde 409 con el texto del script.
func TestStatusRouteLBOff(t *testing.T) {
	const msg = "El balanceador lb1 está apagado; enciéndalo para llegar al servidor"
	ts, _ := newLBServer(t, map[string]service.RunResult{"haproxy/lb-status.sh": {ExitCode: 12, Stderr: msg + "\n"}})

	status, body := do(t, http.MethodGet, ts.URL+"/api/haproxy/lbs/lb1/status", "")

	if status != http.StatusConflict {
		t.Errorf("status = %d, se esperaba 409", status)
	}
	if got, _ := body["error"].(string); got != msg {
		t.Errorf("error = %q, se esperaba %q", got, msg)
	}
}

// AC-35: GET /api/haproxy/history devuelve solo las operaciones del módulo, con su stderr.
func TestAC35_HistoryRoute(t *testing.T) {
	ts, _ := newLBServer(t, map[string]service.RunResult{
		"haproxy/refresh-lb.sh": {ExitCode: 10, Stderr: "Configuración inválida\n"},
	})
	do(t, http.MethodPost, ts.URL+"/api/haproxy/lbs/lb1/refresh", "")

	status, body := do(t, http.MethodGet, ts.URL+"/api/haproxy/history", "")

	if status != http.StatusOK {
		t.Fatalf("status = %d, se esperaba 200", status)
	}
	ops, _ := body["operations"].([]any)
	if len(ops) != 1 {
		t.Fatalf("operations = %v, se esperaba 1", body["operations"])
	}
	row, _ := ops[0].(map[string]any)
	if row["operation"] != "refresh_lb" || row["vm"] != "lb1" || row["result"] != "error" || row["exit_code"] != float64(10) ||
		row["stderr"] != "Configuración inválida" {
		t.Errorf("fila = %v", row)
	}
}

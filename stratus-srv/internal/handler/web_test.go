package handler

import (
	"context"
	"net/http"
	"testing"

	"stratus-srv/internal/repository"
	"stratus-srv/internal/service"
)

// registerWebServer registra un servidor web directamente en SQLite.
func (ts testServer) registerWebServer(t *testing.T, name, typ string) {
	t.Helper()
	ctx := context.Background()
	if _, err := ts.vms.Insert(ctx, name); err != nil {
		t.Fatal(err)
	}
	if err := ts.web.Insert(ctx, repository.WebServer{Name: name, Type: typ}); err != nil {
		t.Fatal(err)
	}
}

// AC-1: POST /api/web/servers crea el servidor web y responde 201 con el mensaje del script.
func TestAC1_CreateWebRoute(t *testing.T) {
	const line = "Servidor web web-a (Nginx) creado; IP 192.168.1.40"
	runner := &fakeRunner{results: map[string]service.RunResult{
		"web/create-web.sh": {Stdout: []byte(line + "\nip=192.168.1.40\n")},
	}}
	ts := newTestServerWithLBs(t, runner)

	status, body := do(t, http.MethodPost, ts.URL+"/api/web/servers",
		`{"name":"web-a","memory_mb":1024,"cpus":1,"type":"nginx"}`)

	if status != http.StatusCreated {
		t.Errorf("status = %d, se esperaba 201", status)
	}
	if msg, _ := body["message"].(string); msg != line {
		t.Errorf("message = %q, se esperaba %q", msg, line)
	}
	if _, ok := body["warning"]; ok {
		t.Errorf("sin advertencia no debe venir warning: %v", body)
	}
	if w, ok, err := ts.web.Get(context.Background(), "web-a"); err != nil || !ok || w.Type != "nginx" {
		t.Errorf("web_servers = %+v, %v, %v", w, ok, err)
	}
}

func TestCreateWebRouteMapsErrors(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		result     service.RunResult
		wantStatus int
		wantError  string
	}{
		{"sin tipo", `{"name":"web-a","memory_mb":1024,"cpus":1}`, service.RunResult{},
			http.StatusBadRequest, "Seleccione el tipo de servidor web"},
		{"JSON inválido", `{`, service.RunResult{}, http.StatusBadRequest, "El cuerpo de la petición no es JSON válido"},
		{"sin disco de Apache", `{"name":"web-a","memory_mb":1024,"cpus":1,"type":"apache"}`,
			service.RunResult{ExitCode: 6, Stderr: "Disco base de Apache no encontrado\n"},
			http.StatusConflict, "Disco base de Apache no encontrado"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeRunner{results: map[string]service.RunResult{"web/create-web.sh": tt.result}}
			ts := newTestServerWithLBs(t, runner)

			status, body := do(t, http.MethodPost, ts.URL+"/api/web/servers", tt.body)

			if status != tt.wantStatus {
				t.Errorf("status = %d, se esperaba %d", status, tt.wantStatus)
			}
			if msg, _ := body["error"].(string); msg != tt.wantError {
				t.Errorf("error = %q, se esperaba %q", msg, tt.wantError)
			}
		})
	}
}

// AC-8: GET /api/web/servers trae solo los servidores web.
func TestAC8_ListWebRoute(t *testing.T) {
	runner := &fakeRunner{results: map[string]service.RunResult{
		"prepare/list-vms.sh": {Stdout: []byte("web-a running 192.168.1.40\nvm1 running 192.168.1.50\n")},
	}}
	ts := newTestServerWithLBs(t, runner)
	ts.registerWebServer(t, "web-a", "nginx")
	if _, err := ts.vms.Insert(context.Background(), "vm1"); err != nil {
		t.Fatal(err)
	}

	status, body := do(t, http.MethodGet, ts.URL+"/api/web/servers", "")

	if status != http.StatusOK {
		t.Fatalf("status = %d, se esperaba 200", status)
	}
	servers, _ := body["servers"].([]any)
	if len(servers) != 1 {
		t.Fatalf("servers = %v, se esperaba solo web-a", body["servers"])
	}
	row, _ := servers[0].(map[string]any)
	want := map[string]any{"name": "web-a", "type": "nginx", "state": "running", "ip": "192.168.1.40", "fqdn": "", "lb": ""}
	for k, v := range want {
		if row[k] != v {
			t.Errorf("%s = %v, se esperaba %v", k, row[k], v)
		}
	}
	if listErr, ok := body["list_error"].(string); !ok || listErr != "" {
		t.Errorf("list_error = %v, se esperaba vacío", body["list_error"])
	}
}

// AC-10: DELETE /api/web/servers/{vm} elimina el servidor; asignado a un balanceador responde 409.
func TestAC10_DeleteWebRoute(t *testing.T) {
	t.Run("sin asignar", func(t *testing.T) {
		ts := newTestServerWithLBs(t, &fakeRunner{})
		ts.registerWebServer(t, "web-a", "nginx")

		status, body := do(t, http.MethodDelete, ts.URL+"/api/web/servers/web-a", "")

		if status != http.StatusOK {
			t.Errorf("status = %d, se esperaba 200", status)
		}
		if msg, _ := body["message"].(string); msg != "Servidor web web-a eliminado junto con su disco" {
			t.Errorf("message = %q", msg)
		}
	})
	t.Run("asignado", func(t *testing.T) {
		runner := &fakeRunner{}
		ts := newTestServerWithLBs(t, runner)
		ts.registerWebServer(t, "web-a", "nginx")
		ctx := context.Background()
		if _, err := ts.vms.Insert(ctx, "lb1"); err != nil {
			t.Fatal(err)
		}
		if err := ts.lbs.InsertLB(ctx, repository.LoadBalancer{Name: "lb1", Subnet: 1, FrontendPort: 80}); err != nil {
			t.Fatal(err)
		}
		if err := ts.lbs.InsertServer(ctx, repository.LBServer{LB: "lb1", Name: "server1", VM: "web-a", Port: 80,
			Enabled: true}); err != nil {
			t.Fatal(err)
		}

		status, body := do(t, http.MethodDelete, ts.URL+"/api/web/servers/web-a", "")

		if status != http.StatusConflict {
			t.Errorf("status = %d, se esperaba 409", status)
		}
		if msg, _ := body["error"].(string); msg != "La VM es servidor de un balanceador; quítela primero" {
			t.Errorf("error = %q", msg)
		}
		if n := runner.callCount(); n != 0 {
			t.Errorf("el runner se llamó %d veces, se esperaba 0", n)
		}
	})
}

// AC-53: GET /api/web/history trae las operaciones del módulo web.
func TestAC53_WebHistoryRoute(t *testing.T) {
	ts := newTestServerWithLBs(t, &fakeRunner{})
	ts.registerWebServer(t, "web-a", "nginx")
	if status, _ := do(t, http.MethodDelete, ts.URL+"/api/web/servers/web-a", ""); status != http.StatusOK {
		t.Fatalf("delete status = %d", status)
	}

	status, body := do(t, http.MethodGet, ts.URL+"/api/web/history", "")

	if status != http.StatusOK {
		t.Fatalf("status = %d, se esperaba 200", status)
	}
	ops, _ := body["operations"].([]any)
	if len(ops) != 1 {
		t.Fatalf("operations = %v, se esperaba una fila", body["operations"])
	}
	if op, _ := ops[0].(map[string]any); op["operation"] != "delete_web" {
		t.Errorf("operation = %v, se esperaba delete_web", op["operation"])
	}
}

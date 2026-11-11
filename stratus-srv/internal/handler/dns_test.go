package handler

import (
	"context"
	"net/http"
	"testing"

	"stratus-srv/internal/service"
)

// registerDNSServer registra el servidor DNS directamente en SQLite.
func (ts testServer) registerDNSServer(t *testing.T, name string) {
	t.Helper()
	ctx := context.Background()
	if _, err := ts.vms.Insert(ctx, name); err != nil {
		t.Fatal(err)
	}
	if err := ts.dns.InsertServer(ctx, name, nil); err != nil {
		t.Fatal(err)
	}
}

// REQ-17, REQ-23: GET /api/dns trae el servidor (null si no hay) y las demás VMs.
func TestAC23_DNSOverviewRoute(t *testing.T) {
	runner := &fakeRunner{results: map[string]service.RunResult{
		"prepare/list-vms.sh": {Stdout: []byte("dns1 running 192.168.1.53\nvm1 poweroff -\n")},
	}}
	ts := newTestServerWithLBs(t, runner)

	status, body := do(t, http.MethodGet, ts.URL+"/api/dns", "")
	if status != http.StatusOK || body["server"] != nil {
		t.Errorf("sin servidor: status = %d, server = %v", status, body["server"])
	}

	ts.registerDNSServer(t, "dns1")
	if _, err := ts.vms.Insert(context.Background(), "vm1"); err != nil {
		t.Fatal(err)
	}

	status, body = do(t, http.MethodGet, ts.URL+"/api/dns", "")

	if status != http.StatusOK {
		t.Fatalf("status = %d, se esperaba 200", status)
	}
	server, _ := body["server"].(map[string]any)
	want := map[string]any{"name": "dns1", "state": "running", "ip": "192.168.1.53", "zone": "stratus.lan"}
	for k, v := range want {
		if server[k] != v {
			t.Errorf("server.%s = %v, se esperaba %v", k, server[k], v)
		}
	}
	vms, _ := body["vms"].([]any)
	if len(vms) != 1 {
		t.Fatalf("vms = %v, se esperaba solo vm1", body["vms"])
	}
	row, _ := vms[0].(map[string]any)
	for k, v := range map[string]any{"name": "vm1", "kind": "", "role": "vm", "state": "poweroff", "ip": "",
		"fqdn": "", "record_ip": ""} {
		if row[k] != v {
			t.Errorf("vm1.%s = %v, se esperaba %v", k, row[k], v)
		}
	}
	if _, ok := row["resolver"]; ok {
		t.Errorf("vm1 trae resolver = %v; el campo se quitó el 2026-11-11", row["resolver"])
	}
	if listErr, ok := body["list_error"].(string); !ok || listErr != "" {
		t.Errorf("list_error = %v, se esperaba vacío", body["list_error"])
	}
}

// AC-22, AC-23: POST /api/dns/server crea el servidor (201); un segundo intento responde 409.
func TestAC22_CreateDNSRoute(t *testing.T) {
	const line = "Servidor DNS dns1 creado en 192.168.1.53; zona stratus.lan"
	runner := &fakeRunner{results: map[string]service.RunResult{"dns/create-dns.sh": {Stdout: []byte(line + "\n")}}}
	ts := newTestServerWithLBs(t, runner)

	status, body := do(t, http.MethodPost, ts.URL+"/api/dns/server", `{"name":"dns1","memory_mb":1024,"cpus":1}`)

	if status != http.StatusCreated || body["message"] != line {
		t.Errorf("status = %d, body = %v; se esperaba 201 con el mensaje", status, body)
	}

	status, body = do(t, http.MethodPost, ts.URL+"/api/dns/server", `{"name":"dns2","memory_mb":1024,"cpus":1}`)

	if status != http.StatusConflict || body["error"] != "Ya existe un servidor DNS" {
		t.Errorf("segundo: status = %d, body = %v; se esperaba 409", status, body)
	}
	if n := runner.callCount(); n != 1 {
		t.Errorf("el runner se llamó %d veces, se esperaba 1 (solo create-dns.sh)", n)
	}
}

// AC-29: DELETE /api/dns/server elimina el servidor DNS (200) sin revisar resolvedores.
func TestAC29_DeleteDNSRoute(t *testing.T) {
	ts := newTestServerWithLBs(t, &fakeRunner{})
	ts.registerDNSServer(t, "dns1")

	status, body := do(t, http.MethodDelete, ts.URL+"/api/dns/server", "")

	if status != http.StatusOK || body["message"] != "Servidor DNS dns1 eliminado junto con su disco" {
		t.Errorf("status = %d, body = %v", status, body)
	}
}

// AC-19, AC-20: POST /api/dns/server/{start|stop|restart} y GET /api/dns/server/status; sin servidor
// DNS responden 409.
func TestAC19_DNSActionRoutes(t *testing.T) {
	for _, action := range []string{"start", "stop", "restart"} {
		t.Run(action, func(t *testing.T) {
			out := "Servicio named listo en dns1\n"
			if action != "stop" {
				out += "active_state=active\n"
			}
			runner := &fakeRunner{results: map[string]service.RunResult{"dns/" + action + "-bind.sh": {Stdout: []byte(out)}}}
			ts := newTestServerWithLBs(t, runner)

			status, body := do(t, http.MethodPost, ts.URL+"/api/dns/server/"+action, "")
			if status != http.StatusConflict || body["error"] != "No hay servidor DNS" {
				t.Errorf("sin servidor: status = %d, body = %v", status, body)
			}

			ts.registerDNSServer(t, "dns1")
			status, body = do(t, http.MethodPost, ts.URL+"/api/dns/server/"+action, "")

			if status != http.StatusOK || body["message"] != "Servicio named listo en dns1" {
				t.Errorf("status = %d, body = %v", status, body)
			}
			if _, has := body["active_state"]; has != (action != "stop") {
				t.Errorf("active_state en %s: %v", action, body)
			}
		})
	}

	t.Run("status", func(t *testing.T) {
		const out = "named.service\n     Active: active\n"
		runner := &fakeRunner{results: map[string]service.RunResult{"dns/status-bind.sh": {Stdout: []byte(out)}}}
		ts := newTestServerWithLBs(t, runner)
		ts.registerDNSServer(t, "dns1")

		status, body := do(t, http.MethodGet, ts.URL+"/api/dns/server/status", "")

		if status != http.StatusOK || body["output"] != out {
			t.Errorf("status = %d, body = %v", status, body)
		}
	})
}

// AC-53: GET /api/dns/history trae las operaciones del módulo dns.
func TestAC53_DNSHistoryRoute(t *testing.T) {
	ts := newTestServerWithLBs(t, &fakeRunner{})
	ts.registerDNSServer(t, "dns1")
	if status, _ := do(t, http.MethodDelete, ts.URL+"/api/dns/server", ""); status != http.StatusOK {
		t.Fatalf("delete status = %d", status)
	}

	status, body := do(t, http.MethodGet, ts.URL+"/api/dns/history", "")

	ops, _ := body["operations"].([]any)
	if status != http.StatusOK || len(ops) != 1 {
		t.Fatalf("status = %d, operations = %v", status, body["operations"])
	}
	if op, _ := ops[0].(map[string]any); op["operation"] != "delete_dns" {
		t.Errorf("operation = %v, se esperaba delete_dns", op["operation"])
	}
}

// AC-30, AC-33: POST /api/dns/records responde 200 con el resultado de cada VM; si no se agregó
// ninguna, 409 con el motivo. DELETE /api/dns/records/{vm} quita el registro.
func TestAC30_DNSRecordRoutes(t *testing.T) {
	runner := &fakeRunner{results: map[string]service.RunResult{
		"prepare/list-vms.sh": {Stdout: []byte("dns1 running 192.168.1.53\nvm1 running 192.168.1.40\nvm2 poweroff -\n")},
	}}
	ts := newTestServerWithLBs(t, runner)
	ts.registerDNSServer(t, "dns1")
	for _, vm := range []string{"vm1", "vm2"} {
		if _, err := ts.vms.Insert(context.Background(), vm); err != nil {
			t.Fatal(err)
		}
	}

	status, body := do(t, http.MethodPost, ts.URL+"/api/dns/records", `{"vms":["vm1","vm2"]}`)

	if status != http.StatusOK || body["message"] != "Registros agregados: vm1" {
		t.Fatalf("agregar: status = %d, body = %v", status, body)
	}
	results, _ := body["results"].([]any)
	if len(results) != 2 {
		t.Fatalf("results = %v, se esperaban 2", body["results"])
	}
	for i, want := range []map[string]any{{"vm": "vm1", "ok": true}, {"vm": "vm2", "ok": false}} {
		got, _ := results[i].(map[string]any)
		if got["vm"] != want["vm"] || got["ok"] != want["ok"] || got["message"] == "" {
			t.Errorf("results[%d] = %v, se esperaba %v con mensaje", i, got, want)
		}
	}

	status, body = do(t, http.MethodPost, ts.URL+"/api/dns/records", `{"vms":["vm1"]}`)

	if status != http.StatusConflict || body["error"] == nil {
		t.Errorf("ninguna agregada: status = %d, body = %v; se esperaba 409", status, body)
	}

	status, body = do(t, http.MethodDelete, ts.URL+"/api/dns/records/vm1", "")

	if status != http.StatusOK || body["message"] != "Registro de vm1 quitado" {
		t.Errorf("quitar: status = %d, body = %v", status, body)
	}
}

// AC-39: POST /api/dns/records/refresh responde 200 con el mensaje y deja una fila refresh_records.
func TestAC39_RefreshRoute(t *testing.T) {
	runner := &fakeRunner{results: map[string]service.RunResult{
		"dns/refresh-records.sh": {Stdout: []byte("reloaded=no\n")},
	}}
	ts := newTestServerWithLBs(t, runner)
	ts.registerDNSServer(t, "dns1")

	status, body := do(t, http.MethodPost, ts.URL+"/api/dns/records/refresh", "")

	if status != http.StatusOK || body["message"] != "Registros sin cambios" {
		t.Fatalf("status = %d, body = %v", status, body)
	}
	_, hist := do(t, http.MethodGet, ts.URL+"/api/dns/history", "")
	ops, _ := hist["operations"].([]any)
	if len(ops) != 1 {
		t.Fatalf("operations = %v, se esperaba una fila", hist["operations"])
	}
	if op, _ := ops[0].(map[string]any); op["operation"] != "refresh_records" {
		t.Errorf("operation = %v, se esperaba refresh_records", op["operation"])
	}
}

// Las rutas del resolvedor (US-7) se quitaron el 2026-11-11: PUT y DELETE /api/dns/resolvers/{vm}
// no existen y no llaman a ningún script.
func TestNoResolverRoutes(t *testing.T) {
	runner := &fakeRunner{results: map[string]service.RunResult{}}
	ts := newTestServerWithLBs(t, runner)
	ts.registerDNSServer(t, "dns1")
	if _, err := ts.vms.Insert(context.Background(), "vm1"); err != nil {
		t.Fatal(err)
	}

	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		req, err := http.NewRequest(method, ts.URL+"/api/dns/resolvers/vm1", nil)
		if err != nil {
			t.Fatal(err)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusNotFound && res.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("%s: status = %d, se esperaba 404 o 405", method, res.StatusCode)
		}
	}
	if n := runner.callCount(); n != 0 {
		t.Errorf("el runner se llamó %d veces, se esperaba 0", n)
	}
}

// AC-66: GET /api/dns/zone devuelve los registros que sirve el servidor DNS; apagado, 409 con el
// texto de REQ-49.
func TestAC66_ZoneRoute(t *testing.T) {
	const out = "NS\tstratus.lan\t60\tdns1.stratus.lan.\nA\tdns1.stratus.lan\t60\t192.168.1.53\n"
	runner := &fakeRunner{results: map[string]service.RunResult{"dns/show-zone.sh": {Stdout: []byte(out)}}}
	ts := newTestServerWithLBs(t, runner)
	ts.registerDNSServer(t, "dns1")

	status, body := do(t, http.MethodGet, ts.URL+"/api/dns/zone", "")

	records, _ := body["records"].([]any)
	if status != http.StatusOK || len(records) != 2 {
		t.Fatalf("status = %d, body = %v; se esperaba 200 con 2 registros", status, body)
	}
	first, _ := records[1].(map[string]any)
	want := map[string]any{"type": "A", "name": "dns1.stratus.lan", "value": "192.168.1.53", "ttl": float64(60)}
	for k, v := range want {
		if first[k] != v {
			t.Errorf("records[1].%s = %v, se esperaba %v", k, first[k], v)
		}
	}

	runner.mu.Lock()
	runner.results["dns/show-zone.sh"] = service.RunResult{ExitCode: 7}
	runner.mu.Unlock()

	status, body = do(t, http.MethodGet, ts.URL+"/api/dns/zone", "")

	if status != http.StatusConflict || body["error"] != "El servidor DNS está apagado; enciéndalo para ver la zona" {
		t.Errorf("apagado: status = %d, body = %v", status, body)
	}
}

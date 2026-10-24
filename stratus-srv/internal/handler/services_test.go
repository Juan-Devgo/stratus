package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"stratus-srv/internal/repository"
	"stratus-srv/internal/service"
)

// maxTestArchive es el tamaño máximo de archivo que usa el servidor de pruebas de servicios.
const maxTestArchive = 1 << 20

// newServicesTestServer arma el handler de servicios con el servicio real, SQLite temporal y el
// runner falso. Registra la VM vm1 para que las rutas la encuentren.
func newServicesTestServer(t *testing.T, runner service.ScriptRunner) *httptest.Server {
	t.Helper()
	srv, _ := newServicesTestServerWithHistory(t, runner)
	return srv
}

// newServicesTestServerWithHistory además devuelve el historial, para insertar filas de otros módulos.
func newServicesTestServerWithHistory(t *testing.T, runner service.ScriptRunner) (*httptest.Server, *repository.HistoryRepository) {
	t.Helper()
	db, err := repository.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	vms := repository.NewVMRepository(db)
	reserve(t, vms, "vm1")
	history := repository.NewHistoryRepository(db)
	svc := service.NewServicesService(service.ServicesConfig{MaxArchiveBytes: maxTestArchive}, runner, vms,
		repository.NewLBRepository(db), history, &service.VMLocks{}, log.New(io.Discard, "", 0))

	mux := http.NewServeMux()
	RegisterServices(mux, svc)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, history
}

func TestAC15_ServicesListRouteReturnsJSON(t *testing.T) {
	runner := &fakeRunner{results: map[string]service.RunResult{
		"services/list-services.sh": {Stdout: []byte(
			"demian-lee\tactive\tenabled\ton-failure\t5\tdemian-lee\t\tPORT,SERVER_NAME\tServidor de producción\n" +
				"api\tinactive\tdisabled\talways\t10\tbin/server\t--port 9090\t\tAPI interna\n")},
	}}
	srv := newServicesTestServer(t, runner)

	status, body := do(t, http.MethodGet, srv.URL+"/api/services/vms/vm1/services", "")

	if status != http.StatusOK {
		t.Fatalf("status = %d, se esperaba 200", status)
	}
	want := []any{
		map[string]any{"name": "demian-lee", "description": "Servidor de producción", "active_state": "active",
			"enabled_state": "enabled", "restart": "on-failure", "restart_sec": float64(5),
			"executable": "demian-lee", "args": "", "env_keys": []any{"PORT", "SERVER_NAME"}},
		map[string]any{"name": "api", "description": "API interna", "active_state": "inactive",
			"enabled_state": "disabled", "restart": "always", "restart_sec": float64(10),
			"executable": "bin/server", "args": "--port 9090", "env_keys": []any{}},
	}
	if !reflect.DeepEqual(body["services"], want) {
		t.Errorf("services = %v, se esperaba %v", body["services"], want)
	}
}

func TestAC15_ServicesListRouteReturnsEmptyArray(t *testing.T) {
	srv := newServicesTestServer(t, &fakeRunner{})

	status, body := do(t, http.MethodGet, srv.URL+"/api/services/vms/vm1/services", "")

	if status != http.StatusOK || !reflect.DeepEqual(body["services"], []any{}) {
		t.Errorf("status = %d, services = %#v; se esperaba 200 y []", status, body["services"])
	}
}

func TestAC17_ServicesListRouteMapsErrors(t *testing.T) {
	tests := []struct {
		name       string
		result     service.RunResult
		path       string
		wantStatus int
		wantError  string
	}{
		{"sin IP", service.RunResult{ExitCode: 7}, "/api/services/vms/vm1/services", http.StatusConflict,
			"La VM no tiene dirección IP; verifique que esté encendida"},
		{"VM desconocida", service.RunResult{}, "/api/services/vms/otra/services", http.StatusNotFound, "La VM no existe"},
		{"nombre de VM inválido", service.RunResult{}, "/api/services/vms/VM_1/services", http.StatusBadRequest,
			"El nombre de la VM debe tener de 1 a 32 caracteres: minúsculas, números y guiones, sin empezar con guion"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeRunner{results: map[string]service.RunResult{"services/list-services.sh": tt.result}}
			srv := newServicesTestServer(t, runner)

			status, body := do(t, http.MethodGet, srv.URL+tt.path, "")

			if status != tt.wantStatus || body["error"] != tt.wantError {
				t.Errorf("status = %d, error = %v; se esperaba %d, %q", status, body["error"], tt.wantStatus, tt.wantError)
			}
		})
	}
}

// part es una parte de un cuerpo multipart; filename vacío es un campo de texto.
type part struct {
	name, filename, content string
}

// doMultipart envía las partes en el orden dado y decodifica el cuerpo JSON de la respuesta.
func doMultipart(t *testing.T, method, url string, parts ...part) (int, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for _, p := range parts {
		var fw io.Writer
		var err error
		if p.filename != "" {
			fw, err = w.CreateFormFile(p.name, p.filename)
		} else {
			fw, err = w.CreateFormField(p.name)
		}
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(fw, p.content); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(method, url, &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("respuesta no es JSON: %v", err)
	}
	return resp.StatusCode, out
}

// createPayload es el JSON de la parte "payload" del AC-1.
const createPayload = `{"name":"demian-lee","description":"Servidor de producción","executable":"demian-lee","args":"",` +
	`"restart":"on-failure","restart_sec":5,"env":[{"key":"PORT","value":"8080"},{"key":"ADMIN_PASSWORD","value":"Secreta123"}]}`

func TestAC1_CreateServiceRouteMultipart(t *testing.T) {
	runner := &fakeRunner{results: map[string]service.RunResult{
		"services/create-service.sh": {Stdout: []byte("Servicio demian-lee creado en vm1\nactive_state=active\n")},
	}}
	srv := newServicesTestServer(t, runner)

	status, body := doMultipart(t, http.MethodPost, srv.URL+"/api/services/vms/vm1/services",
		part{name: "payload", content: createPayload},
		part{name: "archive", filename: "demian-lee.tar.gz", content: "contenido"})

	if status != http.StatusCreated {
		t.Fatalf("status = %d (%v), se esperaba 201", status, body)
	}
	want := map[string]any{"message": "Servicio demian-lee creado en vm1", "active_state": "active"}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("cuerpo = %v, se esperaba %v", body, want)
	}
	if runner.calls != 1 {
		t.Errorf("el runner se llamó %d veces, se esperaba 1", runner.calls)
	}
}

func TestAC1_CreateServiceRouteRejectsBadMultipart(t *testing.T) {
	tests := []struct {
		name  string
		parts []part
	}{
		{"archivo antes del payload", []part{
			{name: "archive", filename: "a.tar.gz", content: "x"}, {name: "payload", content: createPayload}}},
		{"sin archivo", []part{{name: "payload", content: createPayload}}},
		{"payload no es JSON", []part{{name: "payload", content: "no json"},
			{name: "archive", filename: "a.tar.gz", content: "x"}}},
		{"parte con otro nombre", []part{{name: "payload", content: createPayload},
			{name: "otro", filename: "a.tar.gz", content: "x"}}},
		{"sin partes", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeRunner{}
			srv := newServicesTestServer(t, runner)

			status, body := doMultipart(t, http.MethodPost, srv.URL+"/api/services/vms/vm1/services", tt.parts...)

			if status != http.StatusBadRequest || body["error"] == nil {
				t.Errorf("status = %d, cuerpo = %v; se esperaba 400 con error", status, body)
			}
			if runner.calls != 0 {
				t.Errorf("el runner se llamó %d veces, se esperaban 0", runner.calls)
			}
		})
	}
}

func TestAC1_CreateServiceRouteRequiresMultipart(t *testing.T) {
	srv := newServicesTestServer(t, &fakeRunner{})

	status, body := do(t, http.MethodPost, srv.URL+"/api/services/vms/vm1/services", createPayload)

	if status != http.StatusBadRequest || body["error"] == nil {
		t.Errorf("status = %d, cuerpo = %v; se esperaba 400 con error", status, body)
	}
}

func TestAC4_AC6_AC7_AC13_ServicesHandlerMapsCreateErrors(t *testing.T) {
	tests := []struct {
		name       string
		result     service.RunResult
		wantStatus int
		wantError  string
	}{
		{"nombre en uso", service.RunResult{ExitCode: 8}, http.StatusConflict, "Ya existe un servicio o usuario con ese nombre en la VM"},
		{"archivo inválido", service.RunResult{ExitCode: 10}, http.StatusUnprocessableEntity, "Archivo comprimido inválido"},
		{"ejecutable ausente", service.RunResult{ExitCode: 11}, http.StatusUnprocessableEntity, "El ejecutable no existe en el directorio de la aplicación"},
		{"limpieza fallida", service.RunResult{ExitCode: 9, Stderr: "Limpieza fallida; puede quedar en la VM: x\n"}, http.StatusInternalServerError,
			"No se pudo crear el servicio demian-lee y la limpieza falló; revise la VM. Limpieza fallida; puede quedar en la VM: x"},
		{"sin IP", service.RunResult{ExitCode: 7}, http.StatusConflict, "La VM no tiene dirección IP; verifique que esté encendida"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeRunner{results: map[string]service.RunResult{"services/create-service.sh": tt.result}}
			srv := newServicesTestServer(t, runner)

			status, body := doMultipart(t, http.MethodPost, srv.URL+"/api/services/vms/vm1/services",
				part{name: "payload", content: createPayload},
				part{name: "archive", filename: "a.tar.gz", content: "x"})

			if status != tt.wantStatus || body["error"] != tt.wantError {
				t.Errorf("status = %d, error = %v; se esperaba %d, %q", status, body["error"], tt.wantStatus, tt.wantError)
			}
		})
	}
}

func TestAC5_ServicesRouteRejectsOversizedBody(t *testing.T) {
	runner := &fakeRunner{}
	srv := newServicesTestServer(t, runner)

	// Más que el máximo del archivo más el margen de 1 MiB del cuerpo HTTP.
	status, body := doMultipart(t, http.MethodPost, srv.URL+"/api/services/vms/vm1/services",
		part{name: "payload", content: createPayload},
		part{name: "archive", filename: "a.tar.gz", content: strings.Repeat("x", maxTestArchive+(1<<20)+1024)})

	if status != http.StatusBadRequest || body["error"] != "El archivo supera 200 MB" {
		t.Errorf("status = %d, error = %v; se esperaba 400 con \"El archivo supera 200 MB\"", status, body["error"])
	}
	if runner.calls != 0 {
		t.Errorf("el runner se llamó %d veces, se esperaban 0", runner.calls)
	}
}

func TestAC5_ServicesRouteRejectsZipExtension(t *testing.T) {
	runner := &fakeRunner{}
	srv := newServicesTestServer(t, runner)

	status, body := doMultipart(t, http.MethodPost, srv.URL+"/api/services/vms/vm1/services",
		part{name: "payload", content: createPayload},
		part{name: "archive", filename: "app.zip", content: "x"})

	if status != http.StatusBadRequest || body["error"] != "El archivo debe ser .tar.gz" {
		t.Errorf("status = %d, error = %v", status, body["error"])
	}
	if runner.calls != 0 {
		t.Errorf("el runner se llamó %d veces, se esperaban 0", runner.calls)
	}
}

func TestAC18_ServiceActionRoutes(t *testing.T) {
	tests := []struct {
		action, stdout string
		want           map[string]any
	}{
		{"start", "Servicio api iniciado en vm1\nactive_state=active\n", map[string]any{"message": "Servicio api iniciado en vm1", "active_state": "active"}},
		{"restart", "Servicio api reiniciado en vm1\nactive_state=failed\n", map[string]any{"message": "Servicio api reiniciado en vm1", "active_state": "failed"}},
		{"stop", "Servicio api detenido en vm1\n", map[string]any{"message": "Servicio api detenido en vm1"}},
		{"enable", "Servicio api habilitado en vm1\n", map[string]any{"message": "Servicio api habilitado en vm1"}},
		{"disable", "Servicio api deshabilitado en vm1\n", map[string]any{"message": "Servicio api deshabilitado en vm1"}},
	}
	for _, tt := range tests {
		t.Run(tt.action, func(t *testing.T) {
			runner := &fakeRunner{results: map[string]service.RunResult{
				"services/" + tt.action + "-service.sh": {Stdout: []byte(tt.stdout)},
			}}
			srv := newServicesTestServer(t, runner)

			status, body := do(t, http.MethodPost, srv.URL+"/api/services/vms/vm1/services/api/"+tt.action, "")

			if status != http.StatusOK || !reflect.DeepEqual(body, tt.want) {
				t.Errorf("status = %d, cuerpo = %v; se esperaba 200 y %v", status, body, tt.want)
			}
			if runner.calls != 1 {
				t.Errorf("el runner se llamó %d veces, se esperaba 1", runner.calls)
			}
		})
	}
}

func TestAC18_ServiceActionRouteMapsErrors(t *testing.T) {
	tests := []struct {
		name       string
		result     service.RunResult
		path       string
		wantStatus int
		wantError  string
	}{
		{"no administrado", service.RunResult{ExitCode: 6}, "/api/services/vms/vm1/services/ssh/start", http.StatusConflict,
			"Solo se pueden gestionar servicios creados por Stratus"},
		{"nombre inválido", service.RunResult{}, "/api/services/vms/vm1/services/Demian/stop", http.StatusBadRequest,
			"El nombre del servicio debe tener de 1 a 32 caracteres: minúsculas, números y \"-\", empezando con una letra"},
		{"sin IP", service.RunResult{ExitCode: 7}, "/api/services/vms/vm1/services/api/enable", http.StatusConflict,
			"La VM no tiene dirección IP; verifique que esté encendida"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeRunner{results: map[string]service.RunResult{
				"services/start-service.sh": tt.result, "services/stop-service.sh": tt.result, "services/enable-service.sh": tt.result,
			}}
			srv := newServicesTestServer(t, runner)

			status, body := do(t, http.MethodPost, srv.URL+tt.path, "")

			if status != tt.wantStatus || body["error"] != tt.wantError {
				t.Errorf("status = %d, error = %v; se esperaba %d, %q", status, body["error"], tt.wantStatus, tt.wantError)
			}
		})
	}
}

func TestAC22_ServiceLogsReturnsOutput(t *testing.T) {
	tests := []struct {
		route, script, stdout string
	}{
		{"status", "services/status-service.sh", "● api.service\n     Active: failed (Result: exit-code)\n"},
		{"logs", "services/logs-service.sh", "oct 22 10:00:00 vm1 api[1]: arrancó\n"},
	}
	for _, tt := range tests {
		t.Run(tt.route, func(t *testing.T) {
			runner := &fakeRunner{results: map[string]service.RunResult{tt.script: {Stdout: []byte(tt.stdout)}}}
			srv := newServicesTestServer(t, runner)

			status, body := do(t, http.MethodGet, srv.URL+"/api/services/vms/vm1/services/api/"+tt.route, "")

			if status != http.StatusOK || !reflect.DeepEqual(body, map[string]any{"output": tt.stdout}) {
				t.Errorf("status = %d, cuerpo = %v; se esperaba 200 con output", status, body)
			}
		})
	}
}

func TestAC21_ServiceStatusAndLogsRouteMapErrors(t *testing.T) {
	for _, route := range []string{"status", "logs"} {
		t.Run(route, func(t *testing.T) {
			runner := &fakeRunner{results: map[string]service.RunResult{
				"services/status-service.sh": {ExitCode: 6}, "services/logs-service.sh": {ExitCode: 6},
			}}
			srv := newServicesTestServer(t, runner)

			status, body := do(t, http.MethodGet, srv.URL+"/api/services/vms/vm1/services/ssh/"+route, "")

			if status != http.StatusConflict || body["error"] != "Solo se pueden gestionar servicios creados por Stratus" {
				t.Errorf("status = %d, error = %v; se esperaba 409 con el texto de la spec", status, body["error"])
			}
		})
	}
}

const editBody = `{"description":"Servidor de producción","executable":"demian-lee","args":"","restart":"always","restart_sec":5,` +
	`"replace_env":false,"env":[]}`

func TestAC23_EditServiceRoute(t *testing.T) {
	runner := &fakeRunner{results: map[string]service.RunResult{
		"services/edit-service.sh": {Stdout: []byte("Servicio demian-lee actualizado en vm1\nactive_state=active\n")},
	}}
	srv := newServicesTestServer(t, runner)

	status, body := do(t, http.MethodPut, srv.URL+"/api/services/vms/vm1/services/demian-lee", editBody)

	want := map[string]any{"message": "Servicio demian-lee actualizado en vm1", "active_state": "active"}
	if status != http.StatusOK || !reflect.DeepEqual(body, want) {
		t.Errorf("status = %d, cuerpo = %v; se esperaba 200 y %v", status, body, want)
	}
}

func TestAC23_EditServiceRouteRejectsBadRequests(t *testing.T) {
	tests := []struct {
		name, path, body string
		wantStatus       int
	}{
		{"no es JSON", "/api/services/vms/vm1/services/demian-lee", "no json", http.StatusBadRequest},
		{"valores sin reemplazo", "/api/services/vms/vm1/services/demian-lee",
			`{"description":"x","executable":"a","args":"","restart":"no","restart_sec":1,"replace_env":false,"env":[{"key":"A","value":"1"}]}`,
			http.StatusBadRequest},
		{"nombre inválido", "/api/services/vms/vm1/services/Demian", editBody, http.StatusBadRequest},
		{"ejecutable inválido", "/api/services/vms/vm1/services/demian-lee",
			`{"description":"x","executable":"../x","args":"","restart":"no","restart_sec":1,"replace_env":false,"env":[]}`,
			http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeRunner{}
			srv := newServicesTestServer(t, runner)

			status, body := do(t, http.MethodPut, srv.URL+tt.path, tt.body)

			if status != tt.wantStatus || body["error"] == nil {
				t.Errorf("status = %d, cuerpo = %v; se esperaba %d con error", status, body, tt.wantStatus)
			}
			if runner.calls != 0 {
				t.Errorf("el runner se llamó %d veces, se esperaban 0", runner.calls)
			}
		})
	}
}

func TestAC40_EditServiceRouteMapsErrors(t *testing.T) {
	tests := []struct {
		name       string
		result     service.RunResult
		wantStatus int
		wantError  string
	}{
		{"restauración fallida", service.RunResult{ExitCode: 9, Stderr: "Restauración fallida; puede quedar en la VM: x\n"},
			http.StatusInternalServerError,
			"No se pudo editar el servicio demian-lee y la restauración falló; revise la VM. Restauración fallida; puede quedar en la VM: x"},
		{"ejecutable ausente", service.RunResult{ExitCode: 11}, http.StatusUnprocessableEntity,
			"El ejecutable no existe en el directorio de la aplicación"},
		{"no administrado", service.RunResult{ExitCode: 6}, http.StatusConflict,
			"Solo se pueden gestionar servicios creados por Stratus"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeRunner{results: map[string]service.RunResult{"services/edit-service.sh": tt.result}}
			srv := newServicesTestServer(t, runner)

			status, body := do(t, http.MethodPut, srv.URL+"/api/services/vms/vm1/services/demian-lee", editBody)

			if status != tt.wantStatus || body["error"] != tt.wantError {
				t.Errorf("status = %d, error = %v; se esperaba %d, %q", status, body["error"], tt.wantStatus, tt.wantError)
			}
		})
	}
}

func TestAC26_RedeployRouteMultipart(t *testing.T) {
	runner := &fakeRunner{results: map[string]service.RunResult{
		"services/redeploy-service.sh": {Stdout: []byte("Servicio demian-lee redesplegado en vm1\nactive_state=active\n")},
	}}
	srv := newServicesTestServer(t, runner)

	status, body := doMultipart(t, http.MethodPost, srv.URL+"/api/services/vms/vm1/services/demian-lee/redeploy",
		part{name: "payload", content: `{"executable":"bin/server"}`},
		part{name: "archive", filename: "nuevo.tar.gz", content: "contenido"})

	want := map[string]any{"message": "Servicio demian-lee redesplegado en vm1", "active_state": "active"}
	if status != http.StatusOK || !reflect.DeepEqual(body, want) {
		t.Errorf("status = %d, cuerpo = %v; se esperaba 200 y %v", status, body, want)
	}
	if runner.calls != 1 {
		t.Errorf("el runner se llamó %d veces, se esperaba 1", runner.calls)
	}
}

func TestAC26_RedeployRouteRejectsBadRequests(t *testing.T) {
	const url = "/api/services/vms/vm1/services/demian-lee/redeploy"
	tests := []struct {
		name  string
		path  string
		parts []part
		want  string
	}{
		{"archivo antes del payload", url, []part{
			{name: "archive", filename: "a.tar.gz", content: "x"}, {name: "payload", content: `{"executable":"a"}`}}, ""},
		{"ejecutable inválido", url, []part{
			{name: "payload", content: `{"executable":"../x"}`}, {name: "archive", filename: "a.tar.gz", content: "x"}}, ""},
		{"extensión .zip", url, []part{
			{name: "payload", content: `{"executable":"a"}`}, {name: "archive", filename: "a.zip", content: "x"}},
			"El archivo debe ser .tar.gz"},
		{"nombre inválido", "/api/services/vms/vm1/services/Demian/redeploy", []part{
			{name: "payload", content: `{"executable":"a"}`}, {name: "archive", filename: "a.tar.gz", content: "x"}}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeRunner{}
			srv := newServicesTestServer(t, runner)

			status, body := doMultipart(t, http.MethodPost, srv.URL+tt.path, tt.parts...)

			if status != http.StatusBadRequest || body["error"] == nil || (tt.want != "" && body["error"] != tt.want) {
				t.Errorf("status = %d, cuerpo = %v; se esperaba 400 con error", status, body)
			}
			if runner.calls != 0 {
				t.Errorf("el runner se llamó %d veces, se esperaban 0", runner.calls)
			}
		})
	}
}

func TestAC38_AC39_RedeployRouteMapsErrors(t *testing.T) {
	tests := []struct {
		name       string
		result     service.RunResult
		wantStatus int
		wantError  string
	}{
		{"archivo inválido", service.RunResult{ExitCode: 10}, http.StatusUnprocessableEntity, "Archivo comprimido inválido"},
		{"ejecutable ausente", service.RunResult{ExitCode: 11}, http.StatusUnprocessableEntity,
			"El ejecutable no existe en el directorio de la aplicación"},
		{"no administrado", service.RunResult{ExitCode: 6}, http.StatusConflict, "Solo se pueden gestionar servicios creados por Stratus"},
		{"restauración fallida", service.RunResult{ExitCode: 9, Stderr: "Restauración fallida; puede quedar en la VM: x\n"},
			http.StatusInternalServerError,
			"No se pudo redesplegar el servicio demian-lee y la restauración falló; revise la VM. Restauración fallida; puede quedar en la VM: x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeRunner{results: map[string]service.RunResult{"services/redeploy-service.sh": tt.result}}
			srv := newServicesTestServer(t, runner)

			status, body := doMultipart(t, http.MethodPost, srv.URL+"/api/services/vms/vm1/services/demian-lee/redeploy",
				part{name: "payload", content: `{"executable":"bin/server"}`},
				part{name: "archive", filename: "a.tar.gz", content: "x"})

			if status != tt.wantStatus || body["error"] != tt.wantError {
				t.Errorf("status = %d, error = %v; se esperaba %d, %q", status, body["error"], tt.wantStatus, tt.wantError)
			}
		})
	}
}

func TestAC28_DeleteServiceRoute(t *testing.T) {
	runner := &fakeRunner{results: map[string]service.RunResult{
		"services/delete-service.sh": {Stdout: []byte("Servicio api eliminado de vm1\n")},
	}}
	srv := newServicesTestServer(t, runner)

	status, body := do(t, http.MethodDelete, srv.URL+"/api/services/vms/vm1/services/api", "")

	if status != http.StatusOK || !reflect.DeepEqual(body, map[string]any{"message": "Servicio api eliminado de vm1"}) {
		t.Errorf("status = %d, cuerpo = %v; se esperaba 200 con message", status, body)
	}
}

func TestAC41_DeleteServiceRouteMapsErrors(t *testing.T) {
	tests := []struct {
		name       string
		result     service.RunResult
		path       string
		wantStatus int
		wantError  string
	}{
		{"paso fallido", service.RunResult{ExitCode: 5, Stderr: "No se pudo eliminar el servicio api: falló el paso 5\n"},
			"/api/services/vms/vm1/services/api", http.StatusInternalServerError,
			"La operación delete_service falló (código 5): No se pudo eliminar el servicio api: falló el paso 5"},
		{"no administrado", service.RunResult{ExitCode: 6}, "/api/services/vms/vm1/services/ssh", http.StatusConflict,
			"Solo se pueden gestionar servicios creados por Stratus"},
		{"nombre inválido", service.RunResult{}, "/api/services/vms/vm1/services/Demian", http.StatusBadRequest,
			"El nombre del servicio debe tener de 1 a 32 caracteres: minúsculas, números y \"-\", empezando con una letra"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeRunner{results: map[string]service.RunResult{"services/delete-service.sh": tt.result}}
			srv := newServicesTestServer(t, runner)

			status, body := do(t, http.MethodDelete, srv.URL+tt.path, "")

			if status != tt.wantStatus || body["error"] != tt.wantError {
				t.Errorf("status = %d, error = %v; se esperaba %d, %q", status, body["error"], tt.wantStatus, tt.wantError)
			}
		})
	}
}

func TestAC20_ServicesHandlerMapsNotManaged(t *testing.T) {
	const base = "/api/services/vms/vm1/services/ssh"
	multipart := []part{{name: "payload", content: `{"executable":"a"}`}, {name: "archive", filename: "a.tar.gz", content: "x"}}
	tests := []struct {
		name string
		do   func(url string) (int, map[string]any)
	}{
		{"start", func(u string) (int, map[string]any) { return do(t, http.MethodPost, u+base+"/start", "") }},
		{"disable", func(u string) (int, map[string]any) { return do(t, http.MethodPost, u+base+"/disable", "") }},
		{"status", func(u string) (int, map[string]any) { return do(t, http.MethodGet, u+base+"/status", "") }},
		{"logs", func(u string) (int, map[string]any) { return do(t, http.MethodGet, u+base+"/logs", "") }},
		{"edit", func(u string) (int, map[string]any) { return do(t, http.MethodPut, u+base, editBody) }},
		{"redeploy", func(u string) (int, map[string]any) {
			return doMultipart(t, http.MethodPost, u+base+"/redeploy", multipart...)
		}},
		{"delete", func(u string) (int, map[string]any) { return do(t, http.MethodDelete, u+base, "") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Todos los scripts de operaciones sobre un servicio existente responden 6.
			results := map[string]service.RunResult{}
			for _, script := range []string{"start", "disable", "status", "logs", "edit", "redeploy", "delete"} {
				results["services/"+script+"-service.sh"] = service.RunResult{ExitCode: 6}
			}
			srv := newServicesTestServer(t, &fakeRunner{results: results})

			status, body := tt.do(srv.URL)

			if status != http.StatusConflict || body["error"] != "Solo se pueden gestionar servicios creados por Stratus" {
				t.Errorf("status = %d, error = %v; se esperaba 409 con el texto de la spec", status, body["error"])
			}
		})
	}
}

func TestAC34_ServicesHistoryRoute(t *testing.T) {
	runner := &fakeRunner{results: map[string]service.RunResult{
		"services/stop-service.sh": {ExitCode: 5, Stderr: "Falló el paso systemctl en la VM (código 5)\n"},
	}}
	srv, history := newServicesTestServerWithHistory(t, runner)
	do(t, http.MethodPost, srv.URL+"/api/services/vms/vm1/services/api/stop", "")
	// Una fila de otro módulo no debe aparecer.
	if err := history.Insert(context.Background(), repository.Operation{Module: "users", Operation: "create_user",
		VM: "vm1", Command: "users/create-user.sh", Result: "ok"}); err != nil {
		t.Fatal(err)
	}

	status, body := do(t, http.MethodGet, srv.URL+"/api/services/history", "")

	if status != http.StatusOK {
		t.Fatalf("status = %d, se esperaba 200", status)
	}
	rows, _ := body["operations"].([]any)
	if len(rows) != 1 {
		t.Fatalf("operations = %v, se esperaba una fila de services", body["operations"])
	}
	row, _ := rows[0].(map[string]any)
	if row["operation"] != "stop_service" || row["vm"] != "vm1" || row["exit_code"] != float64(5) ||
		row["stderr"] != "Falló el paso systemctl en la VM (código 5)" || row["result"] != "error" ||
		row["command"] != "services/stop-service.sh" {
		t.Errorf("fila = %v", row)
	}
	for _, key := range []string{"id", "args", "created_at"} {
		if _, ok := row[key]; !ok {
			t.Errorf("a la fila le falta el campo snake_case %q: %v", key, row)
		}
	}
}

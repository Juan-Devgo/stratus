package handler

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"stratus-srv/internal/repository"
	"stratus-srv/internal/service"
)

// newUsersTestServer arma el handler de usuarios con el servicio real, SQLite temporal y el
// runner falso. Registra la VM vm1 para que las rutas la encuentren.
func newUsersTestServer(t *testing.T, runner service.ScriptRunner) *httptest.Server {
	t.Helper()
	srv, _ := newUsersTestServerWithHistory(t, runner)
	return srv
}

// newUsersTestServerWithHistory además devuelve el historial, para insertar filas de otros módulos.
func newUsersTestServerWithHistory(t *testing.T, runner service.ScriptRunner) (*httptest.Server, *repository.HistoryRepository) {
	t.Helper()
	db, err := repository.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	vms := repository.NewVMRepository(db)
	reserve(t, vms, "vm1")
	history := repository.NewHistoryRepository(db)
	svc := service.NewUsersService(service.UsersConfig{KeyTTL: 10 * time.Minute}, runner, vms,
		repository.NewLBRepository(db), history, &service.VMLocks{}, log.New(io.Discard, "", 0))

	mux := http.NewServeMux()
	RegisterUsers(mux, svc)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, history
}

func TestAC1_UsersListRouteReturnsJSON(t *testing.T) {
	runner := &fakeRunner{results: map[string]service.RunResult{
		"users/list-users.sh": {Stdout: []byte("ana 1001 yes yes\nluis 1002 no no\n")},
	}}
	srv := newUsersTestServer(t, runner)

	status, body := do(t, http.MethodGet, srv.URL+"/api/users/vms/vm1/users", "")

	if status != http.StatusOK {
		t.Fatalf("status = %d, se esperaba 200", status)
	}
	want := []any{
		map[string]any{"username": "ana", "uid": float64(1001), "sudo": true, "ssh_key": true},
		map[string]any{"username": "luis", "uid": float64(1002), "sudo": false, "ssh_key": false},
	}
	if !reflect.DeepEqual(body["users"], want) {
		t.Errorf("users = %v, se esperaba %v", body["users"], want)
	}
}

func TestAC3_UsersListRouteMapsErrors(t *testing.T) {
	tests := []struct {
		name       string
		result     service.RunResult
		path       string
		wantStatus int
		wantError  string
	}{
		{"sin IP", service.RunResult{ExitCode: 7}, "/api/users/vms/vm1/users", http.StatusConflict,
			"La VM no tiene dirección IP; verifique que esté encendida"},
		{"VM desconocida", service.RunResult{}, "/api/users/vms/otra/users", http.StatusNotFound,
			"La VM no existe"},
		{"nombre inválido", service.RunResult{}, "/api/users/vms/VM_1/users", http.StatusBadRequest,
			"El nombre de la VM debe tener de 1 a 32 caracteres: minúsculas, números y guiones, sin empezar con guion"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeRunner{results: map[string]service.RunResult{"users/list-users.sh": tt.result}}
			srv := newUsersTestServer(t, runner)

			status, body := do(t, http.MethodGet, srv.URL+tt.path, "")

			if status != tt.wantStatus || body["error"] != tt.wantError {
				t.Errorf("status %d cuerpo %v, se esperaba %d %q", status, body, tt.wantStatus, tt.wantError)
			}
		})
	}
}

func TestAC4_UsersCreateRouteReturnsCreated(t *testing.T) {
	runner := &fakeRunner{results: map[string]service.RunResult{
		"users/create-user.sh": {Stdout: []byte("-----BEGIN OPENSSH PRIVATE KEY-----\nX\n-----END OPENSSH PRIVATE KEY-----\n")},
	}}
	srv := newUsersTestServer(t, runner)

	status, body := do(t, http.MethodPost, srv.URL+"/api/users/vms/vm1/users",
		`{"username":"ana","sudo":true,"ssh_key":true}`)

	if status != http.StatusCreated {
		t.Fatalf("status = %d, se esperaba 201", status)
	}
	if body["key_available"] != true {
		t.Errorf("key_available = %v, se esperaba true", body["key_available"])
	}
	if msg, _ := body["message"].(string); !strings.HasPrefix(msg, "Usuario ana creado en vm1") {
		t.Errorf("message = %q", msg)
	}
	// La respuesta nunca trae la llave privada.
	if raw, _ := json.Marshal(body); strings.Contains(string(raw), "PRIVATE KEY") {
		t.Errorf("la respuesta contiene la llave privada: %s", raw)
	}
}

func TestAC5_UsersCreateRouteRejectsInvalidInput(t *testing.T) {
	runner := &fakeRunner{results: map[string]service.RunResult{}}
	srv := newUsersTestServer(t, runner)

	status, _ := do(t, http.MethodPost, srv.URL+"/api/users/vms/vm1/users", `{"username":"Ana","ssh_key":true}`)

	if status != http.StatusBadRequest {
		t.Errorf("status = %d, se esperaba 400", status)
	}
	if n := runner.callCount(); n != 0 {
		t.Errorf("el runner se llamó %d veces, se esperaban 0", n)
	}
}

func TestAC6_AC29_AC33_UsersHandlerMapsCreateErrors(t *testing.T) {
	const cleanupStderr = "Limpieza fallida; puede quedar en la VM: cuenta ana, /home/ana, /etc/sudoers.d/stratus-ana\n"
	tests := []struct {
		name       string
		result     service.RunResult
		wantStatus int
		wantError  string
	}{
		{"usuario existente", service.RunResult{ExitCode: 6}, http.StatusConflict, "El usuario ya existe en la VM"},
		{"grupo existente", service.RunResult{ExitCode: 8}, http.StatusConflict, "Ya existe un grupo con ese nombre en la VM"},
		{"limpieza fallida", service.RunResult{ExitCode: 9, Stderr: cleanupStderr}, http.StatusInternalServerError,
			"No se pudo crear el usuario ana y la limpieza falló; revise la VM. " + strings.TrimSpace(cleanupStderr)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeRunner{results: map[string]service.RunResult{"users/create-user.sh": tt.result}}
			srv := newUsersTestServer(t, runner)

			status, body := do(t, http.MethodPost, srv.URL+"/api/users/vms/vm1/users",
				`{"username":"ana","sudo":false,"ssh_key":true}`)

			if status != tt.wantStatus || body["error"] != tt.wantError {
				t.Errorf("status %d cuerpo %v, se esperaba %d %q", status, body, tt.wantStatus, tt.wantError)
			}
		})
	}
}

func TestAC13_UserKeyDownloadHeadersAnd410(t *testing.T) {
	const key = "-----BEGIN OPENSSH PRIVATE KEY-----\nX\n-----END OPENSSH PRIVATE KEY-----\n"
	runner := &fakeRunner{results: map[string]service.RunResult{"users/create-user.sh": {Stdout: []byte(key)}}}
	srv := newUsersTestServer(t, runner)
	if status, _ := do(t, http.MethodPost, srv.URL+"/api/users/vms/vm1/users",
		`{"username":"ana","sudo":false,"ssh_key":true}`); status != http.StatusCreated {
		t.Fatalf("crear: status = %d", status)
	}
	url := srv.URL + "/api/users/vms/vm1/users/ana/key/download"

	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(body) != key {
		t.Errorf("primera descarga: status %d cuerpo %q", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Content-Disposition"); got != `attachment; filename="id_ed25519_vm1_ana"` {
		t.Errorf("Content-Disposition = %q", got)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, se esperaba no-store", got)
	}
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/plain") {
		t.Errorf("Content-Type = %q, se esperaba text/plain", got)
	}

	status, errBody := do(t, http.MethodGet, url, "")
	const want = "La llave ya no está disponible; elimine y vuelva a crear el usuario para obtener una nueva"
	if status != http.StatusGone || errBody["error"] != want {
		t.Errorf("segunda descarga: status %d cuerpo %v, se esperaba 410 %q", status, errBody, want)
	}
}

func TestAC17_UsersHandlerMapsDeleteError(t *testing.T) {
	runner := &fakeRunner{results: map[string]service.RunResult{"users/delete-user.sh": {ExitCode: 6}}}
	srv := newUsersTestServer(t, runner)

	status, body := do(t, http.MethodDelete, srv.URL+"/api/users/vms/vm1/users/root", "")

	const want = "Solo se pueden eliminar usuarios regulares existentes"
	if status != http.StatusConflict || body["error"] != want {
		t.Errorf("status %d cuerpo %v, se esperaba 409 %q", status, body, want)
	}
	if n := runner.callCount(); n != 1 {
		t.Errorf("el runner se llamó %d veces, se esperaba 1", n)
	}
}

func TestAC17_UsersHandlerRejectsInvalidRemoveFiles(t *testing.T) {
	runner := &fakeRunner{results: map[string]service.RunResult{}}
	srv := newUsersTestServer(t, runner)

	for _, value := range []string{"si", "1", "TRUE", ""} {
		status, _ := do(t, http.MethodDelete, srv.URL+"/api/users/vms/vm1/users/luis?remove_files="+value, "")
		if status != http.StatusBadRequest {
			t.Errorf("remove_files=%q: status = %d, se esperaba 400", value, status)
		}
	}
	if n := runner.callCount(); n != 0 {
		t.Errorf("el runner se llamó %d veces, se esperaban 0", n)
	}
}

func TestAC18_UsersDeleteRouteReturnsMessage(t *testing.T) {
	runner := &fakeRunner{results: map[string]service.RunResult{
		"users/delete-user.sh": {Stdout: []byte("Usuario luis eliminado de vm1 junto con todos sus archivos\n")},
	}}
	srv := newUsersTestServer(t, runner)

	// Sin el parámetro, remove_files es false; con true o false se acepta tal cual.
	for _, query := range []string{"", "?remove_files=false", "?remove_files=true"} {
		status, body := do(t, http.MethodDelete, srv.URL+"/api/users/vms/vm1/users/luis"+query, "")
		if status != http.StatusOK || body["message"] != "Usuario luis eliminado de vm1 junto con todos sus archivos" {
			t.Errorf("%q: status %d cuerpo %v", query, status, body)
		}
	}
}

func TestAC26_UsersHistoryRouteReturnsModuleRows(t *testing.T) {
	runner := &fakeRunner{results: map[string]service.RunResult{
		"users/delete-user.sh":    {ExitCode: 5, Stderr: "No se pudieron terminar los procesos de luis\n"},
		"prepare/set-hostname.sh": {},
	}}
	srv := newUsersTestServer(t, runner)
	// Una operación de users fallida deja una fila; una de prepare no debe aparecer.
	do(t, http.MethodDelete, srv.URL+"/api/users/vms/vm1/users/luis", "")
	if _, err := runner.Run(context.Background(), "prepare/set-hostname.sh"); err != nil {
		t.Fatal(err)
	}

	status, body := do(t, http.MethodGet, srv.URL+"/api/users/history", "")

	if status != http.StatusOK {
		t.Fatalf("status = %d, se esperaba 200", status)
	}
	rows, _ := body["operations"].([]any)
	if len(rows) != 1 {
		t.Fatalf("operations = %v, se esperaba una fila de users", body["operations"])
	}
	row, _ := rows[0].(map[string]any)
	if row["operation"] != "delete_user" || row["vm"] != "vm1" || row["exit_code"] != float64(5) ||
		row["stderr"] != "No se pudieron terminar los procesos de luis" || row["result"] != "error" ||
		row["command"] != "users/delete-user.sh" {
		t.Errorf("fila = %v", row)
	}
	for _, key := range []string{"id", "args", "created_at"} {
		if _, ok := row[key]; !ok {
			t.Errorf("a la fila le falta el campo snake_case %q: %v", key, row)
		}
	}
}

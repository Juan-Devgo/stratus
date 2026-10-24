package handler

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"stratus-srv/internal/repository"
	"stratus-srv/internal/service"
)

// fakeRunner responde con el resultado preparado para cada script y cuenta las llamadas.
// Si block no es nil, Run avisa por started y espera a que se cierre block.
type fakeRunner struct {
	mu      sync.Mutex
	calls   int
	results map[string]service.RunResult
	block   chan struct{}
	started chan struct{}
}

func (f *fakeRunner) Run(_ context.Context, script string, _ ...string) (service.RunResult, error) {
	f.mu.Lock()
	f.calls++
	res := f.results[script]
	block, started := f.block, f.started
	f.mu.Unlock()

	if block != nil {
		started <- struct{}{}
		<-block
	}
	return res, nil
}

func (f *fakeRunner) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// testServer agrupa el servidor de pruebas con los repositorios que usan las pruebas.
type testServer struct {
	*httptest.Server
	vms *repository.VMRepository
	lbs *repository.LBRepository
}

// newTestServer arma el handler con el servicio real, SQLite temporal y el runner falso.
func newTestServer(t *testing.T, runner service.ScriptRunner) (*httptest.Server, *repository.VMRepository) {
	t.Helper()
	ts := newTestServerWithLBs(t, runner)
	return ts.Server, ts.vms
}

// newTestServerWithLBs es newTestServer más el repositorio de balanceadores; registra también las
// rutas de /api/haproxy.
func newTestServerWithLBs(t *testing.T, runner service.ScriptRunner) testServer {
	t.Helper()
	db, err := repository.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	vms := repository.NewVMRepository(db)
	lbs := repository.NewLBRepository(db)
	history := repository.NewHistoryRepository(db)
	locks := &service.VMLocks{}
	logger := log.New(io.Discard, "", 0)
	haproxy := service.NewHAProxyService(service.HAProxyConfig{BaseDisk: "/vms/haproxy-base.vdi", BridgeAdapter: "eth0"},
		runner, vms, lbs, history, locks, logger)
	cfg := service.Config{BaseDisk: "/vms/base.vdi", BridgeAdapter: "eth0", KeyTTL: 10 * time.Minute}
	svc := service.NewPrepareService(cfg, runner, vms, lbs, history, locks, haproxy, logger)

	mux := http.NewServeMux()
	RegisterPrepare(mux, svc)
	RegisterHAProxy(mux, haproxy)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return testServer{Server: srv, vms: vms, lbs: lbs}
}

func reserve(t *testing.T, vms *repository.VMRepository, name string) {
	t.Helper()
	if _, err := vms.Insert(context.Background(), name); err != nil {
		t.Fatalf("Insert(%q): %v", name, err)
	}
}

// do envía la petición y decodifica el cuerpo JSON en un mapa.
func do(t *testing.T, method, url, body string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, se esperaba application/json", ct)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("respuesta no es JSON: %v", err)
	}
	return resp.StatusCode, out
}

func TestAC1_PostVMsReturnsCreated(t *testing.T) {
	const line = "VM vm1 creada y encendida; IP 192.168.1.50"
	runner := &fakeRunner{results: map[string]service.RunResult{
		"prepare/create-vm.sh": {Stdout: []byte(line + "\n")},
	}}
	srv, _ := newTestServer(t, runner)

	status, body := do(t, http.MethodPost, srv.URL+"/api/prepare/vms",
		`{"name":"vm1","memory_mb":2048,"cpus":2}`)

	if status != http.StatusCreated {
		t.Errorf("status = %d, se esperaba 201", status)
	}
	if msg, _ := body["message"].(string); msg != line {
		t.Errorf("cuerpo = %v, se esperaba el mensaje %q", body, line)
	}
	if n := runner.callCount(); n != 1 {
		t.Errorf("el runner se llamó %d veces, se esperaba 1", n)
	}
}

func TestAC3_GetVMsReturnsStates(t *testing.T) {
	runner := &fakeRunner{results: map[string]service.RunResult{
		"prepare/list-vms.sh": {Stdout: []byte("vm1 running 192.168.1.50\nvm2 poweroff -\n")},
	}}
	srv, vms := newTestServer(t, runner)
	reserve(t, vms, "vm1")
	reserve(t, vms, "vm2")

	status, body := do(t, http.MethodGet, srv.URL+"/api/prepare/vms", "")

	if status != http.StatusOK {
		t.Fatalf("status = %d, se esperaba 200", status)
	}
	if listErr, ok := body["list_error"].(string); !ok || listErr != "" {
		t.Errorf("list_error = %v, se esperaba una cadena vacía", body["list_error"])
	}
	list, _ := body["vms"].([]any)
	if len(list) != 2 {
		t.Fatalf("vms = %v, se esperaban 2", body["vms"])
	}
	first, _ := list[0].(map[string]any)
	want := map[string]any{"name": "vm1", "state": "running", "ip": "192.168.1.50", "user_key_generated": false,
		"role": "vm", "lb": "", "lb_ip": ""}
	if len(first) != len(want) {
		t.Errorf("vms[0] = %v, se esperaban exactamente los campos %v", first, want)
	}
	for key, value := range want {
		if first[key] != value {
			t.Errorf("vms[0].%s = %v, se esperaba %v", key, first[key], value)
		}
	}
	if second, _ := list[1].(map[string]any); second["state"] != "poweroff" || second["ip"] != "" {
		t.Errorf("vms[1] = %v, se esperaba poweroff sin IP", second)
	}
}

func TestAC19_BusyVMReturns409(t *testing.T) {
	runner := &fakeRunner{block: make(chan struct{}), started: make(chan struct{}, 1)}
	srv, vms := newTestServer(t, runner)
	reserve(t, vms, "vm1")

	firstStatus := make(chan int, 1)
	go func() {
		resp, err := http.Post(srv.URL+"/api/prepare/vms/vm1/start", "application/json", nil)
		if err != nil {
			firstStatus <- 0
			return
		}
		resp.Body.Close()
		firstStatus <- resp.StatusCode
	}()
	<-runner.started

	status, body := do(t, http.MethodPost, srv.URL+"/api/prepare/vms/vm1/stop", "")
	if status != http.StatusConflict {
		t.Errorf("status = %d, se esperaba 409", status)
	}
	if body["error"] != "Operación en curso en esta VM" {
		t.Errorf("error = %v, se esperaba \"Operación en curso en esta VM\"", body["error"])
	}

	close(runner.block)
	if s := <-firstStatus; s != http.StatusOK {
		t.Errorf("primera petición: status = %d, se esperaba 200", s)
	}
}

func TestAC46_ConcurrentCreateReturns409(t *testing.T) {
	runner := &fakeRunner{block: make(chan struct{}), started: make(chan struct{}, 1)}
	srv, _ := newTestServer(t, runner)

	firstStatus := make(chan int, 1)
	go func() {
		resp, err := http.Post(srv.URL+"/api/prepare/vms", "application/json",
			strings.NewReader(`{"name":"vm1","memory_mb":2048,"cpus":2}`))
		if err != nil {
			firstStatus <- 0
			return
		}
		resp.Body.Close()
		firstStatus <- resp.StatusCode
	}()
	<-runner.started

	status, body := do(t, http.MethodPost, srv.URL+"/api/prepare/vms", `{"name":"vm2","memory_mb":2048,"cpus":2}`)
	if status != http.StatusConflict {
		t.Errorf("status = %d, se esperaba 409", status)
	}
	if body["error"] != "Ya hay una creación de VM en curso" {
		t.Errorf("error = %v, se esperaba \"Ya hay una creación de VM en curso\"", body["error"])
	}

	close(runner.block)
	if s := <-firstStatus; s != http.StatusCreated {
		t.Errorf("primera petición: status = %d, se esperaba 201", s)
	}
}

func TestAC47_PostStartReturnsMessage(t *testing.T) {
	runner := &fakeRunner{}
	srv, vms := newTestServer(t, runner)
	reserve(t, vms, "vm1")

	status, body := do(t, http.MethodPost, srv.URL+"/api/prepare/vms/vm1/start", "")

	if status != http.StatusOK {
		t.Errorf("status = %d, se esperaba 200", status)
	}
	if body["message"] != "VM vm1 encendida" {
		t.Errorf("cuerpo = %v, se esperaba el mensaje \"VM vm1 encendida\"", body)
	}
	if n := runner.callCount(); n != 1 {
		t.Errorf("el runner se llamó %d veces, se esperaba 1", n)
	}

	// Un script que falla (VM ausente en VirtualBox, código 3) se muestra como fallo.
	runner2 := &fakeRunner{results: map[string]service.RunResult{
		"prepare/start-vm.sh": {ExitCode: 3, Stderr: "VM no encontrada\n"},
	}}
	srv2, vms2 := newTestServer(t, runner2)
	reserve(t, vms2, "vm1")
	status, body = do(t, http.MethodPost, srv2.URL+"/api/prepare/vms/vm1/start", "")
	if status != http.StatusInternalServerError {
		t.Errorf("status = %d, se esperaba 500", status)
	}
	if msg, _ := body["error"].(string); !strings.Contains(msg, "VM no encontrada") {
		t.Errorf("error = %v, se esperaba el detalle \"VM no encontrada\"", body["error"])
	}
}

func TestAC20_TimeoutReturns504(t *testing.T) {
	runner := &fakeRunner{results: map[string]service.RunResult{
		"prepare/start-vm.sh": {ExitCode: -1, TimedOut: true},
	}}
	srv, vms := newTestServer(t, runner)
	reserve(t, vms, "vm1")

	status, body := do(t, http.MethodPost, srv.URL+"/api/prepare/vms/vm1/start", "")

	if status != http.StatusGatewayTimeout {
		t.Errorf("status = %d, se esperaba 504", status)
	}
	if msg, _ := body["error"].(string); !strings.Contains(msg, "tiempo límite de 15 minutos") {
		t.Errorf("error = %v, se esperaba el mensaje de timeout", body["error"])
	}
}

func TestAC29_DuplicateReturns409(t *testing.T) {
	runner := &fakeRunner{}
	srv, vms := newTestServer(t, runner)
	reserve(t, vms, "vm1")

	status, body := do(t, http.MethodPost, srv.URL+"/api/prepare/vms",
		`{"name":"vm1","memory_mb":2048,"cpus":2}`)

	if status != http.StatusConflict {
		t.Errorf("status = %d, se esperaba 409", status)
	}
	if body["error"] != "Ya existe una VM con ese nombre" {
		t.Errorf("error = %v, se esperaba \"Ya existe una VM con ese nombre\"", body["error"])
	}
	if n := runner.callCount(); n != 0 {
		t.Errorf("el runner se llamó %d veces, se esperaba 0", n)
	}
}

func TestAC37_PlaceholderReturns500Message(t *testing.T) {
	runner := &fakeRunner{results: map[string]service.RunResult{
		"prepare/start-vm.sh": {ExitCode: 99, Stderr: "Operación no implementada\n"},
	}}
	srv, vms := newTestServer(t, runner)
	reserve(t, vms, "vm1")

	status, body := do(t, http.MethodPost, srv.URL+"/api/prepare/vms/vm1/start", "")

	if status != http.StatusInternalServerError {
		t.Errorf("status = %d, se esperaba 500", status)
	}
	msg, _ := body["error"].(string)
	if !strings.Contains(msg, "código 99") || !strings.Contains(msg, "Operación no implementada") {
		t.Errorf("error = %q, se esperaba el código 99 y el detalle", msg)
	}
	for _, trace := range []string{"goroutine", "panic", ".go:"} {
		if strings.Contains(msg, trace) {
			t.Errorf("error = %q, no debe contener rastros de pila", msg)
		}
	}
}

func TestAC7_InvalidPackageReturns400(t *testing.T) {
	runner := &fakeRunner{}
	srv, vms := newTestServer(t, runner)
	reserve(t, vms, "vm1")

	for _, action := range []string{"install-package", "remove-package"} {
		t.Run(action, func(t *testing.T) {
			status, body := do(t, http.MethodPost, srv.URL+"/api/prepare/vms/vm1/"+action,
				`{"package":"nginx; rm -rf /"}`)

			if status != http.StatusBadRequest {
				t.Errorf("status = %d, se esperaba 400", status)
			}
			if msg, _ := body["error"].(string); msg == "" {
				t.Errorf("cuerpo = %v, se esperaba un \"error\"", body)
			}
		})
	}
	if n := runner.callCount(); n != 0 {
		t.Errorf("el runner se llamó %d veces, se esperaba 0", n)
	}
}

func TestAC33_RemoveOpenSSHReturns409(t *testing.T) {
	runner := &fakeRunner{}
	srv, vms := newTestServer(t, runner)
	reserve(t, vms, "vm1") // VM recién creada

	status, body := do(t, http.MethodPost, srv.URL+"/api/prepare/vms/vm1/remove-package",
		`{"package":"openssh-server"}`)

	if status != http.StatusConflict {
		t.Errorf("status = %d, se esperaba 409", status)
	}
	if body["error"] != "No se puede eliminar openssh-server: el backend lo usa para conectarse" {
		t.Errorf("error = %v, se esperaba el mensaje de REQ-25", body["error"])
	}
	if n := runner.callCount(); n != 0 {
		t.Errorf("el runner se llamó %d veces, se esperaba 0", n)
	}
}

func TestAC43_NoIPReturns409(t *testing.T) {
	runner := &fakeRunner{results: map[string]service.RunResult{
		"prepare/install-package.sh": {ExitCode: 7, Stderr: "La VM no tiene dirección IP; verifique que esté encendida\n"},
	}}
	srv, vms := newTestServer(t, runner)
	reserve(t, vms, "vm1")

	status, body := do(t, http.MethodPost, srv.URL+"/api/prepare/vms/vm1/install-package", `{"package":"nginx"}`)

	if status != http.StatusConflict {
		t.Errorf("status = %d, se esperaba 409", status)
	}
	if body["error"] != "La VM no tiene dirección IP; verifique que esté encendida" {
		t.Errorf("error = %v, se esperaba el mensaje de REQ-29", body["error"])
	}
}

func TestAC41_BaseDiskMissingReturnsMessage(t *testing.T) {
	runner := &fakeRunner{results: map[string]service.RunResult{
		"prepare/create-vm.sh": {ExitCode: 6, Stderr: "Disco base no encontrado: /vms/base.vdi\n"},
	}}
	srv, _ := newTestServer(t, runner)

	status, body := do(t, http.MethodPost, srv.URL+"/api/prepare/vms", `{"name":"vm1","memory_mb":2048,"cpus":2}`)

	if status != http.StatusInternalServerError {
		t.Errorf("status = %d, se esperaba 500", status)
	}
	if msg, _ := body["error"].(string); !strings.Contains(msg, "Disco base no encontrado") {
		t.Errorf("error = %q, se esperaba que contuviera \"Disco base no encontrado\"", msg)
	}
}

func TestAC6_GetCatalogReturnsPackages(t *testing.T) {
	srv, _ := newTestServer(t, &fakeRunner{})

	status, body := do(t, http.MethodGet, srv.URL+"/api/prepare/catalog", "")

	if status != http.StatusOK {
		t.Fatalf("status = %d, se esperaba 200", status)
	}
	list, _ := body["packages"].([]any)
	if len(list) != len(service.PackageCatalog) {
		t.Fatalf("packages = %v, se esperaba %v", body["packages"], service.PackageCatalog)
	}
	for i, pkg := range service.PackageCatalog {
		if list[i] != pkg {
			t.Errorf("packages[%d] = %v, se esperaba %q", i, list[i], pkg)
		}
	}
}

func TestAC10_SecondDownloadReturns410(t *testing.T) {
	key := "-----BEGIN OPENSSH PRIVATE KEY-----\nclave-de-prueba\n-----END OPENSSH PRIVATE KEY-----\n"
	runner := &fakeRunner{results: map[string]service.RunResult{
		"prepare/generate-user-key.sh": {Stdout: []byte(key)},
	}}
	srv, vms := newTestServer(t, runner)
	reserve(t, vms, "vm1")
	downloadURL := srv.URL + "/api/prepare/vms/vm1/user-key/download"

	status, body := do(t, http.MethodPost, srv.URL+"/api/prepare/vms/vm1/user-key", "")
	if status != http.StatusOK {
		t.Fatalf("generar: status = %d, cuerpo = %v, se esperaba 200", status, body)
	}
	if msg, _ := body["message"].(string); msg == "" || strings.Contains(msg, "PRIVATE KEY") {
		t.Errorf("generar: cuerpo = %v, se esperaba un mensaje sin la llave", body)
	}

	// Primera descarga: el archivo de la llave como adjunto.
	resp, err := http.Get(downloadURL)
	if err != nil {
		t.Fatalf("primera descarga: %v", err)
	}
	got, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("primera descarga: status = %d, se esperaba 200", resp.StatusCode)
	}
	if string(got) != key {
		t.Errorf("primera descarga = %q, se esperaba la llave", got)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q, se esperaba text/plain", ct)
	}
	if cd := resp.Header.Get("Content-Disposition"); cd != `attachment; filename="id_ed25519_vm1"` {
		t.Errorf("Content-Disposition = %q", cd)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, se esperaba no-store", cc)
	}

	// Segunda descarga: ya no está disponible.
	status, body = do(t, http.MethodGet, downloadURL, "")
	if status != http.StatusGone {
		t.Errorf("segunda descarga: status = %d, se esperaba 410", status)
	}
	if body["error"] != "La llave ya no está disponible, genere una nueva" {
		t.Errorf("segunda descarga: cuerpo = %v", body)
	}
}

func TestAC17_PostHostnameReturnsMessage(t *testing.T) {
	runner := &fakeRunner{}
	srv, vms := newTestServer(t, runner)
	reserve(t, vms, "vm1")

	status, body := do(t, http.MethodPost, srv.URL+"/api/prepare/vms/vm1/hostname", `{"hostname":"web-01"}`)

	if status != http.StatusOK {
		t.Fatalf("status = %d, se esperaba 200 (cuerpo %v)", status, body)
	}
	if msg, _ := body["message"].(string); !strings.Contains(msg, "web-01") {
		t.Errorf("message = %v, se esperaba que nombrara web-01", body["message"])
	}
	if n := runner.callCount(); n != 1 {
		t.Errorf("el runner se llamó %d veces, se esperaba 1", n)
	}
}

func TestAC18_InvalidHostnameReturns400(t *testing.T) {
	runner := &fakeRunner{}
	srv, vms := newTestServer(t, runner)
	reserve(t, vms, "vm1")

	status, body := do(t, http.MethodPost, srv.URL+"/api/prepare/vms/vm1/hostname", `{"hostname":"Web_01"}`)

	if status != http.StatusBadRequest {
		t.Errorf("status = %d, se esperaba 400", status)
	}
	if msg, _ := body["error"].(string); msg == "" {
		t.Errorf("cuerpo = %v, se esperaba un \"error\"", body)
	}
	if n := runner.callCount(); n != 0 {
		t.Errorf("el runner se llamó %d veces, se esperaba 0", n)
	}
}

func TestAC14_PostIssueReturnsMessage(t *testing.T) {
	runner := &fakeRunner{}
	srv, vms := newTestServer(t, runner)
	reserve(t, vms, "vm1")

	status, body := do(t, http.MethodPost, srv.URL+"/api/prepare/vms/vm1/issue",
		`{"message":"Hola","fields":["users","hostname"]}`)

	if status != http.StatusOK {
		t.Fatalf("status = %d, se esperaba 200 (cuerpo %v)", status, body)
	}
	if msg, _ := body["message"].(string); !strings.Contains(msg, "/etc/issue") {
		t.Errorf("message = %v, se esperaba que nombrara /etc/issue", body["message"])
	}
	if n := runner.callCount(); n != 1 {
		t.Errorf("el runner se llamó %d veces, se esperaba 1", n)
	}
}

func TestAC25_BackslashIssueReturns400(t *testing.T) {
	runner := &fakeRunner{}
	srv, vms := newTestServer(t, runner)
	reserve(t, vms, "vm1")

	status, body := do(t, http.MethodPost, srv.URL+"/api/prepare/vms/vm1/issue", `{"message":"a\\b","fields":[]}`)

	if status != http.StatusBadRequest {
		t.Errorf("status = %d, se esperaba 400", status)
	}
	if msg, _ := body["error"].(string); msg == "" {
		t.Errorf("cuerpo = %v, se esperaba un \"error\"", body)
	}
	if n := runner.callCount(); n != 0 {
		t.Errorf("el runner se llamó %d veces, se esperaba 0", n)
	}
}

func TestAC22_GetHistoryReturnsRows(t *testing.T) {
	runner := &fakeRunner{results: map[string]service.RunResult{
		"prepare/stop-vm.sh": {ExitCode: 5, Stderr: "boom\n"},
	}}
	srv, vms := newTestServer(t, runner)
	reserve(t, vms, "vm1")

	// Las 5 primeras operaciones son stop_vm y las 50 siguientes start_vm: solo
	// las 50 más recientes deben aparecer, así que ninguna stop_vm debe salir.
	for i := range 55 {
		action := "start"
		if i < 5 {
			action = "stop"
		}
		do(t, http.MethodPost, srv.URL+"/api/prepare/vms/vm1/"+action, "")
	}

	status, body := do(t, http.MethodGet, srv.URL+"/api/prepare/history", "")

	if status != http.StatusOK {
		t.Fatalf("status = %d, se esperaba 200", status)
	}
	ops, _ := body["operations"].([]any)
	if len(ops) != 50 {
		t.Fatalf("operations tiene %d filas, se esperaban 50", len(ops))
	}
	prevID := float64(1 << 30)
	for i, item := range ops {
		row, _ := item.(map[string]any)
		id, _ := row["id"].(float64)
		if id >= prevID {
			t.Errorf("operations[%d].id = %v, se esperaba menor que %v (más reciente primero)", i, id, prevID)
		}
		prevID = id
		if row["operation"] != "start_vm" {
			t.Errorf("operations[%d].operation = %v, se esperaba start_vm", i, row["operation"])
		}
	}

	first, _ := ops[0].(map[string]any)
	want := map[string]any{"id": float64(55), "vm": "vm1", "command": "prepare/start-vm.sh",
		"exit_code": float64(0), "stderr": "", "result": "ok"}
	for key, value := range want {
		if first[key] != value {
			t.Errorf("operations[0].%s = %v, se esperaba %v", key, first[key], value)
		}
	}
	if args, _ := first["args"].([]any); len(args) != 1 || args[0] != "vm1" {
		t.Errorf("operations[0].args = %v, se esperaba [vm1]", first["args"])
	}
	if created, _ := first["created_at"].(string); created == "" {
		t.Errorf("operations[0].created_at = %v, se esperaba una fecha", first["created_at"])
	}
}

// AC-19: eliminar desde 001 una VM asignada a un balanceador devuelve 409 con el texto de la spec.
func TestAC19_DeleteAssignedVMReturnsConflict(t *testing.T) {
	runner := &fakeRunner{}
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

	status, body := do(t, http.MethodDelete, ts.URL+"/api/prepare/vms/web-a", "")

	if status != http.StatusConflict {
		t.Errorf("status = %d, se esperaba 409", status)
	}
	if msg, _ := body["error"].(string); msg != "La VM es servidor de un balanceador; quítela primero" {
		t.Errorf("error = %q", msg)
	}
	if n := runner.callCount(); n != 0 {
		t.Errorf("el runner se llamó %d veces, se esperaban 0", n)
	}
}

// AC-41: encender un servidor devuelve {message} y, si el refresco falló, también {warning}.
func TestAC41_StartRouteReturnsWarning(t *testing.T) {
	runner := &fakeRunner{results: map[string]service.RunResult{
		"haproxy/refresh-lb.sh": {ExitCode: 12, Stderr: "El balanceador lb1 está apagado; enciéndalo para llegar al servidor\n"},
	}}
	ts := newTestServerWithLBs(t, runner)
	ctx := context.Background()
	reserve(t, ts.vms, "lb1")
	reserve(t, ts.vms, "web-a")
	reserve(t, ts.vms, "vm1")
	if err := ts.lbs.InsertLB(ctx, repository.LoadBalancer{Name: "lb1", Subnet: 1, FrontendPort: 80}); err != nil {
		t.Fatal(err)
	}
	if err := ts.lbs.InsertServer(ctx, repository.LBServer{LB: "lb1", Name: "server1", VM: "web-a", Port: 80,
		LastIP: "10.10.1.11", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	status, body := do(t, http.MethodPost, ts.URL+"/api/prepare/vms/web-a/start", "")
	if status != http.StatusOK {
		t.Fatalf("status = %d, se esperaba 200", status)
	}
	if body["message"] != "VM web-a encendida" {
		t.Errorf("message = %v", body["message"])
	}
	if body["warning"] != "VM iniciada; no se pudieron actualizar las IPs del balanceador lb1, use Actualizar IPs" {
		t.Errorf("warning = %v", body["warning"])
	}

	// Una VM común no trae advertencia: el campo no aparece.
	status, body = do(t, http.MethodPost, ts.URL+"/api/prepare/vms/vm1/start", "")
	if status != http.StatusOK || body["message"] != "VM vm1 encendida" {
		t.Errorf("VM común: status = %d, body = %v", status, body)
	}
	if _, present := body["warning"]; present {
		t.Errorf("la VM común no debe traer warning: %v", body)
	}
}

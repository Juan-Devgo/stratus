package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"stratus-srv/internal/repository"
)

// servicesEnv agrupa el servicio de servicios con sus dependencias reales (SQLite temporal).
type servicesEnv struct {
	svc     *ServicesService
	vms     *repository.VMRepository
	lbs     *repository.LBRepository
	history *repository.HistoryRepository
	logs    *bytes.Buffer
}

// newServicesEnv crea un ServicesService con su propio VMLocks y un máximo de archivo de 1 MiB.
// Una prueba de integración con otros módulos usa newServicesEnvWithLocks para compartir el bloqueo.
func newServicesEnv(t *testing.T, runner ScriptRunner) servicesEnv {
	t.Helper()
	return newServicesEnvWithLocks(t, runner, &VMLocks{})
}

func newServicesEnvWithLocks(t *testing.T, runner ScriptRunner, locks *VMLocks) servicesEnv {
	t.Helper()
	db, err := repository.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	env := servicesEnv{
		vms:     repository.NewVMRepository(db),
		lbs:     repository.NewLBRepository(db),
		history: repository.NewHistoryRepository(db),
		logs:    &bytes.Buffer{},
	}
	env.svc = NewServicesService(ServicesConfig{MaxArchiveBytes: 1 << 20}, runner, env.vms, env.lbs, env.history,
		locks, log.New(env.logs, "", 0))
	return env
}

// register registra una VM directamente en SQLite, sin pasar por el script.
func (e servicesEnv) register(t *testing.T, name string) {
	t.Helper()
	if _, err := e.vms.Insert(context.Background(), name); err != nil {
		t.Fatalf("Insert(%q): %v", name, err)
	}
}

func TestAC15_GetVMValidatesNameAndFindsRegisteredVM(t *testing.T) {
	env := newServicesEnv(t, &fakeRunner{})
	env.register(t, "vm1")

	vm, err := env.svc.getVM(context.Background(), "vm1")
	if err != nil || vm.Name != "vm1" {
		t.Errorf("getVM(vm1) = %+v, %v", vm, err)
	}
	if _, err := env.svc.getVM(context.Background(), "vm2"); !errors.Is(err, ErrVMNotFound) {
		t.Errorf("getVM(vm2) = %v, se esperaba ErrVMNotFound", err)
	}
	if _, err := env.svc.getVM(context.Background(), "VM_1"); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("getVM(VM_1) = %v, se esperaba ErrInvalidInput", err)
	}
	if got := env.svc.MaxArchiveBytes(); got != 1<<20 {
		t.Errorf("MaxArchiveBytes = %d, se esperaba %d", got, 1<<20)
	}
}

func TestAC4_MapServiceErrorTranslatesScriptCodes(t *testing.T) {
	op := func(code int) error {
		return fmt.Errorf("envuelto: %w", &OperationError{Operation: "x", ExitCode: code, Detail: "detalle"})
	}
	tests := []struct {
		name      string
		operation string
		err       error
		wantIs    error
	}{
		{"6 no administrado", "start", op(6), ErrNotManagedService},
		{"8 nombre en uso", "create", op(8), ErrServiceExists},
		{"10 archivo inválido", "create", op(10), ErrInvalidArchive},
		{"11 ejecutable ausente", "redeploy", op(11), ErrExecutableMissing},
		{"5 sigue siendo OperationError", "create", op(5), ErrScriptFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapServiceError(tt.operation, "api", tt.err)
			if !errors.Is(got, tt.wantIs) {
				t.Errorf("mapServiceError = %v, se esperaba %v", got, tt.wantIs)
			}
		})
	}

	t.Run("5 conserva el OperationError", func(t *testing.T) {
		var opErr *OperationError
		if !errors.As(mapServiceError("create", "api", op(5)), &opErr) || opErr.ExitCode != 5 {
			t.Errorf("se esperaba el *OperationError con código 5")
		}
	})
}

func TestAC13_MapServiceErrorRollbackFailure(t *testing.T) {
	opErr := &OperationError{Operation: "x", ExitCode: 9, Detail: "Limpieza fallida; puede quedar en la VM: api"}
	for _, operation := range []string{"create", "edit", "redeploy"} {
		t.Run(operation, func(t *testing.T) {
			var rollback *ServiceRollbackError
			got := mapServiceError(operation, "api", opErr)
			if !errors.As(got, &rollback) {
				t.Fatalf("mapServiceError = %v, se esperaba *ServiceRollbackError", got)
			}
			if rollback.Operation != operation || rollback.Name != "api" || rollback.Detail != opErr.Detail {
				t.Errorf("rollback = %+v", rollback)
			}
		})
	}

	// Una operación sin restauración no usa el código 9: el error pasa igual.
	if got := mapServiceError("delete", "api", opErr); got != error(opErr) {
		t.Errorf("mapServiceError(delete) = %v, se esperaba el OperationError original", got)
	}
}

func TestAC4_MapServiceErrorLeavesOtherErrors(t *testing.T) {
	for _, err := range []error{nil, ErrVMNoIP, ErrOperationInProgress, ErrTimeout, errors.New("otro")} {
		if got := mapServiceError("create", "api", err); got != err {
			t.Errorf("mapServiceError(%v) = %v, se esperaba el mismo error", err, got)
		}
	}
}

func TestAC14_ParseResultReadsMessageAndActiveState(t *testing.T) {
	tests := []struct {
		name   string
		stdout string
		want   ServiceResult
	}{
		{"mensaje y estado", "Servicio demian-lee creado en vm1\nactive_state=active\n", ServiceResult{"Servicio demian-lee creado en vm1", "active"}},
		{"estado con subestado", "Servicio api iniciado en vm1\nactive_state=activating (auto-restart)\n", ServiceResult{"Servicio api iniciado en vm1", "activating (auto-restart)"}},
		{"solo mensaje", "Servicio api detenido en vm1\n", ServiceResult{Message: "Servicio api detenido en vm1"}},
		{"sin salto final ni espacios", "  Servicio api eliminado de vm1  ", ServiceResult{Message: "Servicio api eliminado de vm1"}},
		{"vacío", "", ServiceResult{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseResult([]byte(tt.stdout)); got != tt.want {
				t.Errorf("parseResult = %+v, se esperaba %+v", got, tt.want)
			}
		})
	}
}

// serviceCall es una operación del módulo con su script, su nombre en el historial y cómo invocarla.
type serviceCall struct {
	name      string
	script    string
	operation string
	existing  bool // actúa sobre un servicio existente: el exit 6 la rechaza (REQ-18)
	recorded  bool // se registra siempre, no solo al fallar (REQ-27)
	call      func(*ServicesService) error
}

// allServiceCalls devuelve las 12 operaciones del módulo con entradas válidas del AC-1. Quien las
// usa debe llamar antes a useTempDir: create y redeploy guardan archivos temporales.
func allServiceCalls() []serviceCall {
	ctx := context.Background()
	calls := []serviceCall{
		{"list", "services/list-services.sh", "list_services", false, false, func(s *ServicesService) error {
			_, err := s.ListServices(ctx, "vm1")
			return err
		}},
		{"create", "services/create-service.sh", "create_service", false, true, func(s *ServicesService) error {
			_, err := s.CreateService(ctx, "vm1", validCreateInput(), tarGzUpload("x"))
			return err
		}},
		{"edit", "services/edit-service.sh", "edit_service", true, true, func(s *ServicesService) error {
			in := validEditInput()
			in.ReplaceEnv = true
			in.Env = []EnvVar{{"ADMIN_PASSWORD", "Secreta123"}}
			_, err := s.EditService(ctx, "vm1", "demian-lee", in)
			return err
		}},
		{"redeploy", "services/redeploy-service.sh", "redeploy_service", true, true, func(s *ServicesService) error {
			_, err := s.RedeployService(ctx, "vm1", "demian-lee", "demian-lee", tarGzUpload("x"))
			return err
		}},
		{"delete", "services/delete-service.sh", "delete_service", true, true, func(s *ServicesService) error {
			_, err := s.DeleteService(ctx, "vm1", "demian-lee")
			return err
		}},
		{"status", "services/status-service.sh", "service_status", true, false, func(s *ServicesService) error {
			_, err := s.ServiceStatus(ctx, "vm1", "demian-lee")
			return err
		}},
		{"logs", "services/logs-service.sh", "service_logs", true, false, func(s *ServicesService) error {
			_, err := s.ServiceLogs(ctx, "vm1", "demian-lee")
			return err
		}},
	}
	for _, action := range serviceActions {
		calls = append(calls, serviceCall{action, "services/" + action + "-service.sh", action + "_service", true, true,
			func(s *ServicesService) error {
				_, err := s.ServiceAction(ctx, "vm1", "demian-lee", action)
				return err
			}})
	}
	return calls
}

func TestAC17_ServicesOperationsMapNoIP(t *testing.T) {
	for _, c := range allServiceCalls() {
		t.Run(c.name, func(t *testing.T) {
			useTempDir(t)
			runner := &fakeRunner{results: map[string]RunResult{c.script: {ExitCode: 7,
				Stderr: "La VM no tiene dirección IP; verifique que esté encendida\n"}}}
			env := newServicesEnv(t, runner)
			env.register(t, "vm1")

			err := c.call(env.svc)

			if !errors.Is(err, ErrVMNoIP) {
				t.Errorf("err = %v, se esperaba ErrVMNoIP", err)
			}
			ops := env.operations(t)
			if len(ops) != 1 || ops[0].Operation != c.operation || ops[0].ExitCode != 7 || ops[0].Result != "error" {
				t.Errorf("historial = %+v, se esperaba una fila %s con código 7", ops, c.operation)
			}
		})
	}
}

func TestAC20_NotManagedMapsError(t *testing.T) {
	for _, c := range allServiceCalls() {
		if !c.existing {
			continue
		}
		t.Run(c.name, func(t *testing.T) {
			useTempDir(t)
			runner := &fakeRunner{results: map[string]RunResult{c.script: {ExitCode: 6,
				Stderr: "Solo se pueden gestionar servicios creados por Stratus\n"}}}
			env := newServicesEnv(t, runner)
			env.register(t, "vm1")

			err := c.call(env.svc)

			if !errors.Is(err, ErrNotManagedService) {
				t.Errorf("err = %v, se esperaba ErrNotManagedService", err)
			}
		})
	}
}

// AC-29: una operación de cualquier módulo en curso en la VM rechaza a las demás, sin llamar al script.
func TestAC29_ServicesRejectedWhileOtherModuleRuns(t *testing.T) {
	t.Run("prepare bloquea a servicios", func(t *testing.T) {
		useTempDir(t)
		locks := &VMLocks{}
		runner := &fakeRunner{results: map[string]RunResult{}, block: make(chan struct{}),
			started: make(chan struct{}, 1), blockOnly: "prepare/install-package.sh"}
		env := newServicesEnvWithLocks(t, runner, locks)
		env.register(t, "vm1")
		prepare := newPrepareForTest(t, runner, env.vms, env.history, locks, env.svc.logger)

		done := make(chan error, 1)
		go func() {
			_, err := prepare.InstallPackage(context.Background(), "vm1", "htop")
			done <- err
		}()
		<-runner.started

		for _, c := range allServiceCalls() {
			if err := c.call(env.svc); !errors.Is(err, ErrOperationInProgress) {
				t.Errorf("%s: err = %v, se esperaba ErrOperationInProgress", c.name, err)
			}
		}
		if n := runner.callCount(); n != 1 {
			t.Errorf("el runner se llamó %d veces, se esperaba 1 (solo la de prepare)", n)
		}
		close(runner.block)
		if err := <-done; err != nil {
			t.Errorf("InstallPackage: %v", err)
		}
	})

	t.Run("create_service bloquea a usuarios", func(t *testing.T) {
		useTempDir(t)
		locks := &VMLocks{}
		runner := &fakeRunner{results: map[string]RunResult{}, block: make(chan struct{}),
			started: make(chan struct{}, 1), blockOnly: "services/create-service.sh"}
		env := newServicesEnvWithLocks(t, runner, locks)
		env.register(t, "vm1")
		users := NewUsersService(UsersConfig{KeyTTL: 1}, runner, env.vms, env.lbs, env.history, locks, env.svc.logger)

		done := make(chan error, 1)
		go func() {
			_, err := env.svc.CreateService(context.Background(), "vm1", validCreateInput(), tarGzUpload("x"))
			done <- err
		}()
		<-runner.started

		if _, err := users.DeleteUser(context.Background(), "vm1", "ana", false); !errors.Is(err, ErrOperationInProgress) {
			t.Errorf("DeleteUser err = %v, se esperaba ErrOperationInProgress", err)
		}
		if n := runner.callCount(); n != 1 {
			t.Errorf("el runner se llamó %d veces, se esperaba 1 (solo create-service)", n)
		}
		close(runner.block)
		if err := <-done; err != nil {
			t.Errorf("CreateService: %v", err)
		}
	})
}

func TestAC33_ServicesRecordPolicy(t *testing.T) {
	useTempDir(t)
	runner := &fakeRunner{results: map[string]RunResult{}}
	env := newServicesEnv(t, runner)
	env.register(t, "vm1")
	ctx := context.Background()

	// 20 lecturas correctas (listar, estado y logs) no dejan filas.
	for i := 0; i < 20; i++ {
		var err error
		switch i % 3 {
		case 0:
			_, err = env.svc.ListServices(ctx, "vm1")
		case 1:
			_, err = env.svc.ServiceStatus(ctx, "vm1", "demian-lee")
		default:
			_, err = env.svc.ServiceLogs(ctx, "vm1", "demian-lee")
		}
		if err != nil {
			t.Fatalf("lectura %d: %v", i, err)
		}
	}
	if ops := env.operations(t); len(ops) != 0 {
		t.Fatalf("20 lecturas correctas dejaron %d filas, se esperaban 0", len(ops))
	}

	// Un estado fallido sí queda.
	runner.results["services/status-service.sh"] = RunResult{ExitCode: 5, Stderr: "Falló el paso sh en la VM (código 5)\n"}
	if _, err := env.svc.ServiceStatus(ctx, "vm1", "demian-lee"); err == nil {
		t.Fatal("ServiceStatus con exit 5 no devolvió error")
	}
	if ops := env.operations(t); len(ops) != 1 || ops[0].Operation != "service_status" {
		t.Fatalf("historial = %+v, se esperaba solo la fila del estado fallido", ops)
	}

	// Las nueve operaciones que cambian algo se registran siempre, con el valor secreto fuera.
	for _, c := range allServiceCalls() {
		if !c.recorded {
			continue
		}
		if err := c.call(env.svc); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
	}
	ops := env.operations(t)
	if len(ops) != 10 {
		t.Fatalf("historial con %d filas, se esperaban 10 (1 lectura fallida y 9 cambios)", len(ops))
	}
	got := map[string]bool{}
	for _, op := range ops {
		got[op.Operation] = true
		if strings.Contains(op.Stderr, "Secreta123") || strings.Contains(strings.Join(op.Args, " "), "Secreta123") {
			t.Errorf("la fila %s contiene un valor de variable: %+v", op.Operation, op)
		}
	}
	for _, want := range []string{"create_service", "edit_service", "redeploy_service", "delete_service", "start_service",
		"stop_service", "restart_service", "enable_service", "disable_service"} {
		if !got[want] {
			t.Errorf("falta la fila %s en el historial", want)
		}
	}
	if strings.Contains(env.logs.String(), "Secreta123") {
		t.Errorf("el log contiene el valor de una variable: %s", env.logs.String())
	}
}

func TestAC34_ServicesHistoryReturns50NewestOfModule(t *testing.T) {
	env := newServicesEnv(t, &fakeRunner{results: map[string]RunResult{}})
	ctx := context.Background()
	// 55 filas de services (la fila i lleva i en su argv) mezcladas con filas de users.
	for i := 1; i <= 55; i++ {
		row := repository.Operation{Module: "services", Operation: "start_service", VM: "vm1",
			Command: "services/start-service.sh", Args: []string{strconv.Itoa(i)}, Result: "ok"}
		if err := env.history.Insert(ctx, row); err != nil {
			t.Fatalf("Insert services %d: %v", i, err)
		}
		other := repository.Operation{Module: "users", Operation: "create_user", VM: "vm1",
			Command: "users/create-user.sh", Result: "ok"}
		if err := env.history.Insert(ctx, other); err != nil {
			t.Fatalf("Insert users %d: %v", i, err)
		}
	}

	ops, err := env.svc.History(ctx)
	if err != nil {
		t.Fatalf("History: %v", err)
	}

	if len(ops) != 50 {
		t.Fatalf("filas = %d, se esperaban 50", len(ops))
	}
	for i, op := range ops {
		if op.Module != "services" {
			t.Fatalf("fila %d es del módulo %q, se esperaba services", i, op.Module)
		}
		if want := strconv.Itoa(55 - i); op.Args[0] != want {
			t.Fatalf("fila %d lleva %s, se esperaba %s (más reciente primero)", i, op.Args[0], want)
		}
	}
}

// AC-20, AC-29: en un servidor asignado, los scripts de servicios llevan --jump <lb> antes de la VM
// (listar, acciones, lecturas y borrado); un balanceador o una VM común no lo llevan.
func TestAC29_ServicesOnServerPassJump(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{
		"services/list-services.sh":  {},
		"services/start-service.sh":  {Stdout: []byte("Servicio api iniciado en web-a\nactive_state=active\n")},
		"services/status-service.sh": {Stdout: []byte("● api.service\n")},
		"services/delete-service.sh": {Stdout: []byte("Servicio api eliminado de web-a\n")},
	}}
	env := newServicesEnv(t, runner)
	ctx := context.Background()
	for _, vm := range []string{"lb1", "web-a", "vm1"} {
		env.register(t, vm)
	}
	if err := env.lbs.InsertLB(ctx, repository.LoadBalancer{Name: "lb1", Subnet: 1, FrontendPort: 80}); err != nil {
		t.Fatal(err)
	}
	if err := env.lbs.InsertServer(ctx, repository.LBServer{LB: "lb1", Name: "server1", VM: "web-a", Port: 80,
		LastIP: "10.10.1.11", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	if _, err := env.svc.ListServices(ctx, "web-a"); err != nil {
		t.Fatalf("ListServices: %v", err)
	}
	if _, err := env.svc.ServiceAction(ctx, "web-a", "api", "start"); err != nil {
		t.Fatalf("ServiceAction: %v", err)
	}
	if _, err := env.svc.ServiceStatus(ctx, "web-a", "api"); err != nil {
		t.Fatalf("ServiceStatus: %v", err)
	}
	if _, err := env.svc.DeleteService(ctx, "web-a", "api"); err != nil {
		t.Fatalf("DeleteService: %v", err)
	}
	// El balanceador y una VM común no llevan --jump.
	if _, err := env.svc.ServiceAction(ctx, "lb1", "api", "start"); err != nil {
		t.Fatalf("ServiceAction(lb1): %v", err)
	}
	if _, err := env.svc.ListServices(ctx, "vm1"); err != nil {
		t.Fatalf("ListServices(vm1): %v", err)
	}

	want := map[string][][]string{
		"services/list-services.sh":  {{"--jump", "lb1", "web-a"}, {"vm1"}},
		"services/start-service.sh":  {{"--jump", "lb1", "web-a", "api"}, {"lb1", "api"}},
		"services/status-service.sh": {{"--jump", "lb1", "web-a", "api"}},
		"services/delete-service.sh": {{"--jump", "lb1", "web-a", "api"}},
	}
	for script, wantArgs := range want {
		calls := runner.callsTo(script)
		if len(calls) != len(wantArgs) {
			t.Errorf("%s: %d llamadas, se esperaban %d", script, len(calls), len(wantArgs))
			continue
		}
		for i, w := range wantArgs {
			if !reflect.DeepEqual(calls[i].Args, w) {
				t.Errorf("%s llamada %d: argv = %q, se esperaba %q", script, i, calls[i].Args, w)
			}
		}
	}
}

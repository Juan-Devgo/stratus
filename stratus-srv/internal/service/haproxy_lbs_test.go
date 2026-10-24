package service

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"stratus-srv/internal/repository"
)

const createLBStdout = "Balanceador lb1 creado y encendido; IP 192.168.1.60; subred 10.10.1.0/24\nsubnet=1\n"

// AC-1: CreateLB pasa los argumentos como elementos separados y guarda el balanceador.
func TestAC1_CreateLBPassesDiscreteArgs(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{
		"haproxy/create-lb.sh": {Stdout: []byte(createLBStdout)},
	}}
	env := newHAProxyEnv(t, runner)
	ctx := context.Background()

	msg, err := env.svc.CreateLB(ctx, validCreate("lb1"))
	if err != nil {
		t.Fatalf("CreateLB: %v", err)
	}
	if want := "Balanceador lb1 creado y encendido; IP 192.168.1.60; subred 10.10.1.0/24"; msg != want {
		t.Errorf("mensaje = %q, se esperaba %q", msg, want)
	}
	calls := runner.callsTo("haproxy/create-lb.sh")
	if len(calls) != 1 {
		t.Fatalf("create-lb.sh se llamó %d veces, se esperaba 1", len(calls))
	}
	want := []string{"lb1", "2048", "2", testHAProxyDisk, testBridge, "-"}
	if !reflect.DeepEqual(calls[0].Args, want) {
		t.Errorf("argv = %q, se esperaba %q", calls[0].Args, want)
	}
	if _, err := env.vms.Get(ctx, "lb1"); err != nil {
		t.Errorf("lb1 no quedó en vms: %v", err)
	}
	lb, err := env.lbs.GetLB(ctx, "lb1")
	if err != nil || lb != (repository.LoadBalancer{Name: "lb1", Subnet: 1, FrontendPort: 80}) {
		t.Errorf("load_balancers = %+v, %v; se esperaba {lb1 1 80}", lb, err)
	}
	if op := env.lastOperation(t); op.Operation != "create_lb" || op.VM != "lb1" || op.Result != "ok" {
		t.Errorf("historial = %+v, se esperaba create_lb ok en lb1", op)
	}
}

// AC-2: una entrada inválida o un nombre repetido no llega al script.
func TestAC2_CreateLBRejectsInvalidOrDuplicate(t *testing.T) {
	tests := []struct {
		name string
		in   CreateVMInput
		want error
	}{
		{"nombre inválido", CreateVMInput{Name: "LB_1", MemoryMB: 1024, CPUs: 1}, ErrInvalidInput},
		{"memoria fuera de rango", CreateVMInput{Name: "lb1", MemoryMB: 256, CPUs: 1}, ErrInvalidInput},
		{"CPUs fuera de rango", CreateVMInput{Name: "lb1", MemoryMB: 1024, CPUs: 5}, ErrInvalidInput},
		{"nombre ya registrado", CreateVMInput{Name: "web-a", MemoryMB: 1024, CPUs: 1}, ErrDuplicateVM},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeRunner{}
			env := newHAProxyEnv(t, runner)
			if _, err := env.vms.Insert(context.Background(), "web-a"); err != nil {
				t.Fatal(err)
			}

			_, err := env.svc.CreateLB(context.Background(), tt.in)

			if !errors.Is(err, tt.want) {
				t.Errorf("err = %v, se esperaba %v", err, tt.want)
			}
			if n := runner.callCount(); n != 0 {
				t.Errorf("el runner se llamó %d veces, se esperaba 0", n)
			}
			// El bloqueo de creación se soltó: una creación válida posterior no se rechaza.
			if !env.locks.TryLockCreate() {
				t.Error("el bloqueo de creación quedó tomado")
			}
		})
	}
}

// AC-3: sin disco base el error trae el texto del script y no queda ningún registro.
func TestAC3_CreateLBMissingDiskRegistersNothing(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{
		"haproxy/create-lb.sh": {ExitCode: 6, Stderr: "Disco base de HAProxy no encontrado: /vms/haproxy-base.vdi\n"},
	}}
	env := newHAProxyEnv(t, runner)
	ctx := context.Background()

	_, err := env.svc.CreateLB(ctx, validCreate("lb1"))

	var opErr *OperationError
	if !errors.As(err, &opErr) || !strings.Contains(err.Error(), "Disco base de HAProxy no encontrado") {
		t.Fatalf("err = %v, se esperaba un OperationError con el texto del script", err)
	}
	if _, err := env.vms.Get(ctx, "lb1"); !errors.Is(err, repository.ErrVMNotFound) {
		t.Errorf("lb1 no debía quedar en vms: err = %v", err)
	}
	if _, err := env.lbs.GetLB(ctx, "lb1"); !errors.Is(err, repository.ErrLBNotFound) {
		t.Errorf("lb1 no debía quedar en load_balancers: err = %v", err)
	}
}

// AC-5: las subredes de los balanceadores registrados se pasan como lista reservada.
func TestAC5_CreateLBPassesReservedSubnets(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{
		"haproxy/create-lb.sh": {Stdout: []byte("Balanceador lb2 creado y encendido; IP 192.168.1.61; subred 10.10.2.0/24\nsubnet=2\n")},
	}}
	env := newHAProxyEnv(t, runner)
	env.registerLB(t, "lb-a", 1, 80)
	env.registerLB(t, "lb-b", 3, 80)

	if _, err := env.svc.CreateLB(context.Background(), validCreate("lb2")); err != nil {
		t.Fatalf("CreateLB: %v", err)
	}

	calls := runner.callsTo("haproxy/create-lb.sh")
	if got := calls[0].Args[len(calls[0].Args)-1]; got != "1,3" {
		t.Errorf("reserved = %q, se esperaba %q", got, "1,3")
	}
	lb, err := env.lbs.GetLB(context.Background(), "lb2")
	if err != nil || lb.Subnet != 2 {
		t.Errorf("lb2 = %+v, %v; se esperaba la subred 2", lb, err)
	}
}

// AC-7: si el script falla, se libera el nombre y queda la fila de historial con el error.
func TestAC7_CreateLBFailureFreesName(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{
		"haproxy/create-lb.sh": {ExitCode: 5, Stderr: "Falló un paso en el balanceador lb1 (código 5): useradd\n"},
	}}
	env := newHAProxyEnv(t, runner)
	ctx := context.Background()

	_, err := env.svc.CreateLB(ctx, validCreate("lb1"))

	var opErr *OperationError
	if !errors.As(err, &opErr) || opErr.ExitCode != 5 {
		t.Fatalf("err = %v, se esperaba *OperationError con código 5", err)
	}
	if _, err := env.vms.Get(ctx, "lb1"); !errors.Is(err, repository.ErrVMNotFound) {
		t.Errorf("lb1 debía quedar libre en vms: err = %v", err)
	}
	if _, err := env.lbs.GetLB(ctx, "lb1"); !errors.Is(err, repository.ErrLBNotFound) {
		t.Errorf("lb1 no debía quedar en load_balancers: err = %v", err)
	}
	if op := env.lastOperation(t); op.Operation != "create_lb" || op.Result != "error" || op.ExitCode != 5 {
		t.Errorf("historial = %+v, se esperaba create_lb error con código 5", op)
	}
	// El nombre se puede volver a usar.
	runner.results["haproxy/create-lb.sh"] = RunResult{Stdout: []byte(createLBStdout)}
	if _, err := env.svc.CreateLB(ctx, validCreate("lb1")); err != nil {
		t.Errorf("CreateLB tras el fallo: %v", err)
	}
}

// AC-50: los códigos 13 y 8 de create-lb.sh se traducen y no dejan filas.
func TestAC50_CreateLBMapsSubnetErrors(t *testing.T) {
	tests := []struct {
		name string
		code int
		want error
	}{
		{"red interna ya registrada", 13, ErrIntnetExists},
		{"sin subredes libres", 8, ErrNoFreeSubnet},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeRunner{results: map[string]RunResult{
				"haproxy/create-lb.sh": {ExitCode: tt.code, Stderr: tt.want.Error() + "\n"},
			}}
			env := newHAProxyEnv(t, runner)
			ctx := context.Background()

			_, err := env.svc.CreateLB(ctx, validCreate("lb1"))

			if !errors.Is(err, tt.want) {
				t.Errorf("err = %v, se esperaba %v", err, tt.want)
			}
			if _, err := env.vms.Get(ctx, "lb1"); !errors.Is(err, repository.ErrVMNotFound) {
				t.Errorf("lb1 no debía quedar en vms: err = %v", err)
			}
			if lbs, _ := env.lbs.ListLBs(ctx); len(lbs) != 0 {
				t.Errorf("load_balancers = %+v, se esperaba vacío", lbs)
			}
		})
	}
}

// AC-10: con servidores no se llama al script; sin ellos se borra todo.
func TestAC10_DeleteLBRejectsWithServers(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{
		"haproxy/delete-lb.sh": {Stdout: []byte("Balanceador lb1 eliminado junto con su red interna\n")},
	}}
	env := newHAProxyEnv(t, runner)
	env.registerLB(t, "lb1", 1, 80)
	env.registerServer(t, "lb1", "server1", "web-a", 80, "10.10.1.11")
	ctx := context.Background()

	_, err := env.svc.DeleteLB(ctx, "lb1")
	if !errors.Is(err, ErrLBHasServers) {
		t.Fatalf("DeleteLB con un servidor: err = %v, se esperaba ErrLBHasServers", err)
	}
	if n := runner.callCount(); n != 0 {
		t.Errorf("el runner se llamó %d veces, se esperaban 0", n)
	}
	if _, err := env.lbs.GetLB(ctx, "lb1"); err != nil {
		t.Errorf("lb1 debía seguir registrado: %v", err)
	}

	if err := env.lbs.DeleteServer(ctx, "lb1", "web-a"); err != nil {
		t.Fatal(err)
	}
	msg, err := env.svc.DeleteLB(ctx, "lb1")
	if err != nil {
		t.Fatalf("DeleteLB sin servidores: %v", err)
	}
	if msg != "Balanceador lb1 eliminado junto con su red interna" {
		t.Errorf("mensaje = %q", msg)
	}
	calls := runner.callsTo("haproxy/delete-lb.sh")
	if len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, []string{"lb1"}) {
		t.Errorf("llamadas a delete-lb.sh = %+v, se esperaba una con [lb1]", calls)
	}
	if _, err := env.lbs.GetLB(ctx, "lb1"); !errors.Is(err, repository.ErrLBNotFound) {
		t.Errorf("lb1 debía borrarse de load_balancers: err = %v", err)
	}
	if _, err := env.vms.Get(ctx, "lb1"); !errors.Is(err, repository.ErrVMNotFound) {
		t.Errorf("lb1 debía borrarse de vms: err = %v", err)
	}
	if op := env.lastOperation(t); op.Operation != "delete_lb" || op.VM != "lb1" {
		t.Errorf("historial = %+v, se esperaba delete_lb en lb1", op)
	}
}

func TestDeleteLBUnknownOrInvalid(t *testing.T) {
	runner := &fakeRunner{}
	env := newHAProxyEnv(t, runner)

	if _, err := env.svc.DeleteLB(context.Background(), "nada"); !errors.Is(err, ErrLBNotFound) {
		t.Errorf("DeleteLB inexistente: err = %v, se esperaba ErrLBNotFound", err)
	}
	if _, err := env.svc.DeleteLB(context.Background(), "LB;1"); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("DeleteLB inválido: err = %v, se esperaba ErrInvalidInput", err)
	}
	if n := runner.callCount(); n != 0 {
		t.Errorf("el runner se llamó %d veces, se esperaban 0", n)
	}
}

func TestDeleteLBFailureKeepsRows(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{
		"haproxy/delete-lb.sh": {ExitCode: 4, Stderr: "No se pudo eliminar la VM lb1\n"},
	}}
	env := newHAProxyEnv(t, runner)
	env.registerLB(t, "lb1", 1, 80)

	_, err := env.svc.DeleteLB(context.Background(), "lb1")

	var opErr *OperationError
	if !errors.As(err, &opErr) {
		t.Fatalf("err = %v, se esperaba *OperationError", err)
	}
	if _, err := env.lbs.GetLB(context.Background(), "lb1"); err != nil {
		t.Errorf("lb1 debía seguir registrado: %v", err)
	}
}

// ListLBs combina SQLite con el estado y la IP de list-vms.sh; es de solo lectura.
func TestListLBs(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{
		"prepare/list-vms.sh": {Stdout: []byte("lb1 running 192.168.1.60\nweb-a running 10.10.1.11\nlb2 poweroff -\n")},
	}}
	env := newHAProxyEnv(t, runner)
	env.registerLB(t, "lb1", 1, 8080)
	env.registerServer(t, "lb1", "server1", "web-a", 80, "10.10.1.11")
	env.registerLB(t, "lb2", 2, 80)
	ctx := context.Background()

	lbs, listErr, err := env.svc.ListLBs(ctx)
	if err != nil {
		t.Fatalf("ListLBs: %v", err)
	}
	if listErr != "" {
		t.Errorf("listErr = %q, se esperaba vacío", listErr)
	}
	want := []LBView{
		{Name: "lb1", State: "running", IP: "192.168.1.60", Subnet: "10.10.1.0/24", FrontendPort: 8080, ServerCount: 1},
		{Name: "lb2", State: "poweroff", IP: "", Subnet: "10.10.2.0/24", FrontendPort: 80, ServerCount: 0},
	}
	if !reflect.DeepEqual(lbs, want) {
		t.Errorf("ListLBs = %+v, se esperaba %+v", lbs, want)
	}
	if n := env.haproxyRows(t); n != 0 {
		t.Errorf("el listado dejó %d filas en el historial, se esperaban 0", n)
	}
}

func TestListLBsWhenListVMsFails(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{
		"prepare/list-vms.sh": {ExitCode: 4, Stderr: "VBoxManage list vms falló\n"},
	}}
	env := newHAProxyEnv(t, runner)
	env.registerLB(t, "lb1", 1, 80)

	lbs, listErr, err := env.svc.ListLBs(context.Background())

	if err != nil {
		t.Fatalf("ListLBs: %v", err)
	}
	if len(lbs) != 1 || lbs[0].State != "unknown" || lbs[0].Name != "lb1" {
		t.Errorf("ListLBs = %+v, se esperaba lb1 en unknown", lbs)
	}
	if !strings.Contains(listErr, "No se pudo consultar el estado de las VMs en VirtualBox") {
		t.Errorf("listErr = %q", listErr)
	}
}

func TestHAProxyHistoryOnlyOwnModule(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{
		"haproxy/create-lb.sh": {Stdout: []byte(createLBStdout)},
	}}
	env := newHAProxyEnv(t, runner)
	ctx := context.Background()
	if _, err := env.svc.CreateLB(ctx, validCreate("lb1")); err != nil {
		t.Fatal(err)
	}
	env.register(t, "vm1")

	ops, err := env.svc.History(ctx)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(ops) != 1 || ops[0].Module != "haproxy" {
		t.Errorf("History = %+v, se esperaba una fila del módulo haproxy", ops)
	}
}

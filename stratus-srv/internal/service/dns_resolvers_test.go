package service

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"stratus-srv/internal/repository"
)

// AC-42, REQ-31, REQ-33: SetResolver ejecuta set-resolver.sh con [vm, IP del DNS], deja una fila
// set_resolver y guarda la marca; UnsetResolver hace lo contrario. Las validaciones rechazan sin
// llamar a ningún script.
func TestAC42_SetUnsetResolverFlag(t *testing.T) {
	env, runner := newRecordsEnv(t)
	runner.results["dns/set-resolver.sh"] = RunResult{Stdout: []byte("Resolvedor DNS 192.168.1.53 configurado en vm1\n")}
	runner.results["dns/unset-resolver.sh"] = RunResult{Stdout: []byte("Resolvedor DNS 192.168.1.53 quitado de vm1\n")}
	ctx := context.Background()

	msg, err := env.svc.SetResolver(ctx, "vm1")

	if err != nil || msg != "Resolvedor DNS 192.168.1.53 configurado en vm1" {
		t.Fatalf("SetResolver = %q, %v", msg, err)
	}
	want := []string{"vm1", "192.168.1.53"}
	if calls := runner.callsTo("dns/set-resolver.sh"); len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, want) {
		t.Errorf("set-resolver.sh = %+v, se esperaba %v", calls, want)
	}
	if op := env.lastDNSOperation(t); op.Operation != "set_resolver" || op.VM != "vm1" || op.Result != "ok" {
		t.Errorf("historial = %+v", op)
	}
	if got, _ := env.dns.Resolvers(ctx); !reflect.DeepEqual(got, []string{"vm1"}) {
		t.Errorf("resolvedores = %v, se esperaba [vm1]", got)
	}

	msg, err = env.svc.UnsetResolver(ctx, "vm1")

	if err != nil || msg != "Resolvedor DNS 192.168.1.53 quitado de vm1" {
		t.Fatalf("UnsetResolver = %q, %v", msg, err)
	}
	if calls := runner.callsTo("dns/unset-resolver.sh"); len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, want) {
		t.Errorf("unset-resolver.sh = %+v, se esperaba %v", calls, want)
	}
	if op := env.lastDNSOperation(t); op.Operation != "unset_resolver" || op.VM != "vm1" || op.Result != "ok" {
		t.Errorf("historial = %+v", op)
	}
	if got, _ := env.dns.Resolvers(ctx); len(got) != 0 {
		t.Errorf("resolvedores = %v, se esperaba vacío", got)
	}

	before := runner.callCount()
	var validation *ValidationError
	for _, tt := range []struct {
		vm    string
		check func(error) bool
	}{
		{"nada", func(err error) bool { return errors.Is(err, ErrVMNotFound) }},
		{"dns1", func(err error) bool { return errors.As(err, &validation) }},
		{"VM;1", func(err error) bool { return errors.Is(err, ErrInvalidInput) || errors.As(err, &validation) }},
	} {
		if _, err := env.svc.SetResolver(ctx, tt.vm); !tt.check(err) {
			t.Errorf("SetResolver(%s): err = %v", tt.vm, err)
		}
		if _, err := env.svc.UnsetResolver(ctx, tt.vm); !tt.check(err) {
			t.Errorf("UnsetResolver(%s): err = %v", tt.vm, err)
		}
	}
	if n := runner.callCount(); n != before {
		t.Errorf("el runner se llamó %d veces más, se esperaba 0", n-before)
	}
}

// AC-35: sin servidor DNS, SetResolver es ErrNoDNSServer.
func TestAC42_ResolverWithoutDNSServer(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{}}
	env := newDNSEnv(t, runner)
	if _, err := env.vms.Insert(context.Background(), "vm1"); err != nil {
		t.Fatal(err)
	}

	if _, err := env.svc.SetResolver(context.Background(), "vm1"); !errors.Is(err, ErrNoDNSServer) {
		t.Errorf("err = %v, se esperaba ErrNoDNSServer", err)
	}
	if n := runner.callCount(); n != 0 {
		t.Errorf("el runner se llamó %d veces, se esperaba 0", n)
	}
}

// AC-43, REQ-32: un servidor asignado pasa por --jump; una VM apagada (exit 7) da el texto de 001 y
// no guarda la marca.
func TestAC43_ResolverOnServerAndOffVM(t *testing.T) {
	env, runner := newRecordsEnv(t)
	ctx := context.Background()
	if _, err := env.vms.Insert(ctx, "web-a"); err != nil {
		t.Fatal(err)
	}
	if err := env.lbs.InsertServer(ctx, repository.LBServer{LB: "lb1", Name: "server1", VM: "web-a", Port: 80,
		LastIP: "10.10.1.11", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	if _, err := env.svc.SetResolver(ctx, "web-a"); err != nil {
		t.Fatalf("SetResolver: %v", err)
	}
	want := []string{"--jump", "lb1", "web-a", "192.168.1.53"}
	if calls := runner.callsTo("dns/set-resolver.sh"); len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, want) {
		t.Errorf("set-resolver.sh = %+v, se esperaba %v", calls, want)
	}

	runner.results["dns/set-resolver.sh"] = RunResult{ExitCode: 7}

	_, err := env.svc.SetResolver(ctx, "vm2")

	if !errors.Is(err, ErrVMNoIP) {
		t.Errorf("err = %v, se esperaba ErrVMNoIP", err)
	}
	if got, _ := env.dns.Resolvers(ctx); !reflect.DeepEqual(got, []string{"web-a"}) {
		t.Errorf("resolvedores = %v, se esperaba solo web-a", got)
	}
	if op := env.lastDNSOperation(t); op.Operation != "set_resolver" || op.VM != "vm2" || op.Result != "error" {
		t.Errorf("historial = %+v", op)
	}
}

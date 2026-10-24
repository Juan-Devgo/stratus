package service

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestAC17_SetHostnameInvokesScript(t *testing.T) {
	runner := &fakeRunner{}
	env := newTestEnv(t, runner)
	env.register(t, "vm1")

	msg, err := env.svc.SetHostname(context.Background(), "vm1", "web-01")
	if err != nil {
		t.Fatalf("SetHostname: %v", err)
	}
	if !strings.Contains(msg, "web-01") {
		t.Errorf("mensaje = %q, se esperaba que nombrara web-01", msg)
	}
	calls := runner.callsTo("prepare/set-hostname.sh")
	if len(calls) != 1 {
		t.Fatalf("set-hostname.sh se llamó %d veces, se esperaba 1", len(calls))
	}
	if want := []string{"vm1", "web-01"}; !reflect.DeepEqual(calls[0].Args, want) {
		t.Errorf("argv = %q, se esperaba %q", calls[0].Args, want)
	}
	op := env.lastOperation(t)
	if op.Operation != "set_hostname" || op.Result != "ok" || op.Command != "prepare/set-hostname.sh" {
		t.Errorf("historial = %+v, se esperaba set_hostname con resultado ok", op)
	}
}

func TestAC18_InvalidHostnameSkipsScript(t *testing.T) {
	for _, hostname := range []string{"Web_01", "-web", strings.Repeat("a", 64)} {
		t.Run(hostname, func(t *testing.T) {
			runner := &fakeRunner{}
			env := newTestEnv(t, runner)
			env.register(t, "vm1")

			_, err := env.svc.SetHostname(context.Background(), "vm1", hostname)
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("err = %v, se esperaba ErrInvalidInput", err)
			}
			if n := runner.callCount(); n != 0 {
				t.Errorf("el runner se llamó %d veces, se esperaba 0", n)
			}
		})
	}
}

func TestAC14_SetIssueCanonicalOrder(t *testing.T) {
	tests := []struct {
		name string
		in   IssueInput
		want []string
	}{
		{"campos desordenados",
			IssueInput{Message: "Hola", Fields: []string{"users", "hostname"}},
			[]string{"vm1", "Hola", "hostname", "users"}},
		{"mensaje de varias líneas en un solo argumento",
			IssueInput{Message: "Configuración\nde producción"},
			[]string{"vm1", "Configuración\nde producción"}},
		{"sin mensaje y con campos repetidos",
			IssueInput{Fields: []string{"tty", "date", "tty"}},
			[]string{"vm1", "", "date", "tty"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runner := &fakeRunner{}
			env := newTestEnv(t, runner)
			env.register(t, "vm1")

			msg, err := env.svc.SetIssue(context.Background(), "vm1", tc.in)
			if err != nil {
				t.Fatalf("SetIssue: %v", err)
			}
			if !strings.Contains(msg, "vm1") {
				t.Errorf("mensaje = %q, se esperaba que nombrara vm1", msg)
			}
			calls := runner.callsTo("prepare/set-issue.sh")
			if len(calls) != 1 {
				t.Fatalf("set-issue.sh se llamó %d veces, se esperaba 1", len(calls))
			}
			if !reflect.DeepEqual(calls[0].Args, tc.want) {
				t.Errorf("argv = %q, se esperaba %q", calls[0].Args, tc.want)
			}
			op := env.lastOperation(t)
			if op.Operation != "set_issue" || op.Result != "ok" || op.Command != "prepare/set-issue.sh" {
				t.Errorf("historial = %+v, se esperaba set_issue con resultado ok", op)
			}
		})
	}
}

// assertIssueSkipsScript comprueba que SetIssue rechace la entrada sin llamar al runner.
func assertIssueSkipsScript(t *testing.T, in IssueInput) {
	t.Helper()
	runner := &fakeRunner{}
	env := newTestEnv(t, runner)
	env.register(t, "vm1")

	_, err := env.svc.SetIssue(context.Background(), "vm1", in)
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("err = %v, se esperaba ErrInvalidInput", err)
	}
	if n := runner.callCount(); n != 0 {
		t.Errorf("el runner se llamó %d veces, se esperaba 0", n)
	}
}

func TestAC15_SetIssueTooLongSkipsScript(t *testing.T) {
	assertIssueSkipsScript(t, IssueInput{Message: strings.Repeat("a", 501), Fields: []string{"hostname"}})
}

func TestAC16_SetIssueEmptySkipsScript(t *testing.T) {
	assertIssueSkipsScript(t, IssueInput{})
}

func TestAC25_SetIssueBackslashSkipsScript(t *testing.T) {
	assertIssueSkipsScript(t, IssueInput{Message: `a\b`})
}

// AC-9: poner el hostname a un balanceador es una operación directa, sin --jump ni --frontend.
func TestAC9_SetHostnameOnLBIsDirect(t *testing.T) {
	runner := &fakeRunner{}
	env := newHAProxyEnv(t, runner)
	env.registerLB(t, "lb1", 1, 80)

	if _, err := env.prepare.SetHostname(context.Background(), "lb1", "web-01"); err != nil {
		t.Fatalf("SetHostname: %v", err)
	}

	calls := runner.callsTo("prepare/set-hostname.sh")
	if len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, []string{"lb1", "web-01"}) {
		t.Errorf("llamadas = %+v, se esperaba [lb1 web-01]", calls)
	}
}

// AC-27, AC-29: en un servidor asignado, hostname e issue llevan --jump <lb> antes de la VM.
func TestAC29_SystemOpsOnServerPassJump(t *testing.T) {
	runner := &fakeRunner{}
	env := newHAProxyEnv(t, runner)
	env.registerLB(t, "lb1", 1, 80)
	env.registerServer(t, "lb1", "server1", "web-a", 80, "10.10.1.11")
	ctx := context.Background()

	if _, err := env.prepare.SetHostname(ctx, "web-a", "web-01"); err != nil {
		t.Fatalf("SetHostname: %v", err)
	}
	if _, err := env.prepare.SetIssue(ctx, "web-a", IssueInput{Message: "Hola", Fields: []string{"users", "hostname"}}); err != nil {
		t.Fatalf("SetIssue: %v", err)
	}

	if got, want := runner.callsTo("prepare/set-hostname.sh")[0].Args, []string{"--jump", "lb1", "web-a", "web-01"}; !reflect.DeepEqual(got, want) {
		t.Errorf("set-hostname argv = %q, se esperaba %q", got, want)
	}
	if got, want := runner.callsTo("prepare/set-issue.sh")[0].Args, []string{"--jump", "lb1", "web-a", "Hola", "hostname", "users"}; !reflect.DeepEqual(got, want) {
		t.Errorf("set-issue argv = %q, se esperaba %q", got, want)
	}
}

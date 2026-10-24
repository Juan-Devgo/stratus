package service

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// AC-11: SaveFrontend pasa el puerto y los servidores guardados, y guarda el puerto si el script termina bien.
func TestAC11_SaveFrontendArgsAndValidation(t *testing.T) {
	env, runner := newAssignEnv(t)
	ctx := context.Background()
	env.registerServer(t, "lb1", "server1", "web-b", 8080, "10.10.1.12")
	runner.results["haproxy/save-frontend.sh"] = RunResult{Stdout: []byte("Frontend de lb1 en el puerto 8081\n")}

	msg, err := env.svc.SaveFrontend(ctx, "lb1", 8081)
	if err != nil {
		t.Fatalf("SaveFrontend: %v", err)
	}
	if msg != "Frontend de lb1 en el puerto 8081" {
		t.Errorf("mensaje = %q", msg)
	}
	calls := runner.callsTo("haproxy/save-frontend.sh")
	want := []string{"lb1", "8081", "server1,web-b,8080,10.10.1.12,on"}
	if len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, want) {
		t.Errorf("llamadas = %+v, se esperaba una con %q", calls, want)
	}
	if lb, _ := env.lbs.GetLB(ctx, "lb1"); lb.FrontendPort != 8081 {
		t.Errorf("frontend_port = %d, se esperaba 8081", lb.FrontendPort)
	}
	if op := env.lastOperation(t); op.Operation != "save_frontend" || op.VM != "lb1" || op.Result != "ok" {
		t.Errorf("historial = %+v", op)
	}
}

func TestAC11_SaveFrontendInvalidPortOrLB(t *testing.T) {
	for _, port := range []int{22, 0, -1, 65536} {
		t.Run(fmt.Sprint(port), func(t *testing.T) {
			env, runner := newAssignEnv(t)

			_, err := env.svc.SaveFrontend(context.Background(), "lb1", port)

			if !errors.Is(err, ErrInvalidInput) {
				t.Errorf("err = %v, se esperaba ErrInvalidInput", err)
			}
			if n := runner.callCount(); n != 0 {
				t.Errorf("el runner se llamó %d veces, se esperaban 0", n)
			}
			if lb, _ := env.lbs.GetLB(context.Background(), "lb1"); lb.FrontendPort != 80 {
				t.Errorf("frontend_port = %d, se esperaba 80", lb.FrontendPort)
			}
		})
	}
	env, runner := newAssignEnv(t)
	if _, err := env.svc.SaveFrontend(context.Background(), "nada", 8080); !errors.Is(err, ErrLBNotFound) {
		t.Errorf("balanceador inexistente: err = %v", err)
	}
	if n := runner.callCount(); n != 0 {
		t.Errorf("el runner se llamó %d veces, se esperaban 0", n)
	}
}

// AC-45: si HAProxy no puede recargar (puerto ocupado) el puerto guardado no cambia y el historial
// conserva la salida.
func TestAC45_ReloadFailedMapsError(t *testing.T) {
	env, runner := newAssignEnv(t)
	runner.results["haproxy/save-frontend.sh"] = RunResult{ExitCode: 11,
		Stderr: "No se pudo recargar HAProxy; se mantuvo la configuración anterior. HAProxy no escucha en el puerto 8080: nginx\n"}

	_, err := env.svc.SaveFrontend(context.Background(), "lb1", 8080)

	if !errors.Is(err, ErrHAProxyReloadFailed) {
		t.Fatalf("err = %v, se esperaba ErrHAProxyReloadFailed", err)
	}
	if lb, _ := env.lbs.GetLB(context.Background(), "lb1"); lb.FrontendPort != 80 {
		t.Errorf("frontend_port = %d, se esperaba 80", lb.FrontendPort)
	}
	op := env.lastOperation(t)
	if op.Result != "error" || op.ExitCode != 11 || !strings.Contains(op.Stderr, "nginx") {
		t.Errorf("historial = %+v, se esperaba error 11 con la salida", op)
	}
}

// AC-21, AC-23: Refresh pasa las specs guardadas y guarda la IP y la bandera que informa el script.
func TestAC23_RefreshStoresEnabledFlag(t *testing.T) {
	env, runner := newAssignEnv(t)
	ctx := context.Background()
	if err := env.lbs.SetFrontendPort(ctx, "lb1", 8080); err != nil {
		t.Fatal(err)
	}
	env.registerServer(t, "lb1", "server1", "web-b", 80, "10.10.1.11")
	env.registerServer(t, "lb1", "server2", "web-c", 8000, "10.10.1.12")
	runner.results["haproxy/refresh-lb.sh"] = RunResult{
		Stdout: []byte("server1 10.10.1.15 on\nserver2 10.10.1.12 off\nreloaded=yes\n")}

	msg, err := env.svc.Refresh(ctx, "lb1")
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if !strings.Contains(msg, "lb1") || !strings.Contains(msg, "recargado") {
		t.Errorf("mensaje = %q, se esperaba que nombrara lb1 y la recarga", msg)
	}
	want := []string{"lb1", "1", "8080", "server1,web-b,80,10.10.1.11,on", "server2,web-c,8000,10.10.1.12,on"}
	if got := runner.callsTo("haproxy/refresh-lb.sh")[0].Args; !reflect.DeepEqual(got, want) {
		t.Errorf("argv = %q, se esperaba %q", got, want)
	}
	rows, _ := env.lbs.Servers(ctx, "lb1")
	if len(rows) != 2 || rows[0].LastIP != "10.10.1.15" || !rows[0].Enabled || rows[1].LastIP != "10.10.1.12" || rows[1].Enabled {
		t.Errorf("lb_servers = %+v, se esperaba server1 10.10.1.15 on y server2 10.10.1.12 off", rows)
	}
	if rows[0].Port != 80 || rows[1].VM != "web-c" {
		t.Errorf("el refresco no debe tocar puerto ni VM: %+v", rows)
	}
	if op := env.lastOperation(t); op.Operation != "refresh_lb" || op.VM != "lb1" || op.Result != "ok" {
		t.Errorf("historial = %+v, se esperaba refresh_lb ok", op)
	}
}

// Sin cambios (reloaded=no) el mensaje lo dice, y un balanceador sin servidores también se puede refrescar.
func TestRefreshWithoutChanges(t *testing.T) {
	env, runner := newAssignEnv(t)
	runner.results["haproxy/refresh-lb.sh"] = RunResult{Stdout: []byte("reloaded=no\n")}

	msg, err := env.svc.Refresh(context.Background(), "lb1")

	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if !strings.Contains(msg, "sin cambios") {
		t.Errorf("mensaje = %q, se esperaba 'sin cambios'", msg)
	}
	if _, err := env.svc.Refresh(context.Background(), "nada"); !errors.Is(err, ErrLBNotFound) {
		t.Errorf("balanceador inexistente: err = %v", err)
	}
}

// AC-40: con el balanceador apagado Refresh devuelve *LBOffError y no cambia las filas.
func TestRefreshLBOffKeepsRows(t *testing.T) {
	env, runner := newAssignEnv(t)
	env.registerServer(t, "lb1", "server1", "web-b", 80, "10.10.1.11")
	runner.results["haproxy/refresh-lb.sh"] = RunResult{ExitCode: 12,
		Stderr: "El balanceador lb1 está apagado; enciéndalo para llegar al servidor\n"}

	_, err := env.svc.Refresh(context.Background(), "lb1")

	var off *LBOffError
	if !errors.As(err, &off) {
		t.Fatalf("err = %v, se esperaba *LBOffError", err)
	}
	if rows, _ := env.lbs.Servers(context.Background(), "lb1"); rows[0].LastIP != "10.10.1.11" || !rows[0].Enabled {
		t.Errorf("lb_servers = %+v, no debía cambiar", rows)
	}
}

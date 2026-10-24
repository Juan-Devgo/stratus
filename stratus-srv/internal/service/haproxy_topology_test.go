package service

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"stratus-srv/internal/repository"
)

func serverRow(name string) repository.LBServer {
	return repository.LBServer{LB: "lb1", Name: name, VM: "vm-" + name, Port: 80, LastIP: "10.10.1.11", Enabled: true}
}

// AC-14: el servidor nuevo toma el número libre más bajo; con diez ocupados no hay ninguno.
func TestAC14_NextServerNameLowestFree(t *testing.T) {
	var full []repository.LBServer
	for i := 1; i <= 10; i++ {
		full = append(full, serverRow("server"+string(rune('0'+i%10))))
	}
	full[9] = serverRow("server10")
	tests := []struct {
		name string
		rows []repository.LBServer
		want string
		err  error
	}{
		{"sin servidores", nil, "server1", nil},
		{"solo server2", []repository.LBServer{serverRow("server2")}, "server1", nil},
		{"server1 y server2", []repository.LBServer{serverRow("server1"), serverRow("server2")}, "server3", nil},
		{"hueco en el medio", []repository.LBServer{serverRow("server1"), serverRow("server3"), serverRow("server4")}, "server2", nil},
		{"solo server10", []repository.LBServer{serverRow("server10")}, "server1", nil},
		{"diez ocupados", full, "", ErrLBFull},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := nextServerName(tt.rows)
			if !errors.Is(err, tt.err) || got != tt.want {
				t.Errorf("nextServerName = %q, %v; se esperaba %q, %v", got, err, tt.want, tt.err)
			}
		})
	}
}

// serverSpecs arma el argumento "<server>,<vm>,<port>,<ip>,<on|off>" de cada fila.
func TestServerSpecs(t *testing.T) {
	rows := []repository.LBServer{
		{LB: "lb1", Name: "server1", VM: "web-a", Port: 80, LastIP: "10.10.1.11", Enabled: true},
		{LB: "lb1", Name: "server2", VM: "web-b", Port: 8080, LastIP: "10.10.1.12", Enabled: false},
	}
	want := []string{"server1,web-a,80,10.10.1.11,on", "server2,web-b,8080,10.10.1.12,off"}
	if got := serverSpecs(rows); !reflect.DeepEqual(got, want) {
		t.Errorf("serverSpecs = %q, se esperaba %q", got, want)
	}
	if got := serverSpecs(nil); len(got) != 0 {
		t.Errorf("serverSpecs(nil) = %q, se esperaba vacío", got)
	}
}

// AC-27, AC-29: un servidor recibe --jump <lb> antes de la VM; un balanceador o una VM común, no.
func TestTargetArgs(t *testing.T) {
	env := newHAProxyEnv(t, &fakeRunner{})
	env.registerLB(t, "lb1", 1, 80)
	env.registerServer(t, "lb1", "server1", "web-a", 80, "10.10.1.11")
	env.register(t, "vm1")
	ctx := context.Background()

	tests := []struct {
		name   string
		vm     string
		extra  []string
		want   []string
		wantLB string
	}{
		{"servidor", "web-a", []string{"nginx"}, []string{"--jump", "lb1", "web-a", "nginx"}, "lb1"},
		{"servidor sin extras", "web-a", nil, []string{"--jump", "lb1", "web-a"}, "lb1"},
		{"balanceador", "lb1", []string{"web-01"}, []string{"lb1", "web-01"}, ""},
		{"VM común", "vm1", []string{"htop"}, []string{"vm1", "htop"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, lb, err := targetArgs(ctx, env.lbs, tt.vm, tt.extra...)
			if err != nil {
				t.Fatalf("targetArgs: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) || lb != tt.wantLB {
				t.Errorf("targetArgs = %q, %q; se esperaba %q, %q", got, lb, tt.want, tt.wantLB)
			}
		})
	}
}

// create-lb.sh imprime "subnet=<N>" en su última línea.
func TestParseSubnet(t *testing.T) {
	tests := []struct {
		name   string
		stdout string
		want   int
		ok     bool
	}{
		{"normal", "Balanceador lb1 creado\nsubnet=1\n", 1, true},
		{"subred alta", "x\nsubnet=254\n", 254, true},
		{"sin salto final", "x\nsubnet=7", 7, true},
		{"con espacios", "x\n  subnet=3  \n", 3, true},
		{"falta", "Balanceador lb1 creado\n", 0, false},
		{"cero", "subnet=0\n", 0, false},
		{"fuera de rango", "subnet=255\n", 0, false},
		{"no numérica", "subnet=a\n", 0, false},
		{"vacía", "", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseSubnet(tt.stdout)
			if got != tt.want || ok != tt.ok {
				t.Errorf("parseSubnet = %d, %v; se esperaba %d, %v", got, ok, tt.want, tt.ok)
			}
		})
	}
}

// refresh-lb.sh imprime "<server> <ip> <on|off>" por servidor y "reloaded=yes|no".
func TestParseRefresh(t *testing.T) {
	stdout := "server1 10.10.1.15 on\nserver2 10.10.1.12 off\nbasura\nserver3 no-es-ip on\nserver4 10.10.1.14 quizá\nreloaded=yes\n"

	entries, reloaded := parseRefresh(stdout)

	want := []refreshEntry{
		{Name: "server1", IP: "10.10.1.15", Enabled: true},
		{Name: "server2", IP: "10.10.1.12", Enabled: false},
	}
	if !reflect.DeepEqual(entries, want) {
		t.Errorf("entradas = %+v, se esperaba %+v", entries, want)
	}
	if !reloaded {
		t.Error("reloaded = false, se esperaba true")
	}
	if _, reloaded := parseRefresh("server1 10.10.1.15 on\nreloaded=no\n"); reloaded {
		t.Error("reloaded=no debe dar false")
	}
	if entries, reloaded := parseRefresh(""); len(entries) != 0 || reloaded {
		t.Errorf("salida vacía: %+v, %v", entries, reloaded)
	}
}

// lb-status.sh imprime "<server> <status>" por servidor.
func TestParseStatus(t *testing.T) {
	got := parseStatus("server1 UP\nserver2 DOWN\nserver3 MAINT\n\nbasura\nserver4 UP 1 2\n")
	want := map[string]string{"server1": "UP", "server2": "DOWN", "server3": "MAINT"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseStatus = %v, se esperaba %v", got, want)
	}
}

package service

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// Salida de show-zone.sh: un registro por línea, separado por tabuladores.
const zoneStdout = "SOA\tstratus.lan\t60\tdns1.stratus.lan. hostmaster.stratus.lan. 1731300000 3600 600 86400 60\n" +
	"NS\tstratus.lan\t60\tdns1.stratus.lan.\n" +
	"A\tdns1.stratus.lan\t60\t192.168.1.53\n" +
	"A\tweb-a.stratus.lan\t60\t192.168.1.40\n"

// AC-66, REQ-46, REQ-48: Zone llama a show-zone.sh con el servidor y el dominio y devuelve cada
// registro; una lectura correcta no deja fila en el historial.
func TestAC66_ZoneParsesRecords(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{"dns/show-zone.sh": {Stdout: []byte(zoneStdout)}}}
	env := newDNSEnv(t, runner)
	env.registerDNS(t, "dns1")

	got, err := env.svc.Zone(context.Background())

	if err != nil {
		t.Fatalf("Zone: %v", err)
	}
	want := []ZoneRecord{
		{Type: "SOA", Name: "stratus.lan", TTL: 60, Value: "dns1.stratus.lan. hostmaster.stratus.lan. 1731300000 3600 600 86400 60"},
		{Type: "NS", Name: "stratus.lan", TTL: 60, Value: "dns1.stratus.lan."},
		{Type: "A", Name: "dns1.stratus.lan", TTL: 60, Value: "192.168.1.53"},
		{Type: "A", Name: "web-a.stratus.lan", TTL: 60, Value: "192.168.1.40"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Zone = %+v\nse esperaba %+v", got, want)
	}
	calls := runner.callsTo("dns/show-zone.sh")
	if len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, []string{"dns1", "stratus.lan"}) {
		t.Errorf("llamadas = %+v, se esperaba argv [dns1 stratus.lan]", calls)
	}
	if ops, _ := env.svc.History(context.Background()); len(ops) != 0 {
		t.Errorf("historial = %+v, se esperaba vacío", ops)
	}
}

// Una zona vacía es una lista vacía, no nil: la UI recibe [] y muestra su texto (REQ-47).
func TestAC66_ZoneEmpty(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{"dns/show-zone.sh": {}}}
	env := newDNSEnv(t, runner)
	env.registerDNS(t, "dns1")

	got, err := env.svc.Zone(context.Background())

	if err != nil || got == nil || len(got) != 0 {
		t.Errorf("Zone = %#v, %v; se esperaba [] sin error", got, err)
	}
}

// Sin servidor DNS no se llama a ningún script.
func TestAC66_ZoneWithoutServer(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{}}
	env := newDNSEnv(t, runner)

	_, err := env.svc.Zone(context.Background())

	if !errors.Is(err, ErrNoDNSServer) {
		t.Errorf("err = %v, se esperaba ErrNoDNSServer", err)
	}
	if n := len(runner.callsTo("dns/show-zone.sh")); n != 0 {
		t.Errorf("show-zone.sh se llamó %d veces, se esperaba 0", n)
	}
}

// Una línea que no tiene 4 campos o cuyo TTL no es un número es un error, no un registro a medias.
func TestAC66_ParseZoneRejectsMalformed(t *testing.T) {
	for _, line := range []string{"A\tvm1.stratus.lan\t60", "A\tvm1.stratus.lan\tsesenta\t192.168.1.40"} {
		if _, err := parseZone([]byte(line + "\n")); err == nil {
			t.Errorf("parseZone(%q) sin error", line)
		}
	}
}

// AC-68, REQ-49: con el servidor DNS apagado (código 7) la lectura falla con su texto propio y deja
// una fila show_zone fallida; 20 lecturas correctas no dejan ninguna.
func TestAC68_ZoneDNSOff(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{"dns/show-zone.sh": {Stdout: []byte(zoneStdout)}}}
	env := newDNSEnv(t, runner)
	env.registerDNS(t, "dns1")
	ctx := context.Background()

	for i := 0; i < 20; i++ {
		if _, err := env.svc.Zone(ctx); err != nil {
			t.Fatalf("lectura %d: %v", i, err)
		}
	}
	runner.mu.Lock()
	runner.results["dns/show-zone.sh"] = RunResult{ExitCode: 7, Stderr: "La VM no tiene dirección IP"}
	runner.mu.Unlock()

	_, err := env.svc.Zone(ctx)

	if !errors.Is(err, ErrDNSOffZone) {
		t.Errorf("err = %v, se esperaba ErrDNSOffZone", err)
	}
	ops, _ := env.svc.History(ctx)
	if n := countOps(ops); len(ops) != 1 || n["show_zone"] != 1 || ops[0].Result != "error" || ops[0].VM != "dns1" {
		t.Errorf("historial = %+v, se esperaba una sola fila show_zone fallida de dns1", ops)
	}
}

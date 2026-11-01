package service

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"stratus-srv/internal/repository"
)

const recordsListVMs = "dns1 running 192.168.1.53\nvm1 running 192.168.1.40\nlb1 running 192.168.1.41\nvm2 poweroff -\n"

// newRecordsEnv deja dns1 como servidor DNS y vm1, lb1 (balanceador) y vm2 registrados, con
// list-vms.sh informando sus IPs y los scripts de registros respondiendo bien.
func newRecordsEnv(t *testing.T) (dnsEnv, *fakeRunner) {
	t.Helper()
	runner := &fakeRunner{results: map[string]RunResult{
		"prepare/list-vms.sh":    {Stdout: []byte(recordsListVMs)},
		"dns/add-records.sh":     {Stdout: []byte("Registros aplicados\nreloaded=yes\n")},
		"dns/remove-record.sh":   {Stdout: []byte("Registro de vm1 quitado\nreloaded=yes\n")},
		"prepare/delete-vm.sh":   {},
		"dns/refresh-records.sh": {},
	}}
	env := newDNSEnv(t, runner)
	env.registerDNS(t, "dns1")
	ctx := context.Background()
	for _, vm := range []string{"vm1", "vm2"} {
		if _, err := env.vms.Insert(ctx, vm); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := env.vms.Insert(ctx, "lb1"); err != nil {
		t.Fatal(err)
	}
	if err := env.lbs.InsertLB(ctx, repository.LoadBalancer{Name: "lb1", Subnet: 1, FrontendPort: 80}); err != nil {
		t.Fatal(err)
	}
	return env, runner
}

func (e dnsEnv) records(t *testing.T) []repository.DNSRecord {
	t.Helper()
	recs, err := e.dns.Records(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return recs
}

// AC-30: las VMs con IPv4 se agregan en un solo add-records.sh con el conjunto completo; la apagada
// se rechaza con el texto de 001 y las demás siguen. Repetir una VM con registro se rechaza, y si
// ninguna se agrega la operación falla sin llamar al script.
func TestAC30_AddRecordsPerVMResult(t *testing.T) {
	env, runner := newRecordsEnv(t)
	ctx := context.Background()

	msg, results, err := env.svc.AddRecords(ctx, []string{"vm1", "lb1", "vm2"})

	if err != nil {
		t.Fatalf("AddRecords: %v", err)
	}
	if !strings.Contains(msg, "vm1") || !strings.Contains(msg, "lb1") {
		t.Errorf("mensaje = %q", msg)
	}
	want := []RecordResult{
		{VM: "vm1", OK: true, Message: "Registro agregado"},
		{VM: "lb1", OK: true, Message: "Registro agregado"},
		{VM: "vm2", OK: false, Message: ErrVMNoIP.Error()},
	}
	if !slices.Equal(results, want) {
		t.Errorf("resultados = %+v, se esperaba %+v", results, want)
	}
	calls := runner.callsTo("dns/add-records.sh")
	wantArgs := []string{"dns1", testDomain, "192.168.1.53", "lb1,192.168.1.41", "vm1,192.168.1.40"}
	if len(calls) != 1 || !slices.Equal(calls[0].Args, wantArgs) {
		t.Errorf("add-records.sh = %+v, se esperaba %v", calls, wantArgs)
	}
	wantRecs := []repository.DNSRecord{{VM: "lb1", IP: "192.168.1.41"}, {VM: "vm1", IP: "192.168.1.40"}}
	if recs := env.records(t); !slices.Equal(recs, wantRecs) {
		t.Errorf("dns_records = %+v, se esperaba %+v", recs, wantRecs)
	}

	_, results, err = env.svc.AddRecords(ctx, []string{"vm1"})

	if !errors.Is(err, ErrRecordExists) {
		t.Errorf("vm1 otra vez: err = %v, se esperaba ErrRecordExists", err)
	}
	if len(results) != 1 || results[0].Message != "La VM ya tiene un registro DNS" {
		t.Errorf("resultados = %+v", results)
	}
	if n := len(runner.callsTo("dns/add-records.sh")); n != 1 {
		t.Errorf("add-records.sh se llamó %d veces, se esperaba 1", n)
	}

	_, _, err = env.svc.AddRecords(ctx, []string{"dns1"})
	if err == nil {
		t.Error("el servidor DNS no debía aceptarse")
	}
	if _, _, err := env.svc.AddRecords(ctx, nil); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("sin VMs: err = %v, se esperaba ErrInvalidInput", err)
	}
}

// AC-31: el registro de un servidor asignado a un balanceador usa su IP interna de list-vms.sh.
func TestAC31_AssignedServerRecordUsesInternalIP(t *testing.T) {
	env, runner := newRecordsEnv(t)
	runner.results["prepare/list-vms.sh"] = RunResult{Stdout: []byte("dns1 running 192.168.1.53\nweb-a running 10.10.1.11\n")}
	env.registerWeb(t, "web-a", WebTypeNginx)
	if err := env.lbs.InsertServer(context.Background(), repository.LBServer{LB: "lb1", Name: "server1", VM: "web-a",
		Port: 80, LastIP: "10.10.1.11", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	if _, _, err := env.svc.AddRecords(context.Background(), []string{"web-a"}); err != nil {
		t.Fatalf("AddRecords: %v", err)
	}

	calls := runner.callsTo("dns/add-records.sh")
	if len(calls) != 1 || !slices.Contains(calls[0].Args, "web-a,10.10.1.11") {
		t.Errorf("add-records.sh = %+v, se esperaba web-a,10.10.1.11", calls)
	}
}

// AC-33: quitar un registro llama a remove-record.sh con los restantes y borra la fila; quitarlo
// otra vez termina bien sin llamar al script.
func TestAC33_RemoveRecordIdempotent(t *testing.T) {
	env, runner := newRecordsEnv(t)
	ctx := context.Background()
	if err := env.dns.InsertRecords(ctx, []repository.DNSRecord{{VM: "vm1", IP: "192.168.1.40"},
		{VM: "lb1", IP: "192.168.1.41"}}); err != nil {
		t.Fatal(err)
	}

	msg, err := env.svc.RemoveRecord(ctx, "vm1")

	if err != nil || msg != "Registro de vm1 quitado" {
		t.Fatalf("RemoveRecord = %q, %v", msg, err)
	}
	calls := runner.callsTo("dns/remove-record.sh")
	want := []string{"dns1", testDomain, "192.168.1.53", "vm1", "lb1,192.168.1.41"}
	if len(calls) != 1 || !slices.Equal(calls[0].Args, want) {
		t.Errorf("remove-record.sh = %+v, se esperaba %v", calls, want)
	}
	if recs := env.records(t); len(recs) != 1 || recs[0].VM != "lb1" {
		t.Errorf("dns_records = %+v, se esperaba solo lb1", recs)
	}
	if op := env.lastDNSOperation(t); op.Operation != "remove_record" || op.VM != "dns1" {
		t.Errorf("historial = %+v, se esperaba remove_record en dns1", op)
	}

	if _, err := env.svc.RemoveRecord(ctx, "vm1"); err != nil {
		t.Errorf("otra vez: err = %v", err)
	}
	if n := len(runner.callsTo("dns/remove-record.sh")); n != 1 {
		t.Errorf("remove-record.sh se llamó %d veces, se esperaba 1", n)
	}
}

// AC-52: la fila de add_records tiene el servidor DNS como VM y las VMs agregadas en los argumentos.
func TestAC52_AddRecordsHistoryRow(t *testing.T) {
	env, _ := newRecordsEnv(t)

	if _, _, err := env.svc.AddRecords(context.Background(), []string{"vm1", "lb1"}); err != nil {
		t.Fatalf("AddRecords: %v", err)
	}

	op := env.lastDNSOperation(t)
	args := strings.Join(op.Args, " ")
	if op.Operation != "add_records" || op.VM != "dns1" || !strings.Contains(args, "vm1,") ||
		!strings.Contains(args, "lb1,") {
		t.Errorf("historial = %+v", op)
	}
}

// AC-35: con el servidor DNS apagado (exit 7), agregar y quitar fallan con ErrDNSOff y SQLite no
// cambia.
func TestAC35_DNSOffMapsError(t *testing.T) {
	t.Run("agregar", func(t *testing.T) {
		env, runner := newRecordsEnv(t)
		runner.results["dns/add-records.sh"] = RunResult{ExitCode: 7}

		_, _, err := env.svc.AddRecords(context.Background(), []string{"vm1"})

		if !errors.Is(err, ErrDNSOff) {
			t.Errorf("err = %v, se esperaba ErrDNSOff", err)
		}
		if recs := env.records(t); len(recs) != 0 {
			t.Errorf("dns_records = %+v, se esperaba vacío", recs)
		}
	})
	t.Run("quitar", func(t *testing.T) {
		env, runner := newRecordsEnv(t)
		runner.results["dns/remove-record.sh"] = RunResult{ExitCode: 7}
		if err := env.dns.InsertRecords(context.Background(), []repository.DNSRecord{{VM: "vm1", IP: "192.168.1.40"}}); err != nil {
			t.Fatal(err)
		}

		_, err := env.svc.RemoveRecord(context.Background(), "vm1")

		if !errors.Is(err, ErrDNSOff) {
			t.Errorf("err = %v, se esperaba ErrDNSOff", err)
		}
		if recs := env.records(t); len(recs) != 1 {
			t.Errorf("dns_records = %+v, se esperaba el de vm1", recs)
		}
	})
	t.Run("actualizar", func(t *testing.T) {
		env, runner := newRecordsEnv(t)
		runner.results["dns/refresh-records.sh"] = RunResult{ExitCode: 7}
		if err := env.dns.InsertRecords(context.Background(), []repository.DNSRecord{{VM: "vm1", IP: "192.168.1.40"}}); err != nil {
			t.Fatal(err)
		}

		_, err := env.svc.Refresh(context.Background())

		if !errors.Is(err, ErrDNSOff) {
			t.Errorf("err = %v, se esperaba ErrDNSOff", err)
		}
		if recs := env.records(t); len(recs) != 1 || recs[0].IP != "192.168.1.40" {
			t.Errorf("dns_records = %+v, se esperaba vm1 sin cambios", recs)
		}
	})
}

// AC-41: una zona rechazada por named-checkzone (exit 10) es ErrZoneInvalid, SQLite no cambia y la
// salida del chequeo queda en el stderr del historial.
func TestAC41_ZoneInvalidMapsError(t *testing.T) {
	const check = "zone stratus.lan/IN: bad owner name"
	t.Run("agregar", func(t *testing.T) {
		env, runner := newRecordsEnv(t)
		runner.results["dns/add-records.sh"] = RunResult{ExitCode: 10,
			Stderr: check + "\nZona DNS inválida; se mantuvo la anterior\n"}

		_, _, err := env.svc.AddRecords(context.Background(), []string{"vm1"})

		if !errors.Is(err, ErrZoneInvalid) {
			t.Errorf("err = %v, se esperaba ErrZoneInvalid", err)
		}
		if recs := env.records(t); len(recs) != 0 {
			t.Errorf("dns_records = %+v, se esperaba vacío", recs)
		}
		if op := env.lastDNSOperation(t); !strings.Contains(op.Stderr, check) {
			t.Errorf("stderr del historial = %q", op.Stderr)
		}
	})
	t.Run("quitar", func(t *testing.T) {
		env, runner := newRecordsEnv(t)
		runner.results["dns/remove-record.sh"] = RunResult{ExitCode: 10, Stderr: check + "\n"}
		if err := env.dns.InsertRecords(context.Background(), []repository.DNSRecord{{VM: "vm1", IP: "192.168.1.40"}}); err != nil {
			t.Fatal(err)
		}

		_, err := env.svc.RemoveRecord(context.Background(), "vm1")

		if !errors.Is(err, ErrZoneInvalid) {
			t.Errorf("err = %v, se esperaba ErrZoneInvalid", err)
		}
		if recs := env.records(t); len(recs) != 1 {
			t.Errorf("dns_records = %+v, se esperaba el de vm1", recs)
		}
	})
	t.Run("actualizar", func(t *testing.T) {
		env, runner := newRecordsEnv(t)
		runner.results["dns/refresh-records.sh"] = RunResult{ExitCode: 10, Stdout: []byte("vm1 192.168.1.45\n"),
			Stderr: check + "\n"}
		if err := env.dns.InsertRecords(context.Background(), []repository.DNSRecord{{VM: "vm1", IP: "192.168.1.40"}}); err != nil {
			t.Fatal(err)
		}

		_, err := env.svc.Refresh(context.Background())

		if !errors.Is(err, ErrZoneInvalid) {
			t.Errorf("err = %v, se esperaba ErrZoneInvalid", err)
		}
		if recs := env.records(t); len(recs) != 1 || recs[0].IP != "192.168.1.40" {
			t.Errorf("dns_records = %+v, se esperaba vm1 sin cambios", recs)
		}
		if op := env.lastDNSOperation(t); !strings.Contains(op.Stderr, check) {
			t.Errorf("stderr del historial = %q", op.Stderr)
		}
	})
}

// REQ-25: sin servidor DNS, agregar y quitar registros se rechazan con ErrNoDNSServer; los hooks
// de otros módulos no hacen nada.
func TestAC35_RecordsWithoutDNSServer(t *testing.T) {
	runner := &fakeRunner{}
	env := newDNSEnv(t, runner)
	ctx := context.Background()

	if _, _, err := env.svc.AddRecords(ctx, []string{"vm1"}); !errors.Is(err, ErrNoDNSServer) {
		t.Errorf("AddRecords: err = %v", err)
	}
	if _, err := env.svc.RemoveRecord(ctx, "vm1"); !errors.Is(err, ErrNoDNSServer) {
		t.Errorf("RemoveRecord: err = %v", err)
	}
	if err := env.svc.AddRecordsFor(ctx, "web-a", "192.168.1.40"); err != nil {
		t.Errorf("AddRecordsFor: err = %v", err)
	}
	if w := env.svc.AfterVMDeleted(ctx, "vm1"); w != "" {
		t.Errorf("AfterVMDeleted: advertencia %q", w)
	}
	if n := runner.callCount(); n != 0 {
		t.Errorf("el runner se llamó %d veces, se esperaba 0", n)
	}
}

// REQ-24, REQ-33: AfterVMDeleted borra el resolvedor y el registro de la VM eliminada y aplica el
// conjunto restante; si el script falla, la fila ya no está, queda una fila fallida y se devuelve la
// advertencia.
func TestAC34_AfterVMDeleted(t *testing.T) {
	env, runner := newRecordsEnv(t)
	ctx := context.Background()
	if err := env.dns.InsertRecords(ctx, []repository.DNSRecord{{VM: "vm1", IP: "192.168.1.40"}}); err != nil {
		t.Fatal(err)
	}
	if err := env.dns.SetResolver(ctx, "vm1", true); err != nil {
		t.Fatal(err)
	}
	runner.results["dns/remove-record.sh"] = RunResult{ExitCode: 7}

	w := env.svc.AfterVMDeleted(ctx, "vm1")

	if w != "VM eliminada; no se pudo quitar su registro DNS, use Actualizar registros" {
		t.Errorf("advertencia = %q", w)
	}
	if recs := env.records(t); len(recs) != 0 {
		t.Errorf("dns_records = %+v, se esperaba vacío", recs)
	}
	if res, _ := env.dns.Resolvers(ctx); len(res) != 0 {
		t.Errorf("dns_resolvers = %v, se esperaba vacío", res)
	}
	if op := env.lastDNSOperation(t); op.Operation != "remove_record" || op.Result != "error" {
		t.Errorf("historial = %+v, se esperaba remove_record fallido", op)
	}

	if w := env.svc.AfterVMDeleted(ctx, "vm2"); w != "" {
		t.Errorf("vm2 sin registro: advertencia %q", w)
	}
}

// REQ-5, REQ-26: AddRecordsFor agrega el registro de una VM nueva; con el servidor DNS bloqueado,
// el rechazo queda como fila fallida en el historial de dns.
func TestAC6_AddRecordsForHook(t *testing.T) {
	env, runner := newRecordsEnv(t)
	ctx := context.Background()

	if err := env.svc.AddRecordsFor(ctx, "vm1", "192.168.1.40"); err != nil {
		t.Fatalf("AddRecordsFor: %v", err)
	}
	calls := runner.callsTo("dns/add-records.sh")
	if len(calls) != 1 || !slices.Equal(calls[0].Args[3:], []string{"vm1,192.168.1.40"}) {
		t.Errorf("add-records.sh = %+v", calls)
	}
	if recs := env.records(t); len(recs) != 1 {
		t.Errorf("dns_records = %+v", recs)
	}

	if !env.locks.TryLock("dns1") {
		t.Fatal("no se pudo bloquear dns1")
	}
	defer env.locks.Unlock("dns1")

	err := env.svc.AddRecordsFor(ctx, "vm2", "192.168.1.42")

	if !errors.Is(err, ErrOperationInProgress) {
		t.Errorf("err = %v, se esperaba ErrOperationInProgress", err)
	}
	if op := env.lastDNSOperation(t); op.Operation != "add_records" || op.Result != "error" {
		t.Errorf("historial = %+v, se esperaba add_records fallido", op)
	}
}

// AC-39, REQ-27: Refresh pasa todas las VMs con registro como espera y guarda las IPs impresas;
// con reloaded=no informa "Registros sin cambios".
func TestAC39_RefreshStoresPrintedIPs(t *testing.T) {
	cases := []struct {
		name, stdout, msg string
	}{
		{"recargada", "lb1 192.168.1.41\nvm1 192.168.1.45\nreloaded=yes\n", "Registros actualizados"},
		{"sin cambios", "lb1 192.168.1.41\nvm1 192.168.1.45\nreloaded=no\n", "Registros sin cambios"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env, runner := newRecordsEnv(t)
			runner.results["dns/refresh-records.sh"] = RunResult{Stdout: []byte(tc.stdout)}
			ctx := context.Background()
			if err := env.dns.InsertRecords(ctx, []repository.DNSRecord{{VM: "vm1", IP: "192.168.1.40"},
				{VM: "lb1", IP: "192.168.1.41"}}); err != nil {
				t.Fatal(err)
			}

			msg, err := env.svc.Refresh(ctx)

			if err != nil || msg != tc.msg {
				t.Fatalf("Refresh = %q, %v; se esperaba %q", msg, err, tc.msg)
			}
			calls := runner.callsTo("dns/refresh-records.sh")
			want := []string{"dns1", "stratus.lan", "192.168.1.53", "lb1,vm1", "lb1,192.168.1.41", "vm1,192.168.1.40"}
			if len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, want) {
				t.Errorf("llamadas = %+v, se esperaba %v", calls, want)
			}
			want2 := []repository.DNSRecord{{VM: "lb1", IP: "192.168.1.41"}, {VM: "vm1", IP: "192.168.1.45"}}
			if recs := env.records(t); !reflect.DeepEqual(recs, want2) {
				t.Errorf("dns_records = %+v, se esperaba %+v", recs, want2)
			}
			if op := env.lastDNSOperation(t); op.Operation != "refresh_records" || op.VM != "dns1" || op.Result != "ok" {
				t.Errorf("historial = %+v", op)
			}
		})
	}
}

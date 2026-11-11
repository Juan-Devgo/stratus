package repository

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// TestAC1_WebAndDNSRepositories (DNS) recorre el servidor DNS, sus registros y los resolvedores
// (AC-22, AC-27, AC-29, AC-30, AC-42).
func TestAC1_WebAndDNSRepositories_DNS(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	vms := NewVMRepository(db)
	repo := NewDNSRepository(db)

	t.Run("sin servidor", func(t *testing.T) {
		name, ok, err := repo.Server(ctx)
		if err != nil || ok || name != "" {
			t.Errorf("Server = %q, %v, %v; se esperaba vacío", name, ok, err)
		}
	})

	t.Run("insertar servidor con registros iniciales", func(t *testing.T) {
		mustInsert(t, vms, "dns1")
		initial := []DNSRecord{{"web-b", "192.168.1.41"}, {"web-a", "192.168.1.40"}}
		if err := repo.InsertServer(ctx, "dns1", initial); err != nil {
			t.Fatalf("InsertServer: %v", err)
		}
		name, ok, err := repo.Server(ctx)
		if err != nil || !ok || name != "dns1" {
			t.Fatalf("Server = %q, %v, %v", name, ok, err)
		}
		recs, err := repo.Records(ctx)
		if err != nil {
			t.Fatalf("Records: %v", err)
		}
		if want := []DNSRecord{{"web-a", "192.168.1.40"}, {"web-b", "192.168.1.41"}}; !reflect.DeepEqual(recs, want) {
			t.Errorf("Records = %+v, se esperaba %+v", recs, want)
		}
	})

	t.Run("un segundo servidor se rechaza", func(t *testing.T) {
		if err := repo.InsertServer(ctx, "dns2", nil); err == nil {
			t.Error("InsertServer(dns2): se esperaba un error")
		}
	})

	t.Run("registros", func(t *testing.T) {
		if err := repo.InsertRecords(ctx, []DNSRecord{{"vm1", "192.168.1.50"}}); err != nil {
			t.Fatalf("InsertRecords: %v", err)
		}
		if err := repo.InsertRecords(ctx, []DNSRecord{{"vm1", "192.168.1.51"}}); err == nil {
			t.Error("InsertRecords repetido: se esperaba un error")
		}
		rec, ok, err := repo.Record(ctx, "vm1")
		if err != nil || !ok || rec != (DNSRecord{"vm1", "192.168.1.50"}) {
			t.Errorf("Record = %+v, %v, %v", rec, ok, err)
		}
		if _, ok, err := repo.Record(ctx, "nada"); err != nil || ok {
			t.Errorf("Record(nada) = %v, %v", ok, err)
		}
		if err := repo.UpdateRecordIPs(ctx, []DNSRecord{{"vm1", "192.168.1.55"}, {"web-a", "192.168.1.45"}}); err != nil {
			t.Fatalf("UpdateRecordIPs: %v", err)
		}
		recs, _ := repo.Records(ctx)
		want := []DNSRecord{{"vm1", "192.168.1.55"}, {"web-a", "192.168.1.45"}, {"web-b", "192.168.1.41"}}
		if !reflect.DeepEqual(recs, want) {
			t.Errorf("Records = %+v, se esperaba %+v", recs, want)
		}
		if err := repo.DeleteRecord(ctx, "vm1"); err != nil {
			t.Fatalf("DeleteRecord: %v", err)
		}
		if err := repo.DeleteRecord(ctx, "vm1"); err != nil {
			t.Errorf("DeleteRecord repetido: %v", err)
		}
		// DeleteRecords: una lista vacía no cambia nada; una lista borra solo esas filas.
		if err := repo.DeleteRecords(ctx, nil); err != nil {
			t.Fatalf("DeleteRecords(nil): %v", err)
		}
		recs, _ = repo.Records(ctx)
		if want := []DNSRecord{{"web-a", "192.168.1.45"}, {"web-b", "192.168.1.41"}}; !reflect.DeepEqual(recs, want) {
			t.Errorf("tras DeleteRecords(nil) Records = %+v, se esperaba %+v", recs, want)
		}
		if err := repo.InsertRecords(ctx, []DNSRecord{{"vm3", "192.168.1.60"}}); err != nil {
			t.Fatalf("InsertRecords vm3: %v", err)
		}
		if err := repo.DeleteRecords(ctx, []string{"web-a", "vm3", "nada"}); err != nil {
			t.Fatalf("DeleteRecords: %v", err)
		}
		recs, _ = repo.Records(ctx)
		if want := []DNSRecord{{"web-b", "192.168.1.41"}}; !reflect.DeepEqual(recs, want) {
			t.Errorf("tras DeleteRecords Records = %+v, se esperaba %+v", recs, want)
		}
	})

	t.Run("resolvedores", func(t *testing.T) {
		for _, vm := range []string{"vm2", "vm1", "vm1"} {
			if err := repo.SetResolver(ctx, vm, true); err != nil {
				t.Fatalf("SetResolver(%s, true): %v", vm, err)
			}
		}
		got, err := repo.Resolvers(ctx)
		if err != nil || !reflect.DeepEqual(got, []string{"vm1", "vm2"}) {
			t.Errorf("Resolvers = %v, %v", got, err)
		}
		for _, vm := range []string{"vm1", "vm1", "vm2"} {
			if err := repo.SetResolver(ctx, vm, false); err != nil {
				t.Fatalf("SetResolver(%s, false): %v", vm, err)
			}
		}
		got, _ = repo.Resolvers(ctx)
		if len(got) != 0 {
			t.Errorf("Resolvers = %v, se esperaba vacío", got)
		}
	})

	t.Run("borrar el servidor borra registros y la fila de vms", func(t *testing.T) {
		if err := repo.DeleteServer(ctx); err != nil {
			t.Fatalf("DeleteServer: %v", err)
		}
		if _, ok, _ := repo.Server(ctx); ok {
			t.Error("el servidor sigue registrado")
		}
		recs, _ := repo.Records(ctx)
		if len(recs) != 0 {
			t.Errorf("Records = %+v, se esperaba vacío", recs)
		}
		if _, err := vms.Get(ctx, "dns1"); !errors.Is(err, ErrVMNotFound) {
			t.Errorf("vms.Get(dns1): err = %v, se esperaba ErrVMNotFound", err)
		}
	})
}

package repository

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// TestAC1_LBRepository recorre el ciclo de vida de un balanceador y sus servidores (AC-1, AC-13).
func TestAC1_LBRepository(t *testing.T) {
	ctx := context.Background()
	repo := NewLBRepository(openTestDB(t))

	t.Run("insertar, obtener y listar", func(t *testing.T) {
		for _, lb := range []LoadBalancer{{"lb3", 3, 80}, {"lb1", 1, 80}} {
			if err := repo.InsertLB(ctx, lb); err != nil {
				t.Fatalf("InsertLB(%v): %v", lb, err)
			}
		}
		got, err := repo.GetLB(ctx, "lb1")
		if err != nil || got != (LoadBalancer{"lb1", 1, 80}) {
			t.Fatalf("GetLB = %+v, %v", got, err)
		}
		list, err := repo.ListLBs(ctx)
		if err != nil {
			t.Fatalf("ListLBs: %v", err)
		}
		if want := []LoadBalancer{{"lb1", 1, 80}, {"lb3", 3, 80}}; !reflect.DeepEqual(list, want) {
			t.Errorf("ListLBs = %+v, se esperaba %+v", list, want)
		}
		subnets, err := repo.Subnets(ctx)
		if err != nil || !reflect.DeepEqual(subnets, []int{1, 3}) {
			t.Errorf("Subnets = %v, %v; se esperaba [1 3]", subnets, err)
		}
	})

	t.Run("una subred repetida se rechaza", func(t *testing.T) {
		if err := repo.InsertLB(ctx, LoadBalancer{"lb9", 1, 80}); err == nil {
			t.Error("InsertLB con la subred 1 repetida: se esperaba un error")
		}
	})

	t.Run("balanceador inexistente", func(t *testing.T) {
		if _, err := repo.GetLB(ctx, "nada"); !errors.Is(err, ErrLBNotFound) {
			t.Errorf("GetLB: err = %v, se esperaba ErrLBNotFound", err)
		}
		if err := repo.SetFrontendPort(ctx, "nada", 8080); !errors.Is(err, ErrLBNotFound) {
			t.Errorf("SetFrontendPort: err = %v, se esperaba ErrLBNotFound", err)
		}
		if err := repo.DeleteLB(ctx, "nada"); !errors.Is(err, ErrLBNotFound) {
			t.Errorf("DeleteLB: err = %v, se esperaba ErrLBNotFound", err)
		}
	})

	t.Run("puerto del frontend", func(t *testing.T) {
		if err := repo.SetFrontendPort(ctx, "lb1", 8080); err != nil {
			t.Fatalf("SetFrontendPort: %v", err)
		}
		got, _ := repo.GetLB(ctx, "lb1")
		if got.FrontendPort != 8080 {
			t.Errorf("FrontendPort = %d, se esperaba 8080", got.FrontendPort)
		}
	})

	t.Run("servidores ordenados por número", func(t *testing.T) {
		// server10 va después de server2 aunque como texto sea menor.
		for _, s := range []LBServer{
			{"lb1", "server10", "vm10", 80, "10.10.1.20", true},
			{"lb1", "server2", "vm2", 8000, "10.10.1.12", true},
			{"lb1", "server1", "vm1", 80, "10.10.1.11", true},
			{"lb3", "server1", "vm-otro", 80, "10.10.3.11", true},
		} {
			if err := repo.InsertServer(ctx, s); err != nil {
				t.Fatalf("InsertServer(%v): %v", s, err)
			}
		}
		rows, err := repo.Servers(ctx, "lb1")
		if err != nil {
			t.Fatalf("Servers: %v", err)
		}
		var names []string
		for _, r := range rows {
			names = append(names, r.Name)
		}
		if want := []string{"server1", "server2", "server10"}; !reflect.DeepEqual(names, want) {
			t.Errorf("orden = %v, se esperaba %v", names, want)
		}
		if err := repo.InsertServer(ctx, LBServer{"lb3", "server2", "vm1", 80, "10.10.3.12", true}); err == nil {
			t.Error("InsertServer con una VM ya asignada: se esperaba un error")
		}
	})

	t.Run("ServerByVM", func(t *testing.T) {
		s, ok, err := repo.ServerByVM(ctx, "vm2")
		if err != nil || !ok || s.LB != "lb1" || s.Name != "server2" || s.Port != 8000 || s.LastIP != "10.10.1.12" || !s.Enabled {
			t.Errorf("ServerByVM(vm2) = %+v, %v, %v", s, ok, err)
		}
		if _, ok, err := repo.ServerByVM(ctx, "libre"); err != nil || ok {
			t.Errorf("ServerByVM(libre) = %v, %v; se esperaba false, nil", ok, err)
		}
	})

	t.Run("UpdateServerIPs en una transacción", func(t *testing.T) {
		err := repo.UpdateServerIPs(ctx, "lb1", []LBServer{
			{LB: "lb1", Name: "server1", LastIP: "10.10.1.15", Enabled: true},
			{LB: "lb1", Name: "server2", LastIP: "10.10.1.12", Enabled: false},
		})
		if err != nil {
			t.Fatalf("UpdateServerIPs: %v", err)
		}
		rows, _ := repo.Servers(ctx, "lb1")
		if rows[0].LastIP != "10.10.1.15" || !rows[0].Enabled || rows[1].Enabled || rows[1].LastIP != "10.10.1.12" {
			t.Errorf("filas tras actualizar = %+v", rows)
		}
		if rows[0].Port != 80 || rows[0].VM != "vm1" {
			t.Errorf("la actualización no debe tocar puerto ni VM: %+v", rows[0])
		}

		// Una fila inexistente deshace toda la actualización.
		err = repo.UpdateServerIPs(ctx, "lb1", []LBServer{
			{LB: "lb1", Name: "server1", LastIP: "10.10.1.99", Enabled: false},
			{LB: "lb1", Name: "server7", LastIP: "10.10.1.17", Enabled: true},
		})
		if err == nil {
			t.Fatal("UpdateServerIPs con una fila inexistente: se esperaba un error")
		}
		rows, _ = repo.Servers(ctx, "lb1")
		if rows[0].LastIP != "10.10.1.15" || !rows[0].Enabled {
			t.Errorf("la transacción no se deshizo: %+v", rows[0])
		}
	})

	t.Run("DeleteServer", func(t *testing.T) {
		if err := repo.DeleteServer(ctx, "lb1", "vm2"); err != nil {
			t.Fatalf("DeleteServer: %v", err)
		}
		if _, ok, _ := repo.ServerByVM(ctx, "vm2"); ok {
			t.Error("vm2 sigue asignada")
		}
		// Repetirlo no falla: quitar un servidor se puede reintentar.
		if err := repo.DeleteServer(ctx, "lb1", "vm2"); err != nil {
			t.Errorf("DeleteServer repetido: %v", err)
		}
	})

	t.Run("DeleteLB borra también sus servidores", func(t *testing.T) {
		if err := repo.DeleteLB(ctx, "lb1"); err != nil {
			t.Fatalf("DeleteLB: %v", err)
		}
		if _, err := repo.GetLB(ctx, "lb1"); !errors.Is(err, ErrLBNotFound) {
			t.Errorf("GetLB tras borrar: err = %v", err)
		}
		if rows, _ := repo.Servers(ctx, "lb1"); len(rows) != 0 {
			t.Errorf("quedaron %d servidores de lb1", len(rows))
		}
		if rows, _ := repo.Servers(ctx, "lb3"); len(rows) != 1 {
			t.Errorf("los servidores de lb3 no deben cambiar: %+v", rows)
		}
	})
}

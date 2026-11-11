package repository

import (
	"context"
	"reflect"
	"testing"
)

// TestAC1_WebAndDNSRepositories (web) recorre el ciclo de vida de un servidor web (AC-1, AC-8).
func TestAC1_WebAndDNSRepositories_Web(t *testing.T) {
	ctx := context.Background()
	repo := NewWebRepository(openTestDB(t))

	for _, w := range []WebServer{{"web-b", "apache"}, {"web-a", "nginx"}} {
		if err := repo.Insert(ctx, w); err != nil {
			t.Fatalf("Insert(%v): %v", w, err)
		}
	}
	if err := repo.Insert(ctx, WebServer{"web-c", "iis"}); err == nil {
		t.Error("Insert con tipo iis: se esperaba un error")
	}
	if err := repo.Insert(ctx, WebServer{"web-a", "apache"}); err == nil {
		t.Error("Insert repetido: se esperaba un error")
	}

	got, ok, err := repo.Get(ctx, "web-a")
	if err != nil || !ok || got != (WebServer{"web-a", "nginx"}) {
		t.Fatalf("Get = %+v, %v, %v", got, ok, err)
	}
	if _, ok, err := repo.Get(ctx, "nada"); err != nil || ok {
		t.Errorf("Get(nada) = %v, %v; se esperaba false, nil", ok, err)
	}

	list, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if want := []WebServer{{"web-a", "nginx"}, {"web-b", "apache"}}; !reflect.DeepEqual(list, want) {
		t.Errorf("List = %+v, se esperaba %+v", list, want)
	}

	if err := repo.Delete(ctx, "web-a"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := repo.Delete(ctx, "web-a"); err != nil {
		t.Errorf("Delete repetido: %v, se esperaba nil", err)
	}
	if _, ok, _ := repo.Get(ctx, "web-a"); ok {
		t.Error("web-a sigue registrado tras Delete")
	}
}

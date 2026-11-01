package service

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"stratus-srv/internal/repository"
)

func TestAC6_InstallRemoveInvokeScriptWithPackage(t *testing.T) {
	tests := []struct {
		name   string
		script string
		call   func(svc *PrepareService) (string, error)
	}{
		{"instalar", "prepare/install-package.sh", func(svc *PrepareService) (string, error) {
			return svc.InstallPackage(context.Background(), "vm1", "nginx")
		}},
		{"eliminar", "prepare/remove-package.sh", func(svc *PrepareService) (string, error) {
			return svc.RemovePackage(context.Background(), "vm1", "nginx")
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runner := &fakeRunner{}
			env := newTestEnv(t, runner)
			env.register(t, "vm1")

			msg, err := tc.call(env.svc)
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if msg == "" {
				t.Error("se esperaba un mensaje de éxito")
			}
			calls := runner.callsTo(tc.script)
			if len(calls) != 1 {
				t.Fatalf("%s se llamó %d veces, se esperaba 1", tc.script, len(calls))
			}
			want := []string{"vm1", "nginx"}
			if !reflect.DeepEqual(calls[0].Args, want) {
				t.Errorf("argv = %q, se esperaba %q", calls[0].Args, want)
			}
			if op := env.lastOperation(t); op.Result != "ok" || op.Command != tc.script {
				t.Errorf("historial = %+v, se esperaba %s con resultado ok", op, tc.script)
			}
		})
	}
}

func TestAC7_InvalidPackageSkipsScript(t *testing.T) {
	for _, pkg := range []string{"nginx; rm -rf /", ""} {
		t.Run(pkg, func(t *testing.T) {
			runner := &fakeRunner{}
			env := newTestEnv(t, runner)
			env.register(t, "vm1")

			_, installErr := env.svc.InstallPackage(context.Background(), "vm1", pkg)
			_, removeErr := env.svc.RemovePackage(context.Background(), "vm1", pkg)

			for _, err := range []error{installErr, removeErr} {
				if !errors.Is(err, ErrInvalidInput) {
					t.Errorf("err = %v, se esperaba ErrInvalidInput", err)
				}
			}
			if n := runner.callCount(); n != 0 {
				t.Errorf("el runner se llamó %d veces, se esperaba 0", n)
			}
		})
	}
}

func TestAC32_InstallPassesOnlyNameAndPackage(t *testing.T) {
	runner := &fakeRunner{}
	env := newTestEnv(t, runner)
	env.register(t, "vm1") // VM recién creada: ya tiene SSH desde la creación

	msg, err := env.svc.InstallPackage(context.Background(), "vm1", "openssh-server")
	if err != nil {
		t.Fatalf("InstallPackage(openssh-server): %v", err)
	}
	if msg != "Paquete openssh-server instalado en vm1" {
		t.Errorf("mensaje = %q", msg)
	}
	calls := runner.callsTo("prepare/install-package.sh")
	if len(calls) != 1 {
		t.Fatalf("install-package.sh se llamó %d veces, se esperaba 1", len(calls))
	}
	if want := []string{"vm1", "openssh-server"}; !reflect.DeepEqual(calls[0].Args, want) {
		t.Errorf("argv = %q, se esperaba %q", calls[0].Args, want)
	}
}

func TestAC33_RemoveOpenSSHBlocked(t *testing.T) {
	runner := &fakeRunner{}
	env := newTestEnv(t, runner)
	env.register(t, "vm1") // VM recién creada

	_, err := env.svc.RemovePackage(context.Background(), "vm1", "openssh-server")
	if !errors.Is(err, ErrSSHRemovalBlocked) {
		t.Fatalf("err = %v, se esperaba ErrSSHRemovalBlocked", err)
	}
	if err.Error() != "No se puede eliminar openssh-server: el backend lo usa para conectarse" {
		t.Errorf("mensaje = %q, se esperaba el de REQ-25", err.Error())
	}
	if n := runner.callCount(); n != 0 {
		t.Errorf("el runner se llamó %d veces, se esperaba 0", n)
	}
}

func TestAC34_InstallUfwPassesPackage(t *testing.T) {
	runner := &fakeRunner{}
	env := newTestEnv(t, runner)
	env.register(t, "vm1")

	if _, err := env.svc.InstallPackage(context.Background(), "vm1", "ufw"); err != nil {
		t.Fatalf("InstallPackage(ufw): %v", err)
	}
	calls := runner.callsTo("prepare/install-package.sh")
	if len(calls) != 1 {
		t.Fatalf("install-package.sh se llamó %d veces, se esperaba 1", len(calls))
	}
	if want := []string{"vm1", "ufw"}; !reflect.DeepEqual(calls[0].Args, want) {
		t.Errorf("argv = %q, se esperaba %q", calls[0].Args, want)
	}
}

func TestAC6_CatalogHasEightPackages(t *testing.T) {
	want := []string{"openssh-server", "haproxy", "nginx", "apache2", "curl", "htop", "git", "ufw"}
	if !reflect.DeepEqual(PackageCatalog, want) {
		t.Fatalf("PackageCatalog = %q, se esperaba %q", PackageCatalog, want)
	}
	for _, pkg := range PackageCatalog {
		if err := ValidatePackage(pkg); err != nil {
			t.Errorf("ValidatePackage(%q) = %v, el catálogo solo debe tener nombres válidos", pkg, err)
		}
	}
}

// AC-49: en un balanceador, quitar haproxy se rechaza y los paquetes llevan --frontend <puerto>.
func TestAC49_PackageRulesOnLB(t *testing.T) {
	runner := &fakeRunner{}
	env := newHAProxyEnv(t, runner)
	env.registerLB(t, "lb1", 1, 80)
	ctx := context.Background()

	if _, err := env.prepare.RemovePackage(ctx, "lb1", "haproxy"); !errors.Is(err, ErrHAProxyRemovalBlocked) {
		t.Fatalf("RemovePackage(haproxy): err = %v, se esperaba ErrHAProxyRemovalBlocked", err)
	}
	if n := runner.callCount(); n != 0 {
		t.Errorf("el runner se llamó %d veces, se esperaban 0", n)
	}

	if _, err := env.prepare.InstallPackage(ctx, "lb1", "ufw"); err != nil {
		t.Fatalf("InstallPackage(ufw): %v", err)
	}
	if got, want := runner.callsTo("prepare/install-package.sh")[0].Args, []string{"--frontend", "80", "lb1", "ufw"}; !reflect.DeepEqual(got, want) {
		t.Errorf("install argv = %q, se esperaba %q", got, want)
	}

	// Con otro puerto de frontend guardado, el argumento lo sigue.
	if err := env.lbs.SetFrontendPort(ctx, "lb1", 8080); err != nil {
		t.Fatal(err)
	}
	if _, err := env.prepare.RemovePackage(ctx, "lb1", "nginx"); err != nil {
		t.Fatalf("RemovePackage(nginx): %v", err)
	}
	if got, want := runner.callsTo("prepare/remove-package.sh")[0].Args, []string{"--frontend", "8080", "lb1", "nginx"}; !reflect.DeepEqual(got, want) {
		t.Errorf("remove argv = %q, se esperaba %q", got, want)
	}
}

// Otra VM puede quitar haproxy: la regla es solo de los balanceadores.
func TestRemoveHAProxyOnPlainVMAllowed(t *testing.T) {
	runner := &fakeRunner{}
	env := newHAProxyEnv(t, runner)
	env.register(t, "vm1")

	if _, err := env.prepare.RemovePackage(context.Background(), "vm1", "haproxy"); err != nil {
		t.Fatalf("RemovePackage: %v", err)
	}
	if got := runner.callsTo("prepare/remove-package.sh")[0].Args; !reflect.DeepEqual(got, []string{"vm1", "haproxy"}) {
		t.Errorf("argv = %q, se esperaba [vm1 haproxy]", got)
	}
}

// AC-27, AC-29: en un servidor asignado los paquetes llevan --jump <lb> antes de la VM.
func TestAC29_PackageOpsOnServerPassJump(t *testing.T) {
	runner := &fakeRunner{}
	env := newHAProxyEnv(t, runner)
	env.registerLB(t, "lb1", 1, 80)
	env.registerServer(t, "lb1", "server1", "web-a", 80, "10.10.1.11")
	ctx := context.Background()

	if _, err := env.prepare.InstallPackage(ctx, "web-a", "nginx"); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}
	if _, err := env.prepare.RemovePackage(ctx, "web-a", "haproxy"); err != nil {
		t.Fatalf("RemovePackage: %v", err)
	}

	if got, want := runner.callsTo("prepare/install-package.sh")[0].Args, []string{"--jump", "lb1", "web-a", "nginx"}; !reflect.DeepEqual(got, want) {
		t.Errorf("install argv = %q, se esperaba %q", got, want)
	}
	if got, want := runner.callsTo("prepare/remove-package.sh")[0].Args, []string{"--jump", "lb1", "web-a", "haproxy"}; !reflect.DeepEqual(got, want) {
		t.Errorf("remove argv = %q, se esperaba %q", got, want)
	}
}

// AC-9: en un servidor Nginx, quitar nginx o instalar apache2 se rechaza con el texto de la
// especificación sin llamar al script, y los paquetes llevan --role web-nginx (ufw permite 80/tcp).
func TestAC9_PackageRulesOnWebServer(t *testing.T) {
	runner := &fakeRunner{}
	env := newWebEnv(t, runner)
	env.registerWeb(t, "web-a", WebTypeNginx)
	env.registerWeb(t, "web-b", WebTypeApache)
	ctx := context.Background()

	blocked := []struct {
		vm, pkg string
		remove  bool
		want    string
	}{
		{"web-a", "nginx", true, "No se puede eliminar nginx: el servidor web lo usa"},
		{"web-a", "apache2", false, "No se puede instalar apache2: el servidor web usa nginx"},
		{"web-b", "apache2", true, "No se puede eliminar apache2: el servidor web lo usa"},
		{"web-b", "nginx", false, "No se puede instalar nginx: el servidor web usa apache2"},
	}
	for _, tt := range blocked {
		var err error
		if tt.remove {
			_, err = env.prepare.RemovePackage(ctx, tt.vm, tt.pkg)
		} else {
			_, err = env.prepare.InstallPackage(ctx, tt.vm, tt.pkg)
		}
		var blockedErr *PackageBlockedError
		if !errors.As(err, &blockedErr) || err.Error() != tt.want || !errors.Is(err, ErrPackageBlocked) {
			t.Errorf("%s %s: err = %v, se esperaba %q", tt.vm, tt.pkg, err, tt.want)
		}
	}
	if n := runner.callCount(); n != 0 {
		t.Fatalf("el runner se llamó %d veces, se esperaban 0", n)
	}

	if _, err := env.prepare.InstallPackage(ctx, "web-a", "ufw"); err != nil {
		t.Fatalf("InstallPackage(ufw): %v", err)
	}
	if got, want := runner.callsTo("prepare/install-package.sh")[0].Args, []string{"--role", "web-nginx", "web-a", "ufw"}; !reflect.DeepEqual(got, want) {
		t.Errorf("install argv = %q, se esperaba %q", got, want)
	}
	if _, err := env.prepare.RemovePackage(ctx, "web-b", "curl"); err != nil {
		t.Fatalf("RemovePackage(curl): %v", err)
	}
	if got, want := runner.callsTo("prepare/remove-package.sh")[0].Args, []string{"--role", "web-apache", "web-b", "curl"}; !reflect.DeepEqual(got, want) {
		t.Errorf("remove argv = %q, se esperaba %q", got, want)
	}
}

// AC-9: un servidor web asignado a un balanceador lleva --jump antes de --role.
func TestAC9_PackageOnAssignedWebServerOrder(t *testing.T) {
	runner := &fakeRunner{}
	env := newWebEnv(t, runner)
	env.registerWeb(t, "web-a", WebTypeNginx)
	ctx := context.Background()
	if _, err := env.vms.Insert(ctx, "lb1"); err != nil {
		t.Fatal(err)
	}
	if err := env.lbs.InsertLB(ctx, repository.LoadBalancer{Name: "lb1", Subnet: 1, FrontendPort: 80}); err != nil {
		t.Fatal(err)
	}
	if err := env.lbs.InsertServer(ctx, repository.LBServer{LB: "lb1", Name: "server1", VM: "web-a", Port: 80,
		LastIP: "10.10.1.11", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	if _, err := env.prepare.InstallPackage(ctx, "web-a", "curl"); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}
	want := []string{"--jump", "lb1", "--role", "web-nginx", "web-a", "curl"}
	if got := runner.callsTo("prepare/install-package.sh")[0].Args; !reflect.DeepEqual(got, want) {
		t.Errorf("argv = %q, se esperaba %q", got, want)
	}
}

// AC-28: en el servidor DNS, quitar bind9 se rechaza sin llamar al script, y los demás paquetes van
// con "--role dns" para que ufw permita también 53/tcp y 53/udp.
func TestAC28_DNSServerRulesPackages(t *testing.T) {
	runner := &fakeRunner{}
	env := newDNSEnv(t, runner)
	env.registerDNS(t, "dns1")
	ctx := context.Background()

	_, err := env.prepare.RemovePackage(ctx, "dns1", "bind9")

	var blockedErr *PackageBlockedError
	if !errors.As(err, &blockedErr) || err.Error() != "No se puede eliminar bind9: el servidor DNS lo usa" {
		t.Errorf("RemovePackage(bind9): err = %v", err)
	}
	if n := runner.callCount(); n != 0 {
		t.Fatalf("el runner se llamó %d veces, se esperaban 0", n)
	}

	if _, err := env.prepare.InstallPackage(ctx, "dns1", "ufw"); err != nil {
		t.Fatalf("InstallPackage(ufw): %v", err)
	}
	if got, want := runner.callsTo("prepare/install-package.sh")[0].Args, []string{"--role", "dns", "dns1", "ufw"}; !reflect.DeepEqual(got, want) {
		t.Errorf("install argv = %q, se esperaba %q", got, want)
	}
	if _, err := env.prepare.RemovePackage(ctx, "dns1", "curl"); err != nil {
		t.Fatalf("RemovePackage(curl): %v", err)
	}
	if got, want := runner.callsTo("prepare/remove-package.sh")[0].Args, []string{"--role", "dns", "dns1", "curl"}; !reflect.DeepEqual(got, want) {
		t.Errorf("remove argv = %q, se esperaba %q", got, want)
	}
}

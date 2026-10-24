package service

import (
	"context"
	"errors"
	"reflect"
	"testing"
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

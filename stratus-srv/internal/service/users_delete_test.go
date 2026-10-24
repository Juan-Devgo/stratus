package service

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

const deleteScript = "users/delete-user.sh"

func TestAC3_DeleteUserMapsNoIP(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{
		deleteScript: {ExitCode: 7, Stderr: "La VM no tiene dirección IP; verifique que esté encendida\n"},
	}}
	env := newUsersEnv(t, runner)
	env.register(t, "vm1")

	_, err := env.svc.DeleteUser(context.Background(), "vm1", "luis", false)

	if !errors.Is(err, ErrVMNoIP) {
		t.Fatalf("err = %v, se esperaba ErrVMNoIP", err)
	}
	ops := env.operations(t)
	if len(ops) != 1 || ops[0].Operation != "delete_user" || ops[0].ExitCode != 7 {
		t.Errorf("historial = %+v, se esperaba una fila delete_user con código 7", ops)
	}
}

func TestAC17_DeleteUserNonRegularMapsError(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{
		deleteScript: {ExitCode: 6, Stderr: "Solo se pueden eliminar usuarios regulares existentes\n"},
	}}
	env := newUsersEnv(t, runner)
	env.register(t, "vm1")

	_, err := env.svc.DeleteUser(context.Background(), "vm1", "root", false)

	if !errors.Is(err, ErrNotRegularUser) {
		t.Errorf("err = %v, se esperaba ErrNotRegularUser", err)
	}
}

func TestAC17_DeleteUserRejectsInvalidInput(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{}}
	env := newUsersEnv(t, runner)
	env.register(t, "vm1")

	for _, username := range []string{"Luis", "luis;rm", "", "1luis"} {
		if _, err := env.svc.DeleteUser(context.Background(), "vm1", username, false); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("DeleteUser(%q) = %v, se esperaba ErrInvalidInput", username, err)
		}
	}
	if _, err := env.svc.DeleteUser(context.Background(), "VM_1", "luis", false); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("VM inválida = %v, se esperaba ErrInvalidInput", err)
	}
	if n := runner.callCount(); n != 0 {
		t.Errorf("el runner se llamó %d veces, se esperaban 0", n)
	}
}

func TestAC18_DeleteUserPassesArgs(t *testing.T) {
	tests := []struct {
		name        string
		removeFiles bool
		stdout      string
		wantArgs    []string
	}{
		{"sin archivos", false, "Usuario luis eliminado de vm1\n", []string{"vm1", "luis", "no"}},
		{"con archivos", true, "Usuario luis eliminado de vm1 junto con todos sus archivos\n", []string{"vm1", "luis", "yes"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeRunner{results: map[string]RunResult{deleteScript: {Stdout: []byte(tt.stdout)}}}
			env := newUsersEnv(t, runner)
			env.register(t, "vm1")

			msg, err := env.svc.DeleteUser(context.Background(), "vm1", "luis", tt.removeFiles)
			if err != nil {
				t.Fatalf("DeleteUser: %v", err)
			}

			if want := tt.stdout[:len(tt.stdout)-1]; msg != want {
				t.Errorf("mensaje = %q, se esperaba %q", msg, want)
			}
			calls := runner.callsTo(deleteScript)
			if len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, tt.wantArgs) {
				t.Errorf("llamadas = %+v, se esperaba una con argv %v", calls, tt.wantArgs)
			}
			// Un borrado correcto se registra aunque el listado no lo haga (REQ-19).
			if ops := env.operations(t); len(ops) != 1 || ops[0].Result != "ok" {
				t.Errorf("historial = %+v, se esperaba una fila ok", ops)
			}
		})
	}
}

func TestAC18_DeleteUserFailureReturnsOperationError(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{
		deleteScript: {ExitCode: 5, Stderr: "No se pudieron terminar los procesos de luis\n"},
	}}
	env := newUsersEnv(t, runner)
	env.register(t, "vm1")

	_, err := env.svc.DeleteUser(context.Background(), "vm1", "luis", false)

	var opErr *OperationError
	if !errors.As(err, &opErr) || opErr.ExitCode != 5 || opErr.Detail != "No se pudieron terminar los procesos de luis" {
		t.Errorf("err = %v, se esperaba *OperationError con código 5 y el detalle de stderr", err)
	}
}

func TestAC32_DeleteUserDiscardsPendingKey(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{
		createScript: {Stdout: []byte(fakePrivateKey)},
		deleteScript: {Stdout: []byte("Usuario ana eliminado de vm1\n")},
	}}
	env := newUsersEnv(t, runner)
	env.register(t, "vm1")
	for _, user := range []string{"ana", "luis"} {
		if _, _, err := env.svc.CreateUser(context.Background(), "vm1", CreateUserInput{Username: user, SSHKey: true}); err != nil {
			t.Fatalf("CreateUser(%s): %v", user, err)
		}
	}

	// Un borrado fallido conserva la llave.
	runner.mu.Lock()
	runner.results[deleteScript] = RunResult{ExitCode: 5, Stderr: "fallo\n"}
	runner.mu.Unlock()
	if _, err := env.svc.DeleteUser(context.Background(), "vm1", "ana", false); err == nil {
		t.Fatal("DeleteUser debía fallar")
	}
	if key, err := env.svc.keys.Take(userKeyID("vm1", "ana")); err != nil || string(key) != fakePrivateKey {
		t.Fatalf("la llave de ana debía seguir pendiente tras un fallo: %v", err)
	}
	env.svc.keys.Put(userKeyID("vm1", "ana"), []byte(fakePrivateKey))

	// Un borrado correcto la descarta; la de otro usuario no se toca.
	runner.mu.Lock()
	runner.results[deleteScript] = RunResult{Stdout: []byte("Usuario ana eliminado de vm1\n")}
	runner.mu.Unlock()
	if _, err := env.svc.DeleteUser(context.Background(), "vm1", "ana", false); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if _, err := env.svc.DownloadUserKey("vm1", "ana"); !errors.Is(err, ErrUserKeyUnavailable) {
		t.Errorf("DownloadUserKey(ana) = %v, se esperaba ErrUserKeyUnavailable", err)
	}
	if _, err := env.svc.DownloadUserKey("vm1", "luis"); err != nil {
		t.Errorf("DownloadUserKey(luis) = %v, la llave de otro usuario debía seguir pendiente", err)
	}
}

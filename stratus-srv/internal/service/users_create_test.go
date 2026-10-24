package service

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

const createScript = "users/create-user.sh"

// fakePrivateKey es un bloque de llave privada como el que imprime create-user.sh.
const fakePrivateKey = "-----BEGIN OPENSSH PRIVATE KEY-----\nFAKEKEYDATA\n-----END OPENSSH PRIVATE KEY-----\n"

const createdWithKeyMsg = "Usuario ana creado en vm1. Descargue su llave privada ahora: se puede descargar una sola vez y se descarta en 10 minutos"

// funcRunner permite que la prueba inspeccione el entorno mientras corre el "script".
type funcRunner func(ctx context.Context, script string, args ...string) (RunResult, error)

func (f funcRunner) Run(ctx context.Context, script string, args ...string) (RunResult, error) {
	return f(ctx, script, args...)
}

// assertCreateRejected comprueba que CreateUser rechaza la entrada sin llamar al script.
func assertCreateRejected(t *testing.T, in CreateUserInput) {
	t.Helper()
	runner := &fakeRunner{results: map[string]RunResult{}}
	env := newUsersEnv(t, runner)
	env.register(t, "vm1")

	_, _, err := env.svc.CreateUser(context.Background(), "vm1", in)

	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("err = %v, se esperaba ErrInvalidInput", err)
	}
	if n := runner.callCount(); n != 0 {
		t.Errorf("el runner se llamó %d veces, se esperaban 0", n)
	}
}

func TestAC4_CreateUserPassesDiscreteArgs(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{createScript: {Stdout: []byte(fakePrivateKey)}}}
	env := newUsersEnv(t, runner)
	env.register(t, "vm1")

	msg, keyAvailable, err := env.svc.CreateUser(context.Background(), "vm1",
		CreateUserInput{Username: "ana", Sudo: true, SSHKey: true})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	if msg != createdWithKeyMsg {
		t.Errorf("mensaje = %q, se esperaba %q", msg, createdWithKeyMsg)
	}
	if !keyAvailable {
		t.Error("keyAvailable = false, se esperaba true")
	}
	calls := runner.callsTo(createScript)
	if len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, []string{"vm1", "ana", "yes", "yes"}) {
		t.Errorf("llamadas = %+v, se esperaba una con argv [vm1 ana yes yes]", calls)
	}
	key, err := env.svc.DownloadUserKey("vm1", "ana")
	if err != nil || string(key) != fakePrivateKey {
		t.Errorf("DownloadUserKey = %q, %v; se esperaba la llave del stdout", key, err)
	}
}

func TestAC4_CreateUserWithPasswordMessage(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{createScript: {Stdout: []byte("Usuario luis creado en vm1\n")}}}
	env := newUsersEnv(t, runner)
	env.register(t, "vm1")

	msg, keyAvailable, err := env.svc.CreateUser(context.Background(), "vm1",
		CreateUserInput{Username: "luis", Password: "Secreta123"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	const want = "Usuario luis creado en vm1; inicia sesión con su contraseña en la consola o con su - luis"
	if msg != want || keyAvailable {
		t.Errorf("mensaje = %q, keyAvailable = %v; se esperaba %q y false", msg, keyAvailable, want)
	}
	args := runner.callsTo(createScript)[0].Args
	if len(args) != 5 || !reflect.DeepEqual(args[:4], []string{"vm1", "luis", "no", "no"}) {
		t.Errorf("argv = %v, se esperaba [vm1 luis no no <archivo>]", args)
	}
	if _, err := env.svc.DownloadUserKey("vm1", "luis"); !errors.Is(err, ErrUserKeyUnavailable) {
		t.Errorf("DownloadUserKey sin llaves = %v, se esperaba ErrUserKeyUnavailable", err)
	}
}

func TestAC3_CreateUserMapsNoIP(t *testing.T) {
	var secretPath string
	runner := funcRunner(func(_ context.Context, _ string, args ...string) (RunResult, error) {
		secretPath = args[len(args)-1]
		return RunResult{ExitCode: 7, Stderr: "La VM no tiene dirección IP; verifique que esté encendida\n"}, nil
	})
	env := newUsersEnv(t, runner)
	env.register(t, "vm1")

	_, _, err := env.svc.CreateUser(context.Background(), "vm1", CreateUserInput{Username: "ana", Password: "Secreta123"})

	if !errors.Is(err, ErrVMNoIP) {
		t.Fatalf("err = %v, se esperaba ErrVMNoIP", err)
	}
	ops := env.operations(t)
	if len(ops) != 1 || ops[0].Operation != "create_user" || ops[0].ExitCode != 7 {
		t.Errorf("historial = %+v, se esperaba una fila create_user con código 7", ops)
	}
	if _, statErr := os.Stat(secretPath); !os.IsNotExist(statErr) {
		t.Errorf("el archivo secreto %q sigue existiendo (%v)", secretPath, statErr)
	}
}

func TestAC6_CreateUserExistingMapsErrUserExists(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{createScript: {ExitCode: 6, Stderr: "El usuario ya existe en la VM\n"}}}
	env := newUsersEnv(t, runner)
	env.register(t, "vm1")

	_, _, err := env.svc.CreateUser(context.Background(), "vm1", CreateUserInput{Username: "ana", SSHKey: true})

	if !errors.Is(err, ErrUserExists) {
		t.Errorf("err = %v, se esperaba ErrUserExists", err)
	}
}

func TestAC29_CreateUserGroupExistsMapsError(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{createScript: {ExitCode: 8, Stderr: "Ya existe un grupo con ese nombre en la VM\n"}}}
	env := newUsersEnv(t, runner)
	env.register(t, "vm1")

	_, _, err := env.svc.CreateUser(context.Background(), "vm1", CreateUserInput{Username: "ana", SSHKey: true})

	if !errors.Is(err, ErrGroupExists) {
		t.Errorf("err = %v, se esperaba ErrGroupExists", err)
	}
}

func TestAC15_CreateUserFailureReturnsOperationError(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{createScript: {
		ExitCode: 5, Stdout: []byte(fakePrivateKey), Stderr: "Falló el paso sh en la VM (código 1)\n"}}}
	env := newUsersEnv(t, runner)
	env.register(t, "vm1")

	_, keyAvailable, err := env.svc.CreateUser(context.Background(), "vm1", CreateUserInput{Username: "ana", SSHKey: true})

	var opErr *OperationError
	if !errors.As(err, &opErr) || opErr.ExitCode != 5 {
		t.Fatalf("err = %v, se esperaba *OperationError con código 5", err)
	}
	if keyAvailable {
		t.Error("keyAvailable = true tras un fallo")
	}
	if _, err := env.svc.DownloadUserKey("vm1", "ana"); !errors.Is(err, ErrUserKeyUnavailable) {
		t.Errorf("DownloadUserKey tras un fallo = %v, se esperaba ErrUserKeyUnavailable", err)
	}
	ops := env.operations(t)
	if len(ops) != 1 || ops[0].Result != "error" || ops[0].ExitCode != 5 {
		t.Errorf("historial = %+v, se esperaba una fila con result error y código 5", ops)
	}
}

func TestAC33_CreateUserCleanupFailureMessage(t *testing.T) {
	const stderr = "Falló el paso sh en la VM (código 1)\n" +
		"Limpieza fallida; puede quedar en la VM: cuenta ana, /home/ana, /etc/sudoers.d/stratus-ana\n"
	runner := &fakeRunner{results: map[string]RunResult{createScript: {ExitCode: 9, Stderr: stderr}}}
	env := newUsersEnv(t, runner)
	env.register(t, "vm1")

	_, _, err := env.svc.CreateUser(context.Background(), "vm1", CreateUserInput{Username: "ana", SSHKey: true})

	var cleanupErr *CleanupError
	if !errors.As(err, &cleanupErr) {
		t.Fatalf("err = %v, se esperaba *CleanupError", err)
	}
	const want = "No se pudo crear el usuario ana y la limpieza falló; revise la VM. " +
		"Limpieza fallida; puede quedar en la VM: cuenta ana, /home/ana, /etc/sudoers.d/stratus-ana"
	if err.Error() != want {
		t.Errorf("mensaje = %q, se esperaba %q", err.Error(), want)
	}
	// El historial conserva los dos errores.
	ops := env.operations(t)
	if len(ops) != 1 || !strings.Contains(ops[0].Stderr, "Falló el paso sh") || !strings.Contains(ops[0].Stderr, "Limpieza fallida") {
		t.Errorf("historial = %+v, se esperaba el stderr con ambos errores", ops)
	}
}

func TestAC12_CreateUserPasswordOnlyInSecretFile(t *testing.T) {
	const password = "Secreta123"
	for _, exitCode := range []int{0, 5} {
		t.Run("código "+string(rune('0'+exitCode)), func(t *testing.T) {
			var (
				secretPath string
				mode       os.FileMode
				content    []byte
				argvHasPw  bool
			)
			runner := funcRunner(func(_ context.Context, _ string, args ...string) (RunResult, error) {
				secretPath = args[len(args)-1]
				info, err := os.Stat(secretPath)
				if err != nil {
					t.Errorf("el archivo secreto no existe durante Run: %v", err)
					return RunResult{ExitCode: exitCode}, nil
				}
				mode = info.Mode().Perm()
				content, _ = os.ReadFile(secretPath)
				for _, a := range args {
					argvHasPw = argvHasPw || a == password
				}
				return RunResult{ExitCode: exitCode, Stderr: "salida\n"}, nil
			})
			env := newUsersEnv(t, runner)
			env.register(t, "vm1")

			_, _, _ = env.svc.CreateUser(context.Background(), "vm1", CreateUserInput{Username: "ana", Password: password})

			if mode != 0o600 {
				t.Errorf("permisos del archivo = %o, se esperaba 600", mode)
			}
			if string(content) != password {
				t.Errorf("contenido del archivo = %q, se esperaba la contraseña sin salto de línea", content)
			}
			if argvHasPw {
				t.Error("la contraseña aparece como argumento")
			}
			if _, err := os.Stat(secretPath); !os.IsNotExist(err) {
				t.Errorf("el archivo secreto sigue existiendo (%v)", err)
			}
			for _, op := range env.operations(t) {
				if strings.Contains(strings.Join(op.Args, " ")+op.Stderr+op.Command, password) {
					t.Errorf("el historial contiene la contraseña: %+v", op)
				}
			}
			if strings.Contains(env.logs.String(), password) {
				t.Errorf("el log contiene la contraseña: %q", env.logs.String())
			}
		})
	}
}

func TestAC13_UserKeyDownloadOnceAndExpires(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{createScript: {Stdout: []byte(fakePrivateKey)}}}
	env := newUsersEnv(t, runner)
	env.svc.keys = NewKeyStore(50 * time.Millisecond)
	env.register(t, "vm1")
	create := func(user string) {
		t.Helper()
		if _, _, err := env.svc.CreateUser(context.Background(), "vm1", CreateUserInput{Username: user, SSHKey: true}); err != nil {
			t.Fatalf("CreateUser(%s): %v", user, err)
		}
	}

	// Una sola descarga.
	create("ana")
	if key, err := env.svc.DownloadUserKey("vm1", "ana"); err != nil || string(key) != fakePrivateKey {
		t.Fatalf("primera descarga = %q, %v", key, err)
	}
	if _, err := env.svc.DownloadUserKey("vm1", "ana"); !errors.Is(err, ErrUserKeyUnavailable) {
		t.Errorf("segunda descarga = %v, se esperaba ErrUserKeyUnavailable", err)
	}

	// Vence por tiempo.
	create("luis")
	time.Sleep(150 * time.Millisecond)
	if _, err := env.svc.DownloadUserKey("vm1", "luis"); !errors.Is(err, ErrUserKeyUnavailable) {
		t.Errorf("descarga tras el plazo = %v, se esperaba ErrUserKeyUnavailable", err)
	}

	// Un nombre inválido ni siquiera busca la llave.
	if _, err := env.svc.DownloadUserKey("vm1", "Ana"); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("nombre inválido = %v, se esperaba ErrInvalidInput", err)
	}
	if _, err := env.svc.DownloadUserKey("VM_1", "ana"); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("VM inválida = %v, se esperaba ErrInvalidInput", err)
	}
}

func TestAC14_UserKeyNeverInHistoryOrLogs(t *testing.T) {
	// La llave sale por stdout; si además se colara en stderr, el historial la enmascara.
	runner := &fakeRunner{results: map[string]RunResult{createScript: {
		Stdout: []byte(fakePrivateKey), Stderr: "diagnóstico\n" + fakePrivateKey}}}
	env := newUsersEnv(t, runner)
	env.register(t, "vm1")

	if _, _, err := env.svc.CreateUser(context.Background(), "vm1", CreateUserInput{Username: "ana", SSHKey: true}); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	ops := env.operations(t)
	if len(ops) != 1 {
		t.Fatalf("filas del historial = %d, se esperaba 1", len(ops))
	}
	everything := strings.Join(ops[0].Args, " ") + ops[0].Stderr + ops[0].Command + ops[0].Operation + env.logs.String()
	for _, secret := range []string{"FAKEKEYDATA", "PRIVATE KEY"} {
		if strings.Contains(everything, secret) {
			t.Errorf("el historial o el log contiene %q: %q", secret, everything)
		}
	}
}

// Los tres casos de validación del módulo, vistos desde CreateUser: ninguno llama al script.
func TestAC5_AC7_AC8_CreateUserValidationCallsNoScript(t *testing.T) {
	for _, in := range []CreateUserInput{
		{Username: "Ana", SSHKey: true},
		{Username: "ana;rm", Password: "Secreta123"},
		{Username: "ana", Password: "Corta12"},
		{Username: "ana", Password: "Contraseña1"},
		{Username: "ana", SSHKey: true, Password: "Secreta123"},
	} {
		assertCreateRejected(t, in)
	}
}

// AC-27, REQ-21: crear un usuario con llave en un servidor lleva --jump y toma los bloqueos de la
// VM y de su balanceador; sin llave (contraseña) solo el de la VM no basta: también va por jump.
func TestAC27_CreateUserOnServerPassesJump(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{createScript: {Stdout: []byte(fakePrivateKey)}},
		block: make(chan struct{}), started: make(chan struct{}, 1), blockOnly: createScript}
	env := newUsersEnv(t, runner)
	env.registerServer(t)
	ctx := context.Background()

	done := make(chan error, 1)
	go func() {
		_, _, err := env.svc.CreateUser(ctx, "web-a", CreateUserInput{Username: "ana", SSHKey: true})
		done <- err
	}()
	<-runner.started
	locks := env.svc.ops.locks
	if locks.TryLock("web-a") || locks.TryLock("lb1") {
		t.Error("web-a y lb1 debían estar bloqueados mientras se crea el usuario")
	}
	close(runner.block)
	if err := <-done; err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	calls := runner.callsTo(createScript)
	if want := []string{"--jump", "lb1", "web-a", "ana", "no", "yes"}; len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, want) {
		t.Errorf("argv = %+v, se esperaba %q", calls, want)
	}
	if !locks.TryLockAll("web-a", "lb1") {
		t.Error("tras terminar, web-a y lb1 debían quedar libres")
	}
}

// AC-27: eliminar un usuario de un servidor lleva --jump y los dos bloqueos.
func TestAC27_DeleteUserOnServerPassesJump(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{deleteScript: {Stdout: []byte("Usuario ana eliminado\n")}}}
	env := newUsersEnv(t, runner)
	env.registerServer(t)
	env.svc.ops.locks.TryLock("lb1")

	if _, err := env.svc.DeleteUser(context.Background(), "web-a", "ana", false); !errors.Is(err, ErrOperationInProgress) {
		t.Fatalf("con lb1 ocupado: err = %v, se esperaba ErrOperationInProgress", err)
	}
	env.svc.ops.locks.Unlock("lb1")

	if _, err := env.svc.DeleteUser(context.Background(), "web-a", "ana", true); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	calls := runner.callsTo(deleteScript)
	if want := []string{"--jump", "lb1", "web-a", "ana", "yes"}; len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, want) {
		t.Errorf("argv = %+v, se esperaba %q", calls, want)
	}
}

// AC-42: el código 15 de create-user.sh y de delete-user.sh es ErrJumpUpdateFailed y no guarda llave.
func TestAC42_JumpFailureMapsError(t *testing.T) {
	runner := &fakeRunner{results: map[string]RunResult{
		createScript: {ExitCode: 15, Stdout: []byte(fakePrivateKey), Stderr: "No se pudo actualizar la cuenta jump de lb1\n"},
		deleteScript: {ExitCode: 15, Stderr: "No se pudo actualizar la cuenta jump de lb1\n"},
	}}
	env := newUsersEnv(t, runner)
	env.registerServer(t)
	ctx := context.Background()

	if _, _, err := env.svc.CreateUser(ctx, "web-a", CreateUserInput{Username: "ana", SSHKey: true}); !errors.Is(err, ErrJumpUpdateFailed) {
		t.Errorf("CreateUser: err = %v, se esperaba ErrJumpUpdateFailed", err)
	}
	if _, err := env.svc.DownloadUserKey("web-a", "ana"); !errors.Is(err, ErrUserKeyUnavailable) {
		t.Errorf("no debía quedar una llave: err = %v", err)
	}
	if _, err := env.svc.DeleteUser(ctx, "web-a", "ana", false); !errors.Is(err, ErrJumpUpdateFailed) {
		t.Errorf("DeleteUser: err = %v, se esperaba ErrJumpUpdateFailed", err)
	}
}

package service

import (
	"context"
	"errors"
	"log"
	"strconv"
	"testing"
	"time"

	"stratus-srv/internal/repository"
)

// AC-24: una operación de prepare en curso en la VM rechaza el listado, que ni siquiera
// llama al script.
func TestAC24_ListUsersRejectedWhilePrepareRuns(t *testing.T) {
	locks := &VMLocks{}
	runner := &fakeRunner{
		results:   map[string]RunResult{},
		block:     make(chan struct{}),
		started:   make(chan struct{}, 1),
		blockOnly: "prepare/install-package.sh",
	}
	users := newUsersEnvWithLocks(t, runner, locks)
	users.register(t, "vm1")
	prepare := newPrepareForTest(t, runner, users.vms, users.history, locks, users.svc.logger)

	done := make(chan error, 1)
	go func() {
		_, err := prepare.InstallPackage(context.Background(), "vm1", "htop")
		done <- err
	}()
	<-runner.started

	_, err := users.svc.ListUsers(context.Background(), "vm1")
	if !errors.Is(err, ErrOperationInProgress) {
		t.Errorf("err = %v, se esperaba ErrOperationInProgress", err)
	}
	if n := len(runner.callsTo(listScript)); n != 0 {
		t.Errorf("list-users.sh se llamó %d veces, se esperaban 0", n)
	}

	close(runner.block)
	if err := <-done; err != nil {
		t.Errorf("InstallPackage: %v", err)
	}
}

// AC-24: un create_user en curso en la VM rechaza las operaciones de prepare.
func TestAC24_CreateUserBlocksPrepare(t *testing.T) {
	locks := &VMLocks{}
	runner := &fakeRunner{
		results:   map[string]RunResult{},
		block:     make(chan struct{}),
		started:   make(chan struct{}, 1),
		blockOnly: createScript,
	}
	users := newUsersEnvWithLocks(t, runner, locks)
	users.register(t, "vm1")
	prepare := newPrepareForTest(t, runner, users.vms, users.history, locks, users.svc.logger)

	done := make(chan error, 1)
	go func() {
		_, _, err := users.svc.CreateUser(context.Background(), "vm1", CreateUserInput{Username: "ana", SSHKey: true})
		done <- err
	}()
	<-runner.started

	_, err := prepare.InstallPackage(context.Background(), "vm1", "htop")
	if !errors.Is(err, ErrOperationInProgress) {
		t.Errorf("err = %v, se esperaba ErrOperationInProgress", err)
	}
	if n := len(runner.callsTo("prepare/install-package.sh")); n != 0 {
		t.Errorf("install-package.sh se llamó %d veces, se esperaban 0", n)
	}

	close(runner.block)
	if err := <-done; err != nil {
		t.Errorf("CreateUser: %v", err)
	}
}

// AC-24: una operación de prepare en curso en la VM rechaza el borrado de un usuario, que ni
// siquiera llama al script.
func TestAC24_DeleteUserRejectedWhilePrepareRuns(t *testing.T) {
	locks := &VMLocks{}
	runner := &fakeRunner{
		results:   map[string]RunResult{},
		block:     make(chan struct{}),
		started:   make(chan struct{}, 1),
		blockOnly: "prepare/set-hostname.sh",
	}
	users := newUsersEnvWithLocks(t, runner, locks)
	users.register(t, "vm1")
	prepare := newPrepareForTest(t, runner, users.vms, users.history, locks, users.svc.logger)

	done := make(chan error, 1)
	go func() {
		_, err := prepare.SetHostname(context.Background(), "vm1", "web-01")
		done <- err
	}()
	<-runner.started

	_, err := users.svc.DeleteUser(context.Background(), "vm1", "luis", false)
	if !errors.Is(err, ErrOperationInProgress) {
		t.Errorf("err = %v, se esperaba ErrOperationInProgress", err)
	}
	if n := len(runner.callsTo(deleteScript)); n != 0 {
		t.Errorf("delete-user.sh se llamó %d veces, se esperaban 0", n)
	}

	close(runner.block)
	if err := <-done; err != nil {
		t.Errorf("SetHostname: %v", err)
	}
}

// AC-26: History devuelve solo las 50 filas más recientes del módulo users, la más reciente primero.
func TestAC26_UsersHistoryReturns50NewestOfModule(t *testing.T) {
	env := newUsersEnv(t, &fakeRunner{results: map[string]RunResult{}})
	ctx := context.Background()
	// 55 filas de users (la fila i lleva i en su argv) mezcladas con filas de prepare.
	for i := 1; i <= 55; i++ {
		row := repository.Operation{Module: "users", Operation: "create_user", VM: "vm1",
			Command: "users/create-user.sh", Args: []string{strconv.Itoa(i)}, Result: "ok"}
		if err := env.history.Insert(ctx, row); err != nil {
			t.Fatalf("Insert users %d: %v", i, err)
		}
		other := repository.Operation{Module: "prepare", Operation: "set_hostname", VM: "vm1",
			Command: "prepare/set-hostname.sh", Result: "ok"}
		if err := env.history.Insert(ctx, other); err != nil {
			t.Fatalf("Insert prepare %d: %v", i, err)
		}
	}

	ops, err := env.svc.History(ctx)
	if err != nil {
		t.Fatalf("History: %v", err)
	}

	if len(ops) != 50 {
		t.Fatalf("filas = %d, se esperaban 50", len(ops))
	}
	for i, op := range ops {
		if op.Module != "users" {
			t.Fatalf("fila %d es del módulo %q, se esperaba users", i, op.Module)
		}
		if want := strconv.Itoa(55 - i); op.Args[0] != want {
			t.Fatalf("fila %d lleva %s, se esperaba %s (más reciente primero)", i, op.Args[0], want)
		}
	}
}

// AC-42, REQ-21: con un Refresh en curso sobre lb1, generar la llave y crear o eliminar usuarios en
// su servidor web-a se rechaza sin llamar a ningún script: esas operaciones también toman lb1.
func TestAC42_KeyAndUserOpsLockServerAndLB(t *testing.T) {
	runner := &fakeRunner{block: make(chan struct{}), started: make(chan struct{}, 1), blockOnly: "haproxy/refresh-lb.sh"}
	env := newHAProxyEnv(t, runner)
	env.registerLB(t, "lb1", 1, 80)
	env.registerServer(t, "lb1", "server1", "web-a", 80, "10.10.1.11")
	users := NewUsersService(UsersConfig{KeyTTL: 10 * time.Minute}, runner, env.vms, env.lbs, env.history, env.locks, log.New(env.logs, "", 0))
	ctx := context.Background()

	done := make(chan error, 1)
	go func() {
		_, err := env.svc.Refresh(ctx, "lb1")
		done <- err
	}()
	<-runner.started

	attempts := map[string]func() error{
		"GenerateUserKey": func() error { _, err := env.prepare.GenerateUserKey(ctx, "web-a"); return err },
		"CreateUser": func() error {
			_, _, err := users.CreateUser(ctx, "web-a", CreateUserInput{Username: "ana", SSHKey: true})
			return err
		},
		"DeleteUser": func() error { _, err := users.DeleteUser(ctx, "web-a", "ana", false); return err },
	}
	for name, attempt := range attempts {
		if err := attempt(); !errors.Is(err, ErrOperationInProgress) {
			t.Errorf("%s: err = %v, se esperaba ErrOperationInProgress", name, err)
		}
	}
	if n := runner.callCount(); n != 1 {
		t.Errorf("el runner se llamó %d veces, se esperaba 1 (solo el refresh)", n)
	}
	close(runner.block)
	if err := <-done; err != nil {
		t.Fatalf("Refresh: %v", err)
	}
}

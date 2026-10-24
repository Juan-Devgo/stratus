package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"stratus-srv/internal/repository"
)

// usersModule es el valor de la columna module en el historial del módulo de usuarios.
const usersModule = "users"

// UsersConfig son los valores del entorno que necesita el módulo de usuarios.
type UsersConfig struct {
	KeyTTL time.Duration // vida de la llave privada de un usuario en memoria (10 min)
}

// UsersService implementa las operaciones del módulo "Gestión de usuarios". Todo lo que ocurre
// en la VM lo hacen los scripts de scripts/users (P1); aquí solo se valida, se invoca y se mapea.
type UsersService struct {
	cfg     UsersConfig
	vms     *repository.VMRepository
	lbs     *repository.LBRepository // para saber si la VM es un servidor de un balanceador (004)
	history *repository.HistoryRepository
	logger  *log.Logger
	ops     *operationRunner
	keys    *KeyStore // llaves privadas de usuario pendientes de descarga, clave "<vm>/<usuario>"
}

// NewUsersService recibe el *VMLocks compartido con PrepareService: una operación en curso de
// cualquiera de los dos módulos rechaza a la otra sobre la misma VM (AC-24).
func NewUsersService(cfg UsersConfig, runner ScriptRunner, vms *repository.VMRepository, lbs *repository.LBRepository,
	history *repository.HistoryRepository, locks *VMLocks, logger *log.Logger) *UsersService {
	return &UsersService{cfg: cfg, vms: vms, lbs: lbs, history: history, logger: logger,
		ops:  newOperationRunner(usersModule, runner, history, locks, logger),
		keys: NewKeyStore(cfg.KeyTTL)}
}

// getVM valida el nombre y devuelve la VM registrada, o ErrVMNotFound.
func (s *UsersService) getVM(ctx context.Context, name string) (repository.VM, error) {
	if err := ValidateVMName(name); err != nil {
		return repository.VM{}, err
	}
	vm, err := s.vms.Get(ctx, name)
	if errors.Is(err, repository.ErrVMNotFound) {
		return repository.VM{}, fmt.Errorf("buscar vm %s: %w", name, ErrVMNotFound)
	}
	return vm, err
}

// History devuelve las últimas 50 operaciones del módulo, la más reciente primero (REQ-20).
func (s *UsersService) History(ctx context.Context) ([]repository.Operation, error) {
	return s.history.ListRecent(ctx, usersModule, historyLimit)
}

// userTarget arma el argv de un script de usuarios y la lista de VMs que bloquea. En un servidor
// asignado a un balanceador el argv empieza con "--jump <lb>" (REQ-19); crear y eliminar usuarios
// también copian o quitan llaves en la cuenta jump del balanceador, así que bloquean los dos
// (REQ-21). Con lockLB=false (listar) solo se bloquea la VM.
func (s *UsersService) userTarget(ctx context.Context, vm string, lockLB bool, extra ...string) (args, lockVMs []string, err error) {
	args, lb, err := targetArgs(ctx, s.lbs, vm, extra...)
	if err != nil {
		return nil, nil, err
	}
	lockVMs = []string{vm}
	if lb != "" && lockLB {
		lockVMs = append(lockVMs, lb)
	}
	return args, lockVMs, nil
}

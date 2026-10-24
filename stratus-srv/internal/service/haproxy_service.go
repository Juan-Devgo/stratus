package service

import (
	"context"
	"errors"
	"log"

	"stratus-srv/internal/repository"
)

// haproxyModule es el valor de la columna module en el historial del módulo 004.
const haproxyModule = "haproxy"

// Códigos de salida de los scripts de scripts/haproxy que el backend traduce a un error propio
// (tabla de códigos del plan, sección 6). El 12 (balanceador apagado) se traduce en
// operationRunner para todos los módulos.
const (
	exitNoFreeSubnet  = 8
	exitConfigInvalid = 10
	exitReloadFailed  = 11
	exitIntnetExists  = 13
	exitJumpFailed    = 15
)

// HAProxyConfig son los valores del entorno que necesita el módulo de balanceadores.
type HAProxyConfig struct {
	BaseDisk      string // disco base de HAProxy (APP_HAPROXY_BASE_DISK); argumento de create-lb.sh
	BridgeAdapter string // interfaz del host para la red en puente; argumento de create-lb.sh y assign-server.sh
}

// HAProxyService implementa las operaciones del módulo "Backends HAProxy": la topología vive en
// SQLite y cada cambio en el sistema lo hace un script de scripts/haproxy.
type HAProxyService struct {
	cfg     HAProxyConfig
	runner  ScriptRunner
	vms     *repository.VMRepository
	lbs     *repository.LBRepository
	history *repository.HistoryRepository
	locks   *VMLocks
	logger  *log.Logger
	ops     *operationRunner // bloqueos, historial y log compartidos con los demás módulos
}

func NewHAProxyService(cfg HAProxyConfig, runner ScriptRunner, vms *repository.VMRepository,
	lbs *repository.LBRepository, history *repository.HistoryRepository, locks *VMLocks,
	logger *log.Logger) *HAProxyService {
	return &HAProxyService{cfg: cfg, runner: runner, vms: vms, lbs: lbs, history: history, locks: locks,
		logger: logger, ops: newOperationRunner(haproxyModule, runner, history, locks, logger)}
}

// History devuelve las últimas 50 operaciones del módulo, la más reciente primero (REQ-24).
func (s *HAProxyService) History(ctx context.Context) ([]repository.Operation, error) {
	return s.history.ListRecent(ctx, haproxyModule, historyLimit)
}

// mapHAProxyError traduce los códigos propios de los scripts de HAProxy a su error centinela.
// Solo toca un *OperationError con los códigos 8, 10, 11, 13 y 15; cualquier otro error queda igual.
func mapHAProxyError(err error) error {
	var opErr *OperationError
	if !errors.As(err, &opErr) {
		return err
	}
	switch opErr.ExitCode {
	case exitNoFreeSubnet:
		return ErrNoFreeSubnet
	case exitConfigInvalid:
		return ErrHAProxyConfigInvalid
	case exitReloadFailed:
		return ErrHAProxyReloadFailed
	case exitIntnetExists:
		return ErrIntnetExists
	}
	return mapJumpError(err)
}

// mapJumpError traduce el código 15 de los scripts que actualizan la cuenta jump (generate-user-key,
// create-user, delete-user, assign-server) a ErrJumpUpdateFailed; cualquier otro error queda igual.
func mapJumpError(err error) error {
	var opErr *OperationError
	if errors.As(err, &opErr) && opErr.ExitCode == exitJumpFailed {
		return ErrJumpUpdateFailed
	}
	return err
}

// getLB valida el nombre y devuelve el balanceador registrado, o ErrLBNotFound.
func (s *HAProxyService) getLB(ctx context.Context, name string) (repository.LoadBalancer, error) {
	if err := ValidateVMName(name); err != nil {
		return repository.LoadBalancer{}, err
	}
	lb, err := s.lbs.GetLB(ctx, name)
	if errors.Is(err, repository.ErrLBNotFound) {
		return repository.LoadBalancer{}, ErrLBNotFound
	}
	return lb, err
}

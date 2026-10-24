// Package service valida las entradas, invoca los scripts y traduce su resultado (P1).
// Ninguna operación de sistema se hace aquí: todo pasa por un script de scripts/.
package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"time"

	"stratus-srv/internal/repository"
)

// prepareModule es el valor de la columna module en el historial.
const prepareModule = "prepare"

// historyLimit es el número de operaciones que muestra la pantalla del módulo (REQ-18).
const historyLimit = 50

// Config son los valores del entorno que necesita el módulo prepare.
type Config struct {
	BaseDisk      string        // disco base multiattach; se pasa como argumento a create-vm.sh
	BridgeAdapter string        // interfaz del host para la red en puente; argumento de create-vm.sh
	SSHKeyPath    string        // llave privada del backend; los scripts la leen del entorno
	KeyTTL        time.Duration // vida de la llave de usuario en memoria (10 min)
}

// PrepareService implementa las operaciones del módulo "Preparar sistema".
type PrepareService struct {
	cfg     Config
	runner  ScriptRunner
	vms     *repository.VMRepository
	lbs     *repository.LBRepository // topología de balanceadores y servidores (004)
	history *repository.HistoryRepository
	logger  *log.Logger
	ops     *operationRunner // bloqueo por VM, historial y log compartidos con otros módulos
	// locks incluye el bloqueo de creación: una sola creación de VM o de balanceador a la vez,
	// porque todas tocan el disco base compartido.
	locks   *VMLocks
	haproxy *HAProxyService // eliminar un balanceador y refrescar sus IPs pasa por el módulo 004
	keys    *KeyStore       // llaves privadas de usuario pendientes de descarga, solo en memoria
}

func NewPrepareService(cfg Config, runner ScriptRunner, vms *repository.VMRepository, lbs *repository.LBRepository,
	history *repository.HistoryRepository, locks *VMLocks, haproxy *HAProxyService, logger *log.Logger) *PrepareService {
	return &PrepareService{cfg: cfg, runner: runner, vms: vms, lbs: lbs, history: history, logger: logger,
		ops: newOperationRunner(prepareModule, runner, history, locks, logger), locks: locks, haproxy: haproxy,
		keys: NewKeyStore(cfg.KeyTTL)}
}

// History devuelve las últimas 50 operaciones del módulo, la más reciente primero (REQ-18).
func (s *PrepareService) History(ctx context.Context) ([]repository.Operation, error) {
	return s.history.ListRecent(ctx, prepareModule, historyLimit)
}

// getVM valida el nombre y devuelve la VM registrada, o ErrVMNotFound.
func (s *PrepareService) getVM(ctx context.Context, name string) (repository.VM, error) {
	if err := ValidateVMName(name); err != nil {
		return repository.VM{}, err
	}
	vm, err := s.vms.Get(ctx, name)
	if errors.Is(err, repository.ErrVMNotFound) {
		return repository.VM{}, fmt.Errorf("buscar vm %s: %w", name, ErrVMNotFound)
	}
	return vm, err
}

// lbFrontendArgs devuelve ["--frontend", <puerto>] si la VM es un balanceador, o nil. Los scripts de
// paquetes lo usan para permitir el puerto del frontend en ufw y para negarse a quitar haproxy (REQ-5).
func (s *PrepareService) lbFrontendArgs(ctx context.Context, vm string) ([]string, error) {
	lb, err := s.lbs.GetLB(ctx, vm)
	if errors.Is(err, repository.ErrLBNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return []string{"--frontend", strconv.Itoa(lb.FrontendPort)}, nil
}

// runOperation ejecuta un script que modifica una VM con bloqueo, historial y log; siempre
// registra la operación en el historial, también cuando termina bien (ver operationRunner.run).
func (s *PrepareService) runOperation(ctx context.Context, operation, vm, script string,
	args []string, onSuccess func(ctx context.Context) error) (RunResult, error) {
	return s.ops.run(ctx, operation, vm, script, args, true, onSuccess)
}

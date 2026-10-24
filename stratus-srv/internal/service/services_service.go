package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"stratus-srv/internal/repository"
)

// servicesModule es el valor de la columna module en el historial del módulo de servicios.
const servicesModule = "services"

// Códigos de salida propios de los scripts de scripts/services (la misma tabla en todos).
// Los demás (2, 3, 4, 5, 7) los maneja el operationRunner o llegan como *OperationError.
const (
	exitNotManaged        = 6
	exitServiceExists     = 8
	exitRollbackFailed    = 9
	exitInvalidArchive    = 10
	exitExecutableMissing = 11
)

// ServicesConfig son los valores del entorno que necesita el módulo de servicios.
type ServicesConfig struct {
	MaxArchiveBytes int64 // tamaño máximo del .tar.gz (200 MiB, REQ-4)
}

// ServicesService implementa las operaciones del módulo "Servicios systemd". Todo lo que ocurre
// en la VM lo hacen los scripts de scripts/services (P1); aquí solo se valida, se invoca y se mapea.
type ServicesService struct {
	cfg     ServicesConfig
	vms     *repository.VMRepository
	lbs     *repository.LBRepository // para saber si la VM es un servidor de un balanceador (004)
	history *repository.HistoryRepository
	logger  *log.Logger
	ops     *operationRunner
}

// NewServicesService recibe el *VMLocks compartido con los demás módulos: una operación en curso
// de cualquiera de ellos rechaza a las otras sobre la misma VM (AC-29).
func NewServicesService(cfg ServicesConfig, runner ScriptRunner, vms *repository.VMRepository, lbs *repository.LBRepository,
	history *repository.HistoryRepository, locks *VMLocks, logger *log.Logger) *ServicesService {
	return &ServicesService{cfg: cfg, vms: vms, lbs: lbs, history: history, logger: logger,
		ops: newOperationRunner(servicesModule, runner, history, locks, logger)}
}

// MaxArchiveBytes es el tamaño máximo del archivo; el handler lo usa para limitar el cuerpo HTTP.
func (s *ServicesService) MaxArchiveBytes() int64 { return s.cfg.MaxArchiveBytes }

// getVM valida el nombre y devuelve la VM registrada, o ErrVMNotFound.
func (s *ServicesService) getVM(ctx context.Context, name string) (repository.VM, error) {
	if err := ValidateVMName(name); err != nil {
		return repository.VM{}, err
	}
	vm, err := s.vms.Get(ctx, name)
	if errors.Is(err, repository.ErrVMNotFound) {
		return repository.VM{}, fmt.Errorf("buscar vm %s: %w", name, ErrVMNotFound)
	}
	return vm, err
}

// ServiceResult es lo que devuelven las operaciones que cambian un servicio. ActiveState solo
// viene si el script leyó el estado después de iniciar o reiniciar (REQ-15).
type ServiceResult struct {
	Message     string `json:"message"`
	ActiveState string `json:"active_state,omitempty"`
}

// mapServiceError traduce los códigos propios de los scripts de servicios; los demás errores, y
// los que no son *OperationError (ErrVMNoIP, ErrTimeout...), pasan igual. operation es "create",
// "edit" o "redeploy" para el código 9 (la limpieza o la restauración también falló).
func mapServiceError(operation, name string, err error) error {
	var opErr *OperationError
	if !errors.As(err, &opErr) {
		return err
	}
	switch opErr.ExitCode {
	case exitNotManaged:
		return ErrNotManagedService
	case exitServiceExists:
		return ErrServiceExists
	case exitInvalidArchive:
		return ErrInvalidArchive
	case exitExecutableMissing:
		return ErrExecutableMissing
	case exitRollbackFailed:
		switch operation {
		case "create", "edit", "redeploy":
			return &ServiceRollbackError{Operation: operation, Name: name, Detail: opErr.Detail}
		}
	}
	return err
}

// parseResult lee el stdout de los scripts de cambio: la primera línea es el mensaje y una línea
// "active_state=<estado>" trae el estado leído tras iniciar o reiniciar.
func parseResult(stdout []byte) ServiceResult {
	var res ServiceResult
	for i, line := range strings.Split(strings.TrimSpace(string(stdout)), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case i == 0:
			res.Message = line
		case strings.HasPrefix(line, "active_state="):
			res.ActiveState = strings.TrimPrefix(line, "active_state=")
		}
	}
	return res
}

// History devuelve las últimas 50 operaciones del módulo, la más reciente primero (REQ-28).
func (s *ServicesService) History(ctx context.Context) ([]repository.Operation, error) {
	return s.history.ListRecent(ctx, servicesModule, historyLimit)
}

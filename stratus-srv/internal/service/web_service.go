package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"stratus-srv/internal/repository"
)

// webModule es el valor de la columna module en el historial del módulo 005 de servidores web.
const webModule = "web"

// Códigos de salida de los scripts de scripts/web que el backend traduce a un error propio (plan,
// sección 6). El 10 (archivo inválido) es el mismo de scripts/services.
const (
	exitWebPrecondition = 6  // disco base no encontrado, interfaz de puente no encontrada o VM ya registrada
	exitIndexMissing    = 11 // el archivo de la página no tiene index.html en la raíz
)

// WebConfig son los valores del entorno que necesita el módulo de servidores web.
type WebConfig struct {
	ApacheBaseDisk  string // disco base de Apache (APP_APACHE_BASE_DISK); argumento de create-web.sh
	NginxBaseDisk   string // disco base de Nginx (APP_NGINX_BASE_DISK); argumento de create-web.sh
	BridgeAdapter   string // interfaz del host para la red en puente
	Domain          string // zona DNS (APP_DNS_DOMAIN): el sitio se llama <vm>.<dominio>
	MaxArchiveBytes int64  // tamaño máximo del .tar.gz de la página
}

// WebService implementa las operaciones del módulo "Servidores web": el tipo de cada servidor vive
// en SQLite y cada cambio en el sistema lo hace un script de scripts/web o de scripts/prepare.
type WebService struct {
	cfg     WebConfig
	runner  ScriptRunner
	vms     *repository.VMRepository
	lbs     *repository.LBRepository
	web     *repository.WebRepository
	dnsRepo *repository.DNSRepository // registros DNS, para mostrar el FQDN
	history *repository.HistoryRepository
	locks   *VMLocks
	logger  *log.Logger
	ops     *operationRunner // bloqueos, historial y log compartidos con los demás módulos
}

func NewWebService(cfg WebConfig, runner ScriptRunner, vms *repository.VMRepository, lbs *repository.LBRepository,
	web *repository.WebRepository, dnsRepo *repository.DNSRepository, history *repository.HistoryRepository,
	locks *VMLocks, logger *log.Logger) *WebService {
	return &WebService{cfg: cfg, runner: runner, vms: vms, lbs: lbs, web: web, dnsRepo: dnsRepo, history: history,
		locks: locks, logger: logger, ops: newOperationRunner(webModule, runner, history, locks, logger)}
}

// History devuelve las últimas 50 operaciones del módulo, la más reciente primero (REQ-36).
func (s *WebService) History(ctx context.Context) ([]repository.Operation, error) {
	return s.history.ListRecent(ctx, webModule, historyLimit)
}

// MaxArchiveBytes es el tamaño máximo del archivo de la página; el handler limita el cuerpo con él.
func (s *WebService) MaxArchiveBytes() int64 {
	return s.cfg.MaxArchiveBytes
}

// webType devuelve el tipo del servidor web y true, o false si la VM no es un servidor web. Lo usa
// también PrepareService para el tipo del selector y las reglas de paquetes.
func (s *WebService) webType(ctx context.Context, vm string) (string, bool, error) {
	w, ok, err := s.web.Get(ctx, vm)
	return w.Type, ok, err
}

// getWeb valida el nombre y devuelve el tipo del servidor web, o ErrVMNotFound si la VM no es un
// servidor web registrado.
func (s *WebService) getWeb(ctx context.Context, vm string) (string, error) {
	if err := ValidateVMName(vm); err != nil {
		return "", err
	}
	typ, ok, err := s.webType(ctx, vm)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("buscar servidor web %s: %w", vm, ErrVMNotFound)
	}
	return typ, nil
}

// baseDiskMissing devuelve el error de disco base no encontrado del tipo.
func baseDiskMissing(typ string) error {
	if typ == WebTypeApache {
		return ErrApacheBaseDiskMissing
	}
	return ErrNginxBaseDiskMissing
}

// mapWebError traduce los códigos propios de los scripts de servidores web a su error centinela:
// el 6 con el texto del disco base, el 10 y el 11. Cualquier otro error queda igual.
func mapWebError(typ string, err error) error {
	var opErr *OperationError
	if !errors.As(err, &opErr) {
		return err
	}
	switch opErr.ExitCode {
	case exitWebPrecondition:
		if strings.HasPrefix(opErr.Detail, "Disco base de ") {
			return baseDiskMissing(typ)
		}
	case exitInvalidArchive:
		return ErrInvalidArchive
	case exitIndexMissing:
		return ErrIndexMissing
	}
	return err
}

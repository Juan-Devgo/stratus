package service

import (
	"context"
	"errors"
	"log"

	"stratus-srv/internal/repository"
)

// dnsModule es el valor de la columna module en el historial del módulo 005 de DNS.
const dnsModule = "dns"

// Códigos de salida de los scripts de scripts/dns que el backend traduce a un error propio (plan,
// sección 6). El 6 es el de create-web.sh y el 7 (sin IPv4) el de todos los scripts de invitado.
const (
	exitDNSPrecondition = 6  // disco base no encontrado, interfaz de puente no encontrada o VM ya registrada
	exitZoneInvalid     = 10 // named-checkzone rechazó la zona nueva; se mantuvo la anterior
	exitDNSIPInUse      = 13 // la IP de APP_DNS_IP responde en la LAN
)

// DNSService implementa las operaciones del módulo "DNS": el servidor, sus registros y los
// resolvedores viven en SQLite y cada cambio en el sistema lo hace un script de scripts/dns o de
// scripts/prepare. Los demás módulos lo llaman después de sus operaciones para mantener la zona.
type DNSService struct {
	cfg     DNSConfig
	runner  ScriptRunner
	vms     *repository.VMRepository
	lbs     *repository.LBRepository
	web     *repository.WebRepository // servidores web: sus registros iniciales (REQ-20)
	dnsRepo *repository.DNSRepository
	history *repository.HistoryRepository
	locks   *VMLocks
	logger  *log.Logger
	ops     *operationRunner // bloqueos, historial y log compartidos con los demás módulos
}

func NewDNSService(cfg DNSConfig, runner ScriptRunner, vms *repository.VMRepository, lbs *repository.LBRepository,
	web *repository.WebRepository, dnsRepo *repository.DNSRepository, history *repository.HistoryRepository,
	locks *VMLocks, logger *log.Logger) *DNSService {
	return &DNSService{cfg: cfg, runner: runner, vms: vms, lbs: lbs, web: web, dnsRepo: dnsRepo, history: history,
		locks: locks, logger: logger, ops: newOperationRunner(dnsModule, runner, history, locks, logger)}
}

// History devuelve las últimas 50 operaciones del módulo, la más reciente primero (REQ-36).
func (s *DNSService) History(ctx context.Context) ([]repository.Operation, error) {
	return s.history.ListRecent(ctx, dnsModule, historyLimit)
}

// IsDNSServer indica si la VM es el servidor DNS registrado.
func (s *DNSService) IsDNSServer(ctx context.Context, vm string) (bool, error) {
	name, ok, err := s.dnsRepo.Server(ctx)
	return ok && name == vm, err
}

// server devuelve el nombre del servidor DNS, o ErrNoDNSServer si no hay ninguno.
func (s *DNSService) server(ctx context.Context) (string, error) {
	name, ok, err := s.dnsRepo.Server(ctx)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", ErrNoDNSServer
	}
	return name, nil
}

// recordSpecs convierte los registros en los argumentos "<vm>,<ipv4>" de los scripts de scripts/dns.
func recordSpecs(recs []repository.DNSRecord) []string {
	specs := make([]string, len(recs))
	for i, rec := range recs {
		specs[i] = rec.VM + "," + rec.IP
	}
	return specs
}

// mapDNSError traduce el resultado de los scripts de registros: sin IPv4 el servidor DNS está
// apagado (REQ-25), el 10 es una zona rechazada (REQ-28) y el 13 la IP del servidor en uso
// (REQ-15). Cualquier otro error queda igual.
func mapDNSError(err error) error {
	if errors.Is(err, ErrVMNoIP) {
		return ErrDNSOff
	}
	var opErr *OperationError
	if !errors.As(err, &opErr) {
		return err
	}
	switch opErr.ExitCode {
	case exitZoneInvalid:
		return ErrZoneInvalid
	case exitDNSIPInUse:
		return ErrDNSIPInUse
	}
	return err
}

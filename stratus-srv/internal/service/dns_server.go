package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"stratus-srv/internal/repository"
)

// DNSServerView es el servidor DNS tal como lo muestra la pantalla DNS (REQ-17).
type DNSServerView struct {
	Name  string `json:"name"`
	State string `json:"state"` // running, poweroff o unknown
	IP    string `json:"ip"`
	Zone  string `json:"zone"`
}

// DNSVMView es una VM de la tabla de registros de la pantalla DNS (REQ-23, REQ-31).
type DNSVMView struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"` // "", apache o nginx
	Role     string `json:"role"` // vm, lb o server (004)
	State    string `json:"state"`
	IP       string `json:"ip"`        // IPv4 actual según list-vms.sh
	FQDN     string `json:"fqdn"`      // <vm>.<dominio> si tiene registro; si no, vacío
	RecordIP string `json:"record_ip"` // IPv4 guardada en su registro; si no tiene, vacío
	Resolver bool   `json:"resolver"`  // la app puso el servidor DNS como su resolvedor
}

// DNSOverview es la respuesta de GET /api/dns. Server es nil si no hay servidor DNS.
type DNSOverview struct {
	Server    *DNSServerView `json:"server"`
	VMs       []DNSVMView    `json:"vms"`
	ListError string         `json:"list_error"`
}

// Overview devuelve el servidor DNS y todas las demás VMs con su registro y su resolvedor. Si
// list-vms.sh falla, las VMs quedan en "unknown", ListError explica el motivo y la lectura fallida
// se guarda como list_dns en el historial (REQ-35).
func (s *DNSService) Overview(ctx context.Context) (DNSOverview, error) {
	serverName, hasServer, err := s.dnsRepo.Server(ctx)
	if err != nil {
		return DNSOverview{}, err
	}
	rows, err := s.vms.List(ctx)
	if err != nil {
		return DNSOverview{}, err
	}
	isLB, serverLB, err := lbTopology(ctx, s.lbs)
	if err != nil {
		return DNSOverview{}, err
	}
	webRows, err := s.web.List(ctx)
	if err != nil {
		return DNSOverview{}, err
	}
	webType := map[string]string{}
	for _, w := range webRows {
		webType[w.Name] = w.Type
	}
	recs, err := s.dnsRepo.Records(ctx)
	if err != nil {
		return DNSOverview{}, err
	}
	recordIP := map[string]string{}
	for _, rec := range recs {
		recordIP[rec.VM] = rec.IP
	}
	resolvers, err := s.dnsRepo.Resolvers(ctx)
	if err != nil {
		return DNSOverview{}, err
	}

	states, listErr, failed := queryVMStates(ctx, s.runner, s.logger)
	if failed != nil {
		failed.Module, failed.Operation, failed.VM = dnsModule, "list_dns", "-"
		if err := s.history.Insert(ctx, *failed); err != nil {
			s.logger.Printf("no se pudo guardar el historial de list_dns: %v", err)
		}
	}

	view := DNSOverview{VMs: []DNSVMView{}}
	var missing []string
	for _, row := range rows {
		status, ok := states[row.Name]
		if !ok {
			status.state = "unknown"
			missing = append(missing, row.Name)
		}
		if hasServer && row.Name == serverName {
			view.Server = &DNSServerView{Name: row.Name, State: status.state, IP: status.ip, Zone: s.cfg.Domain}
			continue
		}
		vm := DNSVMView{Name: row.Name, Kind: webType[row.Name], Role: "vm", State: status.state, IP: status.ip,
			RecordIP: recordIP[row.Name], Resolver: slices.Contains(resolvers, row.Name)}
		switch {
		case isLB[row.Name]:
			vm.Role = "lb"
		case serverLB[row.Name] != "":
			// Un servidor de balanceador no se registra: se oculta la fila que aún conserve (REQ-23)
			vm.Role = "server"
			vm.RecordIP = ""
		}
		if vm.RecordIP != "" {
			vm.FQDN = row.Name + "." + s.cfg.Domain
		}
		view.VMs = append(view.VMs, vm)
	}
	if listErr == "" && len(missing) > 0 {
		listErr = "Estas VMs no aparecen en VirtualBox: " + strings.Join(missing, ", ")
	}
	view.ListError = listErr
	return view, nil
}

// CreateDNS crea el único servidor DNS (REQ-13): valida la entrada con las reglas de 001 y la
// configuración APP_DNS_* (REQ-15), toma el bloqueo de creación compartido, registra el nombre y
// ejecuta create-dns.sh con los registros iniciales de los servidores web sin balanceador y con una
// IPv4 de la LAN (REQ-20, REQ-22). Si el script falla, el nombre se libera; si termina bien, guarda
// el servidor y esos registros. El mensaje nombra los servidores web omitidos.
func (s *DNSService) CreateDNS(ctx context.Context, in CreateVMInput) (string, error) {
	if err := ValidateCreateVM(in); err != nil {
		return "", err
	}
	if err := ValidateDNSConfig(s.cfg); err != nil {
		return "", err
	}
	if _, exists, err := s.dnsRepo.Server(ctx); err != nil {
		return "", err
	} else if exists {
		return "", ErrDNSExists
	}
	if s.cfg.BaseDisk == "" {
		return "", ErrDNSBaseDiskMissing
	}

	initial, skipped, err := s.webRecords(ctx)
	if err != nil {
		return "", err
	}

	if !s.locks.TryLockCreate() {
		return "", ErrCreateInProgress
	}
	defer s.locks.UnlockCreate()

	if _, err := s.vms.Insert(ctx, in.Name); errors.Is(err, repository.ErrDuplicateVM) {
		return "", fmt.Errorf("crear servidor DNS %s: %w", in.Name, ErrDuplicateVM)
	} else if err != nil {
		return "", err
	}

	args := []string{in.Name, strconv.Itoa(in.MemoryMB), strconv.Itoa(in.CPUs), s.cfg.BaseDisk, s.cfg.BridgeAdapter,
		s.cfg.IP, s.cfg.Gateway, s.cfg.Domain, s.cfg.Forwarders}
	args = append(args, recordSpecs(initial)...)
	scriptOK := false // el hook solo corre si el script terminó bien: entonces la VM existe
	save := func(ctx context.Context, _ RunResult) error {
		scriptOK = true
		return s.dnsRepo.InsertServer(ctx, in.Name, initial)
	}
	res, err := s.ops.runLocked(ctx, "create_dns", in.Name, []string{in.Name}, "dns/create-dns.sh", args, true, save)
	if err != nil {
		if !scriptOK {
			// El script limpia lo que haya creado en VirtualBox; aquí solo se libera el nombre.
			if delErr := s.vms.Delete(context.WithoutCancel(ctx), in.Name); delErr != nil {
				s.logger.Printf("no se pudo liberar el nombre %s: %v", in.Name, delErr)
			}
		}
		return "", mapCreateDNSError(err)
	}

	msg := firstLine(string(res.Stdout))
	if msg == "" {
		msg = fmt.Sprintf("Servidor DNS %s creado en %s; zona %s", in.Name, s.cfg.Address(), s.cfg.Domain)
	}
	if len(skipped) > 0 {
		msg += ". Servidores web sin registro DNS (sin IPv4 en la LAN o asignados a un balanceador): " + strings.Join(skipped, ", ")
	}
	return msg, nil
}

// webRecords devuelve los registros iniciales de los servidores web que no están asignados a un
// balanceador y cuya IPv4 de list-vms.sh está en la LAN (REQ-20, REQ-22), y los nombres de los demás:
// los asignados y los que no tienen una IPv4 de la LAN.
func (s *DNSService) webRecords(ctx context.Context) (initial []repository.DNSRecord, skipped []string, err error) {
	webRows, err := s.web.List(ctx)
	if err != nil || len(webRows) == 0 {
		return nil, nil, err
	}
	_, serverLB, err := lbTopology(ctx, s.lbs)
	if err != nil {
		return nil, nil, err
	}
	states, _, _ := queryVMStates(ctx, s.runner, s.logger)
	for _, w := range webRows {
		if ip := states[w.Name].ip; serverLB[w.Name] == "" && s.cfg.OnLAN(ip) {
			initial = append(initial, repository.DNSRecord{VM: w.Name, IP: ip})
		} else {
			skipped = append(skipped, w.Name)
		}
	}
	return initial, skipped, nil
}

// mapCreateDNSError traduce los códigos propios de create-dns.sh: el 6 con el texto del disco base y
// el 13 de la IP en uso. Cualquier otro error queda igual (el 7 es el texto de 001 de VM sin IP).
func mapCreateDNSError(err error) error {
	var opErr *OperationError
	if !errors.As(err, &opErr) {
		return err
	}
	switch {
	case opErr.ExitCode == exitDNSPrecondition && strings.HasPrefix(opErr.Detail, "Disco base de "):
		return ErrDNSBaseDiskMissing
	case opErr.ExitCode == exitDNSIPInUse:
		return ErrDNSIPInUse
	}
	return err
}

// DeleteDNS elimina el servidor DNS con delete-vm.sh (VM y disco diferencial) y, si termina bien,
// borra en una transacción todos los registros, el servidor y su fila de vms (REQ-21). Mientras
// alguna VM lo use como resolvedor, aunque esté apagada, se rechaza sin llamar al script (REQ-33).
func (s *DNSService) DeleteDNS(ctx context.Context) (string, error) {
	name, err := s.server(ctx)
	if err != nil {
		return "", err
	}
	resolvers, err := s.dnsRepo.Resolvers(ctx)
	if err != nil {
		return "", err
	}
	if len(resolvers) > 0 {
		return "", &ResolversSetError{VMs: resolvers}
	}
	if _, err := s.ops.run(ctx, "delete_dns", name, "prepare/delete-vm.sh", []string{name}, true,
		s.dnsRepo.DeleteServer); err != nil {
		return "", err
	}
	return fmt.Sprintf("Servidor DNS %s eliminado junto con su disco", name), nil
}

// DNSAction inicia, detiene o reinicia bind9 en el servidor DNS con su bloqueo (REQ-12, REQ-34).
// Iniciar y reiniciar devuelven además el estado leído unos segundos después (AC-21).
func (s *DNSService) DNSAction(ctx context.Context, action string) (ServiceResult, error) {
	if !slices.Contains(unitActions, action) {
		return ServiceResult{}, &ValidationError{Message: "Acción no válida: debe ser start, stop o restart"}
	}
	name, err := s.server(ctx)
	if err != nil {
		return ServiceResult{}, err
	}
	res, err := s.ops.run(ctx, action+"_bind", name, "dns/"+action+"-bind.sh", []string{name}, true, nil)
	if err != nil {
		return ServiceResult{}, err
	}
	return parseResult(res.Stdout), nil
}

// DNSStatus devuelve la salida de "systemctl status named" (REQ-12). Una lectura correcta no queda
// en el historial.
func (s *DNSService) DNSStatus(ctx context.Context) (string, error) {
	name, err := s.server(ctx)
	if err != nil {
		return "", err
	}
	res, err := s.ops.run(ctx, "status_bind", name, "dns/status-bind.sh", []string{name}, false, nil)
	if err != nil {
		return "", err
	}
	return string(res.Stdout), nil
}

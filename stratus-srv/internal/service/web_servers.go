package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"stratus-srv/internal/repository"
)

// CreateWebInput es el cuerpo de POST /api/web/servers.
type CreateWebInput struct {
	Name     string `json:"name"`
	MemoryMB int    `json:"memory_mb"`
	CPUs     int    `json:"cpus"`
	Type     string `json:"type"` // apache o nginx
}

// WebServerView es un servidor web tal como lo ve la UI. FQDN y LB vacíos se muestran como "-".
type WebServerView struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	State string `json:"state"` // running, poweroff o unknown
	IP    string `json:"ip"`
	FQDN  string `json:"fqdn"` // <vm>.<dominio> si tiene registro DNS
	LB    string `json:"lb"`   // balanceador al que está asignado
}

// ListWebServers devuelve solo los servidores web con su estado, su IP, su FQDN y su balanceador
// (REQ-6). Si list-vms.sh falla, los servidores quedan en "unknown", listErr explica el motivo y la
// lectura fallida se guarda como list_web en el historial (REQ-35).
func (s *WebService) ListWebServers(ctx context.Context) ([]WebServerView, string, error) {
	rows, err := s.web.List(ctx)
	if err != nil {
		return nil, "", err
	}

	states, listErr, failed := queryVMStates(ctx, s.runner, s.logger)
	if failed != nil {
		failed.Module, failed.Operation, failed.VM = webModule, "list_web", "-"
		if err := s.history.Insert(ctx, *failed); err != nil {
			s.logger.Printf("no se pudo guardar el historial de list_web: %v", err)
		}
	}

	var missing []string
	servers := make([]WebServerView, 0, len(rows))
	for _, row := range rows {
		status, ok := states[row.Name]
		if !ok {
			status.state = "unknown"
			missing = append(missing, row.Name)
		}
		view := WebServerView{Name: row.Name, Type: row.Type, State: status.state, IP: status.ip}
		if _, hasRecord, err := s.dnsRepo.Record(ctx, row.Name); err != nil {
			return nil, "", err
		} else if hasRecord {
			view.FQDN = row.Name + "." + s.cfg.Domain
		}
		if server, assigned, err := s.lbs.ServerByVM(ctx, row.Name); err != nil {
			return nil, "", err
		} else if assigned {
			view.LB = server.LB
		}
		servers = append(servers, view)
	}
	if listErr == "" && len(missing) > 0 {
		listErr = "Estas VMs no aparecen en VirtualBox: " + strings.Join(missing, ", ")
	}
	return servers, listErr, nil
}

// createWebWarning es la advertencia de REQ-5 cuando el servidor web se creó pero no su registro DNS.
const createWebWarning = "Servidor web creado; no se pudo registrar en DNS, agréguelo desde DNS"

// CreateWeb crea el servidor web y, ya sueltos los bloqueos, agrega su registro DNS con la IP que
// imprime create-web.sh si hay servidor DNS (REQ-5). Un fallo del registro no es un fallo de la
// creación: se devuelve como advertencia y queda como fila fallida en el historial de dns.
func (s *WebService) CreateWeb(ctx context.Context, in CreateWebInput) (ActionResult, error) {
	stdout, err := s.createWeb(ctx, in)
	if err != nil {
		return ActionResult{}, err
	}
	res := ActionResult{Message: firstLine(stdout)}
	if res.Message == "" {
		res.Message = fmt.Sprintf("Servidor web %s creado", in.Name)
	}
	if ip, ok := parseIP(stdout); ok {
		if err := s.dns.AddRecordsFor(ctx, in.Name, ip); err != nil {
			s.logger.Printf("no se pudo registrar %s en DNS: %v", in.Name, err)
			res.Warning = createWebWarning
		}
	} else if _, hasDNS, err := s.dnsRepo.Server(ctx); err != nil || hasDNS {
		s.logger.Printf("no se pudo registrar %s en DNS: create-web.sh no informó su IP", in.Name)
		res.Warning = createWebWarning
	}
	return res, nil
}

// createWeb valida la entrada con las reglas de 001 y el tipo, toma el bloqueo de creación (compartido
// con create_vm y create_lb), registra el nombre y ejecuta create-web.sh con el disco base del tipo.
// Si el script falla, el nombre se libera; si termina bien, guarda el servidor con su tipo y devuelve
// el stdout del script.
func (s *WebService) createWeb(ctx context.Context, in CreateWebInput) (string, error) {
	if err := ValidateCreateVM(CreateVMInput{Name: in.Name, MemoryMB: in.MemoryMB, CPUs: in.CPUs}); err != nil {
		return "", err
	}
	if err := ValidateWebType(in.Type); err != nil {
		return "", err
	}
	disk := s.cfg.NginxBaseDisk
	if in.Type == WebTypeApache {
		disk = s.cfg.ApacheBaseDisk
	}
	if disk == "" {
		return "", baseDiskMissing(in.Type)
	}
	if !s.locks.TryLockCreate() {
		return "", ErrCreateInProgress
	}
	defer s.locks.UnlockCreate()

	if _, err := s.vms.Insert(ctx, in.Name); errors.Is(err, repository.ErrDuplicateVM) {
		return "", fmt.Errorf("crear servidor web %s: %w", in.Name, ErrDuplicateVM)
	} else if err != nil {
		return "", err
	}

	args := []string{in.Name, strconv.Itoa(in.MemoryMB), strconv.Itoa(in.CPUs), in.Type, disk,
		s.cfg.BridgeAdapter, s.cfg.Domain}
	scriptOK := false // el hook solo corre si el script terminó bien: entonces la VM existe
	save := func(ctx context.Context, _ RunResult) error {
		scriptOK = true
		return s.web.Insert(ctx, repository.WebServer{Name: in.Name, Type: in.Type})
	}
	res, err := s.ops.runLocked(ctx, "create_web", in.Name, []string{in.Name}, "web/create-web.sh", args, true, save)
	if err != nil {
		if !scriptOK {
			// El script limpia lo que haya creado en VirtualBox; aquí solo se libera el nombre.
			if delErr := s.vms.Delete(context.WithoutCancel(ctx), in.Name); delErr != nil {
				s.logger.Printf("no se pudo liberar el nombre %s: %v", in.Name, delErr)
			}
		}
		return "", mapWebError(in.Type, err)
	}

	return string(res.Stdout), nil
}

// DeleteWeb elimina el servidor web con delete-vm.sh (VM y disco diferencial; el disco base no se
// toca) y, si termina bien, borra sus filas de web_servers y de vms y después su registro DNS
// (REQ-30); un fallo del registro es la advertencia de REQ-24. Un servidor asignado a un
// balanceador se rechaza sin llamar al script (REQ-8).
func (s *WebService) DeleteWeb(ctx context.Context, vm string) (ActionResult, error) {
	if _, err := s.getWeb(ctx, vm); err != nil {
		return ActionResult{}, err
	}
	if _, assigned, err := s.lbs.ServerByVM(ctx, vm); err != nil {
		return ActionResult{}, err
	} else if assigned {
		return ActionResult{}, ErrVMIsServer
	}

	deleteRows := func(ctx context.Context) error {
		if err := s.web.Delete(ctx, vm); err != nil {
			return err
		}
		return s.vms.Delete(ctx, vm)
	}
	if _, err := s.ops.run(ctx, "delete_web", vm, "prepare/delete-vm.sh", []string{vm}, true, deleteRows); err != nil {
		return ActionResult{}, err
	}
	return ActionResult{Message: fmt.Sprintf("Servidor web %s eliminado junto con su disco", vm),
		Warning: s.dns.AfterVMDeleted(ctx, vm)}, nil
}

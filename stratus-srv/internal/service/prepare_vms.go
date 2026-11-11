package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"stratus-srv/internal/repository"
)

// VMView es una VM tal como la ve la UI: los datos de SQLite más el estado de VirtualBox.
type VMView struct {
	Name             string `json:"name"`
	State            string `json:"state"` // running, poweroff o unknown
	IP               string `json:"ip"`    // IPv4 de la VM; vacío si está apagada, no la reporta o el estado es unknown
	UserKeyGenerated bool   `json:"user_key_generated"`
	Role             string `json:"role"`  // vm, lb o server (004)
	LB               string `json:"lb"`    // de un servidor, su balanceador; si no, vacío
	LBIP             string `json:"lb_ip"` // de un servidor, la IPv4 en puente de su balanceador
	Kind             string `json:"kind"`  // "", apache, nginx o dns (005): el tipo para el selector
}

// ListVMs devuelve las VMs registradas con su estado y su rol (vm, lb o server). Si list-vms.sh
// falla, o una VM no aparece en su salida, esa VM queda en "unknown" y listErr explica el motivo.
// El listado es de solo lectura: no toma bloqueos ni se guarda en el historial.
func (s *PrepareService) ListVMs(ctx context.Context) (vms []VMView, listErr string, err error) {
	rows, err := s.vms.List(ctx)
	if err != nil {
		return nil, "", err
	}
	// La topología sale de SQLite: el rol nunca se deduce de VirtualBox (plan, sección 3).
	isLB, serverLB, err := lbTopology(ctx, s.lbs)
	if err != nil {
		return nil, "", err
	}

	dnsServer, _, err := s.dns.dnsRepo.Server(ctx)
	if err != nil {
		return nil, "", err
	}

	states, listErr, _ := queryVMStates(ctx, s.runner, s.logger)

	var missing []string
	vms = make([]VMView, 0, len(rows))
	for _, row := range rows {
		status, ok := states[row.Name]
		if !ok {
			status.state = "unknown"
			missing = append(missing, row.Name)
		}
		view := VMView{
			Name:             row.Name,
			State:            status.state,
			IP:               status.ip,
			UserKeyGenerated: row.UserKeyGenerated,
			Role:             "vm",
		}
		if typ, ok, err := s.web.webType(ctx, row.Name); err != nil {
			return nil, "", err
		} else if ok {
			view.Kind = typ
		}
		if row.Name == dnsServer {
			view.Kind = "dns"
		}
		switch {
		case isLB[row.Name]:
			view.Role = "lb"
		case serverLB[row.Name] != "":
			view.Role = "server"
			view.LB = serverLB[row.Name]
			view.LBIP = states[view.LB].ip
		}
		vms = append(vms, view)
	}
	if listErr == "" && len(missing) > 0 {
		listErr = "Estas VMs no aparecen en VirtualBox: " + strings.Join(missing, ", ")
	}
	return vms, listErr, nil
}

// CreateVM valida la entrada, registra el nombre en SQLite e invoca create-vm.sh con el disco
// base y la interfaz de puente. Si el script falla, el registro se borra para liberar el nombre.
// El mensaje es la salida del script ("VM <vm> creada y encendida; IP <ip>").
func (s *PrepareService) CreateVM(ctx context.Context, in CreateVMInput) (string, error) {
	if err := ValidateCreateVM(in); err != nil {
		return "", err
	}
	// Todas las creaciones leen, y a veces cambian, el tipo del disco base compartido y lo conectan a
	// su VM: dos a la vez podrían chocar en VirtualBox. Se rechaza de inmediato, sin cola. Va después
	// de validar, para que una entrada inválida no cuente como creación en curso.
	if !s.locks.TryLockCreate() {
		return "", ErrCreateInProgress
	}
	defer s.locks.UnlockCreate()

	_, err := s.vms.Insert(ctx, in.Name)
	if errors.Is(err, repository.ErrDuplicateVM) {
		return "", fmt.Errorf("crear vm %s: %w", in.Name, ErrDuplicateVM)
	}
	if err != nil {
		return "", err
	}

	args := []string{in.Name, strconv.Itoa(in.MemoryMB), strconv.Itoa(in.CPUs), s.cfg.BaseDisk, s.cfg.BridgeAdapter}
	res, err := s.runOperation(ctx, "create_vm", in.Name, "prepare/create-vm.sh", args, nil)
	if err != nil {
		// El script limpia lo que haya creado en VirtualBox; aquí solo se libera el nombre.
		if delErr := s.vms.Delete(context.WithoutCancel(ctx), in.Name); delErr != nil {
			s.logger.Printf("no se pudo liberar el nombre %s: %v", in.Name, delErr)
		}
		return "", err
	}
	// create-vm.sh solo imprime el mensaje de resultado (nunca secretos).
	if msg := strings.TrimSpace(string(res.Stdout)); msg != "" {
		return msg, nil
	}
	return fmt.Sprintf("VM %s creada y encendida", in.Name), nil
}

// ActionResult es el resultado de una operación que termina bien pero puede dejar un paso
// posterior pendiente: el mensaje de éxito y, si ese paso falló (actualizar las IPs del balanceador,
// 004 REQ-14, o los registros DNS, 005 REQ-26), una advertencia. Notice informa algo que no es un
// fallo (005 REQ-41, REQ-42): el registro DNS que quedó quitado o ausente.
type ActionResult struct {
	Message string `json:"message"`
	Notice  string `json:"notice,omitempty"`
	Warning string `json:"warning,omitempty"`
}

// StartVM enciende la VM con start-vm.sh. Si la VM es un balanceador o un servidor asignado, después
// refresca las IPs internas de ese balanceador (HAProxyService.Refresh): la IP de un servidor puede
// haber cambiado con el nuevo arranque. Un fallo del refresco no es un fallo de encender la VM: se
// devuelve como advertencia y queda en el historial de haproxy.
func (s *PrepareService) StartVM(ctx context.Context, name string) (ActionResult, error) {
	if _, err := s.getVM(ctx, name); err != nil {
		return ActionResult{}, err
	}
	if _, err := s.runOperation(ctx, "start_vm", name, "prepare/start-vm.sh", []string{name}, nil); err != nil {
		return ActionResult{}, err
	}
	result := ActionResult{Message: fmt.Sprintf("VM %s encendida", name)}

	lb, err := s.balancerOf(ctx, name)
	if err != nil {
		return ActionResult{}, err
	}
	if lb != "" {
		if _, err := s.haproxy.Refresh(ctx, lb); err != nil {
			s.logger.Printf("operación=start_vm vm=%s no se pudo refrescar el balanceador %s: %v", name, lb, err)
			result.Warning = fmt.Sprintf("VM iniciada; no se pudieron actualizar las IPs del balanceador %s, use Actualizar IPs", lb)
		}
	}

	// REQ-26, REQ-27: el servidor DNS actualiza todos los registros; otra VM, solo el suyo.
	isDNS, err := s.dns.IsDNSServer(ctx, name)
	if err != nil {
		return ActionResult{}, err
	}
	refresh := s.dns.RefreshVM
	if isDNS {
		refresh = func(ctx context.Context, _ string) error { return s.dns.RefreshAll(ctx) }
	}
	if err := refresh(ctx, name); err != nil {
		s.logger.Printf("operación=start_vm vm=%s no se pudo actualizar el registro DNS: %v", name, err)
		result.Warning = joinWarnings(result.Warning, refreshWarning(name))
	}
	return result, nil
}

// balancerOf devuelve el balanceador al que pertenece la VM: ella misma si es un balanceador, el suyo
// si es un servidor asignado, y "" si es una VM común.
func (s *PrepareService) balancerOf(ctx context.Context, vm string) (string, error) {
	if _, err := s.lbs.GetLB(ctx, vm); err == nil {
		return vm, nil
	} else if !errors.Is(err, repository.ErrLBNotFound) {
		return "", err
	}
	server, assigned, err := s.lbs.ServerByVM(ctx, vm)
	if err != nil || !assigned {
		return "", err
	}
	return server.LB, nil
}

// StopVM apaga la VM con stop-vm.sh (ACPI y apagado forzado tras 60 s).
func (s *PrepareService) StopVM(ctx context.Context, name string) (string, error) {
	if _, err := s.getVM(ctx, name); err != nil {
		return "", err
	}
	if _, err := s.runOperation(ctx, "stop_vm", name, "prepare/stop-vm.sh", []string{name}, nil); err != nil {
		return "", err
	}
	return fmt.Sprintf("VM %s apagada", name), nil
}

// DeleteVM elimina la VM con delete-vm.sh y, si termina bien, borra su fila y la llave de
// usuario pendiente: así se limpia su bandera (REQ-27) y no queda una llave
// descargable de una VM que ya no existe.
//
// Reglas del módulo 004 (REQ-6, REQ-12): una VM asignada a un balanceador se rechaza con
// ErrVMIsServer; un balanceador con servidores, con ErrLBHasServers; y un balanceador sin
// servidores se elimina con delete-lb.sh, que también quita su red interna. Un servidor web (005)
// se elimina con WebService.DeleteWeb, que también borra su tipo, y el servidor DNS con
// DNSService.DeleteDNS, que se niega mientras alguna VM lo use como resolvedor. Después se quita el
// registro DNS de la VM eliminada; si falla, la eliminación termina bien con la advertencia de 005
// REQ-24.
func (s *PrepareService) DeleteVM(ctx context.Context, name string) (ActionResult, error) {
	if _, err := s.getVM(ctx, name); err != nil {
		return ActionResult{}, err
	}
	if _, assigned, err := s.lbs.ServerByVM(ctx, name); err != nil {
		return ActionResult{}, err
	} else if assigned {
		return ActionResult{}, ErrVMIsServer
	}
	if _, err := s.lbs.GetLB(ctx, name); err == nil {
		res, err := s.haproxy.DeleteLB(ctx, name)
		if err != nil {
			return ActionResult{}, err
		}
		s.keys.Discard(name)
		return res, nil
	} else if !errors.Is(err, repository.ErrLBNotFound) {
		return ActionResult{}, err
	}
	if _, isWeb, err := s.web.webType(ctx, name); err != nil {
		return ActionResult{}, err
	} else if isWeb {
		res, err := s.web.DeleteWeb(ctx, name)
		if err != nil {
			return ActionResult{}, err
		}
		s.keys.Discard(name)
		return res, nil
	}
	if isDNS, err := s.dns.IsDNSServer(ctx, name); err != nil {
		return ActionResult{}, err
	} else if isDNS {
		msg, err := s.dns.DeleteDNS(ctx)
		if err != nil {
			return ActionResult{}, err
		}
		s.keys.Discard(name)
		return ActionResult{Message: msg}, nil
	}
	deleteRow := func(ctx context.Context) error {
		s.keys.Discard(name)
		return s.vms.Delete(ctx, name)
	}
	if _, err := s.runOperation(ctx, "delete_vm", name, "prepare/delete-vm.sh", []string{name}, deleteRow); err != nil {
		return ActionResult{}, err
	}
	return ActionResult{Message: fmt.Sprintf("VM %s eliminada junto con su disco", name),
		Warning: s.dns.AfterVMDeleted(ctx, name)}, nil
}

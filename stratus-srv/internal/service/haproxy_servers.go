package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"stratus-srv/internal/repository"
)

// ServerView es un servidor de un balanceador tal como lo ve la UI. State es "-" cuando solo se
// leyó SQLite (balanceador apagado o aún sin leer el estado) o UP, DOWN, MAINT... cuando se leyó
// de HAProxy.
type ServerView struct {
	Name  string `json:"name"`
	VM    string `json:"vm"`
	IP    string `json:"ip"` // última IP interna conocida
	Port  int    `json:"port"`
	State string `json:"state"`
}

// ListServers devuelve los servidores guardados del balanceador, ordenados por número, con el
// estado "-": no llama a ningún script (REQ-20). El estado real lo agrega ServerStatus.
func (s *HAProxyService) ListServers(ctx context.Context, lb string) ([]ServerView, error) {
	if _, err := s.getLB(ctx, lb); err != nil {
		return nil, err
	}
	rows, err := s.lbs.Servers(ctx, lb)
	if err != nil {
		return nil, err
	}
	servers := make([]ServerView, 0, len(rows))
	for _, r := range rows {
		servers = append(servers, ServerView{Name: r.Name, VM: r.VM, IP: r.LastIP, Port: r.Port, State: "-"})
	}
	return servers, nil
}

// AssignServer asigna la VM al balanceador con assign-server.sh (REQ-8 a REQ-10). Antes de llamar al
// script comprueba, sin cambiar nada: puerto válido, balanceador y VM registrados, que la VM no sea
// un balanceador ni servidor de ningún balanceador (ErrVMNotAssignable) y que el balanceador tenga
// menos de 10 servidores (ErrLBFull). Toma el bloqueo del balanceador y de la VM a la vez (REQ-21).
// Si el script termina bien, guarda la fila con la IP que imprime ("ip=<ip>") antes de soltarlos.
func (s *HAProxyService) AssignServer(ctx context.Context, lb, vm string, port int) (string, error) {
	if err := ValidatePort(port); err != nil {
		return "", err
	}
	lbRow, err := s.getLB(ctx, lb)
	if err != nil {
		return "", err
	}
	if err := ValidateVMName(vm); err != nil {
		return "", err
	}
	if _, err := s.vms.Get(ctx, vm); errors.Is(err, repository.ErrVMNotFound) {
		return "", fmt.Errorf("buscar vm %s: %w", vm, ErrVMNotFound)
	} else if err != nil {
		return "", err
	}
	if _, err := s.lbs.GetLB(ctx, vm); err == nil {
		return "", ErrVMNotAssignable // un balanceador no puede ser servidor
	} else if !errors.Is(err, repository.ErrLBNotFound) {
		return "", err
	}
	if _, assigned, err := s.lbs.ServerByVM(ctx, vm); err != nil {
		return "", err
	} else if assigned {
		return "", ErrVMNotAssignable // ya es servidor de este balanceador o de otro
	}
	rows, err := s.lbs.Servers(ctx, lb)
	if err != nil {
		return "", err
	}
	name, err := nextServerName(rows)
	if err != nil {
		return "", err
	}

	args := []string{lb, strconv.Itoa(lbRow.Subnet), vm, name, strconv.Itoa(port),
		strconv.Itoa(lbRow.FrontendPort), s.cfg.BridgeAdapter}
	args = append(args, serverSpecs(rows)...)

	saveRow := func(ctx context.Context, res RunResult) error {
		ip, ok := parseIP(string(res.Stdout))
		if !ok {
			return errors.New("assign-server.sh no informó la IP del servidor (ip=<ip>)")
		}
		return s.lbs.InsertServer(ctx, repository.LBServer{LB: lb, Name: name, VM: vm, Port: port, LastIP: ip, Enabled: true})
	}
	res, err := s.ops.runLocked(ctx, "assign_server", lb, []string{lb, vm}, "haproxy/assign-server.sh", args, true, saveRow)
	if err != nil {
		return "", mapHAProxyError(err)
	}
	if msg := firstLine(string(res.Stdout)); msg != "" {
		return msg, nil
	}
	return fmt.Sprintf("Servidor %s (%s) asignado a %s", name, vm, lb), nil
}

// RemoveServer quita la VM del balanceador con remove-server.sh (REQ-11). La VM debe ser servidor de
// ese balanceador. Pasa al script los servidores que quedan; si termina bien, borra la fila. Con el
// balanceador apagado el script sale con 12 y la fila se conserva.
func (s *HAProxyService) RemoveServer(ctx context.Context, lb, vm string) (string, error) {
	lbRow, err := s.getLB(ctx, lb)
	if err != nil {
		return "", err
	}
	if err := ValidateVMName(vm); err != nil {
		return "", err
	}
	rows, err := s.lbs.Servers(ctx, lb)
	if err != nil {
		return "", err
	}
	var remaining []repository.LBServer
	found := false
	for _, r := range rows {
		if r.VM == vm {
			found = true
			continue
		}
		remaining = append(remaining, r)
	}
	if !found {
		return "", fmt.Errorf("quitar %s de %s: no es un servidor de este balanceador: %w", vm, lb, ErrVMNotFound)
	}

	args := []string{lb, vm, strconv.Itoa(lbRow.FrontendPort), s.cfg.BridgeAdapter}
	args = append(args, serverSpecs(remaining)...)

	deleteRow := func(ctx context.Context, _ RunResult) error { return s.lbs.DeleteServer(ctx, lb, vm) }
	res, err := s.ops.runLocked(ctx, "remove_server", lb, []string{lb, vm}, "haproxy/remove-server.sh", args, true, deleteRow)
	if err != nil {
		return "", mapHAProxyError(err)
	}
	if msg := firstLine(string(res.Stdout)); msg != "" {
		return msg, nil
	}
	return fmt.Sprintf("Servidor %s quitado de %s", vm, lb), nil
}

// parseIP lee la línea "ip=<ipv4>" de la salida de assign-server.sh.
func parseIP(stdout string) (string, bool) {
	for _, line := range strings.Split(stdout, "\n") {
		if value, found := strings.CutPrefix(strings.TrimSpace(line), "ip="); found && validIPv4(value) {
			return value, true
		}
	}
	return "", false
}

// ServerStatus lee el estado de cada servidor de HAProxy con lb-status.sh y lo devuelve como un mapa
// server<N> -> UP, DOWN, MAINT... (REQ-20). Toma el bloqueo del balanceador. Es una lectura: un
// resultado exitoso no deja fila en el historial; un fallo (por ejemplo el balanceador apagado,
// *LBOffError) sí. El mapa nunca es nil.
func (s *HAProxyService) ServerStatus(ctx context.Context, lb string) (map[string]string, error) {
	if _, err := s.getLB(ctx, lb); err != nil {
		return nil, err
	}
	res, err := s.ops.run(ctx, "lb_status", lb, "haproxy/lb-status.sh", []string{lb}, false, nil)
	if err != nil {
		return nil, mapHAProxyError(err)
	}
	return parseStatus(string(res.Stdout)), nil
}

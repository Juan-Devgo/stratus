package service

import (
	"context"
	"fmt"
	"strconv"

	"stratus-srv/internal/repository"
)

// SaveFrontend cambia el puerto del frontend del balanceador con save-frontend.sh (REQ-7). Valida el
// puerto (1-65535, no 22) antes de llamar al script, le pasa los servidores guardados y toma el
// bloqueo del balanceador. Si el script termina bien, guarda el puerto; si HAProxy rechaza la
// configuración (10) o no puede recargar (11), el puerto guardado no cambia.
func (s *HAProxyService) SaveFrontend(ctx context.Context, lb string, port int) (string, error) {
	if err := ValidateFrontendPort(port); err != nil {
		return "", err
	}
	if _, err := s.getLB(ctx, lb); err != nil {
		return "", err
	}
	rows, err := s.lbs.Servers(ctx, lb)
	if err != nil {
		return "", err
	}
	args := append([]string{lb, strconv.Itoa(port)}, serverSpecs(rows)...)

	savePort := func(ctx context.Context) error { return s.lbs.SetFrontendPort(ctx, lb, port) }
	res, err := s.ops.run(ctx, "save_frontend", lb, "haproxy/save-frontend.sh", args, true, savePort)
	if err != nil {
		return "", mapHAProxyError(err)
	}
	if msg := firstLine(string(res.Stdout)); msg != "" {
		return msg, nil
	}
	return fmt.Sprintf("Frontend de %s en el puerto %d", lb, port), nil
}

// Refresh pone al día las IPs internas de los servidores del balanceador con refresh-lb.sh (REQ-14,
// REQ-15). Pasa las specs guardadas y toma el bloqueo del balanceador. Si el script termina bien,
// guarda en una sola transacción la IP y la bandera enabled que informa por servidor (un servidor
// apagado o sin IP queda con su última IP y enabled=false).
func (s *HAProxyService) Refresh(ctx context.Context, lb string) (string, error) {
	lbRow, err := s.getLB(ctx, lb)
	if err != nil {
		return "", err
	}
	rows, err := s.lbs.Servers(ctx, lb)
	if err != nil {
		return "", err
	}
	args := append([]string{lb, strconv.Itoa(lbRow.Subnet), strconv.Itoa(lbRow.FrontendPort)}, serverSpecs(rows)...)

	var reloaded bool
	saveIPs := func(ctx context.Context, res RunResult) error {
		entries, didReload := parseRefresh(string(res.Stdout))
		reloaded = didReload
		byName := map[string]refreshEntry{}
		for _, e := range entries {
			byName[e.Name] = e
		}
		var updated []repository.LBServer
		for _, r := range rows {
			if e, ok := byName[r.Name]; ok {
				r.LastIP, r.Enabled = e.IP, e.Enabled
				updated = append(updated, r)
			}
		}
		return s.lbs.UpdateServerIPs(ctx, lb, updated)
	}
	if _, err := s.ops.runLocked(ctx, "refresh_lb", lb, []string{lb}, "haproxy/refresh-lb.sh", args, true, saveIPs); err != nil {
		return "", mapHAProxyError(err)
	}
	if reloaded {
		return fmt.Sprintf("IPs de %s actualizadas y HAProxy recargado", lb), nil
	}
	return fmt.Sprintf("IPs de %s sin cambios; HAProxy no se recargó", lb), nil
}

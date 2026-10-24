package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"stratus-srv/internal/repository"
)

// defaultFrontendPort es el puerto del frontend de un balanceador recién creado (REQ-7).
const defaultFrontendPort = 80

// LBView es un balanceador tal como lo ve la UI: SQLite más el estado de VirtualBox.
type LBView struct {
	Name         string `json:"name"`
	State        string `json:"state"`  // running, poweroff o unknown
	IP           string `json:"ip"`     // IPv4 en puente; vacía si está apagado o no la reporta
	Subnet       string `json:"subnet"` // "10.10.N.0/24"
	FrontendPort int    `json:"frontend_port"`
	ServerCount  int    `json:"server_count"`
}

// ListLBs devuelve los balanceadores registrados con su estado. Igual que ListVMs, si list-vms.sh
// falla el balanceador queda en "unknown" y listErr explica el motivo. Es de solo lectura: no toma
// bloqueos ni se guarda en el historial.
func (s *HAProxyService) ListLBs(ctx context.Context) (lbs []LBView, listErr string, err error) {
	rows, err := s.lbs.ListLBs(ctx)
	if err != nil {
		return nil, "", err
	}
	states, listErr := queryVMStates(ctx, s.runner, s.logger)

	var missing []string
	lbs = make([]LBView, 0, len(rows))
	for _, row := range rows {
		servers, err := s.lbs.Servers(ctx, row.Name)
		if err != nil {
			return nil, "", err
		}
		status, ok := states[row.Name]
		if !ok {
			status.state = "unknown"
			missing = append(missing, row.Name)
		}
		lbs = append(lbs, LBView{
			Name:         row.Name,
			State:        status.state,
			IP:           status.ip,
			Subnet:       fmt.Sprintf("10.10.%d.0/24", row.Subnet),
			FrontendPort: row.FrontendPort,
			ServerCount:  len(servers),
		})
	}
	if listErr == "" && len(missing) > 0 {
		listErr = "Estos balanceadores no aparecen en VirtualBox: " + strings.Join(missing, ", ")
	}
	return lbs, listErr, nil
}

// CreateLB valida la entrada con las reglas de 001, toma el bloqueo de creación (compartido con
// create_vm), registra el nombre y ejecuta create-lb.sh con las subredes ya usadas. Si el script
// falla, el nombre se libera; si termina bien, guarda el balanceador con la subred que imprime
// ("subnet=<N>") y el puerto 80.
func (s *HAProxyService) CreateLB(ctx context.Context, in CreateVMInput) (string, error) {
	if err := ValidateCreateVM(in); err != nil {
		return "", err
	}
	if !s.locks.TryLockCreate() {
		return "", ErrCreateInProgress
	}
	defer s.locks.UnlockCreate()

	if _, err := s.vms.Insert(ctx, in.Name); errors.Is(err, repository.ErrDuplicateVM) {
		return "", fmt.Errorf("crear balanceador %s: %w", in.Name, ErrDuplicateVM)
	} else if err != nil {
		return "", err
	}

	subnets, err := s.lbs.Subnets(ctx)
	if err != nil {
		s.releaseName(ctx, in.Name)
		return "", err
	}
	args := []string{in.Name, strconv.Itoa(in.MemoryMB), strconv.Itoa(in.CPUs), s.cfg.BaseDisk,
		s.cfg.BridgeAdapter, reservedList(subnets)}
	res, err := s.ops.run(ctx, "create_lb", in.Name, "haproxy/create-lb.sh", args, true, nil)
	if err != nil {
		// El script limpia lo que haya creado en VirtualBox; aquí solo se libera el nombre.
		s.releaseName(ctx, in.Name)
		return "", mapHAProxyError(err)
	}

	stdout := string(res.Stdout)
	subnet, ok := parseSubnet(stdout)
	if !ok {
		// El balanceador existe en VirtualBox pero no se sabe su subred: no se borra el registro de la VM.
		return "", fmt.Errorf("crear balanceador %s: create-lb.sh no informó la subred (subnet=<N>)", in.Name)
	}
	lb := repository.LoadBalancer{Name: in.Name, Subnet: subnet, FrontendPort: defaultFrontendPort}
	if err := s.lbs.InsertLB(context.WithoutCancel(ctx), lb); err != nil {
		return "", fmt.Errorf("crear balanceador %s: el script terminó bien pero no se guardó: %w", in.Name, err)
	}
	if msg := firstLine(stdout); msg != "" {
		return msg, nil
	}
	return fmt.Sprintf("Balanceador %s creado y encendido", in.Name), nil
}

// releaseName borra el registro de la VM de un balanceador cuya creación falló.
func (s *HAProxyService) releaseName(ctx context.Context, name string) {
	if err := s.vms.Delete(context.WithoutCancel(ctx), name); err != nil {
		s.logger.Printf("no se pudo liberar el nombre %s: %v", name, err)
	}
}

// DeleteLB elimina el balanceador con delete-lb.sh (VM, disco, red interna y servidor DHCP). Con al
// menos un servidor asignado se rechaza sin llamar al script (REQ-6). Si el script termina bien,
// borra sus filas de load_balancers y de vms.
func (s *HAProxyService) DeleteLB(ctx context.Context, name string) (string, error) {
	if _, err := s.getLB(ctx, name); err != nil {
		return "", err
	}
	servers, err := s.lbs.Servers(ctx, name)
	if err != nil {
		return "", err
	}
	if len(servers) > 0 {
		return "", ErrLBHasServers
	}
	deleteRows := func(ctx context.Context) error {
		if err := s.lbs.DeleteLB(ctx, name); err != nil {
			return err
		}
		return s.vms.Delete(ctx, name)
	}
	res, err := s.ops.run(ctx, "delete_lb", name, "haproxy/delete-lb.sh", []string{name}, true, deleteRows)
	if err != nil {
		return "", mapHAProxyError(err)
	}
	if msg := firstLine(string(res.Stdout)); msg != "" {
		return msg, nil
	}
	return fmt.Sprintf("Balanceador %s eliminado junto con su red interna", name), nil
}

// reservedList arma el argumento "reserved" de create-lb.sh: "-" sin subredes, o "1,3,...".
func reservedList(subnets []int) string {
	if len(subnets) == 0 {
		return "-"
	}
	parts := make([]string, len(subnets))
	for i, n := range subnets {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ",")
}

// parseSubnet lee la línea "subnet=<N>" de la salida de create-lb.sh; N debe estar entre 1 y 254.
func parseSubnet(stdout string) (int, bool) {
	for _, line := range strings.Split(stdout, "\n") {
		value, found := strings.CutPrefix(strings.TrimSpace(line), "subnet=")
		if !found {
			continue
		}
		n, err := strconv.Atoi(value)
		if err == nil && n >= 1 && n <= 254 {
			return n, true
		}
	}
	return 0, false
}

// firstLine devuelve la primera línea no vacía del texto.
func firstLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

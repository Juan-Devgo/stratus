package service

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"

	"stratus-srv/internal/repository"
)

// maxServers es el máximo de servidores de un balanceador (REQ-8).
const maxServers = 10

// targetArgs arma el argv de los scripts de invitado de 001, 002 y 003 (REQ-19): para un servidor
// asignado a un balanceador es ["--jump", <lb>, <vm>, extra...] y para cualquier otra VM, el
// balanceador incluido, [<vm>, extra...]. Devuelve también el balanceador del servidor, o "".
// El rol sale de SQLite: los scripts nunca lo deducen de VirtualBox.
func targetArgs(ctx context.Context, lbs *repository.LBRepository, vm string, extra ...string) (args []string, lb string, err error) {
	server, assigned, err := lbs.ServerByVM(ctx, vm)
	if err != nil {
		return nil, "", err
	}
	if assigned {
		return append([]string{"--jump", server.LB, vm}, extra...), server.LB, nil
	}
	return append([]string{vm}, extra...), "", nil
}

// serverSpecs convierte las filas de lb_servers en el argumento de spec de los scripts de HAProxy:
// "<server_name>,<vm>,<port>,<ip>,<on|off>".
func serverSpecs(rows []repository.LBServer) []string {
	specs := make([]string, 0, len(rows))
	for _, r := range rows {
		state := "off"
		if r.Enabled {
			state = "on"
		}
		specs = append(specs, fmt.Sprintf("%s,%s,%d,%s,%s", r.Name, r.VM, r.Port, r.LastIP, state))
	}
	return specs
}

// nextServerName devuelve el nombre server<N> libre más bajo del balanceador, o ErrLBFull si los
// diez están ocupados (REQ-9).
func nextServerName(rows []repository.LBServer) (string, error) {
	used := map[string]bool{}
	for _, r := range rows {
		used[r.Name] = true
	}
	for n := 1; n <= maxServers; n++ {
		if name := "server" + strconv.Itoa(n); !used[name] {
			return name, nil
		}
	}
	return "", ErrLBFull
}

// refreshEntry es una línea "<server> <ip> <on|off>" de refresh-lb.sh.
type refreshEntry struct {
	Name    string
	IP      string
	Enabled bool
}

// parseRefresh lee la salida de refresh-lb.sh: una línea "<server> <ip> <on|off>" por servidor y
// "reloaded=yes|no". Las líneas con otro formato se ignoran.
func parseRefresh(stdout string) (entries []refreshEntry, reloaded bool) {
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if value, found := strings.CutPrefix(line, "reloaded="); found {
			reloaded = value == "yes"
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 || (fields[2] != "on" && fields[2] != "off") {
			continue
		}
		if !validIPv4(fields[1]) {
			continue
		}
		entries = append(entries, refreshEntry{Name: fields[0], IP: fields[1], Enabled: fields[2] == "on"})
	}
	return entries, reloaded
}

// parseStatus lee la salida de lb-status.sh: una línea "<server> <status>" por servidor. Las líneas
// con otro formato se ignoran.
func parseStatus(stdout string) map[string]string {
	states := map[string]string{}
	for _, line := range strings.Split(stdout, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 {
			states[fields[0]] = fields[1]
		}
	}
	return states
}

// validIPv4 dice si el texto es una dirección IPv4 (no IPv6).
func validIPv4(text string) bool {
	ip := net.ParseIP(text)
	return ip != nil && ip.To4() != nil && !strings.Contains(text, ":")
}

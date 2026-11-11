package service

import (
	"context"
	"fmt"
	"log"
	"net"
	"strings"

	"stratus-srv/internal/repository"
)

// queryVMStates ejecuta list-vms.sh y devuelve el estado y la IP de cada VM de VirtualBox. Si el
// script falla, el mapa queda vacío, listErr explica el motivo (sin detalles internos) y failed es
// la fila de historial de la lectura fallida, sin módulo, operación ni VM, para quien deba
// registrarla (005 REQ-35). Una lectura correcta devuelve failed nil.
func queryVMStates(ctx context.Context, runner ScriptRunner, logger *log.Logger) (
	states map[string]vmStatus, listErr string, failed *repository.Operation) {
	const script = "prepare/list-vms.sh"
	states = map[string]vmStatus{}
	res, runErr := runner.Run(ctx, script)
	switch {
	case runErr != nil:
		listErr = "No se pudo consultar el estado de las VMs en VirtualBox"
		logger.Printf("operación=list_vms error=%q", Redact(runErr.Error()))
		failed = &repository.Operation{Command: script, ExitCode: -1, Stderr: Redact(runErr.Error()), Result: "error"}
	case res.TimedOut:
		listErr = "No se pudo consultar el estado de las VMs en VirtualBox: se superó el tiempo límite"
		logger.Printf("operación=list_vms resultado=timeout")
		failed = &repository.Operation{Command: script, ExitCode: -1, Stderr: strings.TrimSpace(Redact(res.Stderr)),
			Result: "timeout"}
	case res.ExitCode != 0:
		stderr := strings.TrimSpace(Redact(res.Stderr))
		listErr = fmt.Sprintf("No se pudo consultar el estado de las VMs en VirtualBox (código %d): %s",
			res.ExitCode, lastLine(stderr))
		logger.Printf("operación=list_vms código=%d stderr=%q", res.ExitCode, stderr)
		failed = &repository.Operation{Command: script, ExitCode: res.ExitCode, Stderr: stderr, Result: "error"}
	default:
		states = parseVMStates(string(res.Stdout))
	}
	// Si el navegador abandonó la petición, el script murió por eso y no por VirtualBox: la lectura
	// no se registra como fallida (REQ-50). Nadie leerá listErr.
	if ctx.Err() != nil {
		failed = nil
	}
	return states, listErr, failed
}

// vmStatus es el estado y la IP que informa list-vms.sh para una VM.
type vmStatus struct {
	state string
	ip    string
}

// parseVMStates lee las líneas "<nombre> <running|poweroff> <ipv4|->" de list-vms.sh. Los dos
// últimos campos son el estado y la IP; lo anterior es el nombre (VirtualBox puede tener VMs con
// espacios, que no coinciden con ninguna VM de la app). La IP solo se conserva si es una IPv4
// válida. Las líneas con otro formato o estado se ignoran.
func parseVMStates(stdout string) map[string]vmStatus {
	states := map[string]vmStatus{}
	for _, line := range strings.Split(stdout, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		n := len(fields)
		state, rawIP := fields[n-2], fields[n-1]
		if state != "running" && state != "poweroff" {
			continue
		}
		ip := ""
		if parsed := net.ParseIP(rawIP); parsed != nil && parsed.To4() != nil && !strings.Contains(rawIP, ":") {
			ip = rawIP
		}
		states[strings.Join(fields[:n-2], " ")] = vmStatus{state: state, ip: ip}
	}
	return states
}

package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"stratus-srv/internal/repository"
)

// RecordResult es el resultado de agregar el registro de una VM (REQ-23).
type RecordResult struct {
	VM      string `json:"vm"`
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

// refreshWarning es la advertencia de REQ-26 cuando la operación terminó bien pero no su refresco.
func refreshWarning(vm string) string {
	return fmt.Sprintf("Operación completada; no se pudo actualizar el registro DNS de %s, use Actualizar registros", vm)
}

// serverRecordNotice es el aviso de REQ-41 cuando asignar una VM a un balanceador le quitó el registro.
func serverRecordNotice(vm string) string {
	return fmt.Sprintf("Registro DNS de %s quitado: los servidores de un balanceador no se registran", vm)
}

// noRecordNotice es el aviso de REQ-42 tras quitar una VM de un balanceador: no se le agrega registro.
func noRecordNotice(vm string) string {
	return fmt.Sprintf("%s no tiene registro DNS; agréguelo desde DNS", vm)
}

// dropRecordWarning es la advertencia de REQ-41 cuando la operación terminó bien pero la zona no
// pudo quitar el registro de la VM.
func dropRecordWarning(vm string) string {
	return fmt.Sprintf("Operación completada; no se pudo quitar el registro DNS de %s, use Actualizar registros", vm)
}

// joinWarnings une dos advertencias con "\n" en el único campo warning de la respuesta.
func joinWarnings(a, b string) string {
	if a == "" {
		return b
	}
	return a + "\n" + b
}

// deleteRecordWarning es la advertencia de REQ-24 cuando la VM se eliminó pero su registro no.
const deleteRecordWarning = "VM eliminada; no se pudo quitar su registro DNS, use Actualizar registros"

// AddRecords agrega los registros A de las VMs elegidas (REQ-23). Cada VM se acepta solo si está
// registrada, no es el servidor DNS ni servidor de un balanceador, no tiene registro y list-vms.sh
// informa una IPv4 dentro de la LAN (REQ-22); las aceptadas van en un solo add-records.sh con el
// conjunto completo y, si termina bien, se guardan. El resultado trae cada VM. Devuelve error solo si
// no se agregó ninguna: el de la primera VM rechazada o el del script.
func (s *DNSService) AddRecords(ctx context.Context, vms []string) (string, []RecordResult, error) {
	dns, err := s.recordsServer(ctx)
	if err != nil {
		return "", nil, err
	}
	if len(vms) == 0 {
		return "", nil, &ValidationError{Message: "Seleccione al menos una VM"}
	}
	states, _, _ := queryVMStates(ctx, s.runner, s.logger)
	_, serverLB, err := lbTopology(ctx, s.lbs)
	if err != nil {
		return "", nil, err
	}

	results := make([]RecordResult, 0, len(vms))
	var accepted []repository.DNSRecord
	var firstErr error
	for _, vm := range vms {
		if slices.ContainsFunc(results, func(r RecordResult) bool { return r.VM == vm }) {
			continue
		}
		ip, err := s.recordCandidate(ctx, dns, vm, states, serverLB)
		if err != nil {
			results = append(results, RecordResult{VM: vm, Message: userMessage(err)})
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		accepted = append(accepted, repository.DNSRecord{VM: vm, IP: ip})
		results = append(results, RecordResult{VM: vm, OK: true, Message: "Registro agregado"})
	}
	if len(accepted) == 0 {
		return "", results, firstErr
	}

	if err := s.applyAdd(ctx, dns, accepted); err != nil {
		return "", nil, err
	}
	names := make([]string, len(accepted))
	for i, rec := range accepted {
		names[i] = rec.VM
	}
	return "Registros agregados: " + strings.Join(names, ", "), results, nil
}

// recordCandidate devuelve la IPv4 con que se registraría la VM, o el motivo por el que no. El orden
// de las comprobaciones es el de REQ-23: registrada, no es el servidor DNS, no es servidor de un
// balanceador, sin registro, con IPv4 y con esa IPv4 en la LAN.
func (s *DNSService) recordCandidate(ctx context.Context, dns, vm string, states map[string]vmStatus,
	serverLB map[string]string) (string, error) {
	if err := ValidateVMName(vm); err != nil {
		return "", err
	}
	if _, err := s.vms.Get(ctx, vm); errors.Is(err, repository.ErrVMNotFound) {
		return "", fmt.Errorf("buscar vm %s: %w", vm, ErrVMNotFound)
	} else if err != nil {
		return "", err
	}
	if vm == dns {
		return "", &ValidationError{Message: "El servidor DNS ya tiene su registro"}
	}
	if lb, assigned := serverLB[vm]; assigned {
		return "", &ServerRecordError{LB: lb}
	}
	if _, has, err := s.dnsRepo.Record(ctx, vm); err != nil {
		return "", err
	} else if has {
		return "", ErrRecordExists
	}
	ip := states[vm].ip
	if ip == "" {
		return "", ErrVMNoIP
	}
	if !s.cfg.OnLAN(ip) {
		return "", ErrVMNotOnLAN
	}
	return ip, nil
}

// zoneRecords devuelve los registros guardados que van en la zona: los que cumplen la regla de la
// LAN (REQ-22). Una fila guardada antes de la enmienda con una dirección interna sigue en SQLite
// pero fuera de la zona hasta que "Actualizar registros" la quite.
func (s *DNSService) zoneRecords(ctx context.Context) ([]repository.DNSRecord, error) {
	current, err := s.dnsRepo.Records(ctx)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(current, func(r repository.DNSRecord) bool { return !s.cfg.OnLAN(r.IP) }), nil
}

// applyAdd ejecuta add-records.sh con los registros guardados más los nuevos, con el bloqueo del
// servidor DNS, y guarda los nuevos si termina bien.
func (s *DNSService) applyAdd(ctx context.Context, dns string, added []repository.DNSRecord) error {
	current, err := s.zoneRecords(ctx)
	if err != nil {
		return err
	}
	all := append(slices.Clone(current), added...)
	slices.SortFunc(all, func(a, b repository.DNSRecord) int { return strings.Compare(a.VM, b.VM) })
	args := append([]string{dns, s.cfg.Domain, s.cfg.IP}, recordSpecs(all)...)
	save := func(ctx context.Context) error { return s.dnsRepo.InsertRecords(ctx, added) }
	_, err = s.ops.run(ctx, "add_records", dns, "dns/add-records.sh", args, true, save)
	return mapDNSError(err)
}

// RemoveRecord quita el registro de la VM con remove-record.sh y el conjunto restante, con el
// bloqueo del servidor DNS, y borra la fila si termina bien (REQ-24). Una VM sin registro termina
// bien sin llamar al script.
func (s *DNSService) RemoveRecord(ctx context.Context, vm string) (string, error) {
	dns, err := s.recordsServer(ctx)
	if err != nil {
		return "", err
	}
	if err := ValidateVMName(vm); err != nil {
		return "", err
	}
	if _, has, err := s.dnsRepo.Record(ctx, vm); err != nil {
		return "", err
	} else if !has {
		return fmt.Sprintf("La VM %s no tiene registro DNS", vm), nil
	}
	deleteRow := func(ctx context.Context) error { return s.dnsRepo.DeleteRecord(ctx, vm) }
	if err := s.applyRemove(ctx, dns, vm, deleteRow); err != nil {
		return "", err
	}
	return fmt.Sprintf("Registro de %s quitado", vm), nil
}

// applyRemove ejecuta remove-record.sh con los registros guardados menos el de vm.
func (s *DNSService) applyRemove(ctx context.Context, dns, vm string, onSuccess func(context.Context) error) error {
	current, err := s.zoneRecords(ctx)
	if err != nil {
		return err
	}
	remaining := slices.DeleteFunc(current, func(r repository.DNSRecord) bool { return r.VM == vm })
	args := append([]string{dns, s.cfg.Domain, s.cfg.IP, vm}, recordSpecs(remaining)...)
	_, err = s.ops.run(ctx, "remove_record", dns, "dns/remove-record.sh", args, true, onSuccess)
	return mapDNSError(err)
}

// Refresh es el botón "Actualizar registros" (REQ-27): espera a todas las VMs con registro y
// guarda las IPs que imprime refresh-records.sh. Si la zona no cambió informa "Registros sin cambios".
func (s *DNSService) Refresh(ctx context.Context) (string, error) {
	dns, err := s.recordsServer(ctx)
	if err != nil {
		return "", err
	}
	reloaded, removed, err := s.refresh(ctx, dns, "")
	if err != nil {
		return "", err
	}
	msg := "Registros sin cambios"
	if reloaded {
		msg = "Registros actualizados"
	}
	if len(removed) > 0 {
		if !reloaded {
			msg = "Registros actualizados"
		}
		msg += "; registros quitados: " + strings.Join(removed, ", ")
	}
	return msg, nil
}

// RefreshVM es el hook de iniciar (001), asignar y quitar (004) una VM (REQ-26): sin servidor DNS o
// sin registro de la VM no hace nada; si no, espera solo a esa VM. Cualquier error lo convierte el
// llamador en advertencia; un rechazo antes del script queda como fila fallida.
func (s *DNSService) RefreshVM(ctx context.Context, vm string) error {
	dns, ok, err := s.dnsRepo.Server(ctx)
	if err != nil || !ok || vm == dns {
		return err
	}
	if _, has, err := s.dnsRepo.Record(ctx, vm); err != nil || !has {
		return err
	}
	return s.refreshHook(ctx, dns, vm)
}

// RefreshAll es el hook de iniciar el servidor DNS (REQ-27): espera a todas las VMs con registro.
func (s *DNSService) RefreshAll(ctx context.Context) error {
	dns, ok, err := s.dnsRepo.Server(ctx)
	if err != nil || !ok {
		return err
	}
	return s.refreshHook(ctx, dns, "")
}

// refreshHook ejecuta el refresco de un hook y guarda como fila fallida un rechazo previo al script.
func (s *DNSService) refreshHook(ctx context.Context, dns, wait string) error {
	err := ValidateDNSConfig(s.cfg)
	if err == nil {
		_, _, err = s.refresh(ctx, dns, wait)
	}
	if errors.Is(err, ErrOperationInProgress) || errors.Is(err, ErrDNSConfigIncomplete) {
		args := []string{"-"}
		if wait != "" {
			args = []string{wait}
		}
		s.recordRejected(ctx, "refresh_records", dns, "dns/refresh-records.sh", args, err)
	}
	return err
}

// refresh ejecuta refresh-records.sh con el bloqueo del servidor DNS y todos los registros
// guardados. wait es la VM que se espera; vacío espera a todas las VMs con registro. Los registros
// de servidores asignados a un balanceador van como drop_vms y el script los quita junto con los
// que no están en la LAN (REQ-62). Si termina bien guarda las IPs impresas, borra los registros
// quitados y devuelve si la zona se recargó y cuáles VMs se quitaron.
func (s *DNSService) refresh(ctx context.Context, dns, wait string) (reloaded bool, removed []string, err error) {
	recs, err := s.dnsRepo.Records(ctx)
	if err != nil {
		return false, nil, err
	}
	_, serverLB, err := lbTopology(ctx, s.lbs)
	if err != nil {
		return false, nil, err
	}
	names := make([]string, len(recs))
	var drop []string
	for i, rec := range recs {
		names[i] = rec.VM
		if _, assigned := serverLB[rec.VM]; assigned {
			drop = append(drop, rec.VM)
		}
	}
	if wait == "" {
		wait = strings.Join(names, ",")
	}
	if wait == "" {
		wait = "-"
	}
	dropArg := "-"
	if len(drop) > 0 {
		dropArg = strings.Join(drop, ",")
	}
	args := append([]string{dns, s.cfg.Domain, s.cfg.IP, wait, dropArg}, recordSpecs(recs)...)
	save := func(ctx context.Context, res RunResult) error {
		var printed []repository.DNSRecord
		printed, removed, reloaded = parseRecordRefresh(res.Stdout)
		if err := s.dnsRepo.UpdateRecordIPs(ctx, printed); err != nil {
			return err
		}
		return s.dnsRepo.DeleteRecords(ctx, removed)
	}
	_, err = s.ops.runLocked(ctx, "refresh_records", dns, []string{dns}, "dns/refresh-records.sh", args, true, save)
	return reloaded, removed, mapDNSError(err)
}

// parseRecordRefresh lee la salida de refresh-records.sh: "<vm> <ip>" por registro conservado,
// "removed=<vm>" por registro quitado y la línea "reloaded=yes|no".
func parseRecordRefresh(stdout []byte) (recs []repository.DNSRecord, removed []string, reloaded bool) {
	for line := range strings.Lines(string(stdout)) {
		line = strings.TrimSpace(line)
		if value, ok := strings.CutPrefix(line, "reloaded="); ok {
			reloaded = value == "yes"
			continue
		}
		if vm, ok := strings.CutPrefix(line, "removed="); ok {
			if vm != "" {
				removed = append(removed, vm)
			}
			continue
		}
		if vm, ip, ok := strings.Cut(line, " "); ok && vm != "" && ip != "" {
			recs = append(recs, repository.DNSRecord{VM: vm, IP: ip})
		}
	}
	return recs, removed, reloaded
}

// recordsServer devuelve el servidor DNS para una operación de registros: sin servidor es
// ErrNoDNSServer y con la configuración APP_DNS_* inválida, ErrDNSConfigIncomplete.
func (s *DNSService) recordsServer(ctx context.Context) (string, error) {
	dns, err := s.server(ctx)
	if err != nil {
		return "", err
	}
	if err := ValidateDNSConfig(s.cfg); err != nil {
		return "", err
	}
	return dns, nil
}

// AddRecordsFor es el hook de crear un servidor web (REQ-5): sin servidor DNS no hace nada; si no,
// agrega el registro de la VM nueva como su propia operación add_records, siempre que su IPv4 esté
// en la LAN (REQ-22); si no, es ErrVMNotOnLAN y queda una fila rechazada. Cualquier error lo
// convierte el llamador en advertencia; un rechazo antes del script queda como fila fallida.
func (s *DNSService) AddRecordsFor(ctx context.Context, vm, ip string) error {
	dns, ok, err := s.dnsRepo.Server(ctx)
	if err != nil || !ok {
		return err
	}
	if err := ValidateDNSConfig(s.cfg); err != nil {
		s.recordRejected(ctx, "add_records", dns, "dns/add-records.sh", []string{vm + "," + ip}, err)
		return err
	}
	if !s.cfg.OnLAN(ip) {
		s.recordRejected(ctx, "add_records", dns, "dns/add-records.sh", []string{vm + "," + ip}, ErrVMNotOnLAN)
		return ErrVMNotOnLAN
	}
	err = s.applyAdd(ctx, dns, []repository.DNSRecord{{VM: vm, IP: ip}})
	if errors.Is(err, ErrOperationInProgress) {
		s.recordRejected(ctx, "add_records", dns, "dns/add-records.sh", []string{vm + "," + ip}, err)
	}
	return err
}

// AfterVMDeleted es el hook de eliminar una VM desde 001, web o HAProxy (REQ-24): si tenía
// registro, borra la fila y aplica el conjunto restante con remove-record.sh.
// Si eso falla devuelve la advertencia de REQ-24; el próximo refresco renderiza desde SQLite y quita
// la línea vieja.
func (s *DNSService) AfterVMDeleted(ctx context.Context, vm string) string {
	dns, ok, err := s.dnsRepo.Server(ctx)
	if err != nil {
		s.logger.Printf("no se pudo leer el servidor DNS: %v", err)
		return deleteRecordWarning
	}
	if !ok || vm == dns {
		return ""
	}
	if _, has, err := s.dnsRepo.Record(ctx, vm); err != nil {
		s.logger.Printf("no se pudo leer el registro DNS de %s: %v", vm, err)
		return deleteRecordWarning
	} else if !has {
		return ""
	}
	if _, err := s.dropRecordRowFirst(ctx, dns, vm); err != nil {
		s.logger.Printf("no se pudo quitar el registro DNS de %s: %v", vm, err)
		return deleteRecordWarning
	}
	return ""
}

// DropServerRecord es el hook de asignar y quitar un servidor (REQ-41, REQ-42): sin servidor DNS o
// sin registro de la VM no hace nada; si no, borra la fila y aplica el conjunto restante como su
// propia operación remove_record. removed indica si la fila se borró, aunque la zona no cambie y
// err no sea nil; el próximo refresco la corrige.
func (s *DNSService) DropServerRecord(ctx context.Context, vm string) (removed bool, err error) {
	dns, ok, err := s.dnsRepo.Server(ctx)
	if err != nil || !ok || vm == dns {
		return false, err
	}
	if _, has, err := s.dnsRepo.Record(ctx, vm); err != nil || !has {
		return false, err
	}
	return s.dropRecordRowFirst(ctx, dns, vm)
}

// dropRecordRowFirst borra primero la fila del registro y después aplica el conjunto restante con
// remove-record.sh (REQ-41): un fallo de la zona nunca deja guardado el registro de una VM que no
// debe tenerlo. Un rechazo antes del script (bloqueo o configuración) queda como fila fallida.
func (s *DNSService) dropRecordRowFirst(ctx context.Context, dns, vm string) (removed bool, err error) {
	if err := s.dnsRepo.DeleteRecord(ctx, vm); err != nil {
		return false, err
	}
	err = ValidateDNSConfig(s.cfg)
	if err == nil {
		err = s.applyRemove(ctx, dns, vm, nil)
	}
	if errors.Is(err, ErrOperationInProgress) || errors.Is(err, ErrDNSConfigIncomplete) {
		s.recordRejected(ctx, "remove_record", dns, "dns/remove-record.sh", []string{vm}, err)
	}
	return true, err
}

// recordRejected guarda como fila fallida del historial de dns una actualización de los hooks que
// no llegó a ejecutar su script (servidor DNS bloqueado o configuración inválida): REQ-24 y REQ-26
// piden su fila aunque se rechace.
func (s *DNSService) recordRejected(ctx context.Context, operation, dns, script string, args []string, err error) {
	row := repository.Operation{Module: dnsModule, Operation: operation, VM: dns, Command: script, Args: args,
		ExitCode: -1, Stderr: userMessage(err), Result: "error"}
	s.logger.Printf("operación=%s vm=%s resultado=rechazada motivo=%q", operation, dns, row.Stderr)
	if insertErr := s.history.Insert(context.WithoutCancel(ctx), row); insertErr != nil {
		s.logger.Printf("no se pudo guardar el historial de %s en %s: %v", operation, dns, insertErr)
	}
}

// userMessage es el texto para el usuario de un error del servicio: el de la validación, el del
// error centinela o, si no, el del error completo.
func userMessage(err error) string {
	var validation *ValidationError
	if errors.As(err, &validation) {
		return validation.Message
	}
	var serverRecord *ServerRecordError
	if errors.As(err, &serverRecord) {
		return serverRecord.Error()
	}
	for _, sentinel := range []error{ErrVMNotFound, ErrRecordExists, ErrVMNoIP, ErrOperationInProgress,
		ErrDNSConfigIncomplete, ErrDNSOff, ErrZoneInvalid, ErrVMNotOnLAN} {
		if errors.Is(err, sentinel) {
			return sentinel.Error()
		}
	}
	return err.Error()
}

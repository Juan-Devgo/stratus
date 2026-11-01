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

// deleteRecordWarning es la advertencia de REQ-24 cuando la VM se eliminó pero su registro no.
const deleteRecordWarning = "VM eliminada; no se pudo quitar su registro DNS, use Actualizar registros"

// AddRecords agrega los registros A de las VMs elegidas (REQ-23). Cada VM se acepta solo si está
// registrada, no es el servidor DNS, no tiene registro y list-vms.sh informa su IPv4 (la interna
// 10.10.N.x de un servidor asignado, REQ-22); las aceptadas van en un solo add-records.sh con el
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

	results := make([]RecordResult, 0, len(vms))
	var accepted []repository.DNSRecord
	var firstErr error
	for _, vm := range vms {
		if slices.ContainsFunc(results, func(r RecordResult) bool { return r.VM == vm }) {
			continue
		}
		ip, err := s.recordCandidate(ctx, dns, vm, states)
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

// recordCandidate devuelve la IPv4 con que se registraría la VM, o el motivo por el que no.
func (s *DNSService) recordCandidate(ctx context.Context, dns, vm string, states map[string]vmStatus) (string, error) {
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
	if _, has, err := s.dnsRepo.Record(ctx, vm); err != nil {
		return "", err
	} else if has {
		return "", ErrRecordExists
	}
	ip := states[vm].ip
	if ip == "" {
		return "", ErrVMNoIP
	}
	return ip, nil
}

// applyAdd ejecuta add-records.sh con los registros guardados más los nuevos, con el bloqueo del
// servidor DNS, y guarda los nuevos si termina bien.
func (s *DNSService) applyAdd(ctx context.Context, dns string, added []repository.DNSRecord) error {
	current, err := s.dnsRepo.Records(ctx)
	if err != nil {
		return err
	}
	all := append(slices.Clone(current), added...)
	slices.SortFunc(all, func(a, b repository.DNSRecord) int { return strings.Compare(a.VM, b.VM) })
	args := append([]string{dns, s.cfg.Domain, s.cfg.Address()}, recordSpecs(all)...)
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
	current, err := s.dnsRepo.Records(ctx)
	if err != nil {
		return err
	}
	remaining := slices.DeleteFunc(current, func(r repository.DNSRecord) bool { return r.VM == vm })
	args := append([]string{dns, s.cfg.Domain, s.cfg.Address(), vm}, recordSpecs(remaining)...)
	_, err = s.ops.run(ctx, "remove_record", dns, "dns/remove-record.sh", args, true, onSuccess)
	return mapDNSError(err)
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
// agrega el registro de la VM nueva como su propia operación add_records. Cualquier error lo
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
	err = s.applyAdd(ctx, dns, []repository.DNSRecord{{VM: vm, IP: ip}})
	if errors.Is(err, ErrOperationInProgress) {
		s.recordRejected(ctx, "add_records", dns, "dns/add-records.sh", []string{vm + "," + ip}, err)
	}
	return err
}

// AfterVMDeleted es el hook de eliminar una VM desde 001, web o HAProxy (REQ-24, REQ-33): borra su
// resolvedor y, si tenía registro, borra la fila y aplica el conjunto restante con remove-record.sh.
// Si eso falla devuelve la advertencia de REQ-24; el próximo refresco renderiza desde SQLite y quita
// la línea vieja.
func (s *DNSService) AfterVMDeleted(ctx context.Context, vm string) string {
	if err := s.dnsRepo.SetResolver(ctx, vm, false); err != nil {
		s.logger.Printf("no se pudo borrar el resolvedor de %s: %v", vm, err)
	}
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
	if err := s.dnsRepo.DeleteRecord(ctx, vm); err != nil {
		s.logger.Printf("no se pudo borrar el registro DNS de %s: %v", vm, err)
		return deleteRecordWarning
	}
	err = ValidateDNSConfig(s.cfg)
	if err == nil {
		err = s.applyRemove(ctx, dns, vm, nil)
	}
	if err != nil {
		if errors.Is(err, ErrOperationInProgress) || errors.Is(err, ErrDNSConfigIncomplete) {
			s.recordRejected(ctx, "remove_record", dns, "dns/remove-record.sh", []string{vm}, err)
		}
		return deleteRecordWarning
	}
	return ""
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
	for _, sentinel := range []error{ErrVMNotFound, ErrRecordExists, ErrVMNoIP, ErrOperationInProgress,
		ErrDNSConfigIncomplete, ErrDNSOff, ErrZoneInvalid} {
		if errors.Is(err, sentinel) {
			return sentinel.Error()
		}
	}
	return err.Error()
}

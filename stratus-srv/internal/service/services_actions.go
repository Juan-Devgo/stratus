package service

import (
	"context"
	"slices"
)

// serviceActions son las acciones de systemctl que tiene cada fila (REQ-17); cada una tiene su script.
var serviceActions = []string{"start", "stop", "restart", "enable", "disable"}

// ServiceAction ejecuta start, stop, restart, enable o disable sobre un servicio creado por
// Stratus con su propio script (REQ-17, REQ-26). Start y restart devuelven además el estado leído
// unos segundos después (REQ-15). Las cinco acciones se registran siempre en el historial (REQ-27).
func (s *ServicesService) ServiceAction(ctx context.Context, vm, name, action string) (ServiceResult, error) {
	if !slices.Contains(serviceActions, action) {
		return ServiceResult{}, &ValidationError{Message: "Acción no válida: debe ser start, stop, restart, enable o disable"}
	}
	if err := ValidateServiceName(name); err != nil {
		return ServiceResult{}, err
	}
	v, err := s.getVM(ctx, vm)
	if err != nil {
		return ServiceResult{}, err
	}
	args, _, err := targetArgs(ctx, s.lbs, v.Name, name)
	if err != nil {
		return ServiceResult{}, err
	}
	res, err := s.ops.run(ctx, action+"_service", v.Name, "services/"+action+"-service.sh", args, true, nil)
	if err != nil {
		return ServiceResult{}, mapServiceError(action, name, err)
	}
	return parseResult(res.Stdout), nil
}

// ServiceStatus devuelve la salida de "systemctl status" de un servicio creado por Stratus
// (REQ-19). Un servicio inactivo o fallido se muestra, no es un fallo. Una lectura correcta no
// queda en el historial; solo los fallos (REQ-27).
func (s *ServicesService) ServiceStatus(ctx context.Context, vm, name string) (string, error) {
	return s.readService(ctx, vm, name, "service_status", "services/status-service.sh")
}

// ServiceLogs devuelve las últimas 100 líneas del diario del servicio (REQ-20), una sola vez y sin
// seguimiento en vivo. Se registra igual que ServiceStatus.
func (s *ServicesService) ServiceLogs(ctx context.Context, vm, name string) (string, error) {
	return s.readService(ctx, vm, name, "service_logs", "services/logs-service.sh")
}

// readService es lo que comparten estado y logs: validar, ejecutar el script de lectura y devolver
// su stdout tal cual.
func (s *ServicesService) readService(ctx context.Context, vm, name, operation, script string) (string, error) {
	if err := ValidateServiceName(name); err != nil {
		return "", err
	}
	v, err := s.getVM(ctx, vm)
	if err != nil {
		return "", err
	}
	args, _, err := targetArgs(ctx, s.lbs, v.Name, name)
	if err != nil {
		return "", err
	}
	res, err := s.ops.run(ctx, operation, v.Name, script, args, false, nil)
	if err != nil {
		return "", mapServiceError(operation, name, err)
	}
	return string(res.Stdout), nil
}

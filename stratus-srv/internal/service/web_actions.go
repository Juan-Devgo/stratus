package service

import (
	"context"
	"slices"
)

// unitActions son las acciones de systemctl de los botones Iniciar, Detener y Reiniciar (REQ-12);
// cada una tiene su script en scripts/web y en scripts/dns.
var unitActions = []string{"start", "stop", "restart"}

// webUnit es la unidad systemd de cada tipo de servidor web.
func webUnit(typ string) string {
	if typ == WebTypeApache {
		return "apache2"
	}
	return "nginx"
}

// WebAction inicia, detiene o reinicia el servicio web con la unidad de su tipo (REQ-12). Iniciar y
// reiniciar devuelven además el estado leído unos segundos después (AC-21). Las tres acciones se
// registran siempre en el historial (REQ-35).
func (s *WebService) WebAction(ctx context.Context, vm, action string) (ServiceResult, error) {
	if !slices.Contains(unitActions, action) {
		return ServiceResult{}, &ValidationError{Message: "Acción no válida: debe ser start, stop o restart"}
	}
	typ, err := s.getWeb(ctx, vm)
	if err != nil {
		return ServiceResult{}, err
	}
	args, _, err := targetArgs(ctx, s.lbs, vm, webUnit(typ))
	if err != nil {
		return ServiceResult{}, err
	}
	res, err := s.ops.run(ctx, action+"_web", vm, "web/"+action+"-web.sh", args, true, nil)
	if err != nil {
		return ServiceResult{}, err
	}
	return parseResult(res.Stdout), nil
}

// WebStatus devuelve la salida de "systemctl status" del servicio web (REQ-12). Un servicio
// inactivo o fallido se muestra, no es un fallo; una lectura correcta no queda en el historial.
func (s *WebService) WebStatus(ctx context.Context, vm string) (string, error) {
	typ, err := s.getWeb(ctx, vm)
	if err != nil {
		return "", err
	}
	args, _, err := targetArgs(ctx, s.lbs, vm, webUnit(typ))
	if err != nil {
		return "", err
	}
	res, err := s.ops.run(ctx, "status_web", vm, "web/status-web.sh", args, false, nil)
	if err != nil {
		return "", err
	}
	return string(res.Stdout), nil
}

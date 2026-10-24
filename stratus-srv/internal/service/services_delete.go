package service

import "context"

// DeleteService elimina un servicio creado por Stratus y todo lo que instaló con delete-service.sh
// (REQ-25) y devuelve el mensaje del script. Si un paso falla, el *OperationError lleva el detalle
// "No se pudo eliminar el servicio <name>: falló el paso <n>" y el servicio sigue listado, así que
// se puede repetir. Siempre se registra en el historial.
func (s *ServicesService) DeleteService(ctx context.Context, vm, name string) (string, error) {
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
	res, err := s.ops.run(ctx, "delete_service", v.Name, "services/delete-service.sh", args, true, nil)
	if err != nil {
		return "", mapServiceError("delete", name, err)
	}
	return parseResult(res.Stdout).Message, nil
}

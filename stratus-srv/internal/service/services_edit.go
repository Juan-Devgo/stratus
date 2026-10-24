package service

import (
	"context"
	"os"
	"strconv"
)

// EditService cambia la unidad de un servicio creado por Stratus y, si in.ReplaceEnv, su archivo de
// variables, con edit-service.sh (REQ-22, REQ-23). Sin ReplaceEnv el último argumento es "-" y
// /etc/<name>.env no cambia; con ReplaceEnv es la ruta de un archivo secreto temporal (0600) con
// todas las variables, que se borra al terminar, con éxito o con fallo (REQ-13).
func (s *ServicesService) EditService(ctx context.Context, vm, name string, in EditServiceInput) (ServiceResult, error) {
	if err := ValidateServiceName(name); err != nil {
		return ServiceResult{}, err
	}
	if err := ValidateEditService(in); err != nil {
		return ServiceResult{}, err
	}
	v, err := s.getVM(ctx, vm)
	if err != nil {
		return ServiceResult{}, err
	}

	envArg := "-"
	if in.ReplaceEnv {
		envArg, err = writeSecretFile("stratus-env-*", envFileContent(in.Env))
		if err != nil {
			return ServiceResult{}, err
		}
		defer os.Remove(envArg)
	}

	args, _, err := targetArgs(ctx, s.lbs, v.Name, name, in.Description, in.Executable, in.Args, in.Restart, strconv.Itoa(in.RestartSec), envArg)
	if err != nil {
		return ServiceResult{}, err
	}
	res, err := s.ops.run(ctx, "edit_service", v.Name, "services/edit-service.sh", args, true, nil)
	if err != nil {
		return ServiceResult{}, mapServiceError("edit", name, err)
	}
	return parseResult(res.Stdout), nil
}

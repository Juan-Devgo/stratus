package service

import (
	"context"
	"os"
	"strconv"
)

// CreateService despliega el archivo como servicio systemd en la VM con create-service.sh
// (REQ-1, REQ-10). El servicio valida la entrada y la VM antes de guardar nada; después guarda el
// archivo y las variables en archivos temporales del host cuyas rutas van como argumentos (REQ-12,
// REQ-13) y los borra al terminar, con éxito o con fallo. Las variables (secretos) nunca van en
// argv ni en el historial.
func (s *ServicesService) CreateService(ctx context.Context, vm string, in CreateServiceInput, a ArchiveUpload) (ServiceResult, error) {
	if err := ValidateCreateService(in); err != nil {
		return ServiceResult{}, err
	}
	v, err := s.getVM(ctx, vm)
	if err != nil {
		return ServiceResult{}, err
	}

	archivePath, err := saveArchive(a, s.cfg.MaxArchiveBytes)
	if err != nil {
		return ServiceResult{}, err
	}
	defer os.Remove(archivePath)

	envPath, err := writeSecretFile("stratus-env-*", envFileContent(in.Env))
	if err != nil {
		return ServiceResult{}, err
	}
	defer os.Remove(envPath)

	// Cada valor es un elemento separado de argv (P3); args es uno solo, con sus tokens unidos.
	args, _, err := targetArgs(ctx, s.lbs, v.Name, in.Name, in.Description, archivePath, in.Executable, in.Args,
		in.Restart, strconv.Itoa(in.RestartSec), envPath)
	if err != nil {
		return ServiceResult{}, err
	}
	res, err := s.ops.run(ctx, "create_service", v.Name, "services/create-service.sh", args, true, nil)
	if err != nil {
		return ServiceResult{}, mapServiceError("create", in.Name, err)
	}
	return parseResult(res.Stdout), nil
}

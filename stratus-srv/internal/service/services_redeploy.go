package service

import (
	"context"
	"os"
)

// RedeployService reemplaza el contenido de /opt/<name> de un servicio creado por Stratus con un
// archivo .tar.gz nuevo, con redeploy-service.sh (REQ-24). El servicio valida el nombre, el
// ejecutable y la VM antes de guardar nada; después guarda el archivo en un archivo temporal del
// host cuya ruta va como argumento y lo borra al terminar, con éxito o con fallo (REQ-12).
func (s *ServicesService) RedeployService(ctx context.Context, vm, name, executable string, a ArchiveUpload) (ServiceResult, error) {
	if err := ValidateServiceName(name); err != nil {
		return ServiceResult{}, err
	}
	if err := ValidateExecutable(executable); err != nil {
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

	args, _, err := targetArgs(ctx, s.lbs, v.Name, name, archivePath, executable)
	if err != nil {
		return ServiceResult{}, err
	}
	res, err := s.ops.run(ctx, "redeploy_service", v.Name, "services/redeploy-service.sh", args, true, nil)
	if err != nil {
		return ServiceResult{}, mapServiceError("redeploy", name, err)
	}
	return parseResult(res.Stdout), nil
}

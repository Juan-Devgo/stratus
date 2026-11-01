package service

import (
	"context"
	"os"
	"strings"
)

// InstallPage guarda la página subida en un archivo temporal del host e instala su contenido en
// /var/www/html con install-page.sh (REQ-9, REQ-10). En un servidor asignado a un balanceador el
// script entra por él (REQ-11). El archivo temporal se borra al terminar, con éxito o con fallo.
func (s *WebService) InstallPage(ctx context.Context, vm string, a ArchiveUpload) (string, error) {
	typ, err := s.getWeb(ctx, vm)
	if err != nil {
		return "", err
	}
	archivePath, err := saveArchive(a, s.cfg.MaxArchiveBytes)
	if err != nil {
		return "", err
	}
	defer os.Remove(archivePath)

	args, _, err := targetArgs(ctx, s.lbs, vm, archivePath)
	if err != nil {
		return "", err
	}
	res, err := s.ops.run(ctx, "install_page", vm, "web/install-page.sh", args, true, nil)
	if err != nil {
		return "", mapWebError(typ, err)
	}
	if msg := strings.TrimSpace(string(res.Stdout)); msg != "" {
		return msg, nil
	}
	return "Página instalada en " + vm, nil
}

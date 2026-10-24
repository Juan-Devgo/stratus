package service

import (
	"context"
	"fmt"
)

// sshServerPackage es el paquete que el backend necesita en toda VM para conectarse por SSH.
const sshServerPackage = "openssh-server"

// haproxyPackage es el paquete que un balanceador necesita para funcionar (REQ-5).
const haproxyPackage = "haproxy"

// InstallPackage instala un paquete con install-package.sh. Instalar openssh-server es una
// instalación normal: la VM ya tiene el acceso con llave desde su creación (REQ-24).
func (s *PrepareService) InstallPackage(ctx context.Context, name, pkg string) (string, error) {
	if err := s.checkPackageVM(ctx, name, pkg); err != nil {
		return "", err
	}
	args, err := s.packageArgs(ctx, name, pkg)
	if err != nil {
		return "", err
	}
	if _, err := s.runOperation(ctx, "install_package", name, "prepare/install-package.sh", args, nil); err != nil {
		return "", err
	}
	return fmt.Sprintf("Paquete %s instalado en %s", pkg, name), nil
}

// RemovePackage elimina un paquete con remove-package.sh. Quitar openssh-server se rechaza
// siempre, en cualquier VM, porque el backend perdería el acceso (REQ-25).
func (s *PrepareService) RemovePackage(ctx context.Context, name, pkg string) (string, error) {
	if err := s.checkPackageVM(ctx, name, pkg); err != nil {
		return "", err
	}
	if pkg == sshServerPackage {
		return "", ErrSSHRemovalBlocked
	}
	// El balanceador usa haproxy (REQ-5): quitarlo se rechaza aquí y también en el script.
	frontend, err := s.lbFrontendArgs(ctx, name)
	if err != nil {
		return "", err
	}
	if frontend != nil && pkg == haproxyPackage {
		return "", ErrHAProxyRemovalBlocked
	}
	args, err := s.packageArgs(ctx, name, pkg)
	if err != nil {
		return "", err
	}
	if _, err := s.runOperation(ctx, "remove_package", name, "prepare/remove-package.sh", args, nil); err != nil {
		return "", err
	}
	return fmt.Sprintf("Paquete %s eliminado de %s", pkg, name), nil
}

// checkPackageVM valida el paquete y que la VM esté registrada.
func (s *PrepareService) checkPackageVM(ctx context.Context, name, pkg string) error {
	if err := ValidatePackage(pkg); err != nil {
		return err
	}
	_, err := s.getVM(ctx, name)
	return err
}

// packageArgs arma el argv de install-package.sh y remove-package.sh: en un servidor lleva
// "--jump <lb>" delante de la VM, y en un balanceador "--frontend <puerto>" (REQ-5, REQ-19).
func (s *PrepareService) packageArgs(ctx context.Context, name, pkg string) ([]string, error) {
	frontend, err := s.lbFrontendArgs(ctx, name)
	if err != nil {
		return nil, err
	}
	args, _, err := targetArgs(ctx, s.lbs, name, pkg)
	if err != nil {
		return nil, err
	}
	return append(frontend, args...), nil
}

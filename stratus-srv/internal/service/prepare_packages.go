package service

import (
	"context"
	"fmt"
)

// sshServerPackage es el paquete que el backend necesita en toda VM para conectarse por SSH.
const sshServerPackage = "openssh-server"

// haproxyPackage es el paquete que un balanceador necesita para funcionar (REQ-5).
const haproxyPackage = "haproxy"

// webPackages son, por tipo de servidor web, el paquete que usa y el del otro tipo (005 REQ-7).
var webPackages = map[string]struct{ own, other string }{
	WebTypeApache: {"apache2", "nginx"},
	WebTypeNginx:  {"nginx", "apache2"},
}

// InstallPackage instala un paquete con install-package.sh. Instalar openssh-server es una
// instalación normal: la VM ya tiene el acceso con llave desde su creación (REQ-24). En un servidor
// web, instalar el paquete del otro tipo se rechaza aquí y también en el script (005 REQ-7).
func (s *PrepareService) InstallPackage(ctx context.Context, name, pkg string) (string, error) {
	if err := s.checkPackageVM(ctx, name, pkg); err != nil {
		return "", err
	}
	if typ, isWeb, err := s.web.webType(ctx, name); err != nil {
		return "", err
	} else if isWeb && pkg == webPackages[typ].other {
		return "", &PackageBlockedError{Message: fmt.Sprintf("No se puede instalar %s: el servidor web usa %s",
			pkg, webPackages[typ].own)}
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
	// El servidor web usa su paquete (005 REQ-7): quitarlo se rechaza aquí y también en el script.
	if typ, isWeb, err := s.web.webType(ctx, name); err != nil {
		return "", err
	} else if isWeb && pkg == webPackages[typ].own {
		return "", &PackageBlockedError{Message: fmt.Sprintf("No se puede eliminar %s: el servidor web lo usa", pkg)}
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

// packageArgs arma el argv de install-package.sh y remove-package.sh, con las opciones en el orden
// del script: "--jump <lb>" en un servidor asignado, "--frontend <puerto>" en un balanceador y
// "--role web-apache|web-nginx" en un servidor web, delante de la VM (REQ-5, REQ-19, 005 REQ-7).
func (s *PrepareService) packageArgs(ctx context.Context, name, pkg string) ([]string, error) {
	frontend, err := s.lbFrontendArgs(ctx, name)
	if err != nil {
		return nil, err
	}
	target, lb, err := targetArgs(ctx, s.lbs, name, pkg)
	if err != nil {
		return nil, err
	}
	var args []string
	if lb != "" {
		args, target = []string{target[0], target[1]}, target[2:] // "--jump <lb>"
	}
	args = append(args, frontend...)
	if typ, isWeb, err := s.web.webType(ctx, name); err != nil {
		return nil, err
	} else if isWeb {
		args = append(args, "--role", "web-"+typ)
	}
	return append(args, target...), nil
}

package service

import (
	"context"
	"errors"
	"fmt"

	"stratus-srv/internal/repository"
)

// SetResolver usa el servidor DNS como resolvedor de la VM con set-resolver.sh y, si termina bien,
// guarda la marca (REQ-31, REQ-33). Un servidor asignado se alcanza por --jump (REQ-32).
func (s *DNSService) SetResolver(ctx context.Context, vm string) (string, error) {
	return s.resolver(ctx, vm, true)
}

// UnsetResolver deja de usar el servidor DNS como resolvedor de la VM con unset-resolver.sh y, si
// termina bien, borra la marca (REQ-31, REQ-33).
func (s *DNSService) UnsetResolver(ctx context.Context, vm string) (string, error) {
	return s.resolver(ctx, vm, false)
}

// resolver valida la petición y ejecuta set-resolver.sh u unset-resolver.sh con el bloqueo de la VM.
// Exige el servidor DNS y su configuración, y una VM registrada que no sea el servidor DNS.
func (s *DNSService) resolver(ctx context.Context, vm string, on bool) (string, error) {
	dns, err := s.recordsServer(ctx)
	if err != nil {
		return "", err
	}
	if err := ValidateVMName(vm); err != nil {
		return "", err
	}
	if _, err := s.vms.Get(ctx, vm); errors.Is(err, repository.ErrVMNotFound) {
		return "", fmt.Errorf("buscar vm %s: %w", vm, ErrVMNotFound)
	} else if err != nil {
		return "", err
	}
	if vm == dns {
		return "", &ValidationError{Message: "El servidor DNS no puede ser su propio resolvedor"}
	}
	args, _, err := targetArgs(ctx, s.lbs, vm, s.cfg.Address())
	if err != nil {
		return "", err
	}

	op, script, msg := "unset_resolver", "dns/unset-resolver.sh", "Resolvedor DNS %s quitado de %s"
	if on {
		op, script, msg = "set_resolver", "dns/set-resolver.sh", "Resolvedor DNS %s configurado en %s"
	}
	save := func(ctx context.Context) error { return s.dnsRepo.SetResolver(ctx, vm, on) }
	res, err := s.ops.run(ctx, op, vm, script, args, true, save)
	if err != nil {
		return "", err
	}
	if line := firstLine(string(res.Stdout)); line != "" {
		return line, nil
	}
	return fmt.Sprintf(msg, s.cfg.Address(), vm), nil
}

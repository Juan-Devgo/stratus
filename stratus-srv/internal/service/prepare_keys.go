package service

import (
	"context"
	"fmt"
)

// GenerateUserKey genera la llave de root del usuario con generate-user-key.sh (REQ-6). El
// script imprime solo la llave privada por stdout; aquí pasa directo al KeyStore, sin
// guardarse ni registrarse (P5). Al terminar bien, la VM queda con user_key_generated (REQ-27).
func (s *PrepareService) GenerateUserKey(ctx context.Context, name string) (string, error) {
	if _, err := s.getVM(ctx, name); err != nil {
		return "", err
	}
	// En un servidor el script entra por el balanceador (--jump) y, al terminar, copia la llave a su
	// cuenta jump: por eso toma el bloqueo de los dos (REQ-21).
	args, lb, err := targetArgs(ctx, s.lbs, name)
	if err != nil {
		return "", err
	}
	lockVMs := []string{name}
	if lb != "" {
		lockVMs = append(lockVMs, lb)
	}

	setFlag := func(ctx context.Context, _ RunResult) error { return s.vms.SetUserKeyGenerated(ctx, name) }
	res, err := s.ops.runLocked(ctx, "generate_user_key", name, lockVMs, "prepare/generate-user-key.sh", args, true, setFlag)
	if err != nil {
		// Si el script alcanzó a imprimir algo, no se conserva en memoria.
		clear(res.Stdout)
		return "", mapJumpError(err)
	}

	s.keys.Put(name, res.Stdout)
	return fmt.Sprintf("Llave de root generada para %s; el acceso con contraseña quedó deshabilitado. "+
		"Descárguela ahora: se puede descargar una sola vez y se descarta en %d minutos",
		name, int(s.cfg.KeyTTL.Minutes())), nil
}

// DownloadUserKey entrega la llave privada pendiente de la VM y la borra de memoria. Si ya se
// descargó, venció o nunca se generó, devuelve ErrKeyUnavailable (REQ-8).
func (s *PrepareService) DownloadUserKey(name string) ([]byte, error) {
	if err := ValidateVMName(name); err != nil {
		return nil, err
	}
	key, err := s.keys.Take(name)
	if err != nil {
		return nil, fmt.Errorf("descargar llave de %s: %w", name, err)
	}
	return key, nil
}

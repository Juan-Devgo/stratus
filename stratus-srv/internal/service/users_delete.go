package service

import (
	"context"
	"errors"
	"strings"
)

// exitNotRegularUser es el código de delete-user.sh cuando el usuario no existe o su UID no es de
// un usuario regular (1000-59999).
const exitNotRegularUser = 6

// DeleteUser elimina un usuario con delete-user.sh. Con removeFiles también se borran todos sus
// archivos. Si el borrado termina bien, la llave privada pendiente de ese usuario se descarta
// (REQ-10); si falla, la cuenta puede seguir existiendo y su llave se conserva.
func (s *UsersService) DeleteUser(ctx context.Context, vm, username string, removeFiles bool) (string, error) {
	if err := ValidateUsername(username); err != nil {
		return "", err
	}
	v, err := s.getVM(ctx, vm)
	if err != nil {
		return "", err
	}

	args, lockVMs, err := s.userTarget(ctx, v.Name, true, username, yesNo(removeFiles))
	if err != nil {
		return "", err
	}
	// La llave se descarta antes de soltar el bloqueo de la VM, para que un create posterior del
	// mismo nombre no pierda su llave nueva.
	discardKey := func(context.Context, RunResult) error {
		s.keys.Discard(userKeyID(v.Name, username))
		return nil
	}
	res, err := s.ops.runLocked(ctx, "delete_user", v.Name, lockVMs, "users/delete-user.sh", args, true, discardKey)
	if err != nil {
		var opErr *OperationError
		if errors.As(err, &opErr) && opErr.ExitCode == exitNotRegularUser {
			return "", ErrNotRegularUser
		}
		return "", mapJumpError(err)
	}
	return strings.TrimSpace(string(res.Stdout)), nil
}

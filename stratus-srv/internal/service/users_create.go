package service

import (
	"context"
	"errors"
	"fmt"
	"os"
)

// Códigos de salida propios de create-user.sh (los de fallo común, 2 a 5 y 7, ya los cubre
// operationRunner).
const (
	exitUserExists    = 6
	exitGroupExists   = 8
	exitCleanupFailed = 9
)

// userKeyID es la clave de un usuario en el KeyStore: el mismo nombre puede existir en dos VMs.
func userKeyID(vm, username string) string { return vm + "/" + username }

// yesNo traduce un booleano al argumento yes/no de los scripts.
func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

// CreateUser crea un usuario con create-user.sh. Con llaves SSH, la llave privada que imprime el
// script queda solo en memoria para una descarga (REQ-10). Sin llaves, la contraseña llega al
// script en un archivo temporal 0600 cuya ruta es el último argumento (REQ-9).
func (s *UsersService) CreateUser(ctx context.Context, vm string, in CreateUserInput) (string, bool, error) {
	if err := ValidateCreateUser(in); err != nil {
		return "", false, err
	}
	v, err := s.getVM(ctx, vm)
	if err != nil {
		return "", false, err
	}

	args, lockVMs, err := s.userTarget(ctx, v.Name, true, in.Username, yesNo(in.Sudo), yesNo(in.SSHKey))
	if err != nil {
		return "", false, err
	}
	if !in.SSHKey {
		path, err := writeSecretFile("stratus-password-*", in.Password)
		if err != nil {
			return "", false, err
		}
		// El archivo se borra al terminar la operación, con éxito o con fallo.
		defer os.Remove(path)
		args = append(args, path)
	}

	res, err := s.ops.runLocked(ctx, "create_user", v.Name, lockVMs, "users/create-user.sh", args, true, nil)
	if err != nil {
		return "", false, mapCreateError(in.Username, err)
	}

	if in.SSHKey {
		s.keys.Put(userKeyID(v.Name, in.Username), res.Stdout)
		return fmt.Sprintf("Usuario %s creado en %s. Descargue su llave privada ahora: "+
			"se puede descargar una sola vez y se descarta en 10 minutos", in.Username, v.Name), true, nil
	}
	return fmt.Sprintf("Usuario %s creado en %s; inicia sesión con su contraseña en la consola o con su - %s",
		in.Username, v.Name, in.Username), false, nil
}

// mapCreateError traduce los códigos propios de create-user.sh; los demás errores pasan igual.
func mapCreateError(username string, err error) error {
	var opErr *OperationError
	if !errors.As(err, &opErr) {
		return err
	}
	switch opErr.ExitCode {
	case exitUserExists:
		return ErrUserExists
	case exitGroupExists:
		return ErrGroupExists
	case exitCleanupFailed:
		return &CleanupError{Username: username, Detail: opErr.Detail}
	case exitJumpFailed:
		return ErrJumpUpdateFailed
	}
	return err
}

// writeSecretFile guarda el contenido (una contraseña o variables de entorno) en un archivo
// temporal cuyo nombre sigue el patrón dado y que solo el usuario del backend puede leer
// (os.CreateTemp usa el modo 0600); devuelve su ruta.
func writeSecretFile(pattern, content string) (string, error) {
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", fmt.Errorf("crear archivo secreto: %w", err)
	}
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", fmt.Errorf("escribir archivo secreto: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", fmt.Errorf("cerrar archivo secreto: %w", err)
	}
	return f.Name(), nil
}

// DownloadUserKey entrega la llave privada pendiente del usuario y la descarta: una segunda
// llamada, o una tras 10 minutos, devuelve ErrUserKeyUnavailable (REQ-10).
func (s *UsersService) DownloadUserKey(vm, username string) ([]byte, error) {
	if err := ValidateVMName(vm); err != nil {
		return nil, err
	}
	if err := ValidateUsername(username); err != nil {
		return nil, err
	}
	key, err := s.keys.Take(userKeyID(vm, username))
	if errors.Is(err, ErrKeyUnavailable) {
		return nil, ErrUserKeyUnavailable
	}
	return key, err
}

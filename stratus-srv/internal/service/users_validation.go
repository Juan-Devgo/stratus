package service

import "regexp"

// Listas permitidas del módulo de usuarios (P3). Las mismas reglas están en
// stratus-ui/src/features/users/validation.ts y en el paso "1. Validar argumentos" de cada script.
var usernamePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

// Largo de la contraseña en bytes: como solo se aceptan ASCII imprimibles, bytes y caracteres coinciden.
const minPasswordLen, maxPasswordLen = 8, 72

// CreateUserInput es el cuerpo de POST /api/users/vms/{vm}/users.
type CreateUserInput struct {
	Username string `json:"username"`
	Sudo     bool   `json:"sudo"`
	SSHKey   bool   `json:"ssh_key"`
	Password string `json:"password,omitempty"`
}

// ValidateUsername comprueba el nombre contra ^[a-z][a-z0-9_-]{0,31}$.
func ValidateUsername(s string) error {
	if !usernamePattern.MatchString(s) {
		return &ValidationError{Message: "El nombre de usuario debe tener de 1 a 32 caracteres: minúsculas, números, \"_\" y \"-\", empezando con una letra"}
	}
	return nil
}

// ValidatePassword exige de 8 a 72 caracteres ASCII imprimibles (0x20-0x7E), sin espacio al
// inicio ni al final. El mensaje nunca incluye la contraseña.
func ValidatePassword(s string) error {
	invalid := &ValidationError{Message: "La contraseña debe tener de 8 a 72 caracteres ASCII imprimibles, sin espacio al inicio ni al final"}
	if len(s) < minPasswordLen || len(s) > maxPasswordLen {
		return invalid
	}
	for i := range len(s) {
		if s[i] < 0x20 || s[i] > 0x7E {
			return invalid
		}
	}
	if s[0] == ' ' || s[len(s)-1] == ' ' {
		return invalid
	}
	return nil
}

// ValidateCreateUser comprueba el nombre y la contraseña antes de invocar create-user.sh:
// con llaves SSH no se envía contraseña, y sin llaves la contraseña es obligatoria y válida.
func ValidateCreateUser(in CreateUserInput) error {
	if err := ValidateUsername(in.Username); err != nil {
		return err
	}
	if in.SSHKey {
		if in.Password != "" {
			return &ValidationError{Message: "Con llaves SSH no se envía contraseña"}
		}
		return nil
	}
	return ValidatePassword(in.Password)
}

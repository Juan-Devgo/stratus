package service

// Listas permitidas del módulo 004 (P3). Las mismas reglas están en
// stratus-ui/src/features/haproxy/validation.ts y en scripts/haproxy/lib/haproxy-common.sh.

// sshPort es el puerto que el frontend de un balanceador no puede usar: es el de SSH (REQ-7).
const sshPort = 22

// ValidatePort comprueba el puerto de un servidor: un entero de 1 a 65535 (REQ-8).
func ValidatePort(p int) error {
	if p < 1 || p > 65535 {
		return &ValidationError{Message: "El puerto debe ser un número entre 1 y 65535"}
	}
	return nil
}

// ValidateFrontendPort comprueba el puerto del frontend: como ValidatePort y distinto de 22 (REQ-7).
func ValidateFrontendPort(p int) error {
	if err := ValidatePort(p); err != nil {
		return err
	}
	if p == sshPort {
		return &ValidationError{Message: "El puerto 22 está reservado para SSH"}
	}
	return nil
}

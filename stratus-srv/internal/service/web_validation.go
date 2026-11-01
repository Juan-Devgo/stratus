package service

// Tipos de servidor web que la app crea (005 REQ-1).
const (
	WebTypeApache = "apache"
	WebTypeNginx  = "nginx"
)

// ValidateWebType acepta solo apache o nginx.
func ValidateWebType(t string) error {
	if t != WebTypeApache && t != WebTypeNginx {
		return &ValidationError{Message: "Seleccione el tipo de servidor web"}
	}
	return nil
}

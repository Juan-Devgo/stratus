package service

import (
	"errors"
	"testing"
)

// AC-2: el tipo de servidor web es apache o nginx; vacío u otro valor se rechaza (REQ-1).
func TestAC2_ValidateWebType(t *testing.T) {
	for _, ok := range []string{"apache", "nginx"} {
		if err := ValidateWebType(ok); err != nil {
			t.Errorf("ValidateWebType(%q) = %v, se esperaba nil", ok, err)
		}
	}
	for _, bad := range []string{"", "iis", "Apache", "apache2"} {
		err := ValidateWebType(bad)
		var v *ValidationError
		if !errors.As(err, &v) || v.Message != "Seleccione el tipo de servidor web" {
			t.Errorf("ValidateWebType(%q) = %v, se esperaba el error de tipo", bad, err)
		}
	}
}

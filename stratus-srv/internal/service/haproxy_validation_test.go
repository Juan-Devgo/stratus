package service

import (
	"errors"
	"testing"
)

// AC-16: el puerto de un servidor va de 1 a 65535.
func TestAC16_ValidatePort(t *testing.T) {
	tests := []struct {
		port int
		ok   bool
	}{
		{0, false}, {-1, false}, {65536, false}, {100000, false},
		{1, true}, {22, true}, {80, true}, {8080, true}, {65535, true},
	}
	for _, tt := range tests {
		err := ValidatePort(tt.port)
		if tt.ok && err != nil {
			t.Errorf("ValidatePort(%d) = %v, se esperaba nil", tt.port, err)
		}
		if !tt.ok {
			var v *ValidationError
			if !errors.As(err, &v) || v.Message != "El puerto debe ser un número entre 1 y 65535" {
				t.Errorf("ValidatePort(%d) = %v, se esperaba el ValidationError de la spec", tt.port, err)
			}
		}
	}
}

// AC-11: el puerto del frontend además no puede ser el 22.
func TestAC11_ValidateFrontendPort(t *testing.T) {
	tests := []struct {
		port int
		want string // vacío si es válido
	}{
		{22, "El puerto 22 está reservado para SSH"},
		{0, "El puerto debe ser un número entre 1 y 65535"},
		{65536, "El puerto debe ser un número entre 1 y 65535"},
		{80, ""}, {8080, ""}, {21, ""}, {23, ""},
	}
	for _, tt := range tests {
		err := ValidateFrontendPort(tt.port)
		if tt.want == "" {
			if err != nil {
				t.Errorf("ValidateFrontendPort(%d) = %v, se esperaba nil", tt.port, err)
			}
			continue
		}
		var v *ValidationError
		if !errors.As(err, &v) || v.Message != tt.want || !errors.Is(err, ErrInvalidInput) {
			t.Errorf("ValidateFrontendPort(%d) = %v, se esperaba %q", tt.port, err, tt.want)
		}
	}
}

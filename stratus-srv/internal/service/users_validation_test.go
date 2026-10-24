package service

import (
	"errors"
	"strings"
	"testing"
)

func TestAC5_CreateUserRejectsInvalidUsername(t *testing.T) {
	tests := []struct {
		name     string
		username string
		valid    bool
	}{
		{"minúscula simple", "ana", true},
		{"con guion bajo y número", "ana_1-b", true},
		{"32 caracteres", strings.Repeat("a", 32), true},
		{"mayúscula", "Ana", false},
		{"empieza con número", "1ana", false},
		{"con punto y coma", "ana;rm", false},
		{"vacío", "", false},
		{"33 caracteres", strings.Repeat("a", 33), false},
		{"con espacio", "ana b", false},
		{"empieza con guion", "-ana", false},
		{"salto de línea final", "ana\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCreateUser(CreateUserInput{Username: tt.username, SSHKey: true})
			if tt.valid && err != nil {
				t.Errorf("ValidateCreateUser(%q) = %v, se esperaba nil", tt.username, err)
			}
			if !tt.valid && !errors.Is(err, ErrInvalidInput) {
				t.Errorf("ValidateCreateUser(%q) = %v, se esperaba ErrInvalidInput", tt.username, err)
			}
			if got := ValidateUsername(tt.username); (got == nil) != tt.valid {
				t.Errorf("ValidateUsername(%q) = %v, valid = %v", tt.username, got, tt.valid)
			}
		})
	}
}

func TestAC7_ValidatePassword(t *testing.T) {
	tests := []struct {
		name     string
		password string
		valid    bool
	}{
		{"8 caracteres", "Secreta1", true},
		{"72 caracteres", strings.Repeat("x", 72), true},
		{"con espacios internos", "una clave con espacios", true},
		{"símbolos ASCII", "!~Secreta123~!", true},
		{"7 caracteres", "Corta12", false},
		{"73 caracteres", strings.Repeat("x", 73), false},
		{"no ASCII", "Contraseña1", false},
		{"espacio inicial", " Secreta123", false},
		{"espacio final", "Secreta123 ", false},
		{"salto de línea", "Secreta\n123", false},
		{"tabulador", "Secreta\t123", false},
		{"vacía", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePassword(tt.password)
			if tt.valid && err != nil {
				t.Errorf("ValidatePassword = %v, se esperaba nil", err)
			}
			if !tt.valid {
				if !errors.Is(err, ErrInvalidInput) {
					t.Fatalf("ValidatePassword = %v, se esperaba ErrInvalidInput", err)
				}
				// El mensaje de error nunca repite la contraseña.
				if tt.password != "" && strings.Contains(err.Error(), tt.password) {
					t.Errorf("el mensaje %q contiene la contraseña", err.Error())
				}
			}
			// Sin llaves, ValidateCreateUser aplica la misma regla.
			create := ValidateCreateUser(CreateUserInput{Username: "ana", Password: tt.password})
			if (create == nil) != tt.valid {
				t.Errorf("ValidateCreateUser sin llaves = %v, valid = %v", create, tt.valid)
			}
		})
	}
}

func TestAC8_CreateUserWithKeyRejectsPassword(t *testing.T) {
	tests := []struct {
		name  string
		input CreateUserInput
		valid bool
	}{
		{"llaves sin contraseña", CreateUserInput{Username: "ana", SSHKey: true}, true},
		{"llaves con contraseña", CreateUserInput{Username: "ana", SSHKey: true, Password: "Secreta123"}, false},
		{"contraseña sin llaves", CreateUserInput{Username: "ana", Password: "Secreta123"}, true},
		{"sin llaves ni contraseña", CreateUserInput{Username: "ana"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCreateUser(tt.input)
			if tt.valid && err != nil {
				t.Errorf("ValidateCreateUser = %v, se esperaba nil", err)
			}
			if !tt.valid && !errors.Is(err, ErrInvalidInput) {
				t.Errorf("ValidateCreateUser = %v, se esperaba ErrInvalidInput", err)
			}
		})
	}
}

package service

import (
	"errors"
	"strings"
	"testing"
)

func TestAC2_ValidateCreateVM(t *testing.T) {
	valid := CreateVMInput{Name: "vm1", MemoryMB: 2048, CPUs: 2}

	tests := []struct {
		name    string
		in      CreateVMInput
		wantErr bool
	}{
		{"valores válidos", valid, false},
		{"límites inferiores", CreateVMInput{Name: "a", MemoryMB: 512, CPUs: 1}, false},
		{"límites superiores", CreateVMInput{Name: strings.Repeat("a", 32), MemoryMB: 8192, CPUs: 4}, false},
		{"nombre con guion interno", CreateVMInput{Name: "web-01", MemoryMB: 2048, CPUs: 2}, false},
		{"nombre con mayúsculas", CreateVMInput{Name: "VM1", MemoryMB: 2048, CPUs: 2}, true},
		{"nombre que empieza con guion", CreateVMInput{Name: "-vm", MemoryMB: 2048, CPUs: 2}, true},
		{"nombre de 33 caracteres", CreateVMInput{Name: strings.Repeat("a", 33), MemoryMB: 2048, CPUs: 2}, true},
		{"nombre vacío", CreateVMInput{Name: "", MemoryMB: 2048, CPUs: 2}, true},
		{"nombre con inyección", CreateVMInput{Name: "vm1;rm", MemoryMB: 2048, CPUs: 2}, true},
		{"memoria 511", CreateVMInput{Name: "vm1", MemoryMB: 511, CPUs: 2}, true},
		{"memoria 8193", CreateVMInput{Name: "vm1", MemoryMB: 8193, CPUs: 2}, true},
		{"CPUs 0", CreateVMInput{Name: "vm1", MemoryMB: 2048, CPUs: 0}, true},
		{"CPUs 5", CreateVMInput{Name: "vm1", MemoryMB: 2048, CPUs: 5}, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateCreateVM(tc.in)
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("ValidateCreateVM(%+v) = %v, se esperaba nil", tc.in, err)
				}
				return
			}
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("ValidateCreateVM(%+v) = %v, se esperaba ErrInvalidInput", tc.in, err)
			}
			var verr *ValidationError
			if !errors.As(err, &verr) || verr.Message == "" {
				t.Errorf("ValidateCreateVM(%+v) = %v, se esperaba un ValidationError con mensaje", tc.in, err)
			}
		})
	}
}

func TestAC7_ValidatePackage(t *testing.T) {
	tests := []struct {
		name    string
		pkg     string
		wantErr bool
	}{
		{"nginx", "nginx", false},
		{"con signo más", "g++", false},
		{"con punto y guion", "python3.13-venv", false},
		{"dos caracteres", "vi", false},
		{"63 caracteres", "a" + strings.Repeat("b", 62), false},
		{"inyección", "nginx; rm -rf /", true},
		{"vacío", "", true},
		{"un carácter", "a", true},
		{"64 caracteres", "a" + strings.Repeat("b", 63), true},
		{"mayúsculas", "Nginx", true},
		{"empieza con guion", "-nginx", true},
		{"con espacio", "nginx curl", true},
		{"salto de línea", "nginx\ncurl", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePackage(tc.pkg)
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("ValidatePackage(%q) = %v, se esperaba nil", tc.pkg, err)
				}
				return
			}
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("ValidatePackage(%q) = %v, se esperaba ErrInvalidInput", tc.pkg, err)
			}
			var verr *ValidationError
			if !errors.As(err, &verr) || verr.Message == "" {
				t.Errorf("ValidatePackage(%q) = %v, se esperaba un ValidationError con mensaje", tc.pkg, err)
			}
		})
	}
}

func TestAC18_ValidateHostname(t *testing.T) {
	tests := []struct {
		name     string
		hostname string
		wantErr  bool
	}{
		{"web-01", "web-01", false},
		{"un carácter", "a", false},
		{"solo números", "01", false},
		{"63 caracteres", "a" + strings.Repeat("b", 61) + "c", false},
		{"mayúsculas y guion bajo", "Web_01", true},
		{"empieza con guion", "-web", true},
		{"termina con guion", "web-", true},
		{"64 caracteres", strings.Repeat("a", 64), true},
		{"vacío", "", true},
		{"con punto (FQDN)", "web.local", true},
		{"inyección", "web; reboot", true},
		{"salto de línea", "web\n01", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateHostname(tc.hostname)
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("ValidateHostname(%q) = %v, se esperaba nil", tc.hostname, err)
				}
				return
			}
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("ValidateHostname(%q) = %v, se esperaba ErrInvalidInput", tc.hostname, err)
			}
			var verr *ValidationError
			if !errors.As(err, &verr) || verr.Message == "" {
				t.Errorf("ValidateHostname(%q) = %v, se esperaba un ValidationError con mensaje", tc.hostname, err)
			}
		})
	}
}

// assertInvalidIssue comprueba que ValidateIssue rechace la entrada con un ValidationError.
func assertInvalidIssue(t *testing.T, in IssueInput) {
	t.Helper()
	err := ValidateIssue(in)
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("ValidateIssue(%+v) = %v, se esperaba ErrInvalidInput", in, err)
	}
	var verr *ValidationError
	if !errors.As(err, &verr) || verr.Message == "" {
		t.Errorf("ValidateIssue(%+v) = %v, se esperaba un ValidationError con mensaje", in, err)
	}
}

func TestAC15_ValidateIssueTooLong(t *testing.T) {
	// "ñ" ocupa 2 bytes: el límite cuenta caracteres, no bytes.
	if err := ValidateIssue(IssueInput{Message: strings.Repeat("ñ", 500)}); err != nil {
		t.Fatalf("500 caracteres: ValidateIssue = %v, se esperaba nil", err)
	}
	assertInvalidIssue(t, IssueInput{Message: strings.Repeat("a", 501)})
	assertInvalidIssue(t, IssueInput{Message: strings.Repeat("ñ", 501), Fields: []string{"hostname"}})
}

func TestAC16_ValidateIssueEmpty(t *testing.T) {
	assertInvalidIssue(t, IssueInput{})
	assertInvalidIssue(t, IssueInput{Message: "", Fields: []string{}})

	// Basta con un campo o con un mensaje.
	if err := ValidateIssue(IssueInput{Fields: []string{"hostname"}}); err != nil {
		t.Errorf("solo un campo: ValidateIssue = %v, se esperaba nil", err)
	}
	if err := ValidateIssue(IssueInput{Message: "Hola"}); err != nil {
		t.Errorf("solo mensaje: ValidateIssue = %v, se esperaba nil", err)
	}
}

func TestAC25_ValidateIssueAllowlist(t *testing.T) {
	tests := []struct {
		name    string
		in      IssueInput
		wantErr bool
	}{
		{"tildes, ñ y salto de línea", IssueInput{Message: "Configuración de producción\nlínea 2"}, false},
		{"los 9 campos", IssueInput{Fields: []string{"hostname", "date", "time", "ipv4", "os", "kernel", "arch", "tty", "users"}}, false},
		{"barra invertida", IssueInput{Message: `a\b`}, true},
		{"escape de agetty", IssueInput{Message: `Host: \n`}, true},
		{"tabulación", IssueInput{Message: "a\tb"}, true},
		{"campana", IssueInput{Message: "a\x07b"}, true},
		{"retorno de carro", IssueInput{Message: "a\r\nb"}, true},
		{"escape ANSI", IssueInput{Message: "\x1b[31mrojo"}, true},
		{"UTF-8 inválido", IssueInput{Message: "a\xffb"}, true},
		{"campo desconocido", IssueInput{Message: "Hola", Fields: []string{"hostname", "password"}}, true},
		{"campo con inyección", IssueInput{Fields: []string{"users; reboot"}}, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.wantErr {
				assertInvalidIssue(t, tc.in)
				return
			}
			if err := ValidateIssue(tc.in); err != nil {
				t.Fatalf("ValidateIssue(%+v) = %v, se esperaba nil", tc.in, err)
			}
		})
	}
}

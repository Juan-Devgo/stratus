package service

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// assertValidity comprueba que un validador acepta o rechaza un valor con ErrInvalidInput.
func assertValidity(t *testing.T, label string, err error, valid bool) {
	t.Helper()
	if valid && err != nil {
		t.Errorf("%s = %v, se esperaba nil", label, err)
	}
	if !valid && !errors.Is(err, ErrInvalidInput) {
		t.Errorf("%s = %v, se esperaba ErrInvalidInput", label, err)
	}
}

func TestAC3_ValidateServiceName(t *testing.T) {
	tests := []struct {
		name  string
		value string
		valid bool
	}{
		{"con guion", "demian-lee", true},
		{"una letra", "a", true},
		{"32 caracteres", strings.Repeat("a", 32), true},
		{"con número", "app-1", true},
		{"mayúscula", "Demian", false},
		{"empieza con número", "1app", false},
		{"con punto y coma", "app;rm", false},
		{"con guion bajo", "app_x", false},
		{"vacío", "", false},
		{"33 caracteres", strings.Repeat("a", 33), false},
		{"empieza con guion", "-api", false},
		{"salto de línea final", "api\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertValidity(t, fmt.Sprintf("ValidateServiceName(%q)", tt.value), ValidateServiceName(tt.value), tt.valid)
			if !tt.valid {
				in := validCreateInput()
				in.Name = tt.value
				assertCreateServiceRejected(t, in)
			}
		})
	}
}

func TestAC7_ValidateExecutableAndMapMissing(t *testing.T) {
	tests := []struct {
		name  string
		value string
		valid bool
	}{
		{"nombre simple", "demian-lee", true},
		{"con subdirectorio", "bin/server", true},
		{"con punto en el nombre", "a.b/c_d-e", true},
		{"255 caracteres", strings.Repeat("a", 255), true},
		{"sube un nivel", "../x", false},
		{"ruta absoluta", "/bin/sh", false},
		{"segmento punto", "a/./b", false},
		{"segmento punto punto", "a/../b", false},
		{"solo punto", ".", false},
		{"vacío", "", false},
		{"con espacio", "a b", false},
		{"con punto y coma", "a;b", false},
		{"barra doble", "a//b", false},
		{"barra final", "bin/", false},
		{"256 caracteres", strings.Repeat("a", 256), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertValidity(t, fmt.Sprintf("ValidateExecutable(%q)", tt.value), ValidateExecutable(tt.value), tt.valid)
			if !tt.valid {
				in := validCreateInput()
				in.Executable = tt.value
				assertCreateServiceRejected(t, in)
				edit := validEditInput()
				edit.Executable = tt.value
				assertEditServiceRejected(t, edit)
			}
		})
	}
}

func TestAC8_ValidateUnitFields(t *testing.T) {
	t.Run("descripción", func(t *testing.T) {
		tests := []struct {
			name  string
			value string
			valid bool
		}{
			{"acentos y ñ", "Servidor de producción ñ", true},
			{"100 caracteres", strings.Repeat("a", 100), true},
			{"100 caracteres con acentos", strings.Repeat("ñ", 100), true},
			{"porcentaje", "50% de uso", false},
			{"barra invertida", `a\b`, false},
			{"comillas", `di"ce`, false},
			{"salto de línea", "dos\nlíneas", false},
			{"tabulador", "tab\taquí", false},
			{"vacía", "", false},
			{"101 caracteres", strings.Repeat("a", 101), false},
			{"UTF-8 inválido", "a\xffb", false},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assertValidity(t, fmt.Sprintf("ValidateDescription(%q)", tt.value), ValidateDescription(tt.value), tt.valid)
				if !tt.valid {
					in := validCreateInput()
					in.Description = tt.value
					assertCreateServiceRejected(t, in)
					edit := validEditInput()
					edit.Description = tt.value
					assertEditServiceRejected(t, edit)
				}
			})
		}
	})

	t.Run("argumentos", func(t *testing.T) {
		tokens := func(n int) string { return strings.TrimSpace(strings.Repeat("a ", n)) }
		tests := []struct {
			name  string
			value string
			valid bool
		}{
			{"vacío", "", true},
			{"dos tokens", "--port 8080", true},
			{"símbolos permitidos", "--listen=0.0.0.0:80 a,b@c+d/e", true},
			{"16 tokens", tokens(16), true},
			{"punto y coma", "a;b", false},
			{"dólar", "$HOME", false},
			{"comillas", `"x"`, false},
			{"17 tokens", tokens(17), false},
			{"espacio inicial", " a", false},
			{"espacio final", "a ", false},
			{"espacio doble", "a  b", false},
			{"salto de línea", "a\nb", false},
			{"token de 129 caracteres", strings.Repeat("x", 129), false},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assertValidity(t, fmt.Sprintf("ValidateArgs(%q)", tt.value), ValidateArgs(tt.value), tt.valid)
				if !tt.valid {
					in := validCreateInput()
					in.Args = tt.value
					assertCreateServiceRejected(t, in)
					edit := validEditInput()
					edit.Args = tt.value
					assertEditServiceRejected(t, edit)
				}
			})
		}
	})

	t.Run("Restart", func(t *testing.T) {
		for value, valid := range map[string]bool{"no": true, "always": true, "on-failure": true,
			"on-abort": false, "": false, "Always": false, "no ": false} {
			assertValidity(t, fmt.Sprintf("ValidateRestart(%q)", value), ValidateRestart(value), valid)
			if !valid {
				in := validCreateInput()
				in.Restart = value
				assertCreateServiceRejected(t, in)
				edit := validEditInput()
				edit.Restart = value
				assertEditServiceRejected(t, edit)
			}
		}
	})

	t.Run("RestartSec", func(t *testing.T) {
		for value, valid := range map[int]bool{0: true, 5: true, 3600: true, -1: false, 3601: false} {
			assertValidity(t, fmt.Sprintf("ValidateRestartSec(%d)", value), ValidateRestartSec(value), valid)
			if !valid {
				in := validCreateInput()
				in.RestartSec = value
				assertCreateServiceRejected(t, in)
				edit := validEditInput()
				edit.RestartSec = value
				assertEditServiceRejected(t, edit)
			}
		}
	})
}

func TestAC9_ValidateEnv(t *testing.T) {
	rows := func(n int) []EnvVar {
		out := make([]EnvVar, n)
		for i := range out {
			out[i] = EnvVar{Key: fmt.Sprintf("K%d", i), Value: "v"}
		}
		return out
	}
	tests := []struct {
		name  string
		env   []EnvVar
		valid bool
	}{
		{"sin filas", nil, true},
		{"filas del ejemplo", []EnvVar{{"PORT", "8080"}, {"SERVER_NAME", "Servidor-Produccion-1"}, {"ADMIN_PASSWORD", "Secreta123"}}, true},
		{"valor vacío", []EnvVar{{"EMPTY", ""}}, true},
		{"valor con símbolos", []EnvVar{{"K", `a b "c" $HOME \x`}}, true},
		{"clave con guion bajo inicial", []EnvVar{{"_X1", "v"}}, true},
		{"50 filas", rows(50), true},
		{"valor de 1024", []EnvVar{{"K", strings.Repeat("x", 1024)}}, true},
		{"clave en minúsculas", []EnvVar{{"port", "1"}}, false},
		{"clave que empieza con número", []EnvVar{{"1PORT", "1"}}, false},
		{"clave vacía", []EnvVar{{"", "1"}}, false},
		{"clave repetida", []EnvVar{{"PORT", "1"}, {"PORT", "2"}}, false},
		{"clave de 65 caracteres", []EnvVar{{strings.Repeat("A", 65), "1"}}, false},
		{"valor con salto de línea", []EnvVar{{"K", "a\nb"}}, false},
		{"valor no ASCII", []EnvVar{{"K", "café"}}, false},
		{"valor con tabulador", []EnvVar{{"K", "a\tb"}}, false},
		{"valor de 1025", []EnvVar{{"K", strings.Repeat("x", 1025)}}, false},
		{"51 filas", rows(51), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertValidity(t, "ValidateEnv", ValidateEnv(tt.env), tt.valid)
			if !tt.valid {
				in := validCreateInput()
				in.Env = tt.env
				assertCreateServiceRejected(t, in)
				// En una edición las filas solo se aceptan con el reemplazo marcado, y también se validan.
				edit := validEditInput()
				edit.ReplaceEnv = true
				edit.Env = tt.env
				assertEditServiceRejected(t, edit)
			}
		})
	}

	t.Run("el mensaje no repite el valor", func(t *testing.T) {
		err := ValidateEnv([]EnvVar{{"port", "Secreta123"}})
		if err == nil || strings.Contains(err.Error(), "Secreta123") {
			t.Errorf("ValidateEnv = %v, se esperaba un error sin el valor", err)
		}
	})
}

func TestAC8_ValidateCreateAndEditService(t *testing.T) {
	create := CreateServiceInput{Name: "demian-lee", Description: "Servidor", Executable: "demian-lee",
		Restart: "on-failure", RestartSec: 5, Env: []EnvVar{{"PORT", "8080"}}}
	edit := EditServiceInput{Description: "Servidor", Executable: "demian-lee", Restart: "always", RestartSec: 5}

	assertValidity(t, "ValidateCreateService válido", ValidateCreateService(create), true)
	assertValidity(t, "ValidateEditService válido", ValidateEditService(edit), true)

	badCreate := create
	badCreate.Name = "Demian"
	assertValidity(t, "ValidateCreateService con nombre inválido", ValidateCreateService(badCreate), false)
	badCreate = create
	badCreate.Restart = "on-abort"
	assertValidity(t, "ValidateCreateService con Restart inválido", ValidateCreateService(badCreate), false)
	badCreate = create
	badCreate.Env = []EnvVar{{"port", "1"}}
	assertValidity(t, "ValidateCreateService con clave inválida", ValidateCreateService(badCreate), false)

	badEdit := edit
	badEdit.Executable = "../x"
	assertValidity(t, "ValidateEditService con ejecutable inválido", ValidateEditService(badEdit), false)
	// Sin reemplazar variables no se envían valores; con reemplazo se validan.
	badEdit = edit
	badEdit.Env = []EnvVar{{"PORT", "1"}}
	assertValidity(t, "ValidateEditService con filas y sin reemplazo", ValidateEditService(badEdit), false)
	okEdit := badEdit
	okEdit.ReplaceEnv = true
	assertValidity(t, "ValidateEditService con reemplazo", ValidateEditService(okEdit), true)
	okEdit.Env = []EnvVar{{"port", "1"}}
	assertValidity(t, "ValidateEditService con reemplazo y clave inválida", ValidateEditService(okEdit), false)
}

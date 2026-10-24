package service

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Listas permitidas del módulo de servicios (P3). Las mismas reglas están en
// stratus-ui/src/features/services/validation.ts y en el paso "1. Validar argumentos" de cada
// script (scripts/services/lib/service-common.sh).
var (
	serviceNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	executablePattern  = regexp.MustCompile(`^[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)*$`)
	argTokenPattern    = regexp.MustCompile(`^[A-Za-z0-9._/:=,@+-]{1,128}$`)
	envKeyPattern      = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,63}$`)
)

const (
	maxDescriptionRunes = 100
	maxExecutableLen    = 255
	maxArgTokens        = 16
	maxRestartSec       = 3600
	maxEnvRows          = 50
	maxEnvValueLen      = 1024
)

// EnvVar es una variable de entorno del servicio. Su valor es un secreto (REQ-13): nunca se
// registra, ni siquiera en mensajes de error.
type EnvVar struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// CreateServiceInput es el JSON de la parte "payload" de POST /api/services/vms/{vm}/services.
type CreateServiceInput struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Executable  string   `json:"executable"`
	Args        string   `json:"args"`
	Restart     string   `json:"restart"`
	RestartSec  int      `json:"restart_sec"`
	Env         []EnvVar `json:"env"`
}

// EditServiceInput es el cuerpo de PUT /api/services/vms/{vm}/services/{name}.
// Con ReplaceEnv=false el archivo /etc/<name>.env no cambia y Env debe venir vacío (REQ-22).
type EditServiceInput struct {
	Description string   `json:"description"`
	Executable  string   `json:"executable"`
	Args        string   `json:"args"`
	Restart     string   `json:"restart"`
	RestartSec  int      `json:"restart_sec"`
	ReplaceEnv  bool     `json:"replace_env"`
	Env         []EnvVar `json:"env"`
}

// ValidateServiceName comprueba el nombre contra ^[a-z][a-z0-9-]{0,31}$ (REQ-2).
func ValidateServiceName(s string) error {
	if !serviceNamePattern.MatchString(s) {
		return &ValidationError{Message: "El nombre del servicio debe tener de 1 a 32 caracteres: minúsculas, números y \"-\", empezando con una letra"}
	}
	return nil
}

// ValidateDescription exige de 1 a 100 caracteres UTF-8 imprimibles, sin %, \ ni " (REQ-3).
func ValidateDescription(s string) error {
	invalid := &ValidationError{Message: "La descripción debe tener de 1 a 100 caracteres imprimibles, sin %, \\, \" ni saltos de línea"}
	if !utf8.ValidString(s) {
		return invalid
	}
	if n := utf8.RuneCountInString(s); n < 1 || n > maxDescriptionRunes {
		return invalid
	}
	for _, r := range s {
		if !unicode.IsPrint(r) || r == '%' || r == '\\' || r == '"' {
			return invalid
		}
	}
	return nil
}

// ValidateExecutable exige una ruta relativa a /opt/<name> de hasta 255 caracteres, sin
// segmentos "." ni ".." (REQ-6).
func ValidateExecutable(s string) error {
	invalid := &ValidationError{Message: "El ejecutable debe ser una ruta relativa de hasta 255 caracteres con letras, números, \".\", \"_\", \"-\" y \"/\", sin segmentos . ni .."}
	if len(s) > maxExecutableLen || !executablePattern.MatchString(s) {
		return invalid
	}
	for _, segment := range strings.Split(s, "/") {
		if segment == "." || segment == ".." {
			return invalid
		}
	}
	return nil
}

// ValidateArgs acepta vacío o de 1 a 16 tokens separados por un solo espacio (REQ-7).
func ValidateArgs(s string) error {
	if s == "" {
		return nil
	}
	invalid := &ValidationError{Message: "Los argumentos deben ser hasta 16 tokens de 1 a 128 caracteres (letras, números y . _ / : = , @ + -) separados por un espacio"}
	tokens := strings.Split(s, " ")
	if len(tokens) > maxArgTokens {
		return invalid
	}
	for _, token := range tokens {
		if !argTokenPattern.MatchString(token) {
			return invalid
		}
	}
	return nil
}

// ValidateRestart acepta no, always u on-failure (REQ-8).
func ValidateRestart(s string) error {
	switch s {
	case "no", "always", "on-failure":
		return nil
	}
	return &ValidationError{Message: "Restart debe ser no, always u on-failure"}
}

// ValidateRestartSec exige un entero de 0 a 3600 (REQ-8).
func ValidateRestartSec(n int) error {
	if n < 0 || n > maxRestartSec {
		return &ValidationError{Message: "RestartSec debe ser un entero de 0 a 3600"}
	}
	return nil
}

// ValidateEnv exige hasta 50 filas con claves ^[A-Z_][A-Z0-9_]{0,63}$ únicas y valores de hasta
// 1024 caracteres ASCII imprimibles (espacio a ~) (REQ-9). Los mensajes nombran la fila o la
// clave, nunca el valor.
func ValidateEnv(env []EnvVar) error {
	if len(env) > maxEnvRows {
		return &ValidationError{Message: "Se permiten hasta 50 variables de entorno"}
	}
	seen := make(map[string]bool, len(env))
	for i, v := range env {
		if !envKeyPattern.MatchString(v.Key) {
			return &ValidationError{Message: fmt.Sprintf("La clave de la variable %d debe cumplir ^[A-Z_][A-Z0-9_]{0,63}$", i+1)}
		}
		if seen[v.Key] {
			return &ValidationError{Message: fmt.Sprintf("La clave %s está repetida", v.Key)}
		}
		seen[v.Key] = true
		if len(v.Value) > maxEnvValueLen {
			return &ValidationError{Message: fmt.Sprintf("El valor de %s supera 1024 caracteres", v.Key)}
		}
		for j := range len(v.Value) {
			if v.Value[j] < 0x20 || v.Value[j] > 0x7E {
				return &ValidationError{Message: fmt.Sprintf("El valor de %s solo admite caracteres ASCII imprimibles", v.Key)}
			}
		}
	}
	return nil
}

// validateUnitFields agrupa las reglas que comparten crear y editar.
func validateUnitFields(description, executable, args, restart string, restartSec int) error {
	for _, err := range []error{
		ValidateDescription(description),
		ValidateExecutable(executable),
		ValidateArgs(args),
		ValidateRestart(restart),
		ValidateRestartSec(restartSec),
	} {
		if err != nil {
			return err
		}
	}
	return nil
}

// ValidateCreateService comprueba todos los campos antes de guardar archivos o invocar el script.
func ValidateCreateService(in CreateServiceInput) error {
	if err := ValidateServiceName(in.Name); err != nil {
		return err
	}
	if err := validateUnitFields(in.Description, in.Executable, in.Args, in.Restart, in.RestartSec); err != nil {
		return err
	}
	return ValidateEnv(in.Env)
}

// ValidateEditService comprueba los campos de una edición. Sin ReplaceEnv no se envían valores.
func ValidateEditService(in EditServiceInput) error {
	if err := validateUnitFields(in.Description, in.Executable, in.Args, in.Restart, in.RestartSec); err != nil {
		return err
	}
	if !in.ReplaceEnv && len(in.Env) > 0 {
		return &ValidationError{Message: "Sin reemplazar variables no se envían valores"}
	}
	return ValidateEnv(in.Env)
}

package service

import (
	"fmt"
	"regexp"
	"slices"
	"unicode"
	"unicode/utf8"
)

// Listas permitidas del módulo prepare (P3). Las mismas reglas están en
// stratus-ui/src/features/prepare/validation.ts y en el paso "1. Validar argumentos" de cada script.
var (
	vmNamePattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)
	packagePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9+.-]{1,62}$`)
	// Una sola etiqueta DNS de 1 a 63 caracteres, sin guion al inicio ni al final (REQ-12).
	hostnamePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
)

// issueFields son las claves de campo de /etc/issue en el orden fijo de REQ-26; set-issue.sh
// traduce cada una a su etiqueta ("hostname" -> "Host: \n").
var issueFields = []string{"hostname", "date", "time", "ipv4", "os", "kernel", "arch", "tty", "users"}

// maxIssueMessage es el máximo de caracteres (no bytes) del mensaje propio de /etc/issue.
const maxIssueMessage = 500

// Rangos de creación de VM (REQ-1).
const (
	minMemoryMB, maxMemoryMB = 512, 8192
	minCPUs, maxCPUs         = 1, 4
)

// CreateVMInput es el cuerpo de POST /api/prepare/vms.
type CreateVMInput struct {
	Name     string `json:"name"`
	MemoryMB int    `json:"memory_mb"`
	CPUs     int    `json:"cpus"`
}

// ValidateVMName comprueba el nombre de la VM contra ^[a-z0-9][a-z0-9-]{0,31}$.
func ValidateVMName(s string) error {
	if !vmNamePattern.MatchString(s) {
		return &ValidationError{Message: "El nombre de la VM debe tener de 1 a 32 caracteres: minúsculas, números y guiones, sin empezar con guion"}
	}
	return nil
}

// ValidateCreateVM comprueba nombre, memoria y CPUs antes de invocar create-vm.sh.
func ValidateCreateVM(in CreateVMInput) error {
	if err := ValidateVMName(in.Name); err != nil {
		return err
	}
	if err := checkRange("La memoria", "MB", in.MemoryMB, minMemoryMB, maxMemoryMB); err != nil {
		return err
	}
	return checkRange("El número de CPUs", "", in.CPUs, minCPUs, maxCPUs)
}

// checkRange devuelve un ValidationError si v está fuera de [lo, hi].
func checkRange(field, unit string, v, lo, hi int) error {
	if v >= lo && v <= hi {
		return nil
	}
	if unit != "" {
		unit = " " + unit
	}
	return &ValidationError{Message: fmt.Sprintf("%s debe estar entre %d y %d%s", field, lo, hi, unit)}
}

// ValidatePackage comprueba el nombre del paquete contra ^[a-z0-9][a-z0-9+.-]{1,62}$ (REQ-4).
// Rechaza vacío, espacios y metacaracteres del shell como ";".
func ValidatePackage(s string) error {
	if !packagePattern.MatchString(s) {
		return &ValidationError{Message: "El nombre del paquete debe tener de 2 a 63 caracteres: minúsculas, números, \"+\", \".\" y \"-\", empezando con letra o número"}
	}
	return nil
}

// ValidateHostname comprueba el hostname contra ^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$ (REQ-12).
// Rechaza mayúsculas, "_", puntos (FQDN) y guion al inicio o al final.
func ValidateHostname(s string) error {
	if !hostnamePattern.MatchString(s) {
		return &ValidationError{Message: "El hostname debe tener de 1 a 63 caracteres: minúsculas, números y guiones, sin empezar ni terminar con guion"}
	}
	return nil
}

// IssueInput es el cuerpo de POST /api/prepare/vms/{name}/issue.
type IssueInput struct {
	Message string   `json:"message"`
	Fields  []string `json:"fields"`
}

// ValidateIssue comprueba el contenido de /etc/issue (REQ-10): al menos un campo o un mensaje,
// un mensaje de hasta 500 caracteres UTF-8 imprimibles o saltos de línea, sin "\\" (agetty lo
// tomaría como escape), y solo claves de campo conocidas.
func ValidateIssue(in IssueInput) error {
	if in.Message == "" && len(in.Fields) == 0 {
		return &ValidationError{Message: "Seleccione al menos un campo o escriba un mensaje"}
	}
	if !utf8.ValidString(in.Message) {
		return &ValidationError{Message: issueCharsMessage}
	}
	if utf8.RuneCountInString(in.Message) > maxIssueMessage {
		return &ValidationError{Message: fmt.Sprintf("El mensaje debe tener como máximo %d caracteres", maxIssueMessage)}
	}
	for _, r := range in.Message {
		// unicode.IsPrint rechaza los caracteres de control (tabulación, \r, ESC...) y deja
		// letras con tilde, ñ, signos y el espacio.
		if r == '\\' || (r != '\n' && !unicode.IsPrint(r)) {
			return &ValidationError{Message: issueCharsMessage}
		}
	}
	for _, f := range in.Fields {
		if !slices.Contains(issueFields, f) {
			return &ValidationError{Message: "Campo de /etc/issue desconocido"}
		}
	}
	return nil
}

const issueCharsMessage = "El mensaje solo admite caracteres imprimibles y saltos de línea, sin barra invertida (\\)"

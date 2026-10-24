package service

import "regexp"

// privateKeyBlock reconoce cualquier bloque PEM de llave privada (OPENSSH, RSA, EC, ...).
// (?s) permite que "." incluya saltos de línea dentro del bloque.
var privateKeyBlock = regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`)

// Redact oculta las llaves privadas antes de guardar o registrar texto (stderr de los
// scripts): cada bloque de llave privada se reemplaza por "[llave privada omitida]".
func Redact(s string) string {
	return privateKeyBlock.ReplaceAllString(s, "[llave privada omitida]")
}

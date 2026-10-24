package service

import (
	"strings"
	"testing"
)

func TestAC28_RedactMasksSecrets(t *testing.T) {
	const keyBody = "b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAMwAAAAtzc2gtZW"
	privateKey := "-----BEGIN OPENSSH PRIVATE KEY-----\n" + keyBody + "\n-----END OPENSSH PRIVATE KEY-----"

	tests := []struct {
		name       string
		in         string
		forbidden  []string
		mustRemain []string
	}{
		{
			name:       "bloque de llave privada OpenSSH",
			in:         "antes\n" + privateKey + "\ndespués\n",
			forbidden:  []string{keyBody, "BEGIN OPENSSH PRIVATE KEY"},
			mustRemain: []string{"antes", "después", "[llave privada omitida]"},
		},
		{
			name:       "llave RSA en una línea con otro texto",
			in:         "error: -----BEGIN RSA PRIVATE KEY-----\nMIIE\n-----END RSA PRIVATE KEY----- fin",
			forbidden:  []string{"MIIE"},
			mustRemain: []string{"error:", "fin", "[llave privada omitida]"},
		},
		{
			name:       "texto sin llaves no cambia",
			in:         "salida normal",
			mustRemain: []string{"salida normal"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Redact(tc.in)
			for _, f := range tc.forbidden {
				if strings.Contains(got, f) {
					t.Errorf("Redact(%q) = %q, todavía contiene %q", tc.in, got, f)
				}
			}
			for _, m := range tc.mustRemain {
				if !strings.Contains(got, m) {
					t.Errorf("Redact(%q) = %q, no contiene %q", tc.in, got, m)
				}
			}
		})
	}
}

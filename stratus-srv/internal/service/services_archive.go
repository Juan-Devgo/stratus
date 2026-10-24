package service

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// ArchiveUpload es el archivo .tar.gz que sube el navegador: el nombre original y su cuerpo.
type ArchiveUpload struct {
	Filename string
	Body     io.Reader
}

// saveArchive guarda el archivo subido en un archivo temporal del host (modo 0600) y devuelve su
// ruta, que va como argumento al script (REQ-12). Rechaza antes de guardar un nombre sin la
// extensión .tar.gz y, mientras copia, un cuerpo de más de limit bytes; en ambos casos y ante
// cualquier error de lectura no queda ningún archivo. Quien recibe la ruta debe borrarla al
// terminar la operación, con éxito o con fallo.
func saveArchive(a ArchiveUpload, limit int64) (string, error) {
	if !strings.HasSuffix(a.Filename, ".tar.gz") {
		return "", &ValidationError{Message: "El archivo debe ser .tar.gz"}
	}
	f, err := os.CreateTemp("", "stratus-archive-*.tar.gz")
	if err != nil {
		return "", fmt.Errorf("crear archivo temporal: %w", err)
	}
	// Se lee un byte más que el límite: si llega, el archivo es demasiado grande.
	n, err := io.Copy(f, io.LimitReader(a.Body, limit+1))
	closeErr := f.Close()
	var tooBig *http.MaxBytesError
	switch {
	case errors.As(err, &tooBig) || (err == nil && n > limit):
		os.Remove(f.Name())
		return "", &ValidationError{Message: "El archivo supera 200 MB"}
	case err != nil:
		os.Remove(f.Name())
		return "", fmt.Errorf("guardar archivo: %w", err)
	case closeErr != nil:
		os.Remove(f.Name())
		return "", fmt.Errorf("cerrar archivo: %w", closeErr)
	}
	return f.Name(), nil
}

// envFileContent arma el contenido del archivo secreto de variables: una línea KEY=VALUE por fila.
// Los valores pasan tal cual; el script los valida de nuevo y los codifica para systemd.
func envFileContent(env []EnvVar) string {
	var b strings.Builder
	for _, v := range env {
		b.WriteString(v.Key)
		b.WriteByte('=')
		b.WriteString(v.Value)
		b.WriteByte('\n')
	}
	return b.String()
}

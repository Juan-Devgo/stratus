package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// errReader falla en la primera lectura, como una conexión cortada o un cuerpo que superó el
// límite de http.MaxBytesReader.
type errReader struct{ err error }

func (r errReader) Read([]byte) (int, error) { return 0, r.err }

// useTempDir apunta os.TempDir a un directorio de la prueba y lo devuelve, para comprobar que no
// queda ningún archivo "stratus-*" al terminar.
func useTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	return dir
}

func leftovers(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestAC5_SaveArchiveStoresBodyInTempFile(t *testing.T) {
	dir := useTempDir(t)

	path, err := saveArchive(ArchiveUpload{Filename: "app.tar.gz", Body: strings.NewReader("contenido")}, 100)
	if err != nil {
		t.Fatalf("saveArchive: %v", err)
	}
	defer os.Remove(path)

	if filepath.Dir(path) != dir || !strings.HasPrefix(filepath.Base(path), "stratus-archive-") ||
		!strings.HasSuffix(path, ".tar.gz") {
		t.Errorf("path = %q, se esperaba %s/stratus-archive-*.tar.gz", path, dir)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "contenido" {
		t.Errorf("contenido = %q, %v; se esperaba %q", got, err, "contenido")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("modo = %v, se esperaba 0600", info.Mode().Perm())
	}
}

func TestAC5_SaveArchiveAcceptsExactlyMaxBytes(t *testing.T) {
	useTempDir(t)
	path, err := saveArchive(ArchiveUpload{Filename: "app.tar.gz", Body: strings.NewReader(strings.Repeat("x", 10))}, 10)
	if err != nil {
		t.Fatalf("saveArchive con exactamente max bytes: %v", err)
	}
	os.Remove(path)
}

func TestAC5_SaveArchiveRejectsSuffixAndSize(t *testing.T) {
	tests := []struct {
		name string
		up   ArchiveUpload
		want string
	}{
		{"extensión .zip", ArchiveUpload{Filename: "app.zip", Body: strings.NewReader("x")}, "El archivo debe ser .tar.gz"},
		{"solo .gz", ArchiveUpload{Filename: "app.gz", Body: strings.NewReader("x")}, "El archivo debe ser .tar.gz"},
		{"sin nombre", ArchiveUpload{Filename: "", Body: strings.NewReader("x")}, "El archivo debe ser .tar.gz"},
		{"un byte de más", ArchiveUpload{Filename: "app.tar.gz", Body: strings.NewReader(strings.Repeat("x", 11))}, "El archivo supera 200 MB"},
		{"límite del cuerpo HTTP", ArchiveUpload{Filename: "app.tar.gz", Body: errReader{&http.MaxBytesError{Limit: 5}}}, "El archivo supera 200 MB"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := useTempDir(t)

			path, err := saveArchive(tt.up, 10)

			if !errors.Is(err, ErrInvalidInput) || err.Error() != tt.want {
				t.Errorf("err = %v, se esperaba ErrInvalidInput con %q", err, tt.want)
			}
			if path != "" {
				t.Errorf("path = %q, se esperaba vacío", path)
			}
			if left := leftovers(t, dir); len(left) != 0 {
				t.Errorf("quedaron archivos temporales: %v", left)
			}
		})
	}
}

func TestAC5_SaveArchiveRemovesFileOnReadError(t *testing.T) {
	dir := useTempDir(t)
	boom := errors.New("conexión cortada")

	_, err := saveArchive(ArchiveUpload{Filename: "app.tar.gz", Body: io.MultiReader(strings.NewReader("abc"), errReader{boom})}, 10)

	if !errors.Is(err, boom) {
		t.Errorf("err = %v, se esperaba el error de lectura", err)
	}
	if left := leftovers(t, dir); len(left) != 0 {
		t.Errorf("quedaron archivos temporales: %v", left)
	}
}

func TestAC11_EnvFileContent(t *testing.T) {
	tests := []struct {
		name string
		env  []EnvVar
		want string
	}{
		{"sin filas", nil, ""},
		{"una fila", []EnvVar{{"PORT", "9090"}}, "PORT=9090\n"},
		{"valor vacío y con símbolos", []EnvVar{{"A", ""}, {"B", `a b "c" $HOME \x`}}, "A=\nB=a b \"c\" $HOME \\x\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := envFileContent(tt.env); got != tt.want {
				t.Errorf("envFileContent = %q, se esperaba %q", got, tt.want)
			}
		})
	}
}

func TestAC5_CreateServiceRejectsArchive(t *testing.T) {
	tests := []struct {
		name string
		up   ArchiveUpload
		want string
	}{
		{"11 bytes con máximo 10", ArchiveUpload{Filename: "app.tar.gz", Body: strings.NewReader(strings.Repeat("x", 11))}, "El archivo supera 200 MB"},
		{"extensión .zip", ArchiveUpload{Filename: "app.zip", Body: strings.NewReader("x")}, "El archivo debe ser .tar.gz"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := useTempDir(t)
			runner := &fakeRunner{}
			env := newServicesEnv(t, runner)
			env.svc.cfg.MaxArchiveBytes = 10
			env.register(t, "vm1")

			_, err := env.svc.CreateService(context.Background(), "vm1", validCreateInput(), tt.up)

			if !errors.Is(err, ErrInvalidInput) || err.Error() != tt.want {
				t.Errorf("err = %v, se esperaba ErrInvalidInput con %q", err, tt.want)
			}
			if n := runner.callCount(); n != 0 {
				t.Errorf("el runner se llamó %d veces, se esperaban 0", n)
			}
			if left := leftovers(t, dir); len(left) != 0 {
				t.Errorf("quedaron archivos temporales: %v", left)
			}
		})
	}
}

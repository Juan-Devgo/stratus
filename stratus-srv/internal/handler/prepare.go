package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"stratus-srv/internal/repository"
	"stratus-srv/internal/service"
)

// maxBodyBytes limita el cuerpo JSON de las peticiones. 256 KiB alcanzan para 50 variables de
// entorno de 1024 caracteres con el escape de JSON (servicios, REQ-9).
const maxBodyBytes = 256 << 10

// RegisterPrepare registra las rutas del módulo "Preparar sistema" bajo /api/prepare.
func RegisterPrepare(mux *http.ServeMux, svc *service.PrepareService) {
	mux.HandleFunc("GET /api/prepare/vms", func(w http.ResponseWriter, r *http.Request) {
		vms, listErr, err := svc.ListVMs(r.Context())
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, struct {
			VMs       []service.VMView `json:"vms"`
			ListError string           `json:"list_error"`
		}{vms, listErr})
	})

	mux.HandleFunc("POST /api/prepare/vms", func(w http.ResponseWriter, r *http.Request) {
		var in service.CreateVMInput
		if !decodeJSON(w, r, &in) {
			return
		}
		msg, err := svc.CreateVM(r.Context(), in)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, messageBody{msg})
	})

	// Encender una VM devuelve {message, warning?}: la advertencia aparece si no se pudieron
	// actualizar las IPs de su balanceador (004, REQ-14).
	mux.HandleFunc("POST /api/prepare/vms/{name}/start", func(w http.ResponseWriter, r *http.Request) {
		res, err := svc.StartVM(r.Context(), r.PathValue("name"))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, res)
	})
	mux.HandleFunc("POST /api/prepare/vms/{name}/stop", vmAction(svc.StopVM))
	mux.HandleFunc("DELETE /api/prepare/vms/{name}", vmAction(svc.DeleteVM))

	mux.HandleFunc("GET /api/prepare/catalog", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, struct {
			Packages []string `json:"packages"`
		}{service.PackageCatalog})
	})
	mux.HandleFunc("POST /api/prepare/vms/{name}/install-package", packageAction(svc.InstallPackage))
	mux.HandleFunc("POST /api/prepare/vms/{name}/remove-package", packageAction(svc.RemovePackage))

	mux.HandleFunc("POST /api/prepare/vms/{name}/user-key", vmAction(svc.GenerateUserKey))
	mux.HandleFunc("GET /api/prepare/vms/{name}/user-key/download", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		key, err := svc.DownloadUserKey(name)
		if err != nil {
			writeError(w, err)
			return
		}
		// La llave sale una sola vez y no debe quedar en la caché del navegador ni de un proxy (P5).
		// El nombre ya pasó la lista permitida de VMs, así que es seguro en la cabecera.
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="id_ed25519_%s"`, name))
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write(key); err != nil {
			log.Printf("no se pudo enviar la llave de %s: %v", name, err)
		}
		clear(key) // la copia en memoria ya no hace falta
	})

	mux.HandleFunc("POST /api/prepare/vms/{name}/issue", func(w http.ResponseWriter, r *http.Request) {
		var in service.IssueInput
		if !decodeJSON(w, r, &in) {
			return
		}
		msg, err := svc.SetIssue(r.Context(), r.PathValue("name"), in)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, messageBody{msg})
	})

	mux.HandleFunc("POST /api/prepare/vms/{name}/hostname", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Hostname string `json:"hostname"`
		}
		if !decodeJSON(w, r, &in) {
			return
		}
		msg, err := svc.SetHostname(r.Context(), r.PathValue("name"), in.Hostname)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, messageBody{msg})
	})

	mux.HandleFunc("GET /api/prepare/history", historyHandler(svc.History))
}

// historyHandler responde con las filas del historial de un módulo; prepare y users comparten la
// misma forma de respuesta.
func historyHandler(history func(ctx context.Context) ([]repository.Operation, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ops, err := history(r.Context())
		if err != nil {
			writeError(w, err)
			return
		}
		rows := make([]operationRow, len(ops))
		for i, op := range ops {
			rows[i] = operationRow{ID: op.ID, Operation: op.Operation, VM: op.VM, Command: op.Command,
				Args: op.Args, ExitCode: op.ExitCode, Stderr: op.Stderr, Result: op.Result,
				CreatedAt: op.CreatedAt.UTC().Format(time.RFC3339)}
		}
		writeJSON(w, http.StatusOK, struct {
			Operations []operationRow `json:"operations"`
		}{rows})
	}
}

// operationRow es una fila del historial tal como la recibe la UI (campos snake_case).
type operationRow struct {
	ID        int64    `json:"id"`
	Operation string   `json:"operation"`
	VM        string   `json:"vm"`
	Command   string   `json:"command"`
	Args      []string `json:"args"`
	ExitCode  int      `json:"exit_code"`
	Stderr    string   `json:"stderr"`
	Result    string   `json:"result"`
	CreatedAt string   `json:"created_at"`
}

// vmAction adapta una operación sin cuerpo sobre la VM {name} de la ruta.
// El servicio valida el nombre, así que aquí solo se pasa tal cual.
func vmAction(action func(ctx context.Context, name string) (string, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		msg, err := action(r.Context(), r.PathValue("name"))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, messageBody{msg})
	}
}

// packageAction adapta instalar o eliminar el paquete {"package": "..."} en la VM {name}.
func packageAction(action func(ctx context.Context, name, pkg string) (string, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Package string `json:"package"`
		}
		if !decodeJSON(w, r, &in) {
			return
		}
		msg, err := action(r.Context(), r.PathValue("name"), in.Package)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, messageBody{msg})
	}
}

// decodeJSON lee el cuerpo en dst; si no es JSON válido responde 400 y devuelve false.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		writeError(w, &service.ValidationError{Message: "El cuerpo de la petición no es JSON válido"})
		return false
	}
	return true
}

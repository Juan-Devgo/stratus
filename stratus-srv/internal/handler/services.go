package handler

import (
	"encoding/json"
	"io"
	"net/http"

	"stratus-srv/internal/service"
)

// RegisterServices registra las rutas del módulo "Servicios systemd" bajo /api/services.
// El servicio valida el nombre de la VM y el del servicio, así que aquí solo se pasan tal cual.
func RegisterServices(mux *http.ServeMux, svc *service.ServicesService) {
	mux.HandleFunc("GET /api/services/vms/{vm}/services", func(w http.ResponseWriter, r *http.Request) {
		services, err := svc.ListServices(r.Context(), r.PathValue("vm"))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, struct {
			Services []service.ServiceView `json:"services"`
		}{services})
	})

	// Últimas 50 operaciones del módulo, la más reciente primero (REQ-28).
	mux.HandleFunc("GET /api/services/history", historyHandler(svc.History))

	mux.HandleFunc("POST /api/services/vms/{vm}/services", func(w http.ResponseWriter, r *http.Request) {
		var in service.CreateServiceInput
		upload, ok := readUpload(w, r, svc, &in)
		if !ok {
			return
		}
		res, err := svc.CreateService(r.Context(), r.PathValue("vm"), in, upload)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, res)
	})

	mux.HandleFunc("POST /api/services/vms/{vm}/services/{name}/redeploy", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Executable string `json:"executable"`
		}
		upload, ok := readUpload(w, r, svc, &in)
		if !ok {
			return
		}
		res, err := svc.RedeployService(r.Context(), r.PathValue("vm"), r.PathValue("name"), in.Executable, upload)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, res)
	})

	mux.HandleFunc("DELETE /api/services/vms/{vm}/services/{name}", func(w http.ResponseWriter, r *http.Request) {
		msg, err := svc.DeleteService(r.Context(), r.PathValue("vm"), r.PathValue("name"))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, messageBody{msg})
	})

	mux.HandleFunc("PUT /api/services/vms/{vm}/services/{name}", func(w http.ResponseWriter, r *http.Request) {
		var in service.EditServiceInput
		if !decodeJSON(w, r, &in) {
			return
		}
		res, err := svc.EditService(r.Context(), r.PathValue("vm"), r.PathValue("name"), in)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, res)
	})

	// Estado y logs devuelven el texto de salida tal cual; no cambian nada en la VM (REQ-19, REQ-20).
	reads := map[string]func(*service.ServicesService, *http.Request) (string, error){
		"status": func(svc *service.ServicesService, r *http.Request) (string, error) {
			return svc.ServiceStatus(r.Context(), r.PathValue("vm"), r.PathValue("name"))
		},
		"logs": func(svc *service.ServicesService, r *http.Request) (string, error) {
			return svc.ServiceLogs(r.Context(), r.PathValue("vm"), r.PathValue("name"))
		},
	}
	for route, read := range reads {
		mux.HandleFunc("GET /api/services/vms/{vm}/services/{name}/"+route, func(w http.ResponseWriter, r *http.Request) {
			output, err := read(svc, r)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, struct {
				Output string `json:"output"`
			}{output})
		})
	}

	// Una ruta por acción de systemctl (REQ-17): start, stop, restart, enable y disable.
	for _, action := range []string{"start", "stop", "restart", "enable", "disable"} {
		mux.HandleFunc("POST /api/services/vms/{vm}/services/{name}/"+action, func(w http.ResponseWriter, r *http.Request) {
			res, err := svc.ServiceAction(r.Context(), r.PathValue("vm"), r.PathValue("name"), action)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, res)
		})
	}
}

// uploadMargin es lo que el cuerpo HTTP puede pesar de más que el archivo: el JSON de "payload" y
// los encabezados multipart.
const uploadMargin = 1 << 20

// readUpload lee el cuerpo multipart de crear y redesplegar (REQ-4). La primera parte debe ser
// "payload" (JSON de hasta 256 KiB, se decodifica en payload) y la segunda "archive"; cualquier
// otro orden o nombre es 400. El archivo no se lee aquí: el servicio lo copia a un archivo
// temporal con su propio límite, así que la parte se entrega como io.Reader. El cuerpo completo
// está limitado a MaxArchiveBytes más un margen.
func readUpload(w http.ResponseWriter, r *http.Request, svc *service.ServicesService, payload any) (service.ArchiveUpload, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, svc.MaxArchiveBytes()+uploadMargin)
	reject := func(message string) (service.ArchiveUpload, bool) {
		writeError(w, &service.ValidationError{Message: message})
		return service.ArchiveUpload{}, false
	}

	reader, err := r.MultipartReader()
	if err != nil {
		return reject("La petición debe ser multipart/form-data con las partes payload y archive")
	}
	first, err := reader.NextPart()
	if err != nil || first.FormName() != "payload" {
		return reject("La primera parte de la petición debe ser payload")
	}
	if err := json.NewDecoder(io.LimitReader(first, maxBodyBytes)).Decode(payload); err != nil {
		return reject("El payload no es JSON válido")
	}
	second, err := reader.NextPart()
	if err != nil || second.FormName() != "archive" {
		return reject("La segunda parte de la petición debe ser archive")
	}
	return service.ArchiveUpload{Filename: second.FileName(), Body: second}, true
}

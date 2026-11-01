package handler

import (
	"net/http"

	"stratus-srv/internal/service"
)

// RegisterWeb registra las rutas del módulo "Servidores web" bajo /api/web.
func RegisterWeb(mux *http.ServeMux, svc *service.WebService) {
	mux.HandleFunc("GET /api/web/servers", func(w http.ResponseWriter, r *http.Request) {
		servers, listErr, err := svc.ListWebServers(r.Context())
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, struct {
			Servers   []service.WebServerView `json:"servers"`
			ListError string                  `json:"list_error"`
		}{servers, listErr})
	})

	mux.HandleFunc("POST /api/web/servers", func(w http.ResponseWriter, r *http.Request) {
		var in service.CreateWebInput
		if !decodeJSON(w, r, &in) {
			return
		}
		res, err := svc.CreateWeb(r.Context(), in)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, res)
	})

	mux.HandleFunc("DELETE /api/web/servers/{vm}", func(w http.ResponseWriter, r *http.Request) {
		res, err := svc.DeleteWeb(r.Context(), r.PathValue("vm"))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, res)
	})

	mux.HandleFunc("POST /api/web/servers/{vm}/page", func(w http.ResponseWriter, r *http.Request) {
		archive, ok := readArchive(w, r, svc.MaxArchiveBytes())
		if !ok {
			return
		}
		msg, err := svc.InstallPage(r.Context(), r.PathValue("vm"), archive)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, messageBody{msg})
	})

	// Últimas 50 operaciones del módulo, la más reciente primero (REQ-36).
	mux.HandleFunc("GET /api/web/history", historyHandler(svc.History))
}

// readArchive lee el cuerpo multipart de instalar una página (REQ-9): una sola parte "archive";
// cualquier otro nombre es 400. Como en 003, el archivo no se lee aquí: el servicio lo copia a un
// archivo temporal con su propio límite, y el cuerpo completo está limitado a maxBytes más un margen.
func readArchive(w http.ResponseWriter, r *http.Request, maxBytes int64) (service.ArchiveUpload, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes+uploadMargin)
	reader, err := r.MultipartReader()
	if err != nil {
		writeError(w, &service.ValidationError{Message: "La petición debe ser multipart/form-data con la parte archive"})
		return service.ArchiveUpload{}, false
	}
	p, err := reader.NextPart()
	if err != nil || p.FormName() != "archive" {
		writeError(w, &service.ValidationError{Message: "La petición debe tener la parte archive"})
		return service.ArchiveUpload{}, false
	}
	return service.ArchiveUpload{Filename: p.FileName(), Body: p}, true
}

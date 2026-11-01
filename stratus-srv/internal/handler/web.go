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

	// Últimas 50 operaciones del módulo, la más reciente primero (REQ-36).
	mux.HandleFunc("GET /api/web/history", historyHandler(svc.History))
}

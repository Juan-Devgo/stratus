package handler

import (
	"net/http"

	"stratus-srv/internal/service"
)

// RegisterDNS registra las rutas del módulo "DNS" bajo /api/dns.
func RegisterDNS(mux *http.ServeMux, svc *service.DNSService) {
	mux.HandleFunc("GET /api/dns", func(w http.ResponseWriter, r *http.Request) {
		view, err := svc.Overview(r.Context())
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	})

	mux.HandleFunc("POST /api/dns/server", func(w http.ResponseWriter, r *http.Request) {
		var in service.CreateVMInput
		if !decodeJSON(w, r, &in) {
			return
		}
		msg, err := svc.CreateDNS(r.Context(), in)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, messageBody{msg})
	})

	mux.HandleFunc("DELETE /api/dns/server", func(w http.ResponseWriter, r *http.Request) {
		msg, err := svc.DeleteDNS(r.Context())
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, messageBody{msg})
	})

	// Una ruta por acción de systemctl sobre bind9 (REQ-12): start, stop y restart.
	for _, action := range []string{"start", "stop", "restart"} {
		mux.HandleFunc("POST /api/dns/server/"+action, func(w http.ResponseWriter, r *http.Request) {
			res, err := svc.DNSAction(r.Context(), action)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, res)
		})
	}

	mux.HandleFunc("GET /api/dns/server/status", func(w http.ResponseWriter, r *http.Request) {
		output, err := svc.DNSStatus(r.Context())
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, struct {
			Output string `json:"output"`
		}{output})
	})

	// Últimas 50 operaciones del módulo, la más reciente primero (REQ-36).
	mux.HandleFunc("GET /api/dns/history", historyHandler(svc.History))
}

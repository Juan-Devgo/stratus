package handler

import (
	"net/http"

	"stratus-srv/internal/service"
)

// RegisterHAProxy registra las rutas del módulo "Backends HAProxy" bajo /api/haproxy.
func RegisterHAProxy(mux *http.ServeMux, svc *service.HAProxyService) {
	mux.HandleFunc("GET /api/haproxy/lbs", func(w http.ResponseWriter, r *http.Request) {
		lbs, listErr, err := svc.ListLBs(r.Context())
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, struct {
			LBs       []service.LBView `json:"lbs"`
			ListError string           `json:"list_error"`
		}{lbs, listErr})
	})

	mux.HandleFunc("POST /api/haproxy/lbs", func(w http.ResponseWriter, r *http.Request) {
		var in service.CreateVMInput
		if !decodeJSON(w, r, &in) {
			return
		}
		msg, err := svc.CreateLB(r.Context(), in)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, messageBody{msg})
	})

	mux.HandleFunc("DELETE /api/haproxy/lbs/{lb}", func(w http.ResponseWriter, r *http.Request) {
		msg, err := svc.DeleteLB(r.Context(), r.PathValue("lb"))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, messageBody{msg})
	})

	mux.HandleFunc("GET /api/haproxy/lbs/{lb}/servers", func(w http.ResponseWriter, r *http.Request) {
		servers, err := svc.ListServers(r.Context(), r.PathValue("lb"))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, struct {
			Servers []service.ServerView `json:"servers"`
		}{servers})
	})

	mux.HandleFunc("POST /api/haproxy/lbs/{lb}/servers", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			VM   string `json:"vm"`
			Port int    `json:"port"`
		}
		if !decodeJSON(w, r, &in) {
			return
		}
		msg, err := svc.AssignServer(r.Context(), r.PathValue("lb"), in.VM, in.Port)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, messageBody{msg})
	})

	mux.HandleFunc("DELETE /api/haproxy/lbs/{lb}/servers/{vm}", func(w http.ResponseWriter, r *http.Request) {
		msg, err := svc.RemoveServer(r.Context(), r.PathValue("lb"), r.PathValue("vm"))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, messageBody{msg})
	})

	mux.HandleFunc("PUT /api/haproxy/lbs/{lb}/frontend", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Port int `json:"port"`
		}
		if !decodeJSON(w, r, &in) {
			return
		}
		msg, err := svc.SaveFrontend(r.Context(), r.PathValue("lb"), in.Port)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, messageBody{msg})
	})

	mux.HandleFunc("POST /api/haproxy/lbs/{lb}/refresh", func(w http.ResponseWriter, r *http.Request) {
		msg, err := svc.Refresh(r.Context(), r.PathValue("lb"))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, messageBody{msg})
	})

	mux.HandleFunc("GET /api/haproxy/lbs/{lb}/status", func(w http.ResponseWriter, r *http.Request) {
		states, err := svc.ServerStatus(r.Context(), r.PathValue("lb"))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, struct {
			States map[string]string `json:"states"`
		}{states})
	})

	// Últimas 50 operaciones del módulo, la más reciente primero (REQ-24).
	mux.HandleFunc("GET /api/haproxy/history", historyHandler(svc.History))
}

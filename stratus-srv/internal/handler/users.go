package handler

import (
	"fmt"
	"log"
	"net/http"

	"stratus-srv/internal/service"
)

// RegisterUsers registra las rutas del módulo "Gestión de usuarios" bajo /api/users.
// El servicio valida el nombre de la VM y el de usuario, así que aquí solo se pasan tal cual.
func RegisterUsers(mux *http.ServeMux, svc *service.UsersService) {
	mux.HandleFunc("GET /api/users/vms/{vm}/users", func(w http.ResponseWriter, r *http.Request) {
		users, err := svc.ListUsers(r.Context(), r.PathValue("vm"))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, struct {
			Users []service.UserView `json:"users"`
		}{users})
	})

	mux.HandleFunc("POST /api/users/vms/{vm}/users", func(w http.ResponseWriter, r *http.Request) {
		var in service.CreateUserInput
		if !decodeJSON(w, r, &in) {
			return
		}
		msg, keyAvailable, err := svc.CreateUser(r.Context(), r.PathValue("vm"), in)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, struct {
			Message      string `json:"message"`
			KeyAvailable bool   `json:"key_available"`
		}{msg, keyAvailable})
	})

	mux.HandleFunc("GET /api/users/vms/{vm}/users/{username}/key/download", func(w http.ResponseWriter, r *http.Request) {
		vm, username := r.PathValue("vm"), r.PathValue("username")
		key, err := svc.DownloadUserKey(vm, username)
		if err != nil {
			writeError(w, err)
			return
		}
		// La llave sale una sola vez y no debe quedar en la caché del navegador ni de un proxy (P5).
		// Ambos nombres ya pasaron la lista permitida, así que son seguros en la cabecera.
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="id_ed25519_%s_%s"`, vm, username))
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write(key); err != nil {
			log.Printf("no se pudo enviar la llave de %s en %s: %v", username, vm, err)
		}
		clear(key) // la copia en memoria ya no hace falta
	})

	mux.HandleFunc("DELETE /api/users/vms/{vm}/users/{username}", func(w http.ResponseWriter, r *http.Request) {
		// remove_files ausente es false; cualquier valor distinto de true o false es un error.
		removeFiles := false
		if q := r.URL.Query(); q.Has("remove_files") {
			switch q.Get("remove_files") {
			case "true":
				removeFiles = true
			case "false":
			default:
				writeError(w, &service.ValidationError{Message: "remove_files debe ser true o false"})
				return
			}
		}
		msg, err := svc.DeleteUser(r.Context(), r.PathValue("vm"), r.PathValue("username"), removeFiles)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, messageBody{msg})
	})

	mux.HandleFunc("GET /api/users/history", historyHandler(svc.History))
}

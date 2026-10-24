package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"stratus-srv/internal/service"
)

func TestAC4_AC6_AC7_AC13_WriteErrorMapsServiceErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantError  string
	}{
		{"nombre en uso", service.ErrServiceExists, http.StatusConflict, "Ya existe un servicio o usuario con ese nombre en la VM"},
		{"no administrado", service.ErrNotManagedService, http.StatusConflict, "Solo se pueden gestionar servicios creados por Stratus"},
		{"archivo inválido", service.ErrInvalidArchive, http.StatusUnprocessableEntity, "Archivo comprimido inválido"},
		{"ejecutable ausente", service.ErrExecutableMissing, http.StatusUnprocessableEntity, "El ejecutable no existe en el directorio de la aplicación"},
		{"envuelto", fmt.Errorf("crear: %w", service.ErrServiceExists), http.StatusConflict, "Ya existe un servicio o usuario con ese nombre en la VM"},
		{"limpieza fallida", &service.ServiceRollbackError{Operation: "create", Name: "api", Detail: "Limpieza fallida; puede quedar en la VM: api"},
			http.StatusInternalServerError, "No se pudo crear el servicio api y la limpieza falló; revise la VM. Limpieza fallida; puede quedar en la VM: api"},
		{"restauración fallida", &service.ServiceRollbackError{Operation: "edit", Name: "api", Detail: "x"},
			http.StatusInternalServerError, "No se pudo editar el servicio api y la restauración falló; revise la VM. x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			writeError(rec, tt.err)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, se esperaba %d", rec.Code, tt.wantStatus)
			}
			var body struct {
				Error string `json:"error"`
			}
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil || body.Error != tt.wantError {
				t.Errorf("error = %q (%v), se esperaba %q", body.Error, err, tt.wantError)
			}
		})
	}
}

// AC-15, AC-24, AC-29: los errores del módulo HAProxy se traducen a su código y texto.
func TestAC15_AC24_AC29_WriteErrorMapsHAProxyErrors(t *testing.T) {
	lbOff := "El balanceador lb1 está apagado; enciéndalo para llegar al servidor"
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantError  string
	}{
		{"balanceador inexistente", service.ErrLBNotFound, http.StatusNotFound, "El balanceador no existe"},
		{"balanceador apagado", &service.LBOffError{Message: lbOff}, http.StatusConflict, lbOff},
		{"balanceador apagado envuelto", fmt.Errorf("install_package en web-a: %w", &service.LBOffError{Message: lbOff}), http.StatusConflict, lbOff},
		{"ErrLBOff sin nombre", service.ErrLBOff, http.StatusConflict, "El balanceador está apagado; enciéndalo para llegar al servidor"},
		{"con servidores", service.ErrLBHasServers, http.StatusConflict, "Quite primero los servidores del balanceador"},
		{"lleno", service.ErrLBFull, http.StatusConflict, "El balanceador ya tiene 10 servidores"},
		{"VM no asignable", service.ErrVMNotAssignable, http.StatusConflict, "La VM no se puede asignar a este balanceador"},
		{"VM es servidor", service.ErrVMIsServer, http.StatusConflict, "La VM es servidor de un balanceador; quítela primero"},
		{"haproxy bloqueado", service.ErrHAProxyRemovalBlocked, http.StatusConflict, "No se puede eliminar haproxy: el balanceador lo usa"},
		{"sin subredes", service.ErrNoFreeSubnet, http.StatusConflict, "No hay subredes internas libres"},
		{"red interna registrada", service.ErrIntnetExists, http.StatusConflict, "Red interna ya registrada en VirtualBox"},
		{"configuración inválida", service.ErrHAProxyConfigInvalid, http.StatusInternalServerError, "Configuración de HAProxy inválida; se mantuvo la anterior"},
		{"recarga fallida", service.ErrHAProxyReloadFailed, http.StatusInternalServerError, "No se pudo recargar HAProxy; se mantuvo la configuración anterior"},
		{"jump no actualizada", service.ErrJumpUpdateFailed, http.StatusInternalServerError, "No se pudo actualizar la cuenta jump del balanceador; se revirtió el cambio en el servidor"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			writeError(rec, tt.err)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, se esperaba %d", rec.Code, tt.wantStatus)
			}
			var body struct {
				Error string `json:"error"`
			}
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil || body.Error != tt.wantError {
				t.Errorf("error = %q (%v), se esperaba %q", body.Error, err, tt.wantError)
			}
		})
	}
}

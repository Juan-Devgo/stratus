package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
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

// AC-23, AC-29, AC-35, AC-41: los errores de los módulos de servidores web y DNS se traducen a su
// código y texto (REQ-15, REQ-21, REQ-25, REQ-28, REQ-38).
func TestAC23_AC29_AC35_AC41_WriteErrorMapsHTTPaaSErrors(t *testing.T) {
	blocked := "No se puede eliminar nginx: el servidor web lo usa"
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantError  string
	}{
		{"disco de Apache", service.ErrApacheBaseDiskMissing, http.StatusConflict, "Disco base de Apache no encontrado"},
		{"disco de Nginx", service.ErrNginxBaseDiskMissing, http.StatusConflict, "Disco base de Nginx no encontrado"},
		{"disco de DNS", service.ErrDNSBaseDiskMissing, http.StatusConflict, "Disco base de DNS no encontrado"},
		{"DNS ya existe", service.ErrDNSExists, http.StatusConflict, "Ya existe un servidor DNS"},
		{"sin servidor DNS", service.ErrNoDNSServer, http.StatusConflict, "No hay servidor DNS"},
		{"configuración incompleta", service.ErrDNSConfigIncomplete, http.StatusConflict, "Configuración de DNS incompleta"},
		{"IP en uso", service.ErrDNSIPInUse, http.StatusConflict, "La IP del servidor DNS ya está en uso"},
		{"DNS apagado", fmt.Errorf("add_records: %w", service.ErrDNSOff), http.StatusConflict, "El servidor DNS está apagado; enciéndalo para cambiar los registros"},
		{"DNS apagado al leer la zona", fmt.Errorf("show_zone: %w", service.ErrDNSOffZone), http.StatusConflict, "El servidor DNS está apagado; enciéndalo para ver la zona"},
		{"registro existente", service.ErrRecordExists, http.StatusConflict, "La VM ya tiene un registro DNS"},
		{"sin index.html", service.ErrIndexMissing, http.StatusUnprocessableEntity, "El archivo debe contener index.html en la raíz"},
		{"zona inválida", service.ErrZoneInvalid, http.StatusInternalServerError, "Zona DNS inválida; se mantuvo la anterior"},
		{"paquete protegido", &service.PackageBlockedError{Message: blocked}, http.StatusConflict, blocked},
		{"sin dirección en la LAN", fmt.Errorf("add_records: %w", service.ErrVMNotOnLAN), http.StatusConflict, "La VM no tiene una dirección en la LAN"},
		{"servidor de un balanceador", &service.ServerRecordError{LB: "lb1"}, http.StatusConflict, "La VM es servidor del balanceador lb1; registre el balanceador"},
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

// AC-71: una petición que el navegador abandonó no se registra como error interno ni recibe
// respuesta (REQ-50); otro error desconocido sí da 500 y la línea del registro.
func TestAC71_CancelledRequestNotLogged(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantLogged bool
		wantStatus int
	}{
		{"cancelada", fmt.Errorf("obtener servidor DNS: %w", context.Canceled), false, http.StatusOK},
		{"otro error", fmt.Errorf("obtener servidor DNS: %w", errors.New("disco lleno")), true, http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			log.SetOutput(&buf)
			t.Cleanup(func() { log.SetOutput(os.Stderr) })

			rec := httptest.NewRecorder()
			writeError(rec, tt.err)

			if got := strings.Contains(buf.String(), "error interno"); got != tt.wantLogged {
				t.Errorf("registro = %q; se esperaba la línea = %v", buf.String(), tt.wantLogged)
			}
			// httptest.ResponseRecorder queda en 200 y sin cuerpo si no se escribió nada.
			if rec.Code != tt.wantStatus {
				t.Errorf("código = %d; se esperaba %d", rec.Code, tt.wantStatus)
			}
			if !tt.wantLogged && rec.Body.Len() != 0 {
				t.Errorf("cuerpo = %q; se esperaba vacío", rec.Body.String())
			}
		})
	}
}

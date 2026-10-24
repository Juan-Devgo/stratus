// Package handler traduce HTTP a llamadas del servicio y sus errores a códigos y mensajes en español.
package handler

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"stratus-srv/internal/service"
)

// messageBody y errorBody son las dos formas de respuesta de las operaciones (P6).
type messageBody struct {
	Message string `json:"message"`
}

type errorBody struct {
	Error string `json:"error"`
}

// writeJSON escribe v como JSON con el código indicado.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("no se pudo escribir la respuesta: %v", err)
	}
}

// sentinelStatus asocia cada error centinela con su código HTTP y el texto que ve el usuario.
var sentinelStatus = []struct {
	err     error
	status  int
	message string
}{
	{service.ErrVMNotFound, http.StatusNotFound, service.ErrVMNotFound.Error()},
	{service.ErrDuplicateVM, http.StatusConflict, service.ErrDuplicateVM.Error()},
	{service.ErrOperationInProgress, http.StatusConflict, service.ErrOperationInProgress.Error()},
	{service.ErrSSHRemovalBlocked, http.StatusConflict, service.ErrSSHRemovalBlocked.Error()},
	{service.ErrVMNoIP, http.StatusConflict, service.ErrVMNoIP.Error()},
	{service.ErrCreateInProgress, http.StatusConflict, service.ErrCreateInProgress.Error()},
	{service.ErrKeyUnavailable, http.StatusGone, service.ErrKeyUnavailable.Error()},
	{service.ErrUserExists, http.StatusConflict, service.ErrUserExists.Error()},
	{service.ErrGroupExists, http.StatusConflict, service.ErrGroupExists.Error()},
	{service.ErrNotRegularUser, http.StatusConflict, service.ErrNotRegularUser.Error()},
	{service.ErrUserKeyUnavailable, http.StatusGone, service.ErrUserKeyUnavailable.Error()},
	{service.ErrServiceExists, http.StatusConflict, service.ErrServiceExists.Error()},
	{service.ErrNotManagedService, http.StatusConflict, service.ErrNotManagedService.Error()},
	{service.ErrInvalidArchive, http.StatusUnprocessableEntity, service.ErrInvalidArchive.Error()},
	{service.ErrExecutableMissing, http.StatusUnprocessableEntity, service.ErrExecutableMissing.Error()},
	// Módulo de HAProxy (004).
	{service.ErrLBNotFound, http.StatusNotFound, service.ErrLBNotFound.Error()},
	{service.ErrLBOff, http.StatusConflict, service.ErrLBOff.Error()},
	{service.ErrLBHasServers, http.StatusConflict, service.ErrLBHasServers.Error()},
	{service.ErrLBFull, http.StatusConflict, service.ErrLBFull.Error()},
	{service.ErrVMNotAssignable, http.StatusConflict, service.ErrVMNotAssignable.Error()},
	{service.ErrVMIsServer, http.StatusConflict, service.ErrVMIsServer.Error()},
	{service.ErrHAProxyRemovalBlocked, http.StatusConflict, service.ErrHAProxyRemovalBlocked.Error()},
	{service.ErrNoFreeSubnet, http.StatusConflict, service.ErrNoFreeSubnet.Error()},
	{service.ErrIntnetExists, http.StatusConflict, service.ErrIntnetExists.Error()},
	{service.ErrHAProxyConfigInvalid, http.StatusInternalServerError, service.ErrHAProxyConfigInvalid.Error()},
	{service.ErrHAProxyReloadFailed, http.StatusInternalServerError, service.ErrHAProxyReloadFailed.Error()},
	{service.ErrJumpUpdateFailed, http.StatusInternalServerError, service.ErrJumpUpdateFailed.Error()},
	{service.ErrTimeout, http.StatusGatewayTimeout, "La operación superó el tiempo límite de 15 minutos"},
}

// writeError traduce el error del servicio a un código HTTP y un mensaje comprensible (REQ-16).
// Los errores inesperados solo se detallan en el log del servidor, nunca en la respuesta.
func writeError(w http.ResponseWriter, err error) {
	var validation *service.ValidationError
	if errors.As(err, &validation) {
		writeJSON(w, http.StatusBadRequest, errorBody{validation.Message})
		return
	}
	// Balanceador apagado: el texto del script nombra el balanceador, por eso va antes que ErrLBOff.
	var lbOff *service.LBOffError
	if errors.As(err, &lbOff) {
		writeJSON(w, http.StatusConflict, errorBody{lbOff.Message})
		return
	}
	if errors.Is(err, service.ErrInvalidInput) {
		writeJSON(w, http.StatusBadRequest, errorBody{"Entrada inválida"})
		return
	}
	for _, s := range sentinelStatus {
		if errors.Is(err, s.err) {
			writeJSON(w, s.status, errorBody{s.message})
			return
		}
	}
	// Creación fallida con limpieza fallida: el texto nombra lo que puede quedar en la VM.
	var cleanup *service.CleanupError
	if errors.As(err, &cleanup) {
		writeJSON(w, http.StatusInternalServerError, errorBody{cleanup.Error()})
		return
	}
	// Edición, redespliegue o creación de un servicio fallida con restauración fallida.
	var rollback *service.ServiceRollbackError
	if errors.As(err, &rollback) {
		writeJSON(w, http.StatusInternalServerError, errorBody{rollback.Error()})
		return
	}
	var operation *service.OperationError
	if errors.As(err, &operation) {
		writeJSON(w, http.StatusInternalServerError, errorBody{operation.Error()})
		return
	}
	log.Printf("error interno: %v", err)
	writeJSON(w, http.StatusInternalServerError, errorBody{"Error interno, revise el registro del servidor"})
}

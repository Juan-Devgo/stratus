package service

import (
	"errors"
	"fmt"
	"testing"
)

func TestAC8_ErrorTypesUnwrapToSentinels(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		target  error
		wantMsg string
	}{
		{
			name:    "ValidationError es ErrInvalidInput",
			err:     &ValidationError{Message: "El nombre de la VM no es válido"},
			target:  ErrInvalidInput,
			wantMsg: "El nombre de la VM no es válido",
		},
		{
			name:    "OperationError es ErrScriptFailed",
			err:     &OperationError{Operation: "stop_vm", ExitCode: 99, Detail: "Operación no implementada"},
			target:  ErrScriptFailed,
			wantMsg: "La operación stop_vm falló (código 99): Operación no implementada",
		},
		{
			name:    "envuelto con fmt.Errorf sigue siendo reconocible",
			err:     fmt.Errorf("crear vm: %w", &ValidationError{Message: "x"}),
			target:  ErrInvalidInput,
			wantMsg: "crear vm: x",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if !errors.Is(tc.err, tc.target) {
				t.Errorf("errors.Is(%v, %v) = false", tc.err, tc.target)
			}
			if got := tc.err.Error(); got != tc.wantMsg {
				t.Errorf("Error() = %q, se esperaba %q", got, tc.wantMsg)
			}
		})
	}
}

func TestAC13_ServiceRollbackErrorMessages(t *testing.T) {
	tests := []struct {
		operation string
		want      string
	}{
		{"create", "No se pudo crear el servicio api y la limpieza falló; revise la VM. detalle"},
		{"edit", "No se pudo editar el servicio api y la restauración falló; revise la VM. detalle"},
		{"redeploy", "No se pudo redesplegar el servicio api y la restauración falló; revise la VM. detalle"},
	}
	for _, tc := range tests {
		t.Run(tc.operation, func(t *testing.T) {
			err := error(&ServiceRollbackError{Operation: tc.operation, Name: "api", Detail: "detalle"})
			if got := err.Error(); got != tc.want {
				t.Errorf("Error() = %q, se esperaba %q", got, tc.want)
			}
			if !errors.Is(err, ErrScriptFailed) {
				t.Errorf("errors.Is(%v, ErrScriptFailed) = false", err)
			}
		})
	}
}

// AC-29: LBOffError muestra la línea de stderr del script y equivale a ErrLBOff.
func TestAC29_LBOffError(t *testing.T) {
	msg := "El balanceador lb1 está apagado; enciéndalo para llegar al servidor"
	err := error(&LBOffError{Message: msg})
	if got := err.Error(); got != msg {
		t.Errorf("Error() = %q, se esperaba %q", got, msg)
	}
	if !errors.Is(err, ErrLBOff) {
		t.Error("errors.Is(*LBOffError, ErrLBOff) = false")
	}
	wrapped := fmt.Errorf("install_package en web-a: %w", err)
	var off *LBOffError
	if !errors.As(wrapped, &off) || off.Message != msg {
		t.Errorf("errors.As sobre el error envuelto falló: %v", wrapped)
	}
}

// REQ-3, REQ-6, REQ-8, REQ-12, REQ-16, REQ-17: los textos de los centinelas del módulo 004 son los de la especificación.
func TestAC29_HAProxySentinelTexts(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{ErrLBOff, "El balanceador está apagado; enciéndalo para llegar al servidor"},
		{ErrLBNotFound, "El balanceador no existe"},
		{ErrLBHasServers, "Quite primero los servidores del balanceador"},
		{ErrLBFull, "El balanceador ya tiene 10 servidores"},
		{ErrVMNotAssignable, "La VM no se puede asignar a este balanceador"},
		{ErrVMIsServer, "La VM es servidor de un balanceador; quítela primero"},
		{ErrHAProxyRemovalBlocked, "No se puede eliminar haproxy: el balanceador lo usa"},
		{ErrNoFreeSubnet, "No hay subredes internas libres"},
		{ErrIntnetExists, "Red interna ya registrada en VirtualBox"},
		{ErrHAProxyConfigInvalid, "Configuración de HAProxy inválida; se mantuvo la anterior"},
		{ErrHAProxyReloadFailed, "No se pudo recargar HAProxy; se mantuvo la configuración anterior"},
		{ErrJumpUpdateFailed, "No se pudo actualizar la cuenta jump del balanceador; se revirtió el cambio en el servidor"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, se esperaba %q", got, tt.want)
			}
		})
	}
}

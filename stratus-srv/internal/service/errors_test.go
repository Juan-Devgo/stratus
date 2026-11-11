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

// Módulo 005: los textos de los centinelas son los de la especificación (REQ-3, REQ-9, REQ-13,
// REQ-15, REQ-23, REQ-25, REQ-28).
func TestAC23_HTTPaaSSentinelTexts(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{ErrApacheBaseDiskMissing, "Disco base de Apache no encontrado"},
		{ErrNginxBaseDiskMissing, "Disco base de Nginx no encontrado"},
		{ErrDNSBaseDiskMissing, "Disco base de DNS no encontrado"},
		{ErrDNSExists, "Ya existe un servidor DNS"},
		{ErrNoDNSServer, "No hay servidor DNS"},
		{ErrDNSConfigIncomplete, "Configuración de DNS incompleta"},
		{ErrDNSIPInUse, "La IP del servidor DNS ya está en uso"},
		{ErrDNSOff, "El servidor DNS está apagado; enciéndalo para cambiar los registros"},
		{ErrZoneInvalid, "Zona DNS inválida; se mantuvo la anterior"},
		{ErrRecordExists, "La VM ya tiene un registro DNS"},
		{ErrIndexMissing, "El archivo debe contener index.html en la raíz"},
		{ErrVMNotOnLAN, "La VM no tiene una dirección en la LAN"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, se esperaba %q", got, tt.want)
			}
		})
	}
}

// AC-9, AC-28: PackageBlockedError muestra su texto y equivale a ErrPackageBlocked.
func TestAC9_PackageBlockedError(t *testing.T) {
	msg := "No se puede eliminar nginx: el servidor web lo usa"
	err := fmt.Errorf("remove_package: %w", &PackageBlockedError{Message: msg})
	if got := err.Error(); got != "remove_package: "+msg {
		t.Errorf("Error() = %q", got)
	}
	if !errors.Is(err, ErrPackageBlocked) {
		t.Error("errors.Is(*PackageBlockedError, ErrPackageBlocked) = false")
	}
}

// AC-31: ServerRecordError nombra el balanceador del que la VM es servidor (REQ-23).
func TestAC31_ServerRecordError(t *testing.T) {
	err := fmt.Errorf("add_records: %w", &ServerRecordError{LB: "lb1"})
	want := "add_records: La VM es servidor del balanceador lb1; registre el balanceador"
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, se esperaba %q", got, want)
	}
	var target *ServerRecordError
	if !errors.As(err, &target) || target.LB != "lb1" {
		t.Error("errors.As no recuperó *ServerRecordError")
	}
}

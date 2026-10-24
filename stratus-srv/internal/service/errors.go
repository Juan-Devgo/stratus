package service

import (
	"errors"
	"fmt"
)

// Errores centinela del servicio. El handler los traduce a un código HTTP y su texto
// se muestra tal cual en la UI, por eso están en español y con mayúscula inicial.
var (
	ErrInvalidInput = errors.New("entrada inválida")
	// ErrDuplicateVM es lo que el servicio devuelve cuando el repositorio responde repository.ErrDuplicateVM.
	ErrDuplicateVM = errors.New("Ya existe una VM con ese nombre")
	// ErrVMNotFound es lo que el servicio devuelve cuando el repositorio responde repository.ErrVMNotFound.
	ErrVMNotFound          = errors.New("La VM no existe")
	ErrOperationInProgress = errors.New("Operación en curso en esta VM")
	ErrSSHRemovalBlocked   = errors.New("No se puede eliminar openssh-server: el backend lo usa para conectarse")
	ErrKeyUnavailable      = errors.New("La llave ya no está disponible, genere una nueva")
	ErrVMNoIP              = errors.New("La VM no tiene dirección IP; verifique que esté encendida")
	ErrCreateInProgress    = errors.New("Ya hay una creación de VM en curso")
	ErrScriptFailed        = errors.New("la operación falló")
	ErrTimeout             = errors.New("la operación superó el tiempo límite de 15 minutos")

	// Módulo de usuarios (002).
	ErrUserExists         = errors.New("El usuario ya existe en la VM")
	ErrGroupExists        = errors.New("Ya existe un grupo con ese nombre en la VM")
	ErrNotRegularUser     = errors.New("Solo se pueden eliminar usuarios regulares existentes")
	ErrUserKeyUnavailable = errors.New("La llave ya no está disponible; elimine y vuelva a crear el usuario para obtener una nueva")
)

// ValidationError describe una entrada rechazada por la lista permitida.
// Message es el texto en español que ve el usuario.
type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string { return e.Message }

// Unwrap permite usar errors.Is(err, ErrInvalidInput).
func (e *ValidationError) Unwrap() error { return ErrInvalidInput }

// OperationError describe un script que terminó con código distinto de cero.
// Detail es la última línea no vacía de stderr (ya enmascarada), de máximo 200 caracteres.
type OperationError struct {
	Operation string
	ExitCode  int
	Detail    string
}

func (e *OperationError) Error() string {
	return fmt.Sprintf("La operación %s falló (código %d): %s", e.Operation, e.ExitCode, e.Detail)
}

// Unwrap permite usar errors.Is(err, ErrScriptFailed).
func (e *OperationError) Unwrap() error { return ErrScriptFailed }

// CleanupError indica que create-user.sh falló después de crear la cuenta y que la limpieza
// también falló (exit 9): puede quedar una cuenta parcial en la VM (REQ-12).
// Detail es la última línea de stderr, que nombra lo que puede quedar.
type CleanupError struct {
	Username string
	Detail   string
}

func (e *CleanupError) Error() string {
	return fmt.Sprintf("No se pudo crear el usuario %s y la limpieza falló; revise la VM. %s", e.Username, e.Detail)
}

// Unwrap permite usar errors.Is(err, ErrScriptFailed).
func (e *CleanupError) Unwrap() error { return ErrScriptFailed }

// Módulo de servicios systemd (003).
var (
	ErrServiceExists     = errors.New("Ya existe un servicio o usuario con ese nombre en la VM")
	ErrNotManagedService = errors.New("Solo se pueden gestionar servicios creados por Stratus")
	ErrInvalidArchive    = errors.New("Archivo comprimido inválido")
	ErrExecutableMissing = errors.New("El ejecutable no existe en el directorio de la aplicación")
)

// ServiceRollbackError indica que una operación de servicios (create, edit o redeploy) falló y
// que su limpieza o restauración también falló (exit 9): puede quedar un estado parcial en la VM
// (REQ-14, REQ-23, REQ-24). Detail es la última línea de stderr, que nombra lo que puede quedar.
type ServiceRollbackError struct {
	Operation string // "create", "edit" o "redeploy"
	Name      string
	Detail    string
}

func (e *ServiceRollbackError) Error() string {
	switch e.Operation {
	case "edit":
		return fmt.Sprintf("No se pudo editar el servicio %s y la restauración falló; revise la VM. %s", e.Name, e.Detail)
	case "redeploy":
		return fmt.Sprintf("No se pudo redesplegar el servicio %s y la restauración falló; revise la VM. %s", e.Name, e.Detail)
	}
	return fmt.Sprintf("No se pudo crear el servicio %s y la limpieza falló; revise la VM. %s", e.Name, e.Detail)
}

// Unwrap permite usar errors.Is(err, ErrScriptFailed).
func (e *ServiceRollbackError) Unwrap() error { return ErrScriptFailed }

// Módulo de HAProxy (004).
var (
	ErrLBOff                 = errors.New("El balanceador está apagado; enciéndalo para llegar al servidor")
	ErrLBNotFound            = errors.New("El balanceador no existe")
	ErrLBHasServers          = errors.New("Quite primero los servidores del balanceador")
	ErrLBFull                = errors.New("El balanceador ya tiene 10 servidores")
	ErrVMNotAssignable       = errors.New("La VM no se puede asignar a este balanceador")
	ErrVMIsServer            = errors.New("La VM es servidor de un balanceador; quítela primero")
	ErrHAProxyRemovalBlocked = errors.New("No se puede eliminar haproxy: el balanceador lo usa")
	ErrNoFreeSubnet          = errors.New("No hay subredes internas libres")
	ErrIntnetExists          = errors.New("Red interna ya registrada en VirtualBox")
	ErrHAProxyConfigInvalid  = errors.New("Configuración de HAProxy inválida; se mantuvo la anterior")
	ErrHAProxyReloadFailed   = errors.New("No se pudo recargar HAProxy; se mantuvo la configuración anterior")
	ErrJumpUpdateFailed      = errors.New("No se pudo actualizar la cuenta jump del balanceador; se revirtió el cambio en el servidor")
)

// LBOffError indica que un script de invitado salió con 12: el balanceador por el que se llega a un
// servidor está apagado o sin IPv4 en puente (REQ-19). Message es la línea de stderr, que nombra el
// balanceador ("El balanceador lb1 está apagado; ...").
type LBOffError struct {
	Message string
}

func (e *LBOffError) Error() string { return e.Message }

// Unwrap permite usar errors.Is(err, ErrLBOff).
func (e *LBOffError) Unwrap() error { return ErrLBOff }

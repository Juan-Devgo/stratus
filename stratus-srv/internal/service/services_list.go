package service

import (
	"context"
	"errors"
	"strconv"
	"strings"
)

// ServiceView es un servicio creado por Stratus tal como lo recibe la UI (REQ-16). EnvKeys solo
// trae las claves del archivo de variables: los valores nunca salen de la VM (REQ-13).
type ServiceView struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	ActiveState  string   `json:"active_state"`
	EnabledState string   `json:"enabled_state"`
	Restart      string   `json:"restart"`
	RestartSec   int      `json:"restart_sec"`
	Executable   string   `json:"executable"`
	Args         string   `json:"args"`
	EnvKeys      []string `json:"env_keys"`
}

var errUnexpectedServicesList = errors.New("salida inesperada de list-services.sh")

// ListServices lee los servicios de Stratus de la VM con list-services.sh. Toma el bloqueo de la
// VM como cualquier operación, pero un listado correcto no deja fila en el historial: solo los
// fallos se registran (REQ-27).
func (s *ServicesService) ListServices(ctx context.Context, vm string) ([]ServiceView, error) {
	v, err := s.getVM(ctx, vm)
	if err != nil {
		return nil, err
	}
	args, _, err := targetArgs(ctx, s.lbs, v.Name)
	if err != nil {
		return nil, err
	}
	res, err := s.ops.run(ctx, "list_services", v.Name, "services/list-services.sh", args, false, nil)
	if err != nil {
		return nil, err
	}
	return parseServices(string(res.Stdout))
}

// serviceFields es el número de campos de cada fila de list-services.sh.
const serviceFields = 9

// parseServices convierte las filas "name⇥active⇥enabled⇥restart⇥restart_sec⇥executable⇥args⇥
// env_keys⇥description" de list-services.sh. Los campos vacíos (args, env_keys) cuentan, por eso
// se separa solo por tabulador. Devuelve siempre una lista no nula, para que la UI reciba [] y no null.
func parseServices(stdout string) ([]ServiceView, error) {
	services := []ServiceView{}
	for _, line := range strings.Split(strings.TrimRight(stdout, "\r\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != serviceFields {
			return nil, errUnexpectedServicesList
		}
		restartSec, err := strconv.Atoi(f[4])
		if err != nil || ValidateRestart(f[3]) != nil {
			return nil, errUnexpectedServicesList
		}
		keys := []string{}
		if f[7] != "" {
			keys = strings.Split(f[7], ",")
		}
		services = append(services, ServiceView{
			Name: f[0], ActiveState: f[1], EnabledState: f[2], Restart: f[3], RestartSec: restartSec,
			Executable: f[5], Args: f[6], EnvKeys: keys, Description: f[8],
		})
	}
	return services, nil
}

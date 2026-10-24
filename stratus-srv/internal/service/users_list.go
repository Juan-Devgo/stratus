package service

import (
	"context"
	"errors"
	"strconv"
	"strings"
)

// UserView es un usuario regular de la VM tal como lo recibe la UI.
type UserView struct {
	Username string `json:"username"`
	UID      int    `json:"uid"`
	Sudo     bool   `json:"sudo"`
	SSHKey   bool   `json:"ssh_key"`
}

// ListUsers lee los usuarios regulares de la VM con list-users.sh. Toma el bloqueo de la VM
// como cualquier operación, pero un listado correcto no deja fila en el historial: solo los
// fallos se registran (REQ-19, AC-30).
func (s *UsersService) ListUsers(ctx context.Context, vm string) ([]UserView, error) {
	v, err := s.getVM(ctx, vm)
	if err != nil {
		return nil, err
	}
	args, _, err := s.userTarget(ctx, v.Name, false)
	if err != nil {
		return nil, err
	}
	res, err := s.ops.run(ctx, "list_users", v.Name, "users/list-users.sh", args, false, nil)
	if err != nil {
		return nil, err
	}
	return parseUsers(string(res.Stdout))
}

// parseUsers convierte las líneas "<usuario> <uid> <yes|no> <yes|no>" de list-users.sh.
// Devuelve siempre una lista no nula, para que la UI reciba [] y no null.
func parseUsers(stdout string) ([]UserView, error) {
	users := []UserView{}
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 4 {
			return nil, errUnexpectedList
		}
		uid, err := strconv.Atoi(fields[1])
		sudo, okSudo := parseYesNo(fields[2])
		key, okKey := parseYesNo(fields[3])
		if err != nil || !okSudo || !okKey {
			return nil, errUnexpectedList
		}
		users = append(users, UserView{Username: fields[0], UID: uid, Sudo: sudo, SSHKey: key})
	}
	return users, nil
}

var errUnexpectedList = errors.New("salida inesperada de list-users.sh")

// parseYesNo interpreta "yes" y "no"; el segundo valor es false si es otra cosa.
func parseYesNo(s string) (value, ok bool) {
	switch s {
	case "yes":
		return true, true
	case "no":
		return false, true
	}
	return false, false
}

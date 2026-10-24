package service

import (
	"context"
	"fmt"
	"slices"
)

// SetIssue reescribe /etc/issue de la VM con set-issue.sh (REQ-10). El mensaje va como un solo
// argumento y los campos se envían en el orden fijo de REQ-26, sin importar el orden recibido.
func (s *PrepareService) SetIssue(ctx context.Context, name string, in IssueInput) (string, error) {
	if err := ValidateIssue(in); err != nil {
		return "", err
	}
	if _, err := s.getVM(ctx, name); err != nil {
		return "", err
	}

	// Recorrer la lista canónica ordena los campos y descarta repetidos.
	args, _, err := targetArgs(ctx, s.lbs, name, in.Message)
	if err != nil {
		return "", err
	}
	for _, field := range issueFields {
		if slices.Contains(in.Fields, field) {
			args = append(args, field)
		}
	}
	if _, err := s.runOperation(ctx, "set_issue", name, "prepare/set-issue.sh", args, nil); err != nil {
		return "", err
	}
	return fmt.Sprintf("/etc/issue de %s actualizado", name), nil
}

// SetHostname cambia el hostname de la VM con set-hostname.sh. El script también deja la
// línea 127.0.1.1 de /etc/hosts con el nuevo nombre (REQ-12, REQ-13).
func (s *PrepareService) SetHostname(ctx context.Context, name, hostname string) (string, error) {
	if err := ValidateHostname(hostname); err != nil {
		return "", err
	}
	if _, err := s.getVM(ctx, name); err != nil {
		return "", err
	}

	args, _, err := targetArgs(ctx, s.lbs, name, hostname)
	if err != nil {
		return "", err
	}
	if _, err := s.runOperation(ctx, "set_hostname", name, "prepare/set-hostname.sh", args, nil); err != nil {
		return "", err
	}
	return fmt.Sprintf("Hostname de %s cambiado a %s", name, hostname), nil
}

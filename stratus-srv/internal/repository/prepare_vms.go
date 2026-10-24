package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var (
	ErrDuplicateVM = errors.New("vm duplicada")
	ErrVMNotFound  = errors.New("vm no encontrada")
)

// VM es una VM registrada por la app, con su bandera de llave de usuario (REQ-27).
type VM struct {
	Name             string
	UserKeyGenerated bool
	CreatedAt        time.Time
}

// VMRepository accede a la tabla vms.
type VMRepository struct {
	db *sql.DB
}

func NewVMRepository(db *sql.DB) *VMRepository {
	return &VMRepository{db: db}
}

// Insert registra la VM. Devuelve ErrDuplicateVM si el nombre ya existe.
func (r *VMRepository) Insert(ctx context.Context, name string) (VM, error) {
	vm := VM{Name: name, CreatedAt: time.Now().UTC().Truncate(time.Second)}
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO vms (name, created_at) VALUES (?, ?) ON CONFLICT(name) DO NOTHING`,
		vm.Name, vm.CreatedAt.Format(time.RFC3339))
	if err != nil {
		return VM{}, fmt.Errorf("registrar vm %s: %w", name, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return VM{}, fmt.Errorf("registrar vm %s: %w", name, err)
	}
	if n == 0 {
		return VM{}, fmt.Errorf("registrar vm %s: %w", name, ErrDuplicateVM)
	}
	return vm, nil
}

// Get devuelve la VM o ErrVMNotFound.
func (r *VMRepository) Get(ctx context.Context, name string) (VM, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT name, user_key_generated, created_at FROM vms WHERE name = ?`, name)
	vm, err := scanVM(row)
	if errors.Is(err, sql.ErrNoRows) {
		return VM{}, fmt.Errorf("obtener vm %s: %w", name, ErrVMNotFound)
	}
	if err != nil {
		return VM{}, fmt.Errorf("obtener vm %s: %w", name, err)
	}
	return vm, nil
}

// List devuelve todas las VMs ordenadas por nombre.
func (r *VMRepository) List(ctx context.Context) ([]VM, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT name, user_key_generated, created_at FROM vms ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("listar vms: %w", err)
	}
	defer rows.Close()

	vms := []VM{}
	for rows.Next() {
		vm, err := scanVM(rows)
		if err != nil {
			return nil, fmt.Errorf("listar vms: %w", err)
		}
		vms = append(vms, vm)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("listar vms: %w", err)
	}
	return vms, nil
}

// Delete borra la fila de la VM; con ella se borra su bandera.
func (r *VMRepository) Delete(ctx context.Context, name string) error {
	return r.exec(ctx, "eliminar vm", `DELETE FROM vms WHERE name = ?`, name)
}

// SetUserKeyGenerated marca que la VM tiene una llave de usuario generada por la app (REQ-27).
func (r *VMRepository) SetUserKeyGenerated(ctx context.Context, name string) error {
	return r.exec(ctx, "marcar llave generada", `UPDATE vms SET user_key_generated = 1 WHERE name = ?`, name)
}

// exec ejecuta una sentencia sobre una VM y devuelve ErrVMNotFound si no afectó ninguna fila.
func (r *VMRepository) exec(ctx context.Context, action, query, name string) error {
	res, err := r.db.ExecContext(ctx, query, name)
	if err != nil {
		return fmt.Errorf("%s %s: %w", action, name, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s %s: %w", action, name, err)
	}
	if n == 0 {
		return fmt.Errorf("%s %s: %w", action, name, ErrVMNotFound)
	}
	return nil
}

// scanner lo cumplen *sql.Row y *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanVM(s scanner) (VM, error) {
	var vm VM
	var createdAt string
	if err := s.Scan(&vm.Name, &vm.UserKeyGenerated, &createdAt); err != nil {
		return VM{}, err
	}
	t, err := time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return VM{}, fmt.Errorf("fecha inválida %q: %w", createdAt, err)
	}
	vm.CreatedAt = t
	return vm, nil
}

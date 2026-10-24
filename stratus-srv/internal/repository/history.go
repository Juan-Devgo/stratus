package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// Operation es una fila del historial (REQ-17). Stderr ya llega enmascarado por el servicio;
// el stdout de los scripts nunca se guarda porque puede contener la llave privada.
type Operation struct {
	ID        int64
	Module    string
	Operation string
	VM        string
	Command   string
	Args      []string
	ExitCode  int
	Stderr    string
	Result    string // "ok", "error" o "timeout"
	CreatedAt time.Time
}

// HistoryRepository accede a la tabla operations.
type HistoryRepository struct {
	db *sql.DB
}

func NewHistoryRepository(db *sql.DB) *HistoryRepository {
	return &HistoryRepository{db: db}
}

// Insert guarda una operación. Si CreatedAt está vacío se usa la hora actual en UTC.
func (r *HistoryRepository) Insert(ctx context.Context, op Operation) error {
	if op.Args == nil {
		op.Args = []string{}
	}
	args, err := json.Marshal(op.Args)
	if err != nil {
		return fmt.Errorf("guardar operación: %w", err)
	}
	if op.CreatedAt.IsZero() {
		op.CreatedAt = time.Now()
	}
	_, err = r.db.ExecContext(ctx,
		`INSERT INTO operations (module, operation, vm, command, args, exit_code, stderr, result, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		op.Module, op.Operation, op.VM, op.Command, string(args), op.ExitCode, op.Stderr, op.Result,
		op.CreatedAt.UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("guardar operación: %w", err)
	}
	return nil
}

// ListRecent devuelve las últimas limit operaciones del módulo, la más reciente primero.
func (r *HistoryRepository) ListRecent(ctx context.Context, module string, limit int) ([]Operation, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, module, operation, vm, command, args, exit_code, stderr, result, created_at
		 FROM operations WHERE module = ? ORDER BY id DESC LIMIT ?`, module, limit)
	if err != nil {
		return nil, fmt.Errorf("listar historial: %w", err)
	}
	defer rows.Close()

	ops := []Operation{}
	for rows.Next() {
		var op Operation
		var args, createdAt string
		err := rows.Scan(&op.ID, &op.Module, &op.Operation, &op.VM, &op.Command, &args,
			&op.ExitCode, &op.Stderr, &op.Result, &createdAt)
		if err != nil {
			return nil, fmt.Errorf("listar historial: %w", err)
		}
		if err := json.Unmarshal([]byte(args), &op.Args); err != nil {
			return nil, fmt.Errorf("listar historial: argumentos inválidos en la fila %d: %w", op.ID, err)
		}
		if op.CreatedAt, err = time.Parse(time.RFC3339, createdAt); err != nil {
			return nil, fmt.Errorf("listar historial: fecha inválida en la fila %d: %w", op.ID, err)
		}
		ops = append(ops, op)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("listar historial: %w", err)
	}
	return ops, nil
}

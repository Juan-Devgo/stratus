package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// WebServer es un servidor web registrado por la app (005): su VM y su tipo, apache o nginx.
type WebServer struct {
	Name string
	Type string
}

// WebRepository accede a la tabla web_servers.
type WebRepository struct {
	db *sql.DB
}

func NewWebRepository(db *sql.DB) *WebRepository {
	return &WebRepository{db: db}
}

// Insert registra el servidor web. Un nombre repetido o un tipo fuera de la lista falla.
func (r *WebRepository) Insert(ctx context.Context, w WebServer) error {
	if _, err := r.db.ExecContext(ctx, `INSERT INTO web_servers (name, type) VALUES (?, ?)`, w.Name, w.Type); err != nil {
		return fmt.Errorf("registrar servidor web %s: %w", w.Name, err)
	}
	return nil
}

// Get devuelve el servidor web y true, o false si la VM no es un servidor web.
func (r *WebRepository) Get(ctx context.Context, name string) (WebServer, bool, error) {
	var w WebServer
	err := r.db.QueryRowContext(ctx, `SELECT name, type FROM web_servers WHERE name = ?`, name).Scan(&w.Name, &w.Type)
	if errors.Is(err, sql.ErrNoRows) {
		return WebServer{}, false, nil
	}
	if err != nil {
		return WebServer{}, false, fmt.Errorf("obtener servidor web %s: %w", name, err)
	}
	return w, true, nil
}

// List devuelve los servidores web ordenados por nombre.
func (r *WebRepository) List(ctx context.Context) ([]WebServer, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT name, type FROM web_servers ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("listar servidores web: %w", err)
	}
	defer rows.Close()

	list := []WebServer{}
	for rows.Next() {
		var w WebServer
		if err := rows.Scan(&w.Name, &w.Type); err != nil {
			return nil, fmt.Errorf("listar servidores web: %w", err)
		}
		list = append(list, w)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("listar servidores web: %w", err)
	}
	return list, nil
}

// Delete borra el servidor web; borrar uno que no existe no es un error.
func (r *WebRepository) Delete(ctx context.Context, name string) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM web_servers WHERE name = ?`, name); err != nil {
		return fmt.Errorf("eliminar servidor web %s: %w", name, err)
	}
	return nil
}

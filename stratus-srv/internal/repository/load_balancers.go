package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ErrLBNotFound indica que el balanceador no está registrado.
var ErrLBNotFound = errors.New("balanceador no encontrado")

// LoadBalancer es un balanceador registrado: su subred 10.10.Subnet.0/24 y el puerto del frontend.
type LoadBalancer struct {
	Name         string
	Subnet       int
	FrontendPort int
}

// LBServer es una VM asignada a un balanceador. Name es server<N> dentro del haproxy.cfg.
type LBServer struct {
	LB      string
	Name    string
	VM      string
	Port    int
	LastIP  string // última IP interna conocida
	Enabled bool   // false: la línea del backend lleva "disabled"
}

// LBRepository accede a las tablas load_balancers y lb_servers: la topología (004).
type LBRepository struct {
	db *sql.DB
}

func NewLBRepository(db *sql.DB) *LBRepository {
	return &LBRepository{db: db}
}

// InsertLB registra el balanceador. La subred y el nombre son únicos.
func (r *LBRepository) InsertLB(ctx context.Context, lb LoadBalancer) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO load_balancers (name, subnet, frontend_port) VALUES (?, ?, ?)`,
		lb.Name, lb.Subnet, lb.FrontendPort)
	if err != nil {
		return fmt.Errorf("registrar balanceador %s: %w", lb.Name, err)
	}
	return nil
}

// GetLB devuelve el balanceador o ErrLBNotFound.
func (r *LBRepository) GetLB(ctx context.Context, name string) (LoadBalancer, error) {
	var lb LoadBalancer
	err := r.db.QueryRowContext(ctx,
		`SELECT name, subnet, frontend_port FROM load_balancers WHERE name = ?`, name).
		Scan(&lb.Name, &lb.Subnet, &lb.FrontendPort)
	if errors.Is(err, sql.ErrNoRows) {
		return LoadBalancer{}, fmt.Errorf("obtener balanceador %s: %w", name, ErrLBNotFound)
	}
	if err != nil {
		return LoadBalancer{}, fmt.Errorf("obtener balanceador %s: %w", name, err)
	}
	return lb, nil
}

// ListLBs devuelve los balanceadores ordenados por nombre.
func (r *LBRepository) ListLBs(ctx context.Context) ([]LoadBalancer, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT name, subnet, frontend_port FROM load_balancers ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("listar balanceadores: %w", err)
	}
	defer rows.Close()

	lbs := []LoadBalancer{}
	for rows.Next() {
		var lb LoadBalancer
		if err := rows.Scan(&lb.Name, &lb.Subnet, &lb.FrontendPort); err != nil {
			return nil, fmt.Errorf("listar balanceadores: %w", err)
		}
		lbs = append(lbs, lb)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("listar balanceadores: %w", err)
	}
	return lbs, nil
}

// Subnets devuelve las subredes N en uso, de menor a mayor (create-lb.sh las evita).
func (r *LBRepository) Subnets(ctx context.Context) ([]int, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT subnet FROM load_balancers ORDER BY subnet`)
	if err != nil {
		return nil, fmt.Errorf("listar subredes: %w", err)
	}
	defer rows.Close()

	subnets := []int{}
	for rows.Next() {
		var n int
		if err := rows.Scan(&n); err != nil {
			return nil, fmt.Errorf("listar subredes: %w", err)
		}
		subnets = append(subnets, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("listar subredes: %w", err)
	}
	return subnets, nil
}

// SetFrontendPort guarda el puerto del frontend. Devuelve ErrLBNotFound si no existe.
func (r *LBRepository) SetFrontendPort(ctx context.Context, name string, port int) error {
	res, err := r.db.ExecContext(ctx, `UPDATE load_balancers SET frontend_port = ? WHERE name = ?`, port, name)
	if err != nil {
		return fmt.Errorf("guardar puerto de %s: %w", name, err)
	}
	return requireRow(res, fmt.Sprintf("guardar puerto de %s", name), ErrLBNotFound)
}

// DeleteLB borra el balanceador y sus servidores en una transacción.
func (r *LBRepository) DeleteLB(ctx context.Context, name string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("eliminar balanceador %s: %w", name, err)
	}
	defer tx.Rollback() //nolint:errcheck // tras Commit no hace nada

	if _, err := tx.ExecContext(ctx, `DELETE FROM lb_servers WHERE lb = ?`, name); err != nil {
		return fmt.Errorf("eliminar servidores de %s: %w", name, err)
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM load_balancers WHERE name = ?`, name)
	if err != nil {
		return fmt.Errorf("eliminar balanceador %s: %w", name, err)
	}
	if err := requireRow(res, fmt.Sprintf("eliminar balanceador %s", name), ErrLBNotFound); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("eliminar balanceador %s: %w", name, err)
	}
	return nil
}

// Servers devuelve los servidores del balanceador ordenados por el número de server<N>
// (server2 antes que server10, que como texto quedaría primero).
func (r *LBRepository) Servers(ctx context.Context, lb string) ([]LBServer, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT lb, name, vm, port, last_ip, enabled FROM lb_servers
		 WHERE lb = ? ORDER BY CAST(substr(name, 7) AS INTEGER)`, lb)
	if err != nil {
		return nil, fmt.Errorf("listar servidores de %s: %w", lb, err)
	}
	defer rows.Close()

	servers := []LBServer{}
	for rows.Next() {
		s, err := scanServer(rows)
		if err != nil {
			return nil, fmt.Errorf("listar servidores de %s: %w", lb, err)
		}
		servers = append(servers, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("listar servidores de %s: %w", lb, err)
	}
	return servers, nil
}

// ServerByVM busca la VM entre los servidores de cualquier balanceador; ok es false si no está asignada.
func (r *LBRepository) ServerByVM(ctx context.Context, vm string) (s LBServer, ok bool, err error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT lb, name, vm, port, last_ip, enabled FROM lb_servers WHERE vm = ?`, vm)
	s, err = scanServer(row)
	if errors.Is(err, sql.ErrNoRows) {
		return LBServer{}, false, nil
	}
	if err != nil {
		return LBServer{}, false, fmt.Errorf("buscar servidor de la VM %s: %w", vm, err)
	}
	return s, true, nil
}

// InsertServer registra el servidor. Falla si la VM ya está asignada o el nombre ya existe en el balanceador.
func (r *LBRepository) InsertServer(ctx context.Context, s LBServer) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO lb_servers (lb, name, vm, port, last_ip, enabled) VALUES (?, ?, ?, ?, ?, ?)`,
		s.LB, s.Name, s.VM, s.Port, s.LastIP, s.Enabled)
	if err != nil {
		return fmt.Errorf("registrar servidor %s de %s: %w", s.Name, s.LB, err)
	}
	return nil
}

// DeleteServer quita la asignación de la VM. No falla si ya no existe: quitar un servidor se puede reintentar.
func (r *LBRepository) DeleteServer(ctx context.Context, lb, vm string) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM lb_servers WHERE lb = ? AND vm = ?`, lb, vm); err != nil {
		return fmt.Errorf("quitar servidor %s de %s: %w", vm, lb, err)
	}
	return nil
}

// UpdateServerIPs actualiza last_ip y enabled de los servidores (por lb y name) en una transacción.
// Si alguna fila no existe se deshace todo.
func (r *LBRepository) UpdateServerIPs(ctx context.Context, lb string, rows []LBServer) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("actualizar IPs de %s: %w", lb, err)
	}
	defer tx.Rollback() //nolint:errcheck // tras Commit no hace nada

	for _, s := range rows {
		res, err := tx.ExecContext(ctx,
			`UPDATE lb_servers SET last_ip = ?, enabled = ? WHERE lb = ? AND name = ?`,
			s.LastIP, s.Enabled, lb, s.Name)
		if err != nil {
			return fmt.Errorf("actualizar IP de %s en %s: %w", s.Name, lb, err)
		}
		if err := requireRow(res, fmt.Sprintf("actualizar IP de %s en %s", s.Name, lb), sql.ErrNoRows); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("actualizar IPs de %s: %w", lb, err)
	}
	return nil
}

// requireRow devuelve notFound (envuelto) si la sentencia no afectó ninguna fila.
func requireRow(res sql.Result, action string, notFound error) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}
	if n == 0 {
		return fmt.Errorf("%s: %w", action, notFound)
	}
	return nil
}

func scanServer(s scanner) (LBServer, error) {
	var srv LBServer
	err := s.Scan(&srv.LB, &srv.Name, &srv.VM, &srv.Port, &srv.LastIP, &srv.Enabled)
	return srv, err
}

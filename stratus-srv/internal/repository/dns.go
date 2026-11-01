package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// DNSRecord es un registro A guardado por la app: la VM y la IPv4 a la que apunta (005 REQ-22).
type DNSRecord struct {
	VM string
	IP string
}

// DNSRepository accede a las tablas dns_server, dns_records y dns_resolvers.
type DNSRepository struct {
	db *sql.DB
}

func NewDNSRepository(db *sql.DB) *DNSRepository {
	return &DNSRepository{db: db}
}

// Server devuelve el nombre del servidor DNS y true, o false si no hay ninguno.
func (r *DNSRepository) Server(ctx context.Context) (string, bool, error) {
	var name string
	err := r.db.QueryRowContext(ctx, `SELECT name FROM dns_server WHERE id = 1`).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("obtener servidor DNS: %w", err)
	}
	return name, true, nil
}

// InsertServer registra el servidor DNS y sus registros iniciales en una transacción. Un segundo
// servidor falla por la restricción de fila única.
func (r *DNSRepository) InsertServer(ctx context.Context, name string, initial []DNSRecord) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("registrar servidor DNS %s: %w", name, err)
	}
	defer tx.Rollback() //nolint:errcheck // tras Commit no hace nada

	if _, err := tx.ExecContext(ctx, `INSERT INTO dns_server (id, name) VALUES (1, ?)`, name); err != nil {
		return fmt.Errorf("registrar servidor DNS %s: %w", name, err)
	}
	if err := insertRecords(ctx, tx, initial); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("registrar servidor DNS %s: %w", name, err)
	}
	return nil
}

// DeleteServer borra el servidor DNS, todos los registros y la fila de vms del servidor en una
// transacción (REQ-21).
func (r *DNSRepository) DeleteServer(ctx context.Context) error {
	name, ok, err := r.Server(ctx)
	if err != nil || !ok {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("eliminar servidor DNS %s: %w", name, err)
	}
	defer tx.Rollback() //nolint:errcheck // tras Commit no hace nada

	for _, stmt := range []string{
		`DELETE FROM dns_records`,
		`DELETE FROM dns_server`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("eliminar servidor DNS %s: %w", name, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM vms WHERE name = ?`, name); err != nil {
		return fmt.Errorf("eliminar servidor DNS %s: %w", name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("eliminar servidor DNS %s: %w", name, err)
	}
	return nil
}

// Records devuelve los registros ordenados por VM.
func (r *DNSRepository) Records(ctx context.Context) ([]DNSRecord, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT vm, ip FROM dns_records ORDER BY vm`)
	if err != nil {
		return nil, fmt.Errorf("listar registros DNS: %w", err)
	}
	defer rows.Close()

	recs := []DNSRecord{}
	for rows.Next() {
		var rec DNSRecord
		if err := rows.Scan(&rec.VM, &rec.IP); err != nil {
			return nil, fmt.Errorf("listar registros DNS: %w", err)
		}
		recs = append(recs, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("listar registros DNS: %w", err)
	}
	return recs, nil
}

// Record devuelve el registro de la VM y true, o false si no tiene.
func (r *DNSRepository) Record(ctx context.Context, vm string) (DNSRecord, bool, error) {
	var rec DNSRecord
	err := r.db.QueryRowContext(ctx, `SELECT vm, ip FROM dns_records WHERE vm = ?`, vm).Scan(&rec.VM, &rec.IP)
	if errors.Is(err, sql.ErrNoRows) {
		return DNSRecord{}, false, nil
	}
	if err != nil {
		return DNSRecord{}, false, fmt.Errorf("obtener registro DNS de %s: %w", vm, err)
	}
	return rec, true, nil
}

// InsertRecords agrega los registros en una transacción; una VM que ya tiene registro falla.
func (r *DNSRepository) InsertRecords(ctx context.Context, recs []DNSRecord) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("agregar registros DNS: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // tras Commit no hace nada

	if err := insertRecords(ctx, tx, recs); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("agregar registros DNS: %w", err)
	}
	return nil
}

func insertRecords(ctx context.Context, tx *sql.Tx, recs []DNSRecord) error {
	for _, rec := range recs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO dns_records (vm, ip) VALUES (?, ?)`, rec.VM, rec.IP); err != nil {
			return fmt.Errorf("agregar registro DNS de %s: %w", rec.VM, err)
		}
	}
	return nil
}

// DeleteRecord borra el registro de la VM; borrar uno que no existe no es un error.
func (r *DNSRepository) DeleteRecord(ctx context.Context, vm string) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM dns_records WHERE vm = ?`, vm); err != nil {
		return fmt.Errorf("quitar registro DNS de %s: %w", vm, err)
	}
	return nil
}

// UpdateRecordIPs guarda la IP de cada registro existente en una transacción (refresco, REQ-27).
func (r *DNSRepository) UpdateRecordIPs(ctx context.Context, recs []DNSRecord) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("actualizar registros DNS: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // tras Commit no hace nada

	for _, rec := range recs {
		if _, err := tx.ExecContext(ctx, `UPDATE dns_records SET ip = ? WHERE vm = ?`, rec.IP, rec.VM); err != nil {
			return fmt.Errorf("actualizar registro DNS de %s: %w", rec.VM, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("actualizar registros DNS: %w", err)
	}
	return nil
}

// Resolvers devuelve las VMs que usan el servidor DNS como resolvedor, ordenadas (REQ-33).
func (r *DNSRepository) Resolvers(ctx context.Context) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT vm FROM dns_resolvers ORDER BY vm`)
	if err != nil {
		return nil, fmt.Errorf("listar resolvedores DNS: %w", err)
	}
	defer rows.Close()

	vms := []string{}
	for rows.Next() {
		var vm string
		if err := rows.Scan(&vm); err != nil {
			return nil, fmt.Errorf("listar resolvedores DNS: %w", err)
		}
		vms = append(vms, vm)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("listar resolvedores DNS: %w", err)
	}
	return vms, nil
}

// SetResolver marca (on) o desmarca la VM como usuaria del servidor DNS; repetir no es un error.
func (r *DNSRepository) SetResolver(ctx context.Context, vm string, on bool) error {
	stmt := `DELETE FROM dns_resolvers WHERE vm = ?`
	if on {
		stmt = `INSERT INTO dns_resolvers (vm) VALUES (?) ON CONFLICT(vm) DO NOTHING`
	}
	if _, err := r.db.ExecContext(ctx, stmt, vm); err != nil {
		return fmt.Errorf("guardar resolvedor de %s: %w", vm, err)
	}
	return nil
}

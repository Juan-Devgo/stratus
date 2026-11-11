package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ZoneRecord es un registro de la zona tal como la sirve bind9 (REQ-46): tipo (A, NS, SOA...),
// nombre absoluto sin el punto final, valor y TTL en segundos.
type ZoneRecord struct {
	Type  string `json:"type"`
	Name  string `json:"name"`
	Value string `json:"value"`
	TTL   int    `json:"ttl"`
}

// Zone lee los registros del archivo de zona del servidor DNS con show-zone.sh (REQ-46, REQ-48).
// Toma el bloqueo del servidor como Estado y solo deja fila en el historial si falla (REQ-49). Con
// el servidor apagado devuelve ErrDNSOffZone.
func (s *DNSService) Zone(ctx context.Context) ([]ZoneRecord, error) {
	name, err := s.server(ctx)
	if err != nil {
		return nil, err
	}
	res, err := s.ops.run(ctx, "show_zone", name, "dns/show-zone.sh", []string{name, s.cfg.Domain}, false, nil)
	if errors.Is(err, ErrVMNoIP) {
		return nil, ErrDNSOffZone
	}
	if err != nil {
		return nil, err
	}
	return parseZone(res.Stdout)
}

// parseZone convierte la salida de show-zone.sh, "<tipo>\t<nombre>\t<ttl>\t<valor>" por línea, en
// registros. Una línea con otra forma es un error: mejor fallar que mostrar una zona a medias.
func parseZone(stdout []byte) ([]ZoneRecord, error) {
	records := []ZoneRecord{}
	for _, line := range strings.Split(strings.TrimRight(string(stdout), "\n"), "\n") {
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, "\t", 4)
		if len(fields) != 4 {
			return nil, fmt.Errorf("salida de show-zone.sh inválida: %q", line)
		}
		ttl, err := strconv.Atoi(fields[2])
		if err != nil {
			return nil, fmt.Errorf("TTL inválido en show-zone.sh: %q", line)
		}
		records = append(records, ZoneRecord{Type: fields[0], Name: fields[1], Value: fields[3], TTL: ttl})
	}
	return records, nil
}

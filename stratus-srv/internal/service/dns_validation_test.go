package service

import (
	"errors"
	"testing"
)

// validDNSConfig es una configuración DNS correcta para las pruebas.
func validDNSConfig() DNSConfig {
	return DNSConfig{BaseDisk: "/vms/dns.vdi", BridgeAdapter: "eth0", IP: "192.168.1.53/24",
		Gateway: "192.168.1.1", Domain: "stratus.lan", Forwarders: "1.1.1.1,8.8.8.8"}
}

// AC-24: una configuración DNS faltante o inválida da "Configuración de DNS incompleta" (REQ-15).
func TestAC24_ValidateDNSConfig(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*DNSConfig)
		ok     bool
	}{
		{"válida", func(*DNSConfig) {}, true},
		{"dominio de 4 etiquetas", func(c *DNSConfig) { c.Domain = "a.b.c.lan" }, true},
		{"un reenviador", func(c *DNSConfig) { c.Forwarders = "9.9.9.9" }, true},
		{"prefijo 30", func(c *DNSConfig) { c.IP = "192.168.1.53/30"; c.Gateway = "192.168.1.54" }, true},
		{"IP vacía", func(c *DNSConfig) { c.IP = "" }, false},
		{"IP sin prefijo", func(c *DNSConfig) { c.IP = "192.168.1.53" }, false},
		{"prefijo 31", func(c *DNSConfig) { c.IP = "192.168.1.53/31" }, false},
		{"prefijo 7", func(c *DNSConfig) { c.IP = "10.0.0.53/7" }, false},
		{"IPv6", func(c *DNSConfig) { c.IP = "fd00::53/64" }, false},
		{"IP igual a la red", func(c *DNSConfig) { c.IP = "192.168.1.0/24" }, false},
		{"puerta de enlace fuera de la subred", func(c *DNSConfig) { c.Gateway = "10.0.0.1" }, false},
		{"puerta de enlace igual a la red", func(c *DNSConfig) { c.Gateway = "192.168.1.0" }, false},
		{"puerta de enlace de difusión", func(c *DNSConfig) { c.Gateway = "192.168.1.255" }, false},
		{"puerta de enlace igual a la IP", func(c *DNSConfig) { c.Gateway = "192.168.1.53" }, false},
		{"puerta de enlace vacía", func(c *DNSConfig) { c.Gateway = "" }, false},
		{"dominio con mayúsculas", func(c *DNSConfig) { c.Domain = "Stratus_Lan" }, false},
		{"dominio de 5 etiquetas", func(c *DNSConfig) { c.Domain = "a.b.c.d.lan" }, false},
		{"dominio vacío", func(c *DNSConfig) { c.Domain = "" }, false},
		{"etiqueta con guion final", func(c *DNSConfig) { c.Domain = "lab-.lan" }, false},
		{"reenviador incompleto", func(c *DNSConfig) { c.Forwarders = "1.1.1" }, false},
		{"cuatro reenviadores", func(c *DNSConfig) { c.Forwarders = "1.1.1.1,8.8.8.8,9.9.9.9,1.0.0.1" }, false},
		{"reenviadores vacíos", func(c *DNSConfig) { c.Forwarders = "" }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validDNSConfig()
			tt.mutate(&cfg)
			err := ValidateDNSConfig(cfg)
			if tt.ok && err != nil {
				t.Errorf("ValidateDNSConfig = %v, se esperaba nil", err)
			}
			if !tt.ok && !errors.Is(err, ErrDNSConfigIncomplete) {
				t.Errorf("ValidateDNSConfig = %v, se esperaba ErrDNSConfigIncomplete", err)
			}
		})
	}
}

// Address devuelve la IPv4 del servidor DNS sin el prefijo.
func TestAC24_DNSConfigAddress(t *testing.T) {
	cfg := validDNSConfig()
	if got := cfg.Address(); got != "192.168.1.53" {
		t.Errorf("Address() = %q, se esperaba 192.168.1.53", got)
	}
}

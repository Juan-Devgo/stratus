package service

import (
	"net/netip"
	"regexp"
	"strings"
)

// DNSConfig es la configuración del servidor DNS leída del entorno (005 REQ-15).
type DNSConfig struct {
	BaseDisk      string
	BridgeAdapter string
	IP            string // IPv4 con prefijo, p. ej. 192.168.1.53/24
	Gateway       string
	Domain        string
	Forwarders    string // 1 a 3 IPv4 separadas por comas
}

// Address devuelve la IPv4 del servidor DNS sin el prefijo.
func (c DNSConfig) Address() string {
	ip, _, _ := strings.Cut(c.IP, "/")
	return ip
}

var dnsLabelRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// ValidateDNSConfig devuelve ErrDNSConfigIncomplete si la IP, la puerta de enlace, el dominio o
// los reenviadores faltan o son inválidos. El disco base vacío se informa aparte (ErrDNSBaseDiskMissing).
func ValidateDNSConfig(c DNSConfig) error {
	if !validDNSAddress(c.IP, c.Gateway) || !validDomain(c.Domain) || !validForwarders(c.Forwarders) {
		return ErrDNSConfigIncomplete
	}
	return nil
}

// validDNSAddress comprueba la IPv4/prefijo (8-30) y que la puerta de enlace sea un host de la
// misma subred distinto de la IP.
func validDNSAddress(cidr, gateway string) bool {
	if !strings.Contains(cidr, "/") {
		return false
	}
	p, err := netip.ParsePrefix(cidr)
	if err != nil || !p.Addr().Is4() || p.Bits() < 8 || p.Bits() > 30 {
		return false
	}
	gw, err := netip.ParseAddr(gateway)
	if err != nil || !gw.Is4() || !p.Contains(gw) {
		return false
	}
	network := p.Masked().Addr()
	broadcast := lastAddr(p)
	ip := p.Addr()
	return ip != network && ip != broadcast && gw != network && gw != broadcast && gw != ip
}

// lastAddr devuelve la dirección de difusión de una subred IPv4.
func lastAddr(p netip.Prefix) netip.Addr {
	b := p.Masked().Addr().As4()
	host := 32 - p.Bits()
	for i := 3; i >= 0 && host > 0; i-- {
		n := min(host, 8)
		b[i] |= byte(1<<n - 1)
		host -= n
	}
	return netip.AddrFrom4(b)
}

func validDomain(d string) bool {
	labels := strings.Split(d, ".")
	if len(labels) < 1 || len(labels) > 4 {
		return false
	}
	for _, l := range labels {
		if !dnsLabelRe.MatchString(l) {
			return false
		}
	}
	return true
}

func validForwarders(f string) bool {
	items := strings.Split(f, ",")
	if f == "" || len(items) > 3 {
		return false
	}
	for _, it := range items {
		a, err := netip.ParseAddr(strings.TrimSpace(it))
		if err != nil || !a.Is4() {
			return false
		}
	}
	return true
}

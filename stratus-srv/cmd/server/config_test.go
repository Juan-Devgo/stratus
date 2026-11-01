package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setRequired define las variables obligatorias con valores de prueba.
func setRequired(t *testing.T) {
	t.Helper()
	t.Setenv("APP_BASE_DISK", "/vms/base.vdi")
	t.Setenv("APP_HAPROXY_BASE_DISK", "/vms/haproxy-base.vdi")
	t.Setenv("APP_BRIDGE_ADAPTER", "eth0")
	t.Setenv("APP_GUEST_PASSWORD_FILE", "/secrets/guest-password")
	t.Setenv("APP_SSH_KEY_PATH", "/keys/stratus_backend")
}

func TestLoadConfigUsesDefaults(t *testing.T) {
	setRequired(t)
	t.Setenv("APP_LISTEN_ADDR", "")
	t.Setenv("APP_DB_PATH", "")
	t.Setenv("APP_SCRIPTS_DIR", "")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	wantScripts, _ := filepath.Abs("../scripts")
	checks := []struct{ name, got, want string }{
		{"ListenAddr", cfg.ListenAddr, "127.0.0.1:8080"},
		{"DBPath", cfg.DBPath, "./stratus.db"},
		{"ScriptsDir", cfg.ScriptsDir, wantScripts},
		{"BaseDisk", cfg.BaseDisk, "/vms/base.vdi"},
		{"HAProxyBaseDisk", cfg.HAProxyBaseDisk, "/vms/haproxy-base.vdi"},
		{"BridgeAdapter", cfg.BridgeAdapter, "eth0"},
		{"GuestPasswordFile", cfg.GuestPasswordFile, "/secrets/guest-password"},
		{"SSHKeyPath", cfg.SSHKeyPath, "/keys/stratus_backend"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %q, se esperaba %q", c.name, c.got, c.want)
		}
	}
}

func TestLoadConfigRequiresVariables(t *testing.T) {
	for _, missing := range []string{"APP_BASE_DISK", "APP_HAPROXY_BASE_DISK", "APP_BRIDGE_ADAPTER", "APP_GUEST_PASSWORD_FILE", "APP_SSH_KEY_PATH"} {
		t.Run(missing, func(t *testing.T) {
			setRequired(t)
			t.Setenv(missing, "")

			_, err := loadConfig()
			if err == nil {
				t.Fatalf("loadConfig sin %s: se esperaba un error", missing)
			}
			if !strings.Contains(err.Error(), missing) {
				t.Errorf("error = %q, se esperaba que nombrara %s", err, missing)
			}
		})
	}
}

// Las variables de antes del 2026-10-22 ya no se exigen.
func TestLoadConfigIgnoresRemovedVariables(t *testing.T) {
	setRequired(t)
	for _, name := range []string{"APP_DEBIAN_ISO", "APP_GUEST_USER", "APP_GUEST_SECRETS_DIR"} {
		t.Setenv(name, "")
	}

	if _, err := loadConfig(); err != nil {
		t.Fatalf("loadConfig sin las variables retiradas: %v", err)
	}
}

// El backend solo guarda la ruta del archivo de contraseña: nunca lo abre (REQ-22, AC-45).
func TestAC45_LoadConfigDoesNotOpenPasswordFile(t *testing.T) {
	setRequired(t)
	passwordFile := filepath.Join(t.TempDir(), "guest-password")
	if err := os.WriteFile(passwordFile, []byte("Pr0ebaClave+/xyz"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APP_GUEST_PASSWORD_FILE", passwordFile)

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig con un archivo ilegible: %v", err)
	}
	if cfg.GuestPasswordFile != passwordFile {
		t.Errorf("GuestPasswordFile = %q, se esperaba %q", cfg.GuestPasswordFile, passwordFile)
	}
}

// AC-3: APP_HAPROXY_BASE_DISK debe ser una ruta absoluta; una relativa falla al arrancar.
func TestAC3_LoadConfigRejectsRelativeHAProxyBaseDisk(t *testing.T) {
	setRequired(t)
	t.Setenv("APP_HAPROXY_BASE_DISK", "discos/haproxy-base.vdi")

	_, err := loadConfig()
	if err == nil {
		t.Fatal("loadConfig con una ruta relativa: se esperaba un error")
	}
	if !strings.Contains(err.Error(), "APP_HAPROXY_BASE_DISK") {
		t.Errorf("error = %q, se esperaba que nombrara APP_HAPROXY_BASE_DISK", err)
	}
}

// AC-24: el servidor arranca sin las variables de servidores web y DNS; se revisan al usarlas
// (REQ-15). APP_DNS_DOMAIN tiene el valor por defecto stratus.lan.
func TestAC24_ConfigOptionalDNS(t *testing.T) {
	setRequired(t)
	for _, name := range []string{"APP_APACHE_BASE_DISK", "APP_NGINX_BASE_DISK", "APP_DNS_BASE_DISK",
		"APP_DNS_IP", "APP_DNS_GATEWAY", "APP_DNS_DOMAIN", "APP_DNS_FORWARDERS"} {
		t.Setenv(name, "")
	}
	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.DNSDomain != "stratus.lan" {
		t.Errorf("DNSDomain = %q, se esperaba stratus.lan", cfg.DNSDomain)
	}

	t.Setenv("APP_APACHE_BASE_DISK", "/vms/apache.vdi")
	t.Setenv("APP_NGINX_BASE_DISK", "/vms/nginx.vdi")
	t.Setenv("APP_DNS_BASE_DISK", "/vms/dns.vdi")
	t.Setenv("APP_DNS_IP", "192.168.1.53/24")
	t.Setenv("APP_DNS_GATEWAY", "192.168.1.1")
	t.Setenv("APP_DNS_DOMAIN", "lab.lan")
	t.Setenv("APP_DNS_FORWARDERS", "1.1.1.1,8.8.8.8")
	cfg, err = loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	checks := []struct{ name, got, want string }{
		{"ApacheBaseDisk", cfg.ApacheBaseDisk, "/vms/apache.vdi"},
		{"NginxBaseDisk", cfg.NginxBaseDisk, "/vms/nginx.vdi"},
		{"DNSBaseDisk", cfg.DNSBaseDisk, "/vms/dns.vdi"},
		{"DNSIP", cfg.DNSIP, "192.168.1.53/24"},
		{"DNSGateway", cfg.DNSGateway, "192.168.1.1"},
		{"DNSDomain", cfg.DNSDomain, "lab.lan"},
		{"DNSForwarders", cfg.DNSForwarders, "1.1.1.1,8.8.8.8"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %q, se esperaba %q", c.name, c.got, c.want)
		}
	}
}

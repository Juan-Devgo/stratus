package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// config son las variables de entorno APP_* del backend.
// APP_GUEST_PASSWORD_FILE y APP_SSH_KEY_PATH llegan a los scripts por herencia del entorno; aquí
// solo se exigen. El archivo de contraseña de la imagen base lo lee VBoxManage dentro de
// create-vm.sh: el backend nunca lo abre (REQ-22).
type config struct {
	ListenAddr        string
	DBPath            string
	ScriptsDir        string // absoluto, para no depender del directorio de trabajo
	BaseDisk          string // disco base multiattach; argumento de create-vm.sh
	HAProxyBaseDisk   string // disco base de los balanceadores; argumento de create-lb.sh (004)
	BridgeAdapter     string // interfaz del host para la red en puente; argumento de create-vm.sh
	GuestPasswordFile string // solo la ruta
	SSHKeyPath        string
	// Servidores web y DNS (005): opcionales al arrancar; el backend los revisa al usarlos.
	ApacheBaseDisk string // disco base de los servidores web Apache; argumento de create-web.sh
	NginxBaseDisk  string // disco base de los servidores web Nginx; argumento de create-web.sh
	DNSBaseDisk    string // disco base del servidor DNS; argumento de create-dns.sh
	DNSIP          string // IPv4 fija del servidor DNS en CIDR, por ejemplo 192.168.1.53/24
	DNSGateway     string // puerta de enlace del servidor DNS
	DNSDomain      string // zona local; por defecto stratus.lan
	DNSForwarders  string // 1 a 3 IPv4 separadas por comas para los nombres fuera de la zona
}

// loadConfig lee el entorno, aplica los valores por defecto y falla si falta alguna
// variable obligatoria. El error nombra las variables, nunca sus valores.
func loadConfig() (config, error) {
	cfg := config{
		ListenAddr:        getenv("APP_LISTEN_ADDR", "127.0.0.1:8080"),
		DBPath:            getenv("APP_DB_PATH", "./stratus.db"),
		BaseDisk:          os.Getenv("APP_BASE_DISK"),
		HAProxyBaseDisk:   os.Getenv("APP_HAPROXY_BASE_DISK"),
		BridgeAdapter:     os.Getenv("APP_BRIDGE_ADAPTER"),
		GuestPasswordFile: os.Getenv("APP_GUEST_PASSWORD_FILE"),
		SSHKeyPath:        os.Getenv("APP_SSH_KEY_PATH"),
		ApacheBaseDisk:    os.Getenv("APP_APACHE_BASE_DISK"),
		NginxBaseDisk:     os.Getenv("APP_NGINX_BASE_DISK"),
		DNSBaseDisk:       os.Getenv("APP_DNS_BASE_DISK"),
		DNSIP:             os.Getenv("APP_DNS_IP"),
		DNSGateway:        os.Getenv("APP_DNS_GATEWAY"),
		DNSDomain:         getenv("APP_DNS_DOMAIN", "stratus.lan"),
		DNSForwarders:     os.Getenv("APP_DNS_FORWARDERS"),
	}

	var missing []string
	required := []struct{ name, value string }{
		{"APP_BASE_DISK", cfg.BaseDisk},
		{"APP_HAPROXY_BASE_DISK", cfg.HAProxyBaseDisk},
		{"APP_BRIDGE_ADAPTER", cfg.BridgeAdapter},
		{"APP_GUEST_PASSWORD_FILE", cfg.GuestPasswordFile},
		{"APP_SSH_KEY_PATH", cfg.SSHKeyPath},
	}
	for _, r := range required {
		if r.value == "" {
			missing = append(missing, r.name)
		}
	}
	if len(missing) > 0 {
		return config{}, fmt.Errorf("faltan variables de entorno obligatorias: %s", strings.Join(missing, ", "))
	}

	// create-lb.sh recibe la ruta como argumento y corre con otro directorio de trabajo: debe ser absoluta.
	if !filepath.IsAbs(cfg.HAProxyBaseDisk) {
		return config{}, fmt.Errorf("APP_HAPROXY_BASE_DISK debe ser una ruta absoluta")
	}

	scriptsDir, err := filepath.Abs(getenv("APP_SCRIPTS_DIR", "../scripts"))
	if err != nil {
		return config{}, fmt.Errorf("resolver APP_SCRIPTS_DIR: %w", err)
	}
	cfg.ScriptsDir = scriptsDir
	return cfg, nil
}

// getenv devuelve la variable o fallback si está vacía.
func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

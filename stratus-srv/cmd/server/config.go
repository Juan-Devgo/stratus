package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// config contiene únicamente rutas y parámetros de coordinación. El backend no lee la contraseña
// de la imagen base: VBoxManage recibe APP_GUEST_PASSWORD_FILE directamente en los scripts.
type config struct {
	ListenAddr        string
	DBPath            string
	ScriptsDir        string
	BaseDisk          string
	HAProxyBaseDisk   string
	BridgeAdapter     string
	GuestPasswordFile string
	SSHKeyPath        string
}

func loadConfig() (config, error) {
	cfg := config{
		ListenAddr:        envOrDefault("APP_LISTEN_ADDR", "127.0.0.1:8080"),
		DBPath:            envOrDefault("APP_DB_PATH", "./stratus.db"),
		ScriptsDir:        envOrDefault("APP_SCRIPTS_DIR", "../scripts"),
		BaseDisk:          os.Getenv("APP_BASE_DISK"),
		HAProxyBaseDisk:   os.Getenv("APP_HAPROXY_BASE_DISK"),
		BridgeAdapter:     os.Getenv("APP_BRIDGE_ADAPTER"),
		GuestPasswordFile: os.Getenv("APP_GUEST_PASSWORD_FILE"),
		SSHKeyPath:        os.Getenv("APP_SSH_KEY_PATH"),
	}

	required := []struct {
		name  string
		value string
	}{
		{"APP_BASE_DISK", cfg.BaseDisk},
		{"APP_HAPROXY_BASE_DISK", cfg.HAProxyBaseDisk},
		{"APP_BRIDGE_ADAPTER", cfg.BridgeAdapter},
		{"APP_GUEST_PASSWORD_FILE", cfg.GuestPasswordFile},
		{"APP_SSH_KEY_PATH", cfg.SSHKeyPath},
	}
	for _, item := range required {
		if item.value == "" {
			return config{}, fmt.Errorf("falta la variable obligatoria %s", item.name)
		}
	}
	for _, disk := range []struct {
		name string
		path string
	}{
		{"APP_BASE_DISK", cfg.BaseDisk},
		{"APP_HAPROXY_BASE_DISK", cfg.HAProxyBaseDisk},
	} {
		if !filepath.IsAbs(disk.path) {
			return config{}, fmt.Errorf("%s debe ser una ruta absoluta", disk.name)
		}
	}

	return cfg, nil
}

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setRequiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("APP_BASE_DISK", "/disks/stratus-base.vdi")
	t.Setenv("APP_HAPROXY_BASE_DISK", "/disks/stratus-haproxy-base.vdi")
	t.Setenv("APP_BRIDGE_ADAPTER", "en0")
	t.Setenv("APP_GUEST_PASSWORD_FILE", "/secrets/guest-password")
	t.Setenv("APP_SSH_KEY_PATH", "/keys/stratus_backend")
}

func TestLoadConfigUsesDefaults(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("APP_LISTEN_ADDR", "")
	t.Setenv("APP_DB_PATH", "")
	t.Setenv("APP_SCRIPTS_DIR", "")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}

	if cfg.ListenAddr != "127.0.0.1:8080" {
		t.Errorf("ListenAddr = %q", cfg.ListenAddr)
	}
	if cfg.DBPath != "./stratus.db" {
		t.Errorf("DBPath = %q", cfg.DBPath)
	}
	if cfg.ScriptsDir != "../scripts" {
		t.Errorf("ScriptsDir = %q", cfg.ScriptsDir)
	}
	if cfg.BaseDisk != "/disks/stratus-base.vdi" || cfg.HAProxyBaseDisk != "/disks/stratus-haproxy-base.vdi" {
		t.Errorf("discos inesperados: %#v", cfg)
	}
	if cfg.BridgeAdapter != "en0" || cfg.GuestPasswordFile != "/secrets/guest-password" || cfg.SSHKeyPath != "/keys/stratus_backend" {
		t.Errorf("variables obligatorias inesperadas: %#v", cfg)
	}
}

func TestLoadConfigRequiresVariables(t *testing.T) {
	required := []string{
		"APP_BASE_DISK",
		"APP_HAPROXY_BASE_DISK",
		"APP_BRIDGE_ADAPTER",
		"APP_GUEST_PASSWORD_FILE",
		"APP_SSH_KEY_PATH",
	}
	for _, name := range required {
		t.Run(name, func(t *testing.T) {
			setRequiredEnv(t)
			t.Setenv(name, "")
			_, err := loadConfig()
			if err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("loadConfig() error = %v; debe nombrar %s", err, name)
			}
		})
	}
}

func TestLoadConfigRequiresAbsoluteDisks(t *testing.T) {
	for _, name := range []string{"APP_BASE_DISK", "APP_HAPROXY_BASE_DISK"} {
		t.Run(name, func(t *testing.T) {
			setRequiredEnv(t)
			t.Setenv(name, "relative/image.vdi")
			_, err := loadConfig()
			if err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("loadConfig() error = %v; debe rechazar la ruta relativa de %s", err, name)
			}
		})
	}
}

func TestLoadConfigIgnoresRemovedVariables(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("APP_DEBIAN_ISO", "")
	t.Setenv("APP_GUEST_USER", "")
	t.Setenv("APP_GUEST_SECRETS_DIR", "")
	if _, err := loadConfig(); err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
}

func TestAC45LoadConfigDoesNotOpenPasswordFile(t *testing.T) {
	setRequiredEnv(t)
	passwordFile := filepath.Join(t.TempDir(), "guest-password")
	if err := os.WriteFile(passwordFile, []byte("secreto\n"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APP_GUEST_PASSWORD_FILE", passwordFile)

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig abrió o rechazó el archivo: %v", err)
	}
	if cfg.GuestPasswordFile != passwordFile {
		t.Fatalf("GuestPasswordFile = %q", cfg.GuestPasswordFile)
	}
}

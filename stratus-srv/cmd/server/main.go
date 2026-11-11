// Comando server: API REST de Stratus. Corre en el host, junto a VirtualBox, y escucha solo
// en 127.0.0.1 porque la consola no tiene autenticación.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"stratus-srv/internal/handler"
	"stratus-srv/internal/repository"
	"stratus-srv/internal/service"
)

const (
	// scriptTimeout es el máximo que espera cada operación (REQ-14).
	scriptTimeout = 15 * time.Minute
	// killGrace es lo que espera un script, tras el SIGTERM del timeout, antes del SIGKILL: le da
	// tiempo de limpiar (create-vm.sh quita la VM a medio crear y su disco diferencial).
	killGrace = 30 * time.Second
	// writeTimeout deja un minuto de margen sobre scriptTimeout para escribir la respuesta.
	writeTimeout = 16 * time.Minute
	// maxArchiveBytes es el tamaño máximo del .tar.gz de un servicio (003 REQ-4) y de una página web
	// (005 REQ-9).
	maxArchiveBytes = 200 << 20
	// userKeyTTL es cuánto vive en memoria la llave privada del usuario (REQ-8).
	userKeyTTL = 10 * time.Minute
)

func main() {
	logger := log.New(os.Stderr, "", log.LstdFlags)

	cfg, err := loadConfig()
	if err != nil {
		logger.Fatal(err)
	}

	db, err := repository.Open(cfg.DBPath)
	if err != nil {
		logger.Fatal(err)
	}
	defer db.Close()

	// Los scripts heredan el entorno (APP_GUEST_PASSWORD_FILE, APP_SSH_KEY_PATH).
	runner := service.ExecRunner{ScriptsDir: cfg.ScriptsDir, Timeout: scriptTimeout, KillGrace: killGrace}

	// Si falta la llave del backend se crea con su script (REQ-23). Un fallo no detiene el
	// servidor: la consola sigue sirviendo (GET /healthz, listado, arranque y apagado de VMs, que
	// no usan SSH) y solo fallan las operaciones por SSH hasta que el par de llaves exista.
	if err := service.EnsureBackendKey(context.Background(), runner, cfg.SSHKeyPath); err != nil {
		logger.Printf("no se pudo crear la llave del backend en APP_SSH_KEY_PATH: %v", err)
	}

	// Un solo bloqueo por VM compartido por todos los módulos: una operación en curso de
	// cualquiera rechaza a las demás sobre la misma VM (AC-24).
	locks := &service.VMLocks{}
	vms := repository.NewVMRepository(db)
	lbs := repository.NewLBRepository(db)
	history := repository.NewHistoryRepository(db)
	webRepo := repository.NewWebRepository(db)
	dnsRepo := repository.NewDNSRepository(db)

	// DNSService va primero: crear y eliminar servidores web, balanceadores y VMs mantiene sus
	// registros, y el servidor DNS se elimina y protege desde 001 y 004.
	dns := service.NewDNSService(service.DNSConfig{
		BaseDisk:      cfg.DNSBaseDisk,
		BridgeAdapter: cfg.BridgeAdapter,
		IP:            cfg.DNSIP,
		Gateway:       cfg.DNSGateway,
		Domain:        cfg.DNSDomain,
		Forwarders:    cfg.DNSForwarders,
	}, runner, vms, lbs, webRepo, dnsRepo, history, locks, logger)

	// HAProxyService va antes que PrepareService: eliminar un balanceador desde 001 y refrescar sus
	// IPs al encender una VM pasan por él.
	haproxy := service.NewHAProxyService(service.HAProxyConfig{
		BaseDisk:      cfg.HAProxyBaseDisk,
		BridgeAdapter: cfg.BridgeAdapter,
	}, runner, vms, lbs, dnsRepo, history, locks, dns, logger)

	// WebService también va antes que PrepareService: el tipo del selector, las reglas de paquetes y
	// la eliminación de un servidor web desde 001 pasan por él.
	web := service.NewWebService(service.WebConfig{
		ApacheBaseDisk:  cfg.ApacheBaseDisk,
		NginxBaseDisk:   cfg.NginxBaseDisk,
		BridgeAdapter:   cfg.BridgeAdapter,
		Domain:          cfg.DNSDomain,
		MaxArchiveBytes: maxArchiveBytes,
	}, runner, vms, lbs, webRepo, dnsRepo, history, locks, dns, logger)

	prepare := service.NewPrepareService(service.Config{
		BaseDisk:      cfg.BaseDisk,
		BridgeAdapter: cfg.BridgeAdapter,
		SSHKeyPath:    cfg.SSHKeyPath,
		KeyTTL:        userKeyTTL,
	}, runner, vms, lbs, history, locks, haproxy, web, dns, logger)

	users := service.NewUsersService(service.UsersConfig{KeyTTL: userKeyTTL}, runner,
		vms, lbs, history, locks, logger)

	services := service.NewServicesService(service.ServicesConfig{MaxArchiveBytes: maxArchiveBytes}, runner,
		vms, lbs, history, locks, logger)

	mux := http.NewServeMux()
	registerHealthz(mux)
	handler.RegisterPrepare(mux, prepare)
	handler.RegisterUsers(mux, users)
	handler.RegisterServices(mux, services)
	handler.RegisterHAProxy(mux, haproxy)
	handler.RegisterWeb(mux, web)
	handler.RegisterDNS(mux, dns)

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      writeTimeout,
	}
	logger.Printf("escuchando en %s (scripts en %s)", cfg.ListenAddr, cfg.ScriptsDir)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Fatal(err)
	}
}

// registerHealthz registra GET /healthz, que responde {"status":"ok"} mientras el proceso vive.
func registerHealthz(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(struct {
			Status string `json:"status"`
		}{Status: "ok"}); err != nil {
			log.Printf("no se pudo responder healthz: %v", err)
		}
	})
}

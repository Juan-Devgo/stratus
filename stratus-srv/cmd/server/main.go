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
	operationTimeout = 15 * time.Minute
	killGrace        = 30 * time.Second
	keyTTL           = 10 * time.Minute
	maxArchiveBytes  = int64(200 << 20)
)

func main() {
	logger := log.New(os.Stderr, "stratus: ", log.Ldate|log.Ltime|log.LUTC)
	cfg, err := loadConfig()
	if err != nil {
		logger.Fatal(err)
	}

	db, err := repository.Open(cfg.DBPath)
	if err != nil {
		logger.Fatal(err)
	}
	defer db.Close()

	runner := service.ExecRunner{
		ScriptsDir: cfg.ScriptsDir,
		Timeout:    operationTimeout,
		KillGrace:  killGrace,
	}
	if err := service.EnsureBackendKey(context.Background(), runner, cfg.SSHKeyPath); err != nil {
		// La especificación permite arrancar para que /healthz y la UI sigan disponibles; las
		// operaciones que requieran la llave informarán el fallo correspondiente.
		logger.Printf("no se pudo preparar la llave SSH del backend: %v", err)
	}

	vms := repository.NewVMRepository(db)
	history := repository.NewHistoryRepository(db)
	lbs := repository.NewLBRepository(db)
	locks := &service.VMLocks{}

	haproxyService := service.NewHAProxyService(service.HAProxyConfig{
		BaseDisk:      cfg.HAProxyBaseDisk,
		BridgeAdapter: cfg.BridgeAdapter,
	}, runner, vms, lbs, history, locks, logger)
	prepareService := service.NewPrepareService(service.Config{
		BaseDisk:      cfg.BaseDisk,
		BridgeAdapter: cfg.BridgeAdapter,
		SSHKeyPath:    cfg.SSHKeyPath,
		KeyTTL:        keyTTL,
	}, runner, vms, lbs, history, locks, haproxyService, logger)
	usersService := service.NewUsersService(service.UsersConfig{KeyTTL: keyTTL}, runner, vms, lbs, history, locks, logger)
	servicesService := service.NewServicesService(service.ServicesConfig{MaxArchiveBytes: maxArchiveBytes}, runner, vms, lbs, history, locks, logger)

	mux := http.NewServeMux()
	registerHealthz(mux)
	handler.RegisterPrepare(mux, prepareService)
	handler.RegisterUsers(mux, usersService)
	handler.RegisterServices(mux, servicesService)
	handler.RegisterHAProxy(mux, haproxyService)

	server := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      16 * time.Minute,
	}
	logger.Printf("servidor escuchando en http://%s", cfg.ListenAddr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Fatal(err)
	}
}

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

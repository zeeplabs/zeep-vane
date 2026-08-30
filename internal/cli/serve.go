package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/api"
	"github.com/zeeplabs/zeep-vane/internal/config"
	"github.com/zeeplabs/zeep-vane/internal/connectors/datadog"
	"github.com/zeeplabs/zeep-vane/internal/crypto"
	"github.com/zeeplabs/zeep-vane/internal/db"
	"github.com/zeeplabs/zeep-vane/internal/logging"
	"github.com/zeeplabs/zeep-vane/internal/poller"
	"github.com/zeeplabs/zeep-vane/internal/retention"
	"github.com/zeeplabs/zeep-vane/internal/router"
	vanetls "github.com/zeeplabs/zeep-vane/internal/tls"
)

// pruneTick and pruneRetention control the retention Pruner started in
// RunE (SHU-16..20) - own independent 1h ticker, deleting closed status
// intervals older than 35 days.
const pruneTick = 1 * time.Hour
const pruneRetention = 35 * 24 * time.Hour

// shutdownTimeout bounds how long the HTTP server gets to finish in-flight
// requests once a shutdown signal arrives.
const shutdownTimeout = 10 * time.Second

// defaultHTTPSPort is used when HTTPS_PORT is not set. 443 is where
// browsers expect a status page's custom domain to answer.
const defaultHTTPSPort = "443"

// defaultCertMagicStoragePath is used when CERTMAGIC_STORAGE_PATH is not
// set. In production this must point at a volume that survives container
// restarts (design.md Risks & Concerns) - operators are expected to set
// CERTMAGIC_STORAGE_PATH explicitly rather than rely on this default.
const defaultCertMagicStoragePath = "./certmagic-data"

// NewServeCmd builds the serve subcommand, wiring config, the Postgres
// pool, the HTTP router, and the SLO poller together. Both the HTTP server
// and the poller run until SIGINT/SIGTERM, then shut down cleanly - the
// poller's own context is canceled so its ticker loop exits, no goroutine
// is left running after RunE returns.
func NewServeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Starts the vane HTTP server",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			logger, err := logging.New(cfg.LogLevel)
			if err != nil {
				return err
			}
			defer func() { _ = logger.Sync() }()

			ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()

			pool, err := db.NewPool(ctx, cfg.DatabaseURL)
			if err != nil {
				return err
			}
			defer pool.Close()

			// Applies every embedded migration before anything else
			// starts, so a container with no migrations/ directory on
			// disk (FROM scratch) is still fully migrated by the time it
			// begins serving (SHD-06). This is a new startup
			// responsibility `vane serve` did not previously have - the
			// explicit `vane migrate up` CLI command still works
			// unchanged for anyone who wants that as its own deliberate
			// step (design.md Risks & Concerns).
			if err := db.MigrateUpEmbedded(cfg.DatabaseURL); err != nil {
				return fmt.Errorf("serve: failed to apply embedded migrations: %w", err)
			}
			logger.Info("serve: embedded migrations applied")

			// pollerManager owns the poller's lifecycle for the rest of this
			// process's life - not just at boot. Restart is also called by
			// IntegrationsHandler.ConnectDatadog after every successful
			// connect/rotate, which is what lets an admin start seeing real
			// data without restarting serve (PLD-01/PLD-05).
			//
			// RunLeaderLoop (not a direct boot-time Restart call) gates
			// which replica actually runs the poller: it acquires a
			// Postgres advisory lock before calling Restart, so with more
			// than one replica sharing this database, only the lock holder
			// ever polls (ha-multi-replica HA-01..HA-07). With a single
			// replica this resolves immediately and behaves exactly like
			// the unconditional Restart it replaces.
			pollerManager := NewPollerManager(ctx, pool, cfg, logger, cfg.DatabaseURL)
			go pollerManager.RunLeaderLoop(ctx)

			// The retention Pruner runs on its own ticker, independent of
			// the poller's (design.md: polling and pruning are unrelated
			// responsibilities) - canceled by the same ctx/stop() as the
			// poller and the HTTP/HTTPS listeners below (SHU-16..20).
			pruner := retention.NewPruner(db.NewStatusIntervalRepository(pool), pruneTick, pruneRetention, logger)
			go pruner.Run(ctx)

			addr := fmt.Sprintf(":%d", cfg.Port)
			srv := &http.Server{Addr: addr, Handler: buildAdminRouter(pool, cfg, logger, pollerManager)}

			var httpsSrv *http.Server
			serverErrs := make(chan error, 2)
			go func() {
				logger.Info("serve: listening", zap.String("addr", addr))
				if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
					serverErrs <- err
					return
				}
				serverErrs <- nil
			}()
			if cfg.HTTPSEnabled {
				httpsSrv = newHTTPSServer(pool, logger)
				go func() {
					logger.Info("serve: https listening (on-demand tls)", zap.String("addr", httpsSrv.Addr))
					if err := httpsSrv.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
						serverErrs <- err
						return
					}
					serverErrs <- nil
				}()
			} else {
				logger.Warn("serve: https listener disabled (VANE_HTTPS_ENABLED=false) - custom status-page domains will not be reachable")
			}

			select {
			case <-ctx.Done():
				// Signal received - fall through to the graceful shutdown
				// below.
			case err := <-serverErrs:
				stop() // cancel ctx so the poller's Run loop exits too
				pollerManager.Stop()
				return err
			}

			shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
			defer cancel()
			if err := srv.Shutdown(shutdownCtx); err != nil {
				logger.Error("serve: http shutdown error", zap.Error(err))
			}
			if httpsSrv != nil {
				if err := httpsSrv.Shutdown(shutdownCtx); err != nil {
					logger.Error("serve: https shutdown error", zap.Error(err))
				}
			}

			pollerManager.Stop()
			return nil
		},
	}
}

// newHTTPSServer builds the HTTPS listener that serves status pages on
// their custom domains, with on-demand TLS via CertMagic (SP-11, SP-12,
// SP-13). Its port and CertMagic's certificate storage path are both
// configurable via env - HTTPS_PORT and CERTMAGIC_STORAGE_PATH - falling
// back to 443 and ./certmagic-data respectively when unset. HostPolicy
// (internal/tls) gates every certificate request against the StatusPage
// table, and OnEvent records the real outcome of each issuance attempt back
// onto the matching StatusPage row.
//
// Its handler is router.HostRouter wrapping a small mux of two routes -
// the public status page handler at "/" and the public logo file handler
// at "/uploads/" (SET-06, SET-12) - rather than the public status handler
// alone: HostRouter forwards every path on a matched hostname to whatever
// single handler it's given (internal/router/host_router.go), so without
// this mux, a request for a status page's own logo would hit the status
// JSON handler instead of the file (design.md Risks & Concerns). A
// request's Host header resolves to a published StatusPage, whose ID is
// threaded down so the public handler's services/incidents queries are
// scoped to that status page (SP-15); the logo route needs no such
// scoping - the logo file is a single, install-wide singleton (SET-06).
// The admin API/SPA is served on the separate HTTP listener built in RunE
// (router.New) - HostRouter here never touches it (design.md placeholder).
func newHTTPSServer(pool *db.Pool, logger *zap.Logger) *http.Server {
	httpsPort := os.Getenv("HTTPS_PORT")
	if httpsPort == "" {
		httpsPort = defaultHTTPSPort
	}

	storagePath := os.Getenv("CERTMAGIC_STORAGE_PATH")
	if storagePath == "" {
		storagePath = defaultCertMagicStoragePath
	}

	statusPages := db.NewStatusPageRepository(pool)
	manager := vanetls.NewManager(statusPages, storagePath)

	services := db.NewServiceRepository(pool)
	intervals := db.NewStatusIntervalRepository(pool)
	incidents := db.NewIncidentRepository(pool)
	companySettings := db.NewCompanySettingsRepository(pool)
	publicHandler := api.NewPublicStatusHandler(services, intervals, incidents, companySettings, logger)
	logoFileHandler := api.NewLogoFileHandler(companySettings)

	publicMux := http.NewServeMux()
	publicMux.Handle("/uploads/", logoFileHandler)
	publicMux.HandleFunc("/", publicHandler.Get)

	// hsts=true - this listener really does terminate TLS, unlike the admin
	// HTTP listener (M14).
	handler := api.SecurityHeaders(true)(router.HostRouter(statusPages, publicMux))

	tlsConfig := manager.TLSConfig()
	tlsConfig.NextProtos = append([]string{"h2", "http/1.1"}, tlsConfig.NextProtos...)

	return &http.Server{
		Addr:      ":" + httpsPort,
		Handler:   handler,
		TLSConfig: tlsConfig,
	}
}

// newPollerFromStoredIntegration builds a Poller from whatever Datadog
// integration is currently connected. started is false (with a nil error)
// if no integration has been connected yet - the poller then simply isn't
// started; PollerManager.Restart is what lets an admin connecting Datadog
// after boot start it without a process restart (PLD-01).
func newPollerFromStoredIntegration(ctx context.Context, pool *db.Pool, cfg config.Config, logger *zap.Logger) (p *poller.Poller, started bool, err error) {
	integrations := db.NewIntegrationRepository(pool)

	integration, err := integrations.GetDatadog(ctx)
	if errors.Is(err, db.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}

	apiKey, err := crypto.Decrypt(cfg.MasterKey, integration.EncryptedAPIKey)
	if err != nil {
		return nil, false, fmt.Errorf("serve: failed to decrypt datadog api key: %w", err)
	}
	appKey, err := crypto.Decrypt(cfg.MasterKey, integration.EncryptedAppKey)
	if err != nil {
		return nil, false, fmt.Errorf("serve: failed to decrypt datadog app key: %w", err)
	}

	client := datadog.NewClient(string(apiKey), string(appKey))
	services := db.NewServiceRepository(pool)
	intervals := db.NewStatusIntervalRepository(pool)
	interval := time.Duration(cfg.PollIntervalSeconds) * time.Second

	return poller.NewPoller(services, services, intervals, integrations, client, interval, logger), true, nil
}

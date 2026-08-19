package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/config"
	"github.com/zeeplabs/zeep-vane/internal/connectors/datadog"
	"github.com/zeeplabs/zeep-vane/internal/crypto"
	"github.com/zeeplabs/zeep-vane/internal/db"
	"github.com/zeeplabs/zeep-vane/internal/logging"
	"github.com/zeeplabs/zeep-vane/internal/poller"
	"github.com/zeeplabs/zeep-vane/internal/router"
)

// shutdownTimeout bounds how long the HTTP server gets to finish in-flight
// requests once a shutdown signal arrives.
const shutdownTimeout = 10 * time.Second

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

			pollerDone := make(chan struct{})
			slaPoller, started, err := newPollerFromStoredIntegration(pool, cfg, logger)
			if err != nil {
				return err
			}
			if started {
				go func() {
					defer close(pollerDone)
					slaPoller.Run(ctx)
				}()
			} else {
				logger.Warn("serve: no datadog integration connected yet, poller not started")
				close(pollerDone)
			}

			addr := fmt.Sprintf(":%d", cfg.Port)
			srv := &http.Server{Addr: addr, Handler: router.New(pool)}

			serverErrs := make(chan error, 1)
			go func() {
				logger.Info("serve: listening", zap.String("addr", addr))
				if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
					serverErrs <- err
					return
				}
				serverErrs <- nil
			}()

			select {
			case <-ctx.Done():
				// Signal received - fall through to the graceful shutdown
				// below.
			case err := <-serverErrs:
				stop() // cancel ctx so the poller's Run loop exits too
				<-pollerDone
				return err
			}

			shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
			defer cancel()
			if err := srv.Shutdown(shutdownCtx); err != nil {
				logger.Error("serve: http shutdown error", zap.Error(err))
			}

			<-pollerDone
			return nil
		},
	}
}

// newPollerFromStoredIntegration builds a Poller from whatever Datadog
// integration is currently connected. started is false (with a nil error)
// if no integration has been connected yet - the poller then simply isn't
// started; the admin can connect Datadog and restart serve.
func newPollerFromStoredIntegration(pool *db.Pool, cfg config.Config, logger *zap.Logger) (p *poller.Poller, started bool, err error) {
	integrations := db.NewIntegrationRepository(pool)

	integration, err := integrations.GetDatadog(context.Background())
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
	snapshots := db.NewStatusSnapshotRepository(pool)
	interval := time.Duration(cfg.PollIntervalSeconds) * time.Second

	return poller.NewPoller(services, services, snapshots, integrations, client, interval, logger), true, nil
}

package cli

import (
	"context"
	"fmt"
	"net/http"

	"github.com/spf13/cobra"
	"github.com/zeeplabs/zeep-vane/internal/config"
	"github.com/zeeplabs/zeep-vane/internal/db"
	"github.com/zeeplabs/zeep-vane/internal/router"
)

// NewServeCmd builds the serve subcommand, wiring config, the Postgres pool,
// and the HTTP router together.
func NewServeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Starts the vane HTTP server",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			pool, err := db.NewPool(context.Background(), cfg.DatabaseURL)
			if err != nil {
				return err
			}
			defer pool.Close()

			addr := fmt.Sprintf(":%d", cfg.Port)
			fmt.Printf("vane serve: listening on %s\n", addr)

			return http.ListenAndServe(addr, router.New(pool))
		},
	}
}

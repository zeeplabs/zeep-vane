package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/zeeplabs/zeep-vane/internal/config"
	"github.com/zeeplabs/zeep-vane/internal/db"
)

// NewMigrateCmd builds the migrate subcommand with its "up" child.
func NewMigrateCmd() *cobra.Command {
	migrateCmd := &cobra.Command{
		Use:   "migrate",
		Short: "Manages database schema migrations",
	}

	migrateCmd.AddCommand(newMigrateUpCmd())

	return migrateCmd
}

func newMigrateUpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "up",
		Short: "Applies all pending migrations",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			if err := db.MigrateUp(cfg.DatabaseURL, "internal/db/migrations"); err != nil {
				return err
			}

			fmt.Println("migrate up: complete")
			return nil
		},
	}
}

package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// NewServeCmd builds the serve subcommand. It has no server logic yet.
func NewServeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Starts the vane HTTP server",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("serve: not implemented")
			return nil
		},
	}
}

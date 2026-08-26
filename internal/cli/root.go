package cli

import "github.com/spf13/cobra"

// NewRootCmd builds the root cobra command for the vane binary.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "vane",
		Short: "zeep-vane — self-hosted status page connected to Datadog SLOs",
	}

	root.AddCommand(NewServeCmd())
	root.AddCommand(NewMigrateCmd())
	root.AddCommand(NewHealthcheckCmd())

	return root
}

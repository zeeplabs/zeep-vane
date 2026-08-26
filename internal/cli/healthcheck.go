package cli

import (
	"fmt"
	"net/http"
	"time"

	"github.com/spf13/cobra"

	"github.com/zeeplabs/zeep-vane/internal/config"
)

// healthcheckTimeout bounds how long the healthcheck request waits before
// treating the admin listener as unhealthy - short enough that a stuck
// listener fails Docker's HEALTHCHECK promptly, not after its own timeout.
const healthcheckTimeout = 3 * time.Second

// NewHealthcheckCmd builds the `vane healthcheck` subcommand: a GET against
// the admin listener's own /healthz, exiting 0 on 200 and non-zero
// otherwise. It exists because the production image (M16) is built FROM
// scratch - no shell, no curl/wget - so Docker's HEALTHCHECK directive has
// nothing else it could exec inside the container. Reads PORT the same way
// `vane serve` does (config.Load), so it always checks the same listener
// serve is actually running on.
func NewHealthcheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "healthcheck",
		Short: "Checks the admin listener's /healthz endpoint, for use as a container HEALTHCHECK",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			client := &http.Client{Timeout: healthcheckTimeout}
			url := fmt.Sprintf("http://127.0.0.1:%d/healthz", cfg.Port)
			resp, err := client.Get(url)
			if err != nil {
				return fmt.Errorf("healthcheck: request to %s failed: %w", url, err)
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("healthcheck: %s returned status %d, want %d", url, resp.StatusCode, http.StatusOK)
			}

			return nil
		},
	}
}

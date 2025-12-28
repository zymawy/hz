// Package hz implements the hz CLI
package hz

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	version   = "0.1.0"
	cfgFile   string
	verbosity int
)

// rootCmd is the base command
var rootCmd = &cobra.Command{
	Use:   "hz",
	Short: "hz - Smart development proxy with ngrok integration",
	Long: `hz is a development proxy that routes traffic to multiple local services
through a single endpoint with integrated ngrok tunnel support.

Features:
  - Multi-service routing (path, header, subdomain based)
  - Integrated ngrok tunnel for external access
  - Health checking and service discovery
  - Hot-reload configuration
  - WebSocket support

Example:
  hz start                    # Start with default config (hz.yaml)
  hz start -c custom.yaml     # Start with custom config
  hz add backend 3001         # Add a service dynamically
  hz tunnel                   # Enable ngrok tunnel
  hz status                   # Show proxy status`,
	Version: version,
}

// Execute runs the root command
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "", "config file (default: hz.yaml)")
	rootCmd.PersistentFlags().CountVarP(&verbosity, "verbose", "v", "increase verbosity (-v, -vv, -vvv)")
}

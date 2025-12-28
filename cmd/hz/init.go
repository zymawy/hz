package hz

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/zymawy/hz/internal/config"
)

var (
	initForce bool
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a new hz configuration",
	Long: `Create a new hz.yaml configuration file in the current directory.

The default configuration includes:
  - A backend service on port 3001
  - Basic tunnel configuration (disabled by default)
  - Standard logging settings

Examples:
  hz init              # Create hz.yaml in current directory
  hz init --force      # Overwrite existing config`,
	RunE: runInit,
}

func init() {
	initCmd.Flags().BoolVarP(&initForce, "force", "f", false, "overwrite existing config file")

	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
	configPath := "hz.yaml"

	// Check if file exists
	if _, err := os.Stat(configPath); err == nil && !initForce {
		return fmt.Errorf("config file already exists. Use --force to overwrite")
	}

	// Create config directory if needed
	dir := filepath.Dir(configPath)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory: %w", err)
		}
	}

	// Create default config
	if err := config.CreateDefaultConfig(configPath); err != nil {
		return fmt.Errorf("failed to create config: %w", err)
	}

	abs, _ := filepath.Abs(configPath)
	fmt.Printf("✅ Created %s\n\n", abs)

	fmt.Printf("Next steps:\n")
	fmt.Printf("  1. Edit hz.yaml to configure your services\n")
	fmt.Printf("  2. Run 'hz start' to start the proxy\n")
	fmt.Printf("  3. Run 'hz tunnel --enable' to enable ngrok\n\n")

	fmt.Printf("Quick start:\n")
	fmt.Printf("  hz add backend 3001 --default\n")
	fmt.Printf("  hz add api 8080 --route '/api/*'\n")
	fmt.Printf("  hz start\n")

	return nil
}

package hz

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/zymawy/hz/internal/config"
	"gopkg.in/yaml.v3"
)

var (
	tunnelEnable  bool
	tunnelDisable bool
	tunnelDomain  string
	tunnelToken   string
)

var tunnelCmd = &cobra.Command{
	Use:   "tunnel",
	Short: "Configure ngrok tunnel settings",
	Long: `Configure the ngrok tunnel for external access.

Examples:
  hz tunnel --enable              # Enable tunnel
  hz tunnel --disable             # Disable tunnel
  hz tunnel --domain myapp.ngrok.io   # Set custom domain
  hz tunnel --token abc123        # Set auth token`,
	RunE: runTunnel,
}

func init() {
	tunnelCmd.Flags().BoolVar(&tunnelEnable, "enable", false, "enable ngrok tunnel")
	tunnelCmd.Flags().BoolVar(&tunnelDisable, "disable", false, "disable ngrok tunnel")
	tunnelCmd.Flags().StringVar(&tunnelDomain, "domain", "", "set custom ngrok domain")
	tunnelCmd.Flags().StringVar(&tunnelToken, "token", "", "set ngrok auth token")

	rootCmd.AddCommand(tunnelCmd)
}

func runTunnel(cmd *cobra.Command, args []string) error {
	// Find config file
	configPath := cfgFile
	if configPath == "" {
		var err error
		configPath, err = config.FindConfigFile()
		if err != nil {
			return fmt.Errorf("no config file found. Run 'hz init' first")
		}
	}

	// Load config
	cfgManager, err := config.NewManager(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	cfg := cfgManager.Get()
	modified := false

	// Apply changes
	if tunnelEnable {
		cfg.Tunnel.Enabled = true
		modified = true
		fmt.Println("✅ Tunnel enabled")
	}

	if tunnelDisable {
		cfg.Tunnel.Enabled = false
		modified = true
		fmt.Println("✅ Tunnel disabled")
	}

	if tunnelDomain != "" {
		cfg.Tunnel.Domain = tunnelDomain
		modified = true
		fmt.Printf("✅ Tunnel domain set to: %s\n", tunnelDomain)
	}

	if tunnelToken != "" {
		cfg.Tunnel.AuthToken = tunnelToken
		modified = true
		fmt.Println("✅ Tunnel auth token updated")
	}

	// If no flags, show current status
	if !modified {
		fmt.Printf("🌐 Tunnel Configuration:\n")
		fmt.Printf("   Enabled:  %v\n", cfg.Tunnel.Enabled)
		fmt.Printf("   Provider: %s\n", cfg.Tunnel.Provider)
		if cfg.Tunnel.Domain != "" {
			fmt.Printf("   Domain:   %s\n", cfg.Tunnel.Domain)
		}
		if cfg.Tunnel.AuthToken != "" {
			fmt.Printf("   Token:    %s***\n", cfg.Tunnel.AuthToken[:4])
		} else {
			fmt.Printf("   Token:    (not set)\n")
		}
		return nil
	}

	// Save config
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	fmt.Printf("\nConfiguration saved to %s\n", configPath)
	return nil
}

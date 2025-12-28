package hz

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/spf13/cobra"
	"github.com/zymawy/hz/internal/config"
)

var (
	statusJSON bool
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show proxy and service status",
	Long: `Display the current status of the hz proxy and all registered services.

Shows:
  - Proxy running status
  - Registered services and their health
  - Tunnel status and public URL
  - Request statistics

Examples:
  hz status           # Show formatted status
  hz status --json    # Output as JSON`,
	RunE: runStatus,
}

func init() {
	statusCmd.Flags().BoolVar(&statusJSON, "json", false, "output as JSON")

	rootCmd.AddCommand(statusCmd)
}

func runStatus(cmd *cobra.Command, args []string) error {
	// Find config file
	configPath := cfgFile
	if configPath == "" {
		var err error
		configPath, err = config.FindConfigFile()
		if err != nil {
			return fmt.Errorf("no config file found. Run 'hz init' first")
		}
	}

	// Load config to show services
	cfgManager, err := config.NewManager(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	cfg := cfgManager.Get()

	// Build status struct
	status := struct {
		Running  bool   `json:"running"`
		Address  string `json:"address"`
		Config   string `json:"config"`
		Services []struct {
			Name    string `json:"name"`
			Target  string `json:"target"`
			Default bool   `json:"default,omitempty"`
			Status  string `json:"status"`
			Routes  int    `json:"routes"`
		} `json:"services"`
		Tunnel struct {
			Enabled   bool   `json:"enabled"`
			PublicURL string `json:"publicUrl,omitempty"`
			Domain    string `json:"domain,omitempty"`
		} `json:"tunnel"`
	}{
		Config: configPath,
	}

	// Check if proxy is running by trying to connect
	addr := fmt.Sprintf("http://%s:%d", cfg.Server.Host, cfg.Server.Port)
	status.Address = addr

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(addr + "/__hz/health")
	if err == nil {
		resp.Body.Close()
		status.Running = true
	}

	// Add services
	for _, svc := range cfg.Services {
		svcStatus := "configured"
		if status.Running {
			// Try health check
			if svc.Health != nil && svc.Health.Path != "" {
				healthResp, err := client.Get(svc.Target + svc.Health.Path)
				if err == nil {
					healthResp.Body.Close()
					if healthResp.StatusCode >= 200 && healthResp.StatusCode < 300 {
						svcStatus = "healthy"
					} else {
						svcStatus = "unhealthy"
					}
				} else {
					svcStatus = "unreachable"
				}
			} else {
				// No health check, try direct connection
				_, err := client.Get(svc.Target)
				if err == nil {
					svcStatus = "reachable"
				} else {
					svcStatus = "unreachable"
				}
			}
		}

		status.Services = append(status.Services, struct {
			Name    string `json:"name"`
			Target  string `json:"target"`
			Default bool   `json:"default,omitempty"`
			Status  string `json:"status"`
			Routes  int    `json:"routes"`
		}{
			Name:    svc.Name,
			Target:  svc.Target,
			Default: svc.Default,
			Status:  svcStatus,
			Routes:  len(svc.Routes),
		})
	}

	// Tunnel info
	status.Tunnel.Enabled = cfg.Tunnel.Enabled
	status.Tunnel.Domain = cfg.Tunnel.Domain

	// Output
	if statusJSON {
		data, _ := json.MarshalIndent(status, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	// Formatted output
	fmt.Printf("\n📊 hz Status\n")
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")

	// Proxy status
	if status.Running {
		fmt.Printf("🟢 Proxy:    Running at %s\n", status.Address)
	} else {
		fmt.Printf("🔴 Proxy:    Not running\n")
	}
	fmt.Printf("📁 Config:   %s\n", status.Config)

	// Services
	fmt.Printf("\n📦 Services:\n")
	for _, svc := range status.Services {
		statusIcon := "⚪"
		switch svc.Status {
		case "healthy", "reachable":
			statusIcon = "🟢"
		case "unhealthy", "unreachable":
			statusIcon = "🔴"
		case "configured":
			statusIcon = "⚪"
		}

		defaultMark := ""
		if svc.Default {
			defaultMark = " [default]"
		}

		fmt.Printf("   %s %s → %s%s\n", statusIcon, svc.Name, svc.Target, defaultMark)
		if svc.Routes > 0 {
			fmt.Printf("      Routes: %d\n", svc.Routes)
		}
	}

	// Tunnel
	fmt.Printf("\n🌐 Tunnel:\n")
	if status.Tunnel.Enabled {
		fmt.Printf("   Status:   Enabled\n")
		if status.Tunnel.PublicURL != "" {
			fmt.Printf("   URL:      %s\n", status.Tunnel.PublicURL)
		}
		if status.Tunnel.Domain != "" {
			fmt.Printf("   Domain:   %s\n", status.Tunnel.Domain)
		}
	} else {
		fmt.Printf("   Status:   Disabled\n")
	}

	fmt.Printf("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")

	return nil
}

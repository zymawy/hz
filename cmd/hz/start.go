package hz

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/zymawy/hz/internal/config"
	"github.com/zymawy/hz/internal/inspector"
	"github.com/zymawy/hz/internal/proxy"
	"github.com/zymawy/hz/internal/registry"
	"github.com/zymawy/hz/internal/router"
	"github.com/zymawy/hz/internal/tunnel"
	"github.com/zymawy/hz/pkg/types"
)

var (
	port        int
	noTunnel    bool
	watch       bool
	inspect     bool
	inspectPort int
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the proxy server",
	Long: `Start the hz proxy server with the specified configuration.

The server will:
  1. Load configuration from hz.yaml (or --config)
  2. Register all configured services
  3. Start health checking
  4. Optionally enable ngrok tunnel
  5. Optionally enable request inspector
  6. Begin proxying requests

Examples:
  hz start                    # Start with defaults
  hz start -p 8080            # Start on port 8080
  hz start --no-tunnel        # Start without ngrok
  hz start -w                 # Watch config for changes
  hz start --inspect          # Enable web inspector at localhost:4040
  hz start --inspect-port 8888 # Use custom inspector port`,
	RunE: runStart,
}

func init() {
	startCmd.Flags().IntVarP(&port, "port", "p", 0, "override port from config")
	startCmd.Flags().BoolVar(&noTunnel, "no-tunnel", false, "disable ngrok tunnel")
	startCmd.Flags().BoolVarP(&watch, "watch", "w", true, "watch config file for changes")
	startCmd.Flags().BoolVar(&inspect, "inspect", false, "enable web request inspector")
	startCmd.Flags().IntVar(&inspectPort, "inspect-port", 4040, "web inspector port")

	rootCmd.AddCommand(startCmd)
}

func runStart(cmd *cobra.Command, args []string) error {
	// Find or use specified config file
	configPath := cfgFile
	if configPath == "" {
		var err error
		configPath, err = config.FindConfigFile()
		if err != nil {
			return fmt.Errorf("no config file found: %w\n\nRun 'hz init' to create one", err)
		}
	}

	fmt.Printf("📁 Loading config: %s\n", configPath)

	// Load configuration
	cfgManager, err := config.NewManager(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	cfg := cfgManager.Get()

	// Override port if specified
	if port > 0 {
		cfg.Server.Port = port
	}

	// Create registry
	reg := registry.New()
	if err := reg.RegisterAll(cfg.Services); err != nil {
		return fmt.Errorf("failed to register services: %w", err)
	}

	// Create router
	rtr := router.New()
	if err := rtr.Build(cfg.Services); err != nil {
		return fmt.Errorf("failed to build routes: %w", err)
	}

	// Create proxy
	prx := proxy.New(reg, rtr)

	// Set up logger
	logger := log.New(os.Stdout, "[hz] ", log.LstdFlags)
	prx.SetLogger(logger)

	// Setup inspector if enabled
	var insp *inspector.Inspector
	if inspect {
		insp = inspector.New(inspectPort)
		insp.SetLogger(logger)
		prx.SetInspector(insp)
	}

	// Start watching config if enabled
	if watch {
		cfgManager.OnReload(func(newCfg *types.Config) {
			fmt.Println("🔄 Reloading configuration...")
			// Re-register services
			for _, svc := range newCfg.Services {
				reg.Register(svc)
			}
			// Rebuild routes
			rtr.Build(newCfg.Services)
		})
		cfgManager.Watch()
	}

	// Create HTTP server
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	server := &http.Server{
		Addr:         addr,
		Handler:      prx,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
	}

	// Setup tunnel if enabled
	var tunnelManager *tunnel.Manager
	if cfg.Tunnel.Enabled && !noTunnel {
		tunnelManager = tunnel.New(&cfg.Tunnel)
		tunnelManager.SetLogger(logger)
	}

	// Graceful shutdown handling
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Start server
	go func() {
		fmt.Printf("\n🚀 hz proxy starting...\n")
		fmt.Printf("   Local:  http://%s\n", addr)

		// Print registered services
		fmt.Printf("\n📦 Services:\n")
		for _, svc := range cfg.Services {
			defaultMark := ""
			if svc.Default {
				defaultMark = " (default)"
			}
			fmt.Printf("   • %s → %s%s\n", svc.Name, svc.Target, defaultMark)
		}

		// Start ngrok tunnel
		if tunnelManager != nil {
			fmt.Printf("\n🌐 Starting ngrok tunnel...\n")
			if err := tunnelManager.Start(prx); err != nil {
				logger.Printf("tunnel error: %v", err)
			} else {
				fmt.Printf("   Public: %s\n", tunnelManager.GetPublicURL())
			}
		}

		// Start inspector if enabled
		if insp != nil {
			fmt.Printf("\n🔍 Web Inspector:\n")
			if err := insp.Start(); err != nil {
				logger.Printf("inspector error: %v", err)
			} else {
				fmt.Printf("   http://127.0.0.1:%d/inspect/http\n", inspectPort)
			}
		}

		fmt.Printf("\n✨ Ready! Press Ctrl+C to stop\n\n")

		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			logger.Fatalf("server error: %v", err)
		}
	}()

	// Wait for interrupt
	<-ctx.Done()

	fmt.Printf("\n\n🛑 Shutting down...\n")

	// Shutdown with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Stop components
	if insp != nil {
		insp.Stop()
	}
	if tunnelManager != nil {
		tunnelManager.Stop()
	}
	cfgManager.Stop()
	reg.Stop()
	server.Shutdown(shutdownCtx)

	fmt.Println("👋 Goodbye!")
	return nil
}

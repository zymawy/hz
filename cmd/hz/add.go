package hz

import (
	"fmt"
	"os"
	"strconv"

	"github.com/spf13/cobra"
	"github.com/zymawy/hz/internal/config"
	"github.com/zymawy/hz/pkg/types"
	"gopkg.in/yaml.v3"
)

var (
	addDefault bool
	addRoutes  []string
	addRewrite string
)

var addCmd = &cobra.Command{
	Use:   "add <name> <port|url>",
	Short: "Add a service to the configuration",
	Long: `Add a new service to the hz configuration file.

The service can be specified by port number or full URL.

Examples:
  hz add backend 3001                    # Add backend on localhost:3001
  hz add api http://localhost:8080       # Add api with full URL
  hz add php 8080 --default              # Add as default service
  hz add sabry 3008 --route '/api/*'     # Add with path route
  hz add ws 9000 --route 'header:b-service=ws'  # Add with header route`,
	Args: cobra.ExactArgs(2),
	RunE: runAdd,
}

func init() {
	addCmd.Flags().BoolVar(&addDefault, "default", false, "set as default service")
	addCmd.Flags().StringArrayVar(&addRoutes, "route", nil, "add routing rule (path, header:key=value, subdomain:name)")
	addCmd.Flags().StringVar(&addRewrite, "rewrite", "", "URL rewrite prefix")

	rootCmd.AddCommand(addCmd)
}

func runAdd(cmd *cobra.Command, args []string) error {
	name := args[0]
	targetArg := args[1]

	// Parse target (port or URL)
	var target string
	if port, err := strconv.Atoi(targetArg); err == nil {
		target = fmt.Sprintf("http://localhost:%d", port)
	} else {
		target = targetArg
	}

	// Build service
	service := types.Service{
		Name:    name,
		Target:  target,
		Default: addDefault,
	}

	// Parse routes
	for _, r := range addRoutes {
		route := parseRouteArg(r)
		service.Routes = append(service.Routes, route)
	}

	// Parse rewrite
	if addRewrite != "" {
		service.Rewrite = &types.RewriteConfig{
			Prefix: addRewrite,
		}
	}

	// Find config file
	configPath := cfgFile
	if configPath == "" {
		var err error
		configPath, err = config.FindConfigFile()
		if err != nil {
			return fmt.Errorf("no config file found. Run 'hz init' first")
		}
	}

	// Load existing config
	cfgManager, err := config.NewManager(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	cfg := cfgManager.Get()

	// Check if service already exists
	for _, svc := range cfg.Services {
		if svc.Name == name {
			return fmt.Errorf("service '%s' already exists. Use 'hz remove %s' first", name, name)
		}
	}

	// Add service
	cfg.Services = append(cfg.Services, &service)

	// If setting as default, unset others
	if addDefault {
		for _, svc := range cfg.Services {
			if svc.Name != name {
				svc.Default = false
			}
		}
	}

	// Write updated config
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	fmt.Printf("✅ Added service '%s' → %s\n", name, target)
	if len(service.Routes) > 0 {
		fmt.Printf("   Routes:\n")
		for _, r := range service.Routes {
			if r.Path != "" {
				fmt.Printf("     • path: %s\n", r.Path)
			}
			if r.Header != "" {
				fmt.Printf("     • header: %s\n", r.Header)
			}
			if r.Subdomain != "" {
				fmt.Printf("     • subdomain: %s\n", r.Subdomain)
			}
		}
	}
	if addDefault {
		fmt.Printf("   Default: yes\n")
	}

	return nil
}

// parseRouteArg parses a route argument like "path:/api/*" or "header:x-service=api"
func parseRouteArg(arg string) types.RouteConfig {
	route := types.RouteConfig{}

	// Check for prefixed formats
	if len(arg) > 7 && arg[:7] == "header:" {
		route.Header = arg[7:]
	} else if len(arg) > 10 && arg[:10] == "subdomain:" {
		route.Subdomain = arg[10:]
	} else if len(arg) > 5 && arg[:5] == "path:" {
		route.Path = arg[5:]
	} else {
		// Default to path
		route.Path = arg
	}

	return route
}

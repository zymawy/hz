package hz

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/zymawy/hz/internal/config"
	"github.com/zymawy/hz/pkg/types"
	"gopkg.in/yaml.v3"
)

var removeCmd = &cobra.Command{
	Use:     "remove <name>",
	Aliases: []string{"rm", "delete"},
	Short:   "Remove a service from configuration",
	Long: `Remove a service from the hz configuration file.

Examples:
  hz remove backend
  hz rm api`,
	Args: cobra.ExactArgs(1),
	RunE: runRemove,
}

func init() {
	rootCmd.AddCommand(removeCmd)
}

func runRemove(cmd *cobra.Command, args []string) error {
	name := args[0]

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

	// Find and remove service
	found := false
	newServices := make([]*types.Service, 0, len(cfg.Services))
	for _, svc := range cfg.Services {
		if svc.Name == name {
			found = true
			continue
		}
		newServices = append(newServices, svc)
	}

	if !found {
		return fmt.Errorf("service '%s' not found", name)
	}

	cfg.Services = newServices

	// Save config
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	fmt.Printf("✅ Removed service '%s'\n", name)
	return nil
}

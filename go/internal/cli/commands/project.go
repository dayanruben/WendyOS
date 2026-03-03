package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wendylabsinc/wendy/internal/shared/appconfig"
)

func newProjectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Manage Wendy project configuration",
	}

	cmd.AddCommand(newEntitlementsCmd())
	return cmd
}

func newEntitlementsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "entitlements",
		Short: "Manage project entitlements",
	}

	cmd.AddCommand(
		newEntitlementsListCmd(),
		newEntitlementsAddCmd(),
		newEntitlementsRemoveCmd(),
	)
	return cmd
}

func newEntitlementsListCmd() *cobra.Command {
	var showAll bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List project entitlements",
		RunE: func(cmd *cobra.Command, args []string) error {
			if showAll {
				return listAllEntitlementTypes(cmd)
			}
			return listProjectEntitlements(cmd)
		},
	}

	cmd.Flags().BoolVar(&showAll, "show-all", false, "Show all available entitlement types")
	return cmd
}

func listAllEntitlementTypes(cmd *cobra.Command) error {
	types := appconfig.ValidEntitlementTypes

	if jsonOutput {
		data, err := json.Marshal(types)
		if err != nil {
			return err
		}
		cmd.Println(string(data))
		return nil
	}

	cmd.Println("Available entitlement types:")
	for _, t := range types {
		cmd.Printf("  %s\n", t)
	}
	return nil
}

func listProjectEntitlements(cmd *cobra.Command) error {
	cfg, _, err := loadProjectConfig()
	if err != nil {
		return err
	}

	if jsonOutput {
		data, err := json.Marshal(cfg.Entitlements)
		if err != nil {
			return err
		}
		cmd.Println(string(data))
		return nil
	}

	if len(cfg.Entitlements) == 0 {
		cmd.Println("No entitlements configured.")
		return nil
	}

	cmd.Println("Project entitlements:")
	for _, e := range cfg.Entitlements {
		cmd.Printf("  %s\n", e.Type)
	}
	return nil
}

func newEntitlementsAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add <type>",
		Short: "Add an entitlement to the project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			entType := args[0]

			if !slices.Contains(appconfig.ValidEntitlementTypes, entType) {
				return fmt.Errorf("unknown entitlement type %q\nValid types: %s",
					entType, strings.Join(appconfig.ValidEntitlementTypes, ", "))
			}

			cfg, cfgPath, err := loadProjectConfig()
			if err != nil {
				return err
			}

			for _, e := range cfg.Entitlements {
				if e.Type == entType {
					return fmt.Errorf("entitlement %q already exists", entType)
				}
			}

			cfg.Entitlements = append(cfg.Entitlements, appconfig.Entitlement{Type: entType})

			if err := saveProjectConfig(cfg, cfgPath); err != nil {
				return err
			}

			fmt.Printf("Added %q entitlement\n", entType)
			return nil
		},
	}
}

func newEntitlementsRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <type>",
		Short: "Remove an entitlement from the project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			entType := args[0]

			cfg, cfgPath, err := loadProjectConfig()
			if err != nil {
				return err
			}

			idx := -1
			for i, e := range cfg.Entitlements {
				if e.Type == entType {
					idx = i
					break
				}
			}

			if idx == -1 {
				return fmt.Errorf("entitlement %q not found in project", entType)
			}

			cfg.Entitlements = slices.Delete(cfg.Entitlements, idx, idx+1)

			if err := saveProjectConfig(cfg, cfgPath); err != nil {
				return err
			}

			fmt.Printf("Removed %q entitlement\n", entType)
			return nil
		},
	}
}

func loadProjectConfig() (*appconfig.AppConfig, string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, "", fmt.Errorf("getting working directory: %w", err)
	}

	cfgPath := filepath.Join(cwd, "wendy.json")
	cfg, err := appconfig.LoadFromFile(cfgPath)
	if err != nil {
		return nil, "", fmt.Errorf("loading wendy.json: %w", err)
	}

	return cfg, cfgPath, nil
}

func saveProjectConfig(cfg *appconfig.AppConfig, path string) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	data = append(data, '\n')

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing wendy.json: %w", err)
	}

	return nil
}

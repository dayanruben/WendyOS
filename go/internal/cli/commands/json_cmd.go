package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/wendylabsinc/wendy/internal/shared/appconfig"
)

func newJSONCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "json",
		Short: "Inspect and validate wendy.json",
	}

	cmd.AddCommand(newJSONSchemaCmd())
	cmd.AddCommand(newJSONValidateCmd())

	return cmd
}

func newJSONSchemaCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "schema",
		Short: "Print the JSON Schema for wendy.json",
		Long:  "Prints the JSON Schema to stdout. Pipe to a file or use $schema in your wendy.json for editor autocompletion.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println(appconfig.SchemaJSON)
			return nil
		},
	}
}

func newJSONValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate [path]",
		Short: "Validate a wendy.json file",
		Long:  "Validates the wendy.json in the current directory (or at the given path) for required fields, valid entitlement types, and unknown keys.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "wendy.json"
			if len(args) > 0 {
				path = args[0]
			} else {
				cwd, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("getting working directory: %w", err)
				}
				path = filepath.Join(cwd, "wendy.json")
			}

			data, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("reading %s: %w", path, err)
			}

			cfg, err := appconfig.LoadFromFile(path)
			if err != nil {
				return err
			}

			hasErrors := false

			if err := cfg.Validate(); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				hasErrors = true
			}

			warnings := appconfig.ValidateJSON(data)
			for _, w := range warnings {
				fmt.Fprintf(os.Stderr, "Warning: %s\n", w)
			}

			if hasErrors {
				return fmt.Errorf("validation failed")
			}

			if len(warnings) > 0 {
				fmt.Println("wendy.json is valid (with warnings).")
			} else {
				fmt.Println("wendy.json is valid.")
			}
			return nil
		},
	}
}

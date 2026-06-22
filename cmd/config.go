package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func init() {
	configCmd := &cobra.Command{
		Use:   "config",
		Short: "View and change the CLI configuration",
		RunE:  runConfigShow,
	}

	setCmd := &cobra.Command{
		Use:   "set <api-url|format> <value>",
		Short: "Change a setting (api-url, format)",
		Args:  cobra.ExactArgs(2),
		RunE:  runConfigSet,
	}

	configCmd.AddCommand(setCmd)
	rootCmd.AddCommand(configCmd)
}

func runConfigShow(cmd *cobra.Command, _ []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	p := cfg.Profile(flagProfile)
	fmt.Printf("Default profile: %s\n", cfg.DefaultProfile)
	fmt.Printf("Format:         %s\n", cfg.DefaultFormat)
	fmt.Printf("Backend (profile): %s\n", p.APIURL)
	return nil
}

func runConfigSet(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	key, val := args[0], args[1]
	switch key {
	case "api-url":
		p := cfg.Profile(flagProfile)
		p.APIURL = val
		cfg.SetProfile(flagProfile, p)
	case "format":
		if val != "table" && val != "json" {
			return fmt.Errorf("invalid format %q (use: table, json)", val)
		}
		cfg.DefaultFormat = val
	default:
		return fmt.Errorf("unknown setting %q (use: api-url, format)", key)
	}
	if err := cfg.Save(); err != nil {
		return err
	}
	fmt.Printf("Setting %s updated.\n", key)
	return nil
}

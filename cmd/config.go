package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func init() {
	configCmd := &cobra.Command{
		Use:   "config",
		Short: "Ver y cambiar la configuración del CLI",
		RunE:  runConfigShow,
	}

	setCmd := &cobra.Command{
		Use:   "set <api-url|format> <valor>",
		Short: "Cambiar un ajuste (api-url, format)",
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
	fmt.Printf("Perfil por defecto: %s\n", cfg.DefaultProfile)
	fmt.Printf("Formato:            %s\n", cfg.DefaultFormat)
	fmt.Printf("Backend (perfil):   %s\n", p.APIURL)
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
			return fmt.Errorf("formato inválido %q (usa: table, json)", val)
		}
		cfg.DefaultFormat = val
	default:
		return fmt.Errorf("ajuste desconocido %q (usa: api-url, format)", key)
	}
	if err := cfg.Save(); err != nil {
		return err
	}
	fmt.Printf("Ajuste %s actualizado.\n", key)
	return nil
}

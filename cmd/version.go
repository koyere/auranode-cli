package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Mostrar la versión del CLI",
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Printf("auranode %s\n", version)
		},
	})
}

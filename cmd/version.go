package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Show the CLI version",
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Printf("auranode %s\n", version)
		},
	})
}

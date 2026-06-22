// Package cmd defines the AuraNode CLI commands (cobra).
package cmd

import (
	"fmt"
	"os"

	"github.com/koyere/auranode-cli/internal/client"
	"github.com/koyere/auranode-cli/internal/config"
	"github.com/koyere/auranode-cli/internal/output"
	"github.com/spf13/cobra"
)

// Global flags (persistent across all subcommands).
var (
	flagOutput  string
	flagProfile string
	flagAPIURL  string
	flagQuiet   bool
	flagVerbose bool
	flagNoColor bool
)

// version is injected by main via SetVersion.
var version = "dev"

// SetVersion sets the binary version (from -ldflags in main).
func SetVersion(v string) { version = v }

var rootCmd = &cobra.Command{
	Use:           "auranode",
	Short:         "AuraNode — manage your servers from the terminal",
	Long:          "auranode is the CLI for the AuraNode panel: servers, metrics, remote execution and more.",
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute runs the root command; returns the exit code.
func Execute() int {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		return 1
	}
	return 0
}

func init() {
	pf := rootCmd.PersistentFlags()
	pf.StringVarP(&flagOutput, "output", "o", "", "output format: table|json")
	pf.StringVar(&flagProfile, "profile", "", "profile to use (multi-account)")
	pf.StringVar(&flagAPIURL, "api-url", "", "override the backend URL")
	pf.BoolVarP(&flagQuiet, "quiet", "q", false, "essential output only")
	pf.BoolVarP(&flagVerbose, "verbose", "v", false, "verbose output")
	pf.BoolVar(&flagNoColor, "no-color", false, "no colors (for pipes)")
}

// loadConfig loads the config from disk (or an empty one if it does not exist).
func loadConfig() (*config.Config, error) {
	return config.Load()
}

// resolveFormat decides the format: flag > config default_format.
func resolveFormat(cfg *config.Config) (output.Format, error) {
	if flagOutput != "" {
		return output.Parse(flagOutput)
	}
	return output.Parse(cfg.DefaultFormat)
}

// newClient builds an authenticated client for the active profile.
// Token precedence: env AURANODE_TOKEN > profile token.
// URL precedence: --api-url > env AURANODE_API_URL > profile.
func newClient(cfg *config.Config) (*client.Client, error) {
	p := cfg.Profile(flagProfile)

	apiURL := p.APIURL
	if env := os.Getenv("AURANODE_API_URL"); env != "" {
		apiURL = env
	}
	if flagAPIURL != "" {
		apiURL = flagAPIURL
	}

	token := p.Token
	if env := os.Getenv("AURANODE_TOKEN"); env != "" {
		token = env
	}
	if token == "" {
		return nil, config.ErrNotAuthenticated
	}
	return client.New(apiURL, token), nil
}

// resolveConn returns (apiURL, token) with the same precedence as newClient.
// Used by commands that need the raw URL/token (e.g. tunnel WebSocket).
func resolveConn(cfg *config.Config) (string, string, error) {
	p := cfg.Profile(flagProfile)

	apiURL := p.APIURL
	if env := os.Getenv("AURANODE_API_URL"); env != "" {
		apiURL = env
	}
	if flagAPIURL != "" {
		apiURL = flagAPIURL
	}

	token := p.Token
	if env := os.Getenv("AURANODE_TOKEN"); env != "" {
		token = env
	}
	if token == "" {
		return "", "", config.ErrNotAuthenticated
	}
	return apiURL, token, nil
}

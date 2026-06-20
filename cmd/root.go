// Package cmd define los comandos del CLI de AuraNode (cobra).
package cmd

import (
	"fmt"
	"os"

	"github.com/koyere/auranode-cli/internal/client"
	"github.com/koyere/auranode-cli/internal/config"
	"github.com/koyere/auranode-cli/internal/output"
	"github.com/spf13/cobra"
)

// Flags globales (persistentes en todos los subcomandos).
var (
	flagOutput  string
	flagProfile string
	flagAPIURL  string
	flagQuiet   bool
	flagVerbose bool
	flagNoColor bool
)

// version la inyecta main vía SetVersion.
var version = "dev"

// SetVersion fija la versión del binario (desde -ldflags en main).
func SetVersion(v string) { version = v }

var rootCmd = &cobra.Command{
	Use:           "auranode",
	Short:         "AuraNode — gestiona tus servidores desde la terminal",
	Long:          "auranode es el CLI del panel AuraNode: servidores, métricas, ejecución remota y más.",
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute corre el comando raíz; devuelve el código de salida.
func Execute() int {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		return 1
	}
	return 0
}

func init() {
	pf := rootCmd.PersistentFlags()
	pf.StringVarP(&flagOutput, "output", "o", "", "formato de salida: table|json")
	pf.StringVar(&flagProfile, "profile", "", "perfil a usar (multi-cuenta)")
	pf.StringVar(&flagAPIURL, "api-url", "", "sobreescribe la URL del backend")
	pf.BoolVarP(&flagQuiet, "quiet", "q", false, "solo salida esencial")
	pf.BoolVarP(&flagVerbose, "verbose", "v", false, "salida detallada")
	pf.BoolVar(&flagNoColor, "no-color", false, "sin colores (para pipes)")
}

// loadConfig carga la config del disco (o una vacía si no existe).
func loadConfig() (*config.Config, error) {
	return config.Load()
}

// resolveFormat decide el formato: flag > default_format de la config.
func resolveFormat(cfg *config.Config) (output.Format, error) {
	if flagOutput != "" {
		return output.Parse(flagOutput)
	}
	return output.Parse(cfg.DefaultFormat)
}

// newClient construye un cliente autenticado para el perfil activo.
// Precedencia del token: env AURANODE_TOKEN > token del perfil.
// Precedencia de la URL: --api-url > env AURANODE_API_URL > perfil.
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

// resolveConn devuelve (apiURL, token) con la misma precedencia que newClient.
// Lo usan los comandos que necesitan la URL/token crudos (p.ej. WebSocket de túneles).
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

package cmd

import (
	"fmt"
	"strings"

	"github.com/koyere/auranode-cli/internal/client"
	"github.com/koyere/auranode-cli/internal/output"
	"github.com/spf13/cobra"
)

var (
	srvFilterTag    string
	srvFilterStatus string
	srvShowMetrics  bool
)

func init() {
	serversCmd := &cobra.Command{
		Use:     "servers",
		Aliases: []string{"server", "srv"},
		Short:   "Gestionar servidores (agentes)",
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "Listar servidores",
		RunE:  runServersList,
	}
	listCmd.Flags().StringVar(&srvFilterTag, "tag", "", "filtrar por tag")
	listCmd.Flags().StringVar(&srvFilterStatus, "status", "", "filtrar por estado (online/offline/...)")

	showCmd := &cobra.Command{
		Use:   "show <name|id>",
		Short: "Detalle de un servidor",
		Args:  cobra.ExactArgs(1),
		RunE:  runServersShow,
	}
	showCmd.Flags().BoolVar(&srvShowMetrics, "metrics", false, "incluir métricas actuales")

	serversCmd.AddCommand(listCmd, showCmd)
	rootCmd.AddCommand(serversCmd)
}

type agent map[string]any

func fetchAgents(c *client.Client) ([]agent, error) {
	var resp struct {
		Agents []agent `json:"agents"`
	}
	if err := c.Get("/agents", &resp); err != nil {
		return nil, err
	}
	return resp.Agents, nil
}

func str(a agent, key string) string {
	if v, ok := a[key]; ok && v != nil {
		return fmt.Sprintf("%v", v)
	}
	return ""
}

// resolveAgent encuentra un agente por id exacto o por nombre.
func resolveAgent(c *client.Client, ref string) (agent, error) {
	agents, err := fetchAgents(c)
	if err != nil {
		return nil, err
	}
	for _, a := range agents {
		if str(a, "id") == ref || str(a, "name") == ref {
			return a, nil
		}
	}
	return nil, fmt.Errorf("servidor no encontrado: %s", ref)
}

func runServersList(cmd *cobra.Command, _ []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	format, err := resolveFormat(cfg)
	if err != nil {
		return err
	}
	c, err := newClient(cfg)
	if err != nil {
		return err
	}

	agents, err := fetchAgents(c)
	if err != nil {
		return err
	}

	// Filtros del lado del cliente (la API no los expone todos).
	filtered := agents[:0]
	for _, a := range agents {
		if srvFilterStatus != "" && !strings.EqualFold(str(a, "status"), srvFilterStatus) {
			continue
		}
		if srvFilterTag != "" && !hasTag(a, srvFilterTag) {
			continue
		}
		filtered = append(filtered, a)
	}

	headers := []string{"NAME", "STATUS", "OS", "IP", "VERSION", "LAST SEEN"}
	rows := make([][]string, 0, len(filtered))
	for _, a := range filtered {
		rows = append(rows, []string{
			str(a, "name"), str(a, "status"), str(a, "os"),
			str(a, "ip_address"), str(a, "agent_version"), str(a, "last_seen_at"),
		})
	}
	return output.Render(format, filtered, headers, rows)
}

func hasTag(a agent, tag string) bool {
	tags, ok := a["tags"].([]any)
	if !ok {
		return false
	}
	for _, t := range tags {
		if fmt.Sprintf("%v", t) == tag {
			return true
		}
	}
	return false
}

func runServersShow(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	format, err := resolveFormat(cfg)
	if err != nil {
		return err
	}
	c, err := newClient(cfg)
	if err != nil {
		return err
	}

	a, err := resolveAgent(c, args[0])
	if err != nil {
		return err
	}

	if srvShowMetrics {
		var m any
		if err := c.Get("/agents/"+str(a, "id")+"/metrics/current", &m); err == nil {
			a["metrics"] = m
		}
	}

	if format == output.FormatJSON {
		return output.JSON(a)
	}
	for _, k := range []string{"name", "id", "status", "hostname", "ip_address", "os", "arch", "agent_version", "project_group", "last_seen_at"} {
		if v := str(a, k); v != "" {
			fmt.Printf("%-14s %s\n", k+":", v)
		}
	}
	return nil
}

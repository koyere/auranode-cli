package cmd

import (
	"fmt"
	"sort"

	"github.com/koyere/auranode-cli/internal/client"
	"github.com/koyere/auranode-cli/internal/output"
	"github.com/spf13/cobra"
)

func init() {
	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Status of your whole infrastructure",
		RunE:  runStatus,
	}
	rootCmd.AddCommand(statusCmd)
}

func statusSymbol(s string) string {
	switch s {
	case "active":
		return "●"
	case "degraded":
		return "⚠"
	case "offline":
		return "✗"
	default:
		return "○"
	}
}

func runStatus(cmd *cobra.Command, _ []string) error {
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
	sort.Slice(agents, func(i, j int) bool { return str(agents[i], "name") < str(agents[j], "name") })

	if format == output.FormatJSON {
		return output.JSON(agents)
	}

	if len(agents) == 0 {
		fmt.Println("No servers registered.")
		return nil
	}

	for _, a := range agents {
		status := str(a, "status")
		line := fmt.Sprintf("%s %-14s %-8s", statusSymbol(status), str(a, "name"), status)
		if status == "active" {
			if cpu, ram := currentUsage(c, str(a, "id")); cpu != "" {
				line += fmt.Sprintf("  CPU: %-5s RAM: %-5s", cpu, ram)
			}
		} else if ls := str(a, "last_seen_at"); ls != "" {
			line += "  " + ls
		}
		fmt.Println(line)
	}
	return nil
}

// currentUsage gets the current CPU%% and RAM%% (best-effort; empty on failure).
func currentUsage(c *client.Client, agentID string) (cpu, ram string) {
	var m map[string]any
	if err := c.Get("/agents/"+agentID+"/metrics/current", &m); err != nil {
		return "", ""
	}
	if v, ok := m["cpu_percent"].(float64); ok {
		cpu = fmt.Sprintf("%.0f%%", v)
	}
	used, _ := m["ram_used_mb"].(float64)
	total, _ := m["ram_total_mb"].(float64)
	if total > 0 {
		ram = fmt.Sprintf("%.0f%%", used/total*100)
	}
	return cpu, ram
}

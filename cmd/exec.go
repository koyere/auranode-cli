package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
)

var execTimeout int

func init() {
	execCmd := &cobra.Command{
		Use:   "exec <name|id> <command>",
		Short: "Run a command on a server",
		Args:  cobra.MinimumNArgs(2),
		RunE:  runExec,
	}
	execCmd.Flags().IntVar(&execTimeout, "timeout", 30, "timeout in seconds")
	rootCmd.AddCommand(execCmd)
}

func runExec(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
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
	command := joinArgs(args[1:])

	var started struct {
		CommandID string `json:"command_id"`
	}
	body := map[string]any{"command": command, "timeout_seconds": execTimeout, "async": false}
	if err := c.Post("/agents/"+str(a, "id")+"/exec", body, &started); err != nil {
		return err
	}
	if started.CommandID == "" {
		return fmt.Errorf("the backend did not return a command_id")
	}

	// Poll until terminal state (command timeout + margin).
	deadline := time.Now().Add(time.Duration(execTimeout+10) * time.Second)
	for {
		var res struct {
			Status   string `json:"status"`
			ExitCode *int   `json:"exit_code"`
			Stdout   string `json:"stdout"`
			Stderr   string `json:"stderr"`
		}
		if err := c.Get("/exec/"+started.CommandID, &res); err != nil {
			return err
		}
		if res.Status != "pending" && res.Status != "running" {
			if res.Stdout != "" {
				fmt.Fprint(os.Stdout, res.Stdout)
				if res.Stdout[len(res.Stdout)-1] != '\n' {
					fmt.Println()
				}
			}
			if res.Stderr != "" {
				fmt.Fprint(os.Stderr, res.Stderr)
			}
			if res.Status != "completed" {
				return fmt.Errorf("command %s", res.Status)
			}
			if res.ExitCode != nil && *res.ExitCode != 0 {
				return fmt.Errorf("exit code %d", *res.ExitCode)
			}
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for the command result")
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func joinArgs(args []string) string {
	out := ""
	for i, a := range args {
		if i > 0 {
			out += " "
		}
		out += a
	}
	return out
}

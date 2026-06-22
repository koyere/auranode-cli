package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/koyere/auranode-cli/internal/client"
	"github.com/koyere/auranode-cli/internal/output"
	"github.com/koyere/auranode-cli/internal/tunnel"
	"github.com/spf13/cobra"
)

var (
	tunAgent      string
	tunRemotePort int
	tunLocalPort  int
	tunRemoteHost string
	tunName       string
	tunReverse    bool
	tunTo         string
	tunVPSPort    int
	tunBind       string
)

func init() {
	tunnelsCmd := &cobra.Command{
		Use:     "tunnel",
		Aliases: []string{"tunnels", "tun"},
		Short:   "Port forwarding: tunnels to services on your VPS",
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List tunnels",
		RunE:  runTunnelsList,
	}

	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Create a local tunnel (the listener lives on your machine)",
		Long: "Create a local tunnel (Type 1) or, with --reverse, a reverse tunnel (Type 2):\n" +
			"  Local   (default): you listen on your machine and forward to a VPS service.\n" +
			"  Reverse (--reverse): the VPS opens a public port and forwards to a local\n" +
			"                       service of yours (webhooks/ngrok case).",
		RunE: runTunnelCreate,
	}
	createCmd.Flags().BoolVar(&tunReverse, "reverse", false, "create a reverse tunnel (Type 2): expose a local service of yours on the VPS")
	createCmd.Flags().StringVar(&tunAgent, "agent", "", "VPS: destination hosting the service (local) or source exposing the port (reverse) (name|id) [required]")
	createCmd.Flags().IntVar(&tunRemotePort, "remote-port", 0, "[local] port of the service on the VPS")
	createCmd.Flags().IntVar(&tunLocalPort, "local-port", 0, "[local] local port (default = remote-port)")
	createCmd.Flags().StringVar(&tunRemoteHost, "remote-host", "127.0.0.1", "[local] host of the service as seen from the VPS")
	createCmd.Flags().IntVar(&tunVPSPort, "vps-port", 0, "[reverse] public port to open on the VPS")
	createCmd.Flags().StringVar(&tunTo, "to", "", "[reverse] local service to expose (host:port or port)")
	createCmd.Flags().StringVar(&tunBind, "bind", "", "[reverse] listener interface on the VPS (default 0.0.0.0)")
	createCmd.Flags().StringVar(&tunName, "name", "", "tunnel name (derived by default)")
	_ = createCmd.MarkFlagRequired("agent")

	openCmd := &cobra.Command{
		Use:   "open <name|id>",
		Short: "Open a local tunnel: listen on your machine and forward to the VPS (blocks until Ctrl+C)",
		Args:  cobra.ExactArgs(1),
		RunE:  runTunnelOpen,
	}
	openCmd.Flags().IntVar(&tunLocalPort, "local-port", 0, "override the local port")

	exposeCmd := &cobra.Command{
		Use:   "expose <name|id>",
		Short: "Open a reverse tunnel: the VPS exposes a local service of yours (blocks until Ctrl+C)",
		Args:  cobra.ExactArgs(1),
		RunE:  runTunnelExpose,
	}
	exposeCmd.Flags().StringVar(&tunTo, "to", "", "override the local service to expose (host:port or port)")

	rmCmd := &cobra.Command{
		Use:     "rm <name|id>",
		Aliases: []string{"delete", "remove"},
		Short:   "Delete a tunnel",
		Args:    cobra.ExactArgs(1),
		RunE:    runTunnelRm,
	}

	tunnelsCmd.AddCommand(listCmd, createCmd, openCmd, exposeCmd, rmCmd)
	rootCmd.AddCommand(tunnelsCmd)
}

type tunnelRec map[string]any

func fetchTunnels(c *client.Client) ([]tunnelRec, error) {
	var resp struct {
		Tunnels []tunnelRec `json:"tunnels"`
	}
	if err := c.Get("/tunnels", &resp); err != nil {
		return nil, err
	}
	return resp.Tunnels, nil
}

func tstr(t tunnelRec, key string) string {
	if v, ok := t[key]; ok && v != nil {
		return fmt.Sprintf("%v", v)
	}
	return ""
}

// resolveTunnel finds a tunnel by exact id or by name.
func resolveTunnel(c *client.Client, ref string) (tunnelRec, error) {
	tunnels, err := fetchTunnels(c)
	if err != nil {
		return nil, err
	}
	for _, t := range tunnels {
		if tstr(t, "id") == ref {
			return t, nil
		}
	}
	var matches []tunnelRec
	for _, t := range tunnels {
		if strings.EqualFold(tstr(t, "name"), ref) {
			matches = append(matches, t)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return nil, fmt.Errorf("tunnel '%s' not found", ref)
	default:
		return nil, fmt.Errorf("multiple tunnels named '%s'; use the id", ref)
	}
}

func runTunnelsList(cmd *cobra.Command, _ []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	c, err := newClient(cfg)
	if err != nil {
		return err
	}
	tunnels, err := fetchTunnels(c)
	if err != nil {
		return err
	}
	format, err := resolveFormat(cfg)
	if err != nil {
		return err
	}
	rows := make([][]string, 0, len(tunnels))
	for _, t := range tunnels {
		rows = append(rows, []string{
			tstr(t, "name"),
			tstr(t, "type"),
			fmt.Sprintf("%s:%s", tstr(t, "remote_host"), tstr(t, "remote_port")),
			tstr(t, "local_port"),
			tstr(t, "status"),
		})
	}
	return output.Render(format, tunnels, []string{"NAME", "TYPE", "REMOTE", "LOCAL", "STATUS"}, rows)
}

func runTunnelCreate(cmd *cobra.Command, _ []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	c, err := newClient(cfg)
	if err != nil {
		return err
	}

	ag, err := resolveAgent(c, tunAgent)
	if err != nil {
		return err
	}
	agentID := str(ag, "id")

	var body map[string]any
	var name, openHint string

	if tunReverse {
		// Reverse tunnel (Type 2): the VPS (source) opens a public port and forwards to a
		// local service of the user, which the CLI (dest) dials.
		if tunVPSPort == 0 {
			return fmt.Errorf("--vps-port is required for --reverse")
		}
		if tunTo == "" {
			return fmt.Errorf("--to is required for --reverse (local service to expose)")
		}
		host, port, err := parseHostPort(tunTo)
		if err != nil {
			return err
		}
		name = tunName
		if name == "" {
			name = fmt.Sprintf("%s-reverse-%d", str(ag, "name"), tunVPSPort)
		}
		body = map[string]any{
			"name":            name,
			"type":            "remote",
			"source_agent_id": agentID,
			"remote_host":     host,
			"remote_port":     port,
			"local_port":      tunVPSPort,
		}
		if tunBind != "" {
			body["bind_address"] = tunBind
		}
		openHint = fmt.Sprintf("auranode tunnel expose %s", name)
	} else {
		// Local tunnel (Type 1): the CLI listens and forwards to a VPS service.
		if tunRemotePort == 0 {
			return fmt.Errorf("--remote-port is required")
		}
		local := tunLocalPort
		if local == 0 {
			local = tunRemotePort
		}
		name = tunName
		if name == "" {
			name = fmt.Sprintf("%s-%d", str(ag, "name"), tunRemotePort)
		}
		body = map[string]any{
			"name":          name,
			"type":          "local",
			"dest_agent_id": agentID,
			"remote_host":   tunRemoteHost,
			"remote_port":   tunRemotePort,
			"local_port":    local,
		}
		openHint = fmt.Sprintf("auranode tunnel open %s", name)
	}

	var created tunnelRec
	if err := c.Post("/tunnels", body, &created); err != nil {
		return err
	}

	format, _ := resolveFormat(cfg)
	if format == output.FormatJSON {
		return output.JSON(created)
	}
	fmt.Printf("Tunnel '%s' created. Open it with:\n  %s\n", name, openHint)
	return nil
}

// parseHostPort accepts "host:port" or just "port" (host defaults to 127.0.0.1).
func parseHostPort(s string) (string, int, error) {
	host := "127.0.0.1"
	portStr := s
	if i := strings.LastIndex(s, ":"); i >= 0 {
		host = s[:i]
		portStr = s[i+1:]
		if host == "" {
			host = "127.0.0.1"
		}
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return "", 0, fmt.Errorf("invalid address '%s': use host:port or port", s)
	}
	return host, port, nil
}

func runTunnelOpen(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	c, err := newClient(cfg)
	if err != nil {
		return err
	}
	apiURL, token, err := resolveConn(cfg)
	if err != nil {
		return err
	}

	t, err := resolveTunnel(c, args[0])
	if err != nil {
		return err
	}
	if tstr(t, "type") != "local" {
		return fmt.Errorf("only 'local' tunnels can be opened from the CLI (this one is '%s')", tstr(t, "type"))
	}
	tunnelID := tstr(t, "id")

	localPort := tunLocalPort
	if localPort == 0 {
		fmt.Sscanf(tstr(t, "local_port"), "%d", &localPort)
	}
	localAddr := fmt.Sprintf("127.0.0.1:%d", localPort)

	logf := func(format string, a ...any) { fmt.Fprintf(os.Stderr, format+"\n", a...) }

	tc, err := tunnel.New(apiURL, token, tunnelID, localAddr, logf)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	onReady := func(lp, rp int, rh string) {
		fmt.Printf("✔ Tunnel '%s' open.\n", tstr(t, "name"))
		fmt.Printf("  Listening on   127.0.0.1:%d\n", localPort)
		fmt.Printf("  Forwarding to  %s:%d (via %s)\n", rh, rp, tstr(t, "name"))
		fmt.Printf("  Example:       mysql -h 127.0.0.1 -P %d -u <user> -p\n", localPort)
		fmt.Println("  Ctrl+C to close.")
	}

	if err := tc.Run(ctx, onReady); err != nil {
		return err
	}
	fmt.Println("\nTunnel closed.")
	return nil
}

func runTunnelExpose(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	c, err := newClient(cfg)
	if err != nil {
		return err
	}
	apiURL, token, err := resolveConn(cfg)
	if err != nil {
		return err
	}

	t, err := resolveTunnel(c, args[0])
	if err != nil {
		return err
	}
	// Reverse dest=CLI: type=remote WITHOUT a destination agent (the dest is this CLI).
	if tstr(t, "type") != "remote" || tstr(t, "dest_agent_id") != "" {
		return fmt.Errorf("only reverse tunnels can be exposed (type 'remote' without a destination VPS); this one is '%s'", tstr(t, "type"))
	}
	tunnelID := tstr(t, "id")

	// Local service to expose: --to overrides it; otherwise the saved host:port.
	dialAddr := ""
	if tunTo != "" {
		host, port, err := parseHostPort(tunTo)
		if err != nil {
			return err
		}
		dialAddr = fmt.Sprintf("%s:%d", host, port)
	}

	logf := func(format string, a ...any) { fmt.Fprintf(os.Stderr, format+"\n", a...) }

	tc, err := tunnel.NewDest(apiURL, token, tunnelID, dialAddr, logf)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	onReady := func(vpsPort, localPort int, localHost string) {
		target := dialAddr
		if target == "" {
			target = fmt.Sprintf("%s:%d", localHost, localPort)
		}
		fmt.Printf("✔ Reverse tunnel '%s' open.\n", tstr(t, "name"))
		fmt.Printf("  Public port on the VPS:    :%d\n", vpsPort)
		fmt.Printf("  Forwarding to your service: %s\n", target)
		fmt.Println("  Ctrl+C to close.")
	}

	if err := tc.Run(ctx, onReady); err != nil {
		return err
	}
	fmt.Println("\nTunnel closed.")
	return nil
}

func runTunnelRm(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	c, err := newClient(cfg)
	if err != nil {
		return err
	}
	t, err := resolveTunnel(c, args[0])
	if err != nil {
		return err
	}
	if err := c.Delete("/tunnels/"+tstr(t, "id"), nil); err != nil {
		return err
	}
	fmt.Printf("Tunnel '%s' deleted.\n", tstr(t, "name"))
	return nil
}

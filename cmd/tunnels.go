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
		Short:   "Port forwarding: túneles a servicios de tus VPS",
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "Listar túneles",
		RunE:  runTunnelsList,
	}

	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Crear un túnel local (el listener vive en tu máquina)",
		Long: "Crear un túnel local (Tipo 1) o, con --reverse, un túnel reverse (Tipo 2):\n" +
			"  Local   (default): escuchas en tu máquina y reenvías a un servicio del VPS.\n" +
			"  Reverse (--reverse): el VPS abre un puerto público y reenvía a un servicio\n" +
			"                       local tuyo (caso webhooks/ngrok).",
		RunE: runTunnelCreate,
	}
	createCmd.Flags().BoolVar(&tunReverse, "reverse", false, "crear un túnel reverse (Tipo 2): expone un servicio local tuyo en el VPS")
	createCmd.Flags().StringVar(&tunAgent, "agent", "", "VPS: destino que aloja el servicio (local) u origen que expone el puerto (reverse) (name|id) [requerido]")
	createCmd.Flags().IntVar(&tunRemotePort, "remote-port", 0, "[local] puerto del servicio en el VPS")
	createCmd.Flags().IntVar(&tunLocalPort, "local-port", 0, "[local] puerto local (por defecto = remote-port)")
	createCmd.Flags().StringVar(&tunRemoteHost, "remote-host", "127.0.0.1", "[local] host del servicio visto desde el VPS")
	createCmd.Flags().IntVar(&tunVPSPort, "vps-port", 0, "[reverse] puerto público a abrir en el VPS")
	createCmd.Flags().StringVar(&tunTo, "to", "", "[reverse] servicio local a exponer (host:port o port)")
	createCmd.Flags().StringVar(&tunBind, "bind", "", "[reverse] interfaz del listener en el VPS (default 0.0.0.0)")
	createCmd.Flags().StringVar(&tunName, "name", "", "nombre del túnel (por defecto derivado)")
	_ = createCmd.MarkFlagRequired("agent")

	openCmd := &cobra.Command{
		Use:   "open <name|id>",
		Short: "Abrir un túnel local: escucha en tu máquina y reenvía al VPS (bloquea hasta Ctrl+C)",
		Args:  cobra.ExactArgs(1),
		RunE:  runTunnelOpen,
	}
	openCmd.Flags().IntVar(&tunLocalPort, "local-port", 0, "sobreescribe el puerto local")

	exposeCmd := &cobra.Command{
		Use:   "expose <name|id>",
		Short: "Abrir un túnel reverse: el VPS expone un servicio local tuyo (bloquea hasta Ctrl+C)",
		Args:  cobra.ExactArgs(1),
		RunE:  runTunnelExpose,
	}
	exposeCmd.Flags().StringVar(&tunTo, "to", "", "sobreescribe el servicio local a exponer (host:port o port)")

	rmCmd := &cobra.Command{
		Use:     "rm <name|id>",
		Aliases: []string{"delete", "remove"},
		Short:   "Eliminar un túnel",
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

// resolveTunnel encuentra un túnel por id exacto o por nombre.
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
		return nil, fmt.Errorf("no se encontró un túnel '%s'", ref)
	default:
		return nil, fmt.Errorf("hay varios túneles llamados '%s'; usa el id", ref)
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
		// Túnel reverse (Tipo 2): el VPS (source) abre un puerto público y reenvía a un
		// servicio local del usuario, al que el CLI (dest) hace el dial.
		if tunVPSPort == 0 {
			return fmt.Errorf("--vps-port es requerido para --reverse")
		}
		if tunTo == "" {
			return fmt.Errorf("--to es requerido para --reverse (servicio local a exponer)")
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
		// Túnel local (Tipo 1): el CLI escucha y reenvía a un servicio del VPS.
		if tunRemotePort == 0 {
			return fmt.Errorf("--remote-port es requerido")
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
	fmt.Printf("Túnel '%s' creado. Ábrelo con:\n  %s\n", name, openHint)
	return nil
}

// parseHostPort acepta "host:port" o sólo "port" (host por defecto 127.0.0.1).
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
		return "", 0, fmt.Errorf("dirección inválida '%s': usa host:port o port", s)
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
		return fmt.Errorf("solo se pueden abrir túneles de tipo 'local' desde el CLI (este es '%s')", tstr(t, "type"))
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
		fmt.Printf("✔ Túnel '%s' abierto.\n", tstr(t, "name"))
		fmt.Printf("  Escuchando en  127.0.0.1:%d\n", localPort)
		fmt.Printf("  Reenviando a   %s:%d (vía %s)\n", rh, rp, tstr(t, "name"))
		fmt.Printf("  Ejemplo:       mysql -h 127.0.0.1 -P %d -u <user> -p\n", localPort)
		fmt.Println("  Ctrl+C para cerrar.")
	}

	if err := tc.Run(ctx, onReady); err != nil {
		return err
	}
	fmt.Println("\nTúnel cerrado.")
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
	// Reverse dest=CLI: type=remote SIN agente destino (el dest es este CLI).
	if tstr(t, "type") != "remote" || tstr(t, "dest_agent_id") != "" {
		return fmt.Errorf("solo se pueden exponer túneles reverse (type 'remote' sin VPS destino); este es '%s'", tstr(t, "type"))
	}
	tunnelID := tstr(t, "id")

	// Servicio local a exponer: --to lo sobreescribe; si no, el host:port guardado.
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
		fmt.Printf("✔ Túnel reverse '%s' abierto.\n", tstr(t, "name"))
		fmt.Printf("  Puerto público en el VPS:  :%d\n", vpsPort)
		fmt.Printf("  Reenviando a tu servicio:  %s\n", target)
		fmt.Println("  Ctrl+C para cerrar.")
	}

	if err := tc.Run(ctx, onReady); err != nil {
		return err
	}
	fmt.Println("\nTúnel cerrado.")
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
	fmt.Printf("Túnel '%s' eliminado.\n", tstr(t, "name"))
	return nil
}

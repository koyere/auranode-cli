package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/koyere/auranode-cli/internal/client"
	"github.com/koyere/auranode-cli/internal/config"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	loginEmail string
	loginTOTP  string
)

func init() {
	authCmd := &cobra.Command{
		Use:   "auth",
		Short: "Authentication and session",
	}

	loginCmd := &cobra.Command{
		Use:   "login",
		Short: "Sign in with email and password",
		RunE:  runLogin,
	}
	loginCmd.Flags().StringVar(&loginEmail, "email", "", "account email")
	loginCmd.Flags().StringVar(&loginTOTP, "totp", "", "2FA code (if enabled)")

	authCmd.AddCommand(
		loginCmd,
		&cobra.Command{Use: "logout", Short: "Sign out locally", RunE: runLogout},
		&cobra.Command{Use: "status", Short: "Show the current user and tenant", RunE: runAuthStatus},
		&cobra.Command{Use: "token", Short: "Print the current token (for scripts)", RunE: runAuthToken},
	)
	rootCmd.AddCommand(authCmd)
}

type loginResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TenantID     string `json:"tenant_id"`
	UserID       string `json:"user_id"`
	Requires2FA  bool   `json:"requires_2fa"`
}

func runLogin(cmd *cobra.Command, _ []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	email := loginEmail
	if email == "" {
		fmt.Print("Email: ")
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		email = strings.TrimSpace(line)
	}
	if email == "" {
		return fmt.Errorf("email is required")
	}

	fmt.Print("Password: ")
	pwBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return fmt.Errorf("could not read the password: %w", err)
	}
	password := string(pwBytes)

	// The URL may come from --api-url; otherwise from the profile/env.
	p := cfg.Profile(flagProfile)
	apiURL := p.APIURL
	if env := os.Getenv("AURANODE_API_URL"); env != "" {
		apiURL = env
	}
	if flagAPIURL != "" {
		apiURL = flagAPIURL
	}
	c := client.New(apiURL, "")

	body := map[string]string{"email": email, "password": password}
	if loginTOTP != "" {
		body["totp_code"] = loginTOTP
	}

	var resp loginResponse
	if err := c.Post("/auth/login", body, &resp); err != nil {
		return err
	}
	if resp.Requires2FA {
		return fmt.Errorf("this account has 2FA enabled: retry with --totp <code>")
	}

	p.APIURL = apiURL
	p.Token = resp.AccessToken
	p.Refresh = resp.RefreshToken
	p.UserEmail = email
	p.TenantID = resp.TenantID
	cfg.SetProfile(flagProfile, p)
	if err := cfg.Save(); err != nil {
		return err
	}

	if !flagQuiet {
		fmt.Printf("Authenticated as %s\n", email)
	}
	return nil
}

func runLogout(cmd *cobra.Command, _ []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	name := flagProfile
	if name == "" {
		name = cfg.DefaultProfile
	}
	// Best-effort: revoke the session on the backend before clearing local state.
	if c, err := newClient(cfg); err == nil {
		_ = c.Post("/auth/logout", nil, nil)
	}
	p := cfg.Profile(name)
	p.Token = ""
	p.Refresh = ""
	cfg.SetProfile(name, p)
	if err := cfg.Save(); err != nil {
		return err
	}
	if !flagQuiet {
		fmt.Println("Signed out.")
	}
	return nil
}

func runAuthStatus(cmd *cobra.Command, _ []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	name := flagProfile
	if name == "" {
		name = cfg.DefaultProfile
	}
	p := cfg.Profile(name)
	if p.Token == "" && os.Getenv("AURANODE_TOKEN") == "" {
		return config.ErrNotAuthenticated
	}
	fmt.Printf("Profile: %s\n", name)
	fmt.Printf("Backend: %s\n", p.APIURL)
	if p.UserEmail != "" {
		fmt.Printf("User:    %s\n", p.UserEmail)
	}
	if p.TenantID != "" {
		fmt.Printf("Tenant:  %s\n", p.TenantID)
	}
	return nil
}

func runAuthToken(cmd *cobra.Command, _ []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if env := os.Getenv("AURANODE_TOKEN"); env != "" {
		fmt.Println(env)
		return nil
	}
	p := cfg.Profile(flagProfile)
	if p.Token == "" {
		return config.ErrNotAuthenticated
	}
	fmt.Println(p.Token)
	return nil
}

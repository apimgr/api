// Package cmd implements the api-cli command dispatcher: global flag
// parsing, mode detection, help/version output, and command execution,
// per AI.md PART 32.
package cmd

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/apimgr/api/src/client/api"
	"github.com/apimgr/api/src/client/config"
	"github.com/apimgr/api/src/client/paths"
)

// Exit codes, per AI.md PART 32 Error Handling.
const (
	ExitSuccess  = 0
	ExitGeneral  = 1
	ExitConfig   = 2
	ExitConn     = 3
	ExitAuth     = 4
	ExitNotFound = 5
	ExitUsageBad = 64
)

// BuildInfo carries version metadata injected at build time via -ldflags.
type BuildInfo struct {
	Version   string
	Commit    string
	BuildDate string
}

// TUILauncher is set by main.go to the tui package's entrypoint. It is a
// func var (rather than a direct import) so this package never imports
// the tui package, which itself imports cmd to reach the command
// registry.
var TUILauncher func(cfg *config.CLIConfig, client *api.Client, buildInfo BuildInfo) int

// Execute is the api-cli entrypoint. argv is os.Args (including argv[0]).
func Execute(argv []string, build BuildInfo) int {
	binName := binaryName(argv)
	api.UserAgent = fmt.Sprintf("%s/%s", binName, build.Version)

	flags := parseGlobalFlags(argv[1:])

	if flags.Help {
		printHelp(binName, build)
		return ExitSuccess
	}
	if flags.Version {
		printVersion(binName, build)
		return ExitSuccess
	}
	if flags.Shell != "" {
		if err := runCompletions(flags.Shell, flags.ShellArg); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %s\n", err)
			return ExitUsageBad
		}
		return ExitSuccess
	}

	if err := paths.EnsureDirs(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot create config directories: %s\n", err)
		return ExitConfig
	}

	cfg, err := config.Load(flags.ConfigName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		return ExitConfig
	}

	if flags.Server != "" && !validServerURL(flags.Server) {
		fmt.Fprintf(os.Stderr, "Error: invalid server URL: %s\n", flags.Server)
		return ExitUsageBad
	}
	server, serverPersist := config.SaveIfEmptyOrInvalid(cfg.Server.Primary, flags.Server, validServerURL)

	token := resolveToken(cfg, flags)
	tokenPersist := flags.Token != "" && cfg.Auth.Token == ""

	if serverPersist {
		cfg.Server.Primary = server
	}
	if tokenPersist {
		cfg.Auth.Token = token
	}
	if serverPersist || tokenPersist {
		if err := config.Save(flags.ConfigName, cfg); err != nil {
			fmt.Fprintf(os.Stderr, "Error: cannot save config: %s\n", err)
			return ExitConfig
		}
	}

	client := api.New(server, token)
	client.Debug = flags.Debug

	mode := detectMode(argv[1:])

	if mode == ModeTUI {
		if TUILauncher == nil {
			fmt.Fprintln(os.Stderr, "Error: TUI mode is not available in this build")
			return ExitGeneral
		}
		return TUILauncher(cfg, client, build)
	}

	return runCLI(flags, client)
}

// runCLI dispatches a single command in CLI/plain mode.
func runCLI(flags parsedFlags, client *api.Client) int {
	if len(flags.Rest) == 0 {
		fmt.Fprintln(os.Stderr, "Error: no command given")
		fmt.Fprintln(os.Stderr, "Run with --help for usage.")
		return ExitUsageBad
	}

	category := flags.Rest[0]
	if len(flags.Rest) < 2 {
		cmds := categoryCommands(category)
		if len(cmds) == 0 {
			fmt.Fprintf(os.Stderr, "Error: unknown command: %s\n", category)
			return ExitUsageBad
		}
		fmt.Printf("Usage: %s <subcommand> [args]\n\nSubcommands:\n", category)
		for _, c := range cmds {
			fmt.Printf("  %-30s %s\n", c.Name, c.Desc)
		}
		return ExitSuccess
	}

	name := flags.Rest[1]
	args := flags.Rest[2:]

	command, ok := findCommand(category, name)
	if !ok {
		fmt.Fprintf(os.Stderr, "Error: unknown command: %s %s\n", category, name)
		return ExitUsageBad
	}

	format := flags.Output
	if format == "" {
		format = "table"
	}

	if err := command.Run(client, &OutputOptions{Format: format}, args); err != nil {
		return exitCodeForError(err)
	}
	return ExitSuccess
}

// exitCodeForError maps a command error to the PART 32 exit-code table.
func exitCodeForError(err error) int {
	fmt.Fprintf(os.Stderr, "Error: %s\n", err)

	if apiErr, ok := err.(*api.Error); ok {
		switch apiErr.StatusCode {
		case 401, 403:
			return ExitAuth
		case 404:
			return ExitNotFound
		}
		return ExitGeneral
	}

	msg := err.Error()
	if strings.Contains(msg, "no server configured") || strings.Contains(msg, "cannot connect") {
		return ExitConn
	}
	if strings.Contains(msg, "missing required argument") {
		return ExitUsageBad
	}
	return ExitGeneral
}

// resolveToken applies the PART 32 auth token priority: --token flag,
// then API_TOKEN env var, then cli.yml auth.token.
func resolveToken(cfg *config.CLIConfig, flags parsedFlags) string {
	if flags.TokenFile != "" {
		data, err := os.ReadFile(flags.TokenFile)
		if err == nil {
			return strings.TrimSpace(string(data))
		}
	}
	if flags.Token != "" {
		return flags.Token
	}
	if env := os.Getenv("API_TOKEN"); env != "" {
		return env
	}
	return cfg.Auth.Token
}

func validServerURL(s string) bool {
	if s == "" {
		return false
	}
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// binaryName returns the actual invoked binary's basename, per PART 32
// binary-rename support: the User-Agent stays fixed to the project name,
// but help/version output shows whatever the binary was actually invoked
// as.
func binaryName(argv []string) string {
	if len(argv) == 0 {
		return "api-cli"
	}
	base := argv[0]
	if i := strings.LastIndexAny(base, `/\`); i >= 0 {
		base = base[i+1:]
	}
	base = strings.TrimSuffix(base, ".exe")
	if base == "" {
		return "api-cli"
	}
	return base
}

func printVersion(binName string, build BuildInfo) {
	fmt.Printf("%s %s (%s) built %s\n", binName, build.Version, build.Commit, build.BuildDate)
}

func printHelp(binName string, build BuildInfo) {
	fmt.Printf("%s %s - CLI for api\n\n", binName, build.Version)
	fmt.Printf("Usage:\n")
	fmt.Printf("  %s [args] [flags]\n", binName)
	fmt.Printf("  # TUI mode (no args)\n")
	fmt.Printf("  %s\n\n", binName)
	fmt.Printf("Flags:\n")
	fmt.Printf("-h, --help                             - Show help\n")
	fmt.Printf("-v, --version                          - Show version\n")
	fmt.Printf("--shell completions [SHELL]            - Print shell completions (auto-detect if SHELL omitted)\n")
	fmt.Printf("--shell init [SHELL]                   - Print shell init command (auto-detect if SHELL omitted)\n")
	fmt.Printf("--shell help                            - Show shell integration help\n\n")
	fmt.Printf("--server URL                           - Server URL (default: from config)\n")
	fmt.Printf("--token TOKEN                          - API token for authentication\n")
	fmt.Printf("--token-file FILE                       - Read token from file\n")
	fmt.Printf("--config NAME                          - Config profile name (default: cli.yml)\n")
	fmt.Printf("--debug                                - Debug output\n")
	fmt.Printf("--color {auto|yes|no}                  - Color output (default: auto)\n")
	fmt.Printf("--lang CODE                            - Language for output (default: auto)\n")
	fmt.Printf("--output {json|table|plain}            - Output format (default: table)\n\n")
	fmt.Printf("Commands:\n")
	for _, cat := range categories() {
		fmt.Printf("  %s\n", cat)
		for _, c := range categoryCommands(cat) {
			fmt.Printf("    %-30s %s\n", c.Name, c.Desc)
		}
	}
	fmt.Printf("\nShells: bash, zsh, fish, sh, dash, ksh, powershell, pwsh\n\n")
	fmt.Printf("Run without arguments for interactive TUI mode.\n")
	fmt.Printf("Run '%s <category> <command>' to execute a command directly.\n", binName)
}

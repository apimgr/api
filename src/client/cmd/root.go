// Package cmd implements the api-cli command dispatcher: global flag
// parsing, mode detection, help/version output, and command execution,
// per AI.md PART 32.
package cmd

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/apimgr/api/src/client/api"
	"github.com/apimgr/api/src/client/caps"
	"github.com/apimgr/api/src/client/config"
	"github.com/apimgr/api/src/client/paths"
	cliupdate "github.com/apimgr/api/src/client/update"
	"github.com/apimgr/api/src/common/i18n"
	commonversion "github.com/apimgr/api/src/common/version"
)

// Exit codes, per AI.md PART 32 Error Handling.
const (
	ExitSuccess  = 0
	ExitGeneral  = 1
	ExitConfig   = 78
	ExitConn     = 69
	ExitAuth     = 77
	ExitNotFound = 66
	ExitUsageBad = 2
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

var outputLanguage = i18n.DefaultLanguage

// Execute is the api-cli entrypoint. argv is os.Args (including argv[0]).
func Execute(argv []string, build BuildInfo) int {
	binName := binaryName(argv)
	api.UserAgent = fmt.Sprintf("%s-cli/%s", paths.ProjectName, build.Version)

	flags := parseGlobalFlags(argv[1:])
	var cfg *config.CLIConfig
	if loaded, err := config.Load(flags.ConfigName); err == nil {
		cfg = loaded
	}
	outputLanguage = resolveLanguage(flags.Lang, cfg)

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
			printCLIError(cliText("cli.client.shell_failure", "Shell integration could not be generated.") + ": " + err.Error())
			return ExitUsageBad
		}
		return ExitSuccess
	}

	if err := paths.EnsureDirs(); err != nil {
		fmt.Fprintln(os.Stderr, cliText("cli.client.config_dir_failure", "Could not create CLI configuration directories."))
		return ExitConfig
	}

	var err error
	if cfg == nil {
		cfg, err = config.Load(flags.ConfigName)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, cliText("cli.client.config_load_failure", "Could not load CLI configuration."))
		return ExitConfig
	}
	logFile := cfg.Logging.File
	if logFile == "" {
		logFile = paths.LogFile()
	}
	if err := cliupdate.RecordCompletion(logFile); err != nil {
		fmt.Fprintln(os.Stderr, cliText("cli.client.update_record_warning", "Warning: could not record CLI update completion."))
	}

	if flags.Server != "" && !validServerURL(flags.Server) {
		fmt.Fprintln(os.Stderr, cliText("cli.client.invalid_server_url", "Error: invalid server URL."))
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
			fmt.Fprintln(os.Stderr, cliText("cli.client.config_save_failure", "Error: could not save CLI configuration."))
			return ExitConfig
		}
	}

	client := api.NewWithAPIVersion(
		server,
		token,
		parseDurationOr(cfg.Server.Timeout, api.DefaultTimeout),
		cfg.Server.Retry,
		parseDurationOr(cfg.Server.RetryDelay, api.DefaultRetryDelay),
		cfg.Server.APIVersion,
	)
	client.Lang = outputLanguage
	client.Debug = flags.Debug || cfg.Debug

	mode, modeError := resolveDisplayMode(detectMode(argv[1:]), cfg.Display.Mode, caps.GUISupported)
	if modeError != "" {
		message := "Error: GUI mode is not available in this build."
		if modeError == "tui_unavailable" {
			message = cliText("cli.client.tui_unavailable", "Error: TUI mode is not available in this build.")
		} else {
			message = cliText("cli.client.gui_unavailable", message)
		}
		fmt.Fprintln(os.Stderr, message)
		return ExitUsageBad
	}
	if mode == ClientDisplayModeHeadless {
		fmt.Fprintln(os.Stderr, cliText("cli.client.no_command", "Error: no command given."))
		fmt.Fprintln(os.Stderr, cliText("cli.client.help_hint", "Run with --help for usage."))
		return ExitUsageBad
	}
	if server == "" && mode != ClientDisplayModeTUI {
		fmt.Fprintln(os.Stderr, cliText("cli.client.no_server", "Error: no server configured."))
		fmt.Fprintln(os.Stderr, i18n.TranslateFormat(outputLanguage, "cli.client.configure_server", map[string]string{"config": paths.ConfigFile()}))
		return ExitConn
	}
	if server == "" && flags.Update != "" {
		fmt.Fprintln(os.Stderr, cliText("cli.client.no_update_server", "Error: no server configured for CLI update."))
		return ExitConn
	}

	// cli.yml tui.enabled: false forces CLI-only operation for
	// accessibility or preference; auto-detection stays the default,
	// per PART 32.
	if mode == ClientDisplayModeTUI && !cfg.TUI.Enabled {
		mode = ClientDisplayModeCLI
	}

	if server != "" {
		decision, updateErr := cliupdate.Check(context.Background(), server, token, build.Version, &http.Client{Timeout: parseDurationOr(cfg.Server.Timeout, api.DefaultTimeout)})
		if updateErr != nil {
			if flags.Update != "" {
				fmt.Fprintln(os.Stderr, cliText("cli.client.update_check_failed", "Error: CLI update check failed."))
				return ExitConn
			}
		} else {
			if decision.VersionMismatch {
				fmt.Fprintln(os.Stderr, i18n.TranslateFormat(outputLanguage, "cli.client.version_mismatch", map[string]string{"cli_version": build.Version, "server_version": decision.Discovery.Data.Version}))
			}
			if flags.Update == "check" {
				if decision.Available {
					_, _ = fmt.Fprintln(os.Stdout, i18n.TranslateFormat(outputLanguage, "cli.client.update_available", map[string]string{"version": decision.Release.Version}))
				} else {
					_, _ = fmt.Fprintln(os.Stdout, cliText("cli.client.up_to_date", "CLI is up to date."))
				}
				return ExitSuccess
			}
			if flags.Update == "yes" {
				if !decision.Available {
					if decision.Forced {
						fmt.Fprintln(os.Stderr, i18n.TranslateFormat(outputLanguage, "cli.client.update_required", map[string]string{"version": decision.Discovery.Data.CLIMinVersion, "binary": paths.ProjectName + "-cli"}))
						return ExitGeneral
					}
					if len(flags.Rest) == 0 {
						_, _ = fmt.Fprintln(os.Stdout, cliText("cli.client.up_to_date", "CLI is up to date."))
						return ExitSuccess
					}
				} else {
					if !stdinIsInteractive() && !cfg.Update.Auto {
						fmt.Fprintln(os.Stderr, cliText("cli.client.update_auto_required", "Error: silent CLI updates require update.auto: true in cli.yml."))
						return ExitGeneral
					}
					if stdinIsInteractive() && !promptForUpdate(decision.Release.Version) {
						return ExitSuccess
					}
					if err := cliupdate.Install(context.Background(), server, token, build.Version, decision.Discovery.Data.API.CurrentVersion, decision.Release, &http.Client{Timeout: 10 * time.Minute}, logFile); err != nil {
						fmt.Fprintln(os.Stderr, cliText("cli.client.update_failed", "Error: CLI update failed."))
						return ExitGeneral
					}
					return ExitSuccess
				}
			}
			if decision.Forced {
				if err := cliupdate.LogForced(logFile, build.Version, decision.Discovery.Data.CLIMinVersion); err != nil {
					fmt.Fprintln(os.Stderr, cliText("cli.client.update_audit_warning", "Warning: could not write CLI update audit event."))
				}
				fmt.Fprintln(os.Stderr, i18n.TranslateFormat(outputLanguage, "cli.client.update_required", map[string]string{"version": decision.Discovery.Data.CLIMinVersion, "binary": paths.ProjectName + "-cli"}))
				return ExitGeneral
			}
			if decision.Available {
				if stdinIsInteractive() && promptForUpdate(decision.Release.Version) {
					if err := cliupdate.Install(context.Background(), server, token, build.Version, decision.Discovery.Data.API.CurrentVersion, decision.Release, &http.Client{Timeout: 10 * time.Minute}, logFile); err != nil {
						fmt.Fprintln(os.Stderr, cliText("cli.client.update_failed", "Error: CLI update failed."))
						return ExitGeneral
					}
					// Successful replacement re-execs with the original argv.
				}
				if !stdinIsInteractive() {
					fmt.Fprintln(os.Stderr, i18n.TranslateFormat(outputLanguage, "cli.client.update_available_hint", map[string]string{"version": decision.Release.Version}))
				}
			}
		}
	}

	if mode == ClientDisplayModeTUI {
		if TUILauncher == nil {
			fmt.Fprintln(os.Stderr, cliText("cli.client.tui_unavailable", "Error: TUI mode is not available in this build."))
			return ExitGeneral
		}
		return TUILauncher(cfg, client, build)
	}

	return runCLI(flags, cfg, client)
}

func resolveLanguage(flagLang string, cfg *config.CLIConfig) string {
	for _, candidate := range []string{flagLang, configLanguage(cfg), os.Getenv("LC_ALL"), os.Getenv("LANG")} {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || strings.EqualFold(candidate, "auto") {
			continue
		}
		parts := strings.FieldsFunc(candidate, func(r rune) bool {
			return r == '_' || r == '-' || r == '.'
		})
		if len(parts) == 0 {
			continue
		}
		candidate = strings.ToLower(parts[0])
		if i18n.IsSupported(candidate) {
			return candidate
		}
		return i18n.DefaultLanguage
	}
	return i18n.DefaultLanguage
}

func configLanguage(cfg *config.CLIConfig) string {
	if cfg == nil {
		return ""
	}
	return cfg.Defaults.Lang
}

func promptForUpdate(version string) bool {
	fmt.Fprint(os.Stderr, i18n.TranslateFormat(outputLanguage, "cli.client.update_prompt", map[string]string{"version": version}))
	var answer string
	if _, err := fmt.Fscanln(os.Stdin, &answer); err != nil {
		return false
	}
	return strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes")
}

func stdinIsInteractive() bool {
	info, err := os.Stdin.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// runCLI dispatches a single command in CLI/plain mode.
func runCLI(flags parsedFlags, cfg *config.CLIConfig, client *api.Client) int {
	if len(flags.Rest) == 0 {
		fmt.Fprintln(os.Stderr, cliText("cli.client.no_command", "Error: no command given."))
		fmt.Fprintln(os.Stderr, cliText("cli.client.help_hint", "Run with --help for usage."))
		return ExitUsageBad
	}

	category := flags.Rest[0]
	if len(flags.Rest) < 2 {
		cmds := categoryCommands(category)
		if len(cmds) == 0 {
			fmt.Fprintln(os.Stderr, i18n.TranslateFormat(outputLanguage, "cli.client.unknown_command", map[string]string{"command": category}))
			return ExitUsageBad
		}
		fmt.Printf("%s\n\n%s:\n", i18n.TranslateFormat(outputLanguage, "cli.client.category_usage", map[string]string{"category": category}), cliText("cli.client.subcommands", "Subcommands"))
		for _, c := range cmds {
			fmt.Printf("  %-30s %s\n", c.Name, c.Desc)
		}
		return ExitSuccess
	}

	name := flags.Rest[1]
	args := flags.Rest[2:]

	command, ok := findCommand(category, name)
	if !ok {
		fmt.Fprintln(os.Stderr, i18n.TranslateFormat(outputLanguage, "cli.client.unknown_command", map[string]string{"command": category + " " + name}))
		return ExitUsageBad
	}

	// Output format precedence: --output flag, then cli.yml
	// output.format, then the project-specific defaults.output, then
	// the compiled default.
	format := firstNonEmpty(flags.Output, cfg.Output.Format, cfg.Defaults.Output, "table")

	if err := command.Run(client, &OutputOptions{Format: format}, args); err != nil {
		return exitCodeForError(err)
	}
	return ExitSuccess
}

// exitCodeForError maps a command error to the PART 32 exit-code table.
func exitCodeForError(err error) int {
	printCLIError(err.Error())

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

func printCLIError(message string) {
	label := i18n.Translate(outputLanguage, "common.error")
	if label == "" {
		label = "Error"
	}
	fmt.Fprintf(os.Stderr, "%s: %s\n", label, message)
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
	if cfg.Auth.Token != "" {
		return cfg.Auth.Token
	}
	if cfg.Auth.TokenFile != "" {
		data, err := os.ReadFile(cfg.Auth.TokenFile)
		if err == nil {
			return strings.TrimSpace(string(data))
		}
	}
	return ""
}

// firstNonEmpty returns the first non-empty string in vals, or "".
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// parseDurationOr parses a cli.yml duration string, falling back to the
// compiled default when the value is empty or malformed. Invalid config
// never fails startup, per PART 5.
func parseDurationOr(s string, fallback time.Duration) time.Duration {
	if s == "" {
		return fallback
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return fallback
	}
	return d
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
	fmt.Print(commonversion.FormatOutput(binName, build.Version, build.Commit, build.BuildDate, runtime.Version(), runtime.GOOS, runtime.GOARCH))
}

func printHelp(binName string, build BuildInfo) {
	fmt.Printf("%s %s - %s\n\n", binName, build.Version, cliText("cli.client.description", "CLI for api"))
	fmt.Printf("%s\n", cliText("cli.usage", "Usage:"))
	fmt.Printf("  %s [args] [flags]\n", binName)
	fmt.Printf("  # %s\n", cliText("cli.client.tui_mode", "TUI mode (no args)"))
	fmt.Printf("  %s\n\n", binName)
	fmt.Printf("%s:\n", cliText("cli.client.flags", "Flags"))
	fmt.Printf("-h, --help                             - %s\n", cliText("cli.show_help", "Show help"))
	fmt.Printf("-v, --version                          - %s\n", cliText("cli.show_version", "Show version"))
	fmt.Printf("--shell completions [SHELL]            - %s\n", cliText("cli.client.completions", "Print shell completions (auto-detect if SHELL omitted)"))
	fmt.Printf("--shell init [SHELL]                   - %s\n", cliText("cli.client.shell_init", "Print shell init command (auto-detect if SHELL omitted)"))
	fmt.Printf("--shell help                            - %s\n\n", cliText("cli.client.shell_help", "Show shell integration help"))
	fmt.Printf("--server URL                           - %s\n", cliText("cli.client.server_url", "Server URL (default: from config)"))
	fmt.Printf("--token TOKEN                          - %s\n", cliText("cli.client.token", "API token for authentication"))
	fmt.Printf("--token-file FILE                       - %s\n", cliText("cli.client.token_file", "Read token from file"))
	fmt.Printf("--config NAME                          - %s\n", cliText("cli.client.config_profile", "Config profile name (default: cli.yml)"))
	fmt.Printf("--debug                                - %s\n", cliText("cli.client.debug", "Debug output"))
	fmt.Printf("--color {auto|yes|no}                  - %s\n", cliText("cli.client.color", "Color output (default: auto)"))
	fmt.Printf("--lang CODE                            - %s\n", cliText("cli.client.language", "Language for output (default: auto)"))
	fmt.Printf("--output {json|table|plain}            - %s\n", cliText("cli.client.output", "Output format (default: table)"))
	fmt.Printf("--update [check|yes]                    - %s\n\n", cliText("cli.client.update", "Check or install a CLI update"))
	fmt.Printf("%s:\n", cliText("cli.client.commands", "Commands"))
	for _, cat := range categories() {
		fmt.Printf("  %s\n", cat)
		for _, c := range categoryCommands(cat) {
			fmt.Printf("    %-30s %s\n", c.Name, commandDescription(c))
		}
	}
	fmt.Printf("\n%s\n\n", cliText("cli.client.shells", "Shells: bash, zsh, fish, sh, dash, ksh, powershell, pwsh"))
	fmt.Printf("%s\n", cliText("cli.client.run_tui", "Run without arguments for interactive TUI mode."))
	fmt.Printf("%s\n", cliText("cli.client.display_mode", "Config display.mode: auto, tui, or gui (GUI is unavailable in the static build)."))
	fmt.Printf("%s\n", i18n.TranslateFormat(outputLanguage, "cli.client.run_command", map[string]string{"command": binName + " <category> <command>"}))
}

func cliText(key, fallback string) string {
	value := i18n.Translate(outputLanguage, key)
	if value == key {
		return fallback
	}
	return value
}

func commandDescription(command Command) string {
	key := "cli.commands." + command.Category + "." + command.Name + ".description"
	return cliText(key, command.Desc)
}

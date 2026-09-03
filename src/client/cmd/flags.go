package cmd

import "strings"

// globalFlagNames are the flags Execute() consumes itself, before command
// dispatch. Space-syntax parsing needs to know which of these take a
// value so it can skip the following argv element.
var globalValueFlags = map[string]bool{
	"--server":     true,
	"--token":      true,
	"--token-file": true,
	"--config":     true,
	"--color":      true,
	"--lang":       true,
	"--output":     true,
	"--shell":      true,
}

// parsedFlags is the result of pulling global flags out of argv, leaving
// the remaining positional command/args behind.
type parsedFlags struct {
	Help       bool
	Version    bool
	Server     string
	Token      string
	TokenFile  string
	ConfigName string
	Color      string
	Lang       string
	Output     string
	Debug      bool
	Shell      string
	ShellArg   string
	Rest       []string
}

// parseGlobalFlags extracts universal/common flags from argv (excluding
// argv[0]), leaving the remaining command and its arguments in Rest.
// Accepts both `--flag=value` and `--flag value` syntax.
func parseGlobalFlags(argv []string) parsedFlags {
	p := parsedFlags{Color: "auto"}

	for i := 0; i < len(argv); i++ {
		arg := argv[i]

		name, value, hasEquals := strings.Cut(arg, "=")

		switch name {
		case "-h", "--help":
			p.Help = true
			continue
		case "-v", "--version":
			p.Version = true
			continue
		case "--debug":
			p.Debug = true
			continue
		}

		if !globalValueFlags[name] {
			p.Rest = append(p.Rest, arg)
			continue
		}

		if !hasEquals {
			if i+1 < len(argv) {
				value = argv[i+1]
				i++
			} else {
				value = ""
			}
		}

		switch name {
		case "--server":
			p.Server = value
		case "--token":
			p.Token = value
		case "--token-file":
			p.TokenFile = value
		case "--config":
			p.ConfigName = value
		case "--color":
			p.Color = value
		case "--lang":
			p.Lang = value
		case "--output":
			p.Output = value
		case "--shell":
			p.Shell = value
			// --shell completions/init/help takes an optional
			// trailing SHELL argument; consume it if present and
			// not itself a flag.
			if i+1 < len(argv) && !strings.HasPrefix(argv[i+1], "-") {
				p.ShellArg = argv[i+1]
				i++
			}
		}
	}

	return p
}

// isConfigOnlyFlag reports whether arg is one of the flags that, per the
// PART 32 mode-detection table, does NOT force CLI mode on its own
// (--config, --server, --token, --debug).
func isConfigOnlyFlag(name string) bool {
	switch name {
	case "--config", "--server", "--token", "--debug":
		return true
	default:
		return false
	}
}

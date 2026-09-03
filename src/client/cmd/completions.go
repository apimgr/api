package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// binName is the program name completions are generated for.
const binName = "api-cli"

// runCompletions implements `api-cli --shell completions [SHELL]` and
// `--shell init [SHELL]`, per AI.md PART 32 Shell Completions.
func runCompletions(action, shell string) error {
	if shell == "" {
		shell = detectShell()
	}
	shell = normalizeShell(shell)

	switch action {
	case "help":
		printCompletionsHelp()
		return nil
	case "completions":
		script, err := completionScript(shell)
		if err != nil {
			return err
		}
		fmt.Print(script)
		return nil
	case "init":
		return printInitSnippet(shell)
	default:
		return fmt.Errorf("unknown --shell action %q (expected completions, init, or help)", action)
	}
}

// detectShell infers the caller's shell from $SHELL when none is given
// explicitly.
func detectShell() string {
	shellEnv := os.Getenv("SHELL")
	if shellEnv == "" {
		return "bash"
	}
	return filepath.Base(shellEnv)
}

func normalizeShell(shell string) string {
	switch shell {
	case "sh", "dash", "ksh":
		return "sh"
	case "pwsh", "powershell":
		return "powershell"
	default:
		return shell
	}
}

func printCompletionsHelp() {
	fmt.Printf(`%s shell completions

Usage:
  %s --shell completions [SHELL]   Print a completion script to stdout
  %s --shell init [SHELL]          Print the line to add to your shell rc file
  %s --shell help                  Show this help

Supported SHELL values: bash, zsh, fish, sh, dash, ksh, powershell, pwsh
If SHELL is omitted, it is detected from $SHELL.
`, binName, binName, binName, binName)
}

func printInitSnippet(shell string) error {
	switch shell {
	case "bash":
		fmt.Printf("source <(%s --shell completions bash)\n", binName)
	case "zsh":
		fmt.Printf("source <(%s --shell completions zsh)\n", binName)
	case "fish":
		fmt.Printf("%s --shell completions fish | source\n", binName)
	case "sh":
		fmt.Printf(". <(%s --shell completions sh)\n", binName)
	case "powershell":
		fmt.Printf("%s --shell completions powershell | Out-String | Invoke-Expression\n", binName)
	default:
		return fmt.Errorf("unsupported shell %q", shell)
	}
	return nil
}

func completionScript(shell string) (string, error) {
	words := completionWords()

	switch shell {
	case "bash":
		return bashCompletionScript(words), nil
	case "zsh":
		return zshCompletionScript(words), nil
	case "fish":
		return fishCompletionScript(words), nil
	case "sh":
		return shCompletionScript(words), nil
	case "powershell":
		return powershellCompletionScript(words), nil
	default:
		return "", fmt.Errorf("unsupported shell %q (expected bash, zsh, fish, sh, dash, ksh, powershell, pwsh)", shell)
	}
}

// completionWords returns every "category command" pair, plus bare
// categories, for use as completion candidates.
func completionWords() []string {
	var words []string
	for _, cat := range categories() {
		words = append(words, cat)
		for _, c := range categoryCommands(cat) {
			words = append(words, cat+" "+c.Name)
		}
	}
	return words
}

func bashCompletionScript(words []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "_%s_completions() {\n", strings.ReplaceAll(binName, "-", "_"))
	b.WriteString("  local cur words\n")
	b.WriteString("  cur=\"${COMP_WORDS[COMP_CWORD]}\"\n")
	b.WriteString("  words=\"")
	for _, w := range words {
		b.WriteString(strings.Fields(w)[0])
		b.WriteString(" ")
	}
	b.WriteString("\"\n")
	b.WriteString("  COMPREPLY=($(compgen -W \"$words\" -- \"$cur\"))\n")
	b.WriteString("}\n")
	fmt.Fprintf(&b, "complete -F _%s_completions %s\n", strings.ReplaceAll(binName, "-", "_"), binName)
	return b.String()
}

func zshCompletionScript(words []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "#compdef %s\n\n", binName)
	fmt.Fprintf(&b, "_%s() {\n", strings.ReplaceAll(binName, "-", "_"))
	b.WriteString("  local -a subcommands\n")
	b.WriteString("  subcommands=(\n")
	seen := map[string]bool{}
	for _, w := range words {
		first := strings.Fields(w)[0]
		if seen[first] {
			continue
		}
		seen[first] = true
		fmt.Fprintf(&b, "    %q\n", first)
	}
	b.WriteString("  )\n")
	b.WriteString("  _describe 'command' subcommands\n")
	b.WriteString("}\n\n")
	fmt.Fprintf(&b, "compdef _%s %s\n", strings.ReplaceAll(binName, "-", "_"), binName)
	return b.String()
}

func fishCompletionScript(words []string) string {
	var b strings.Builder
	seen := map[string]bool{}
	for _, w := range words {
		first := strings.Fields(w)[0]
		if seen[first] {
			continue
		}
		seen[first] = true
		fmt.Fprintf(&b, "complete -c %s -n '__fish_use_subcommand' -a '%s'\n", binName, first)
	}
	return b.String()
}

func shCompletionScript(words []string) string {
	var b strings.Builder
	b.WriteString("# POSIX sh has no native completion framework;\n")
	b.WriteString("# this script only lists valid top-level commands.\n")
	fmt.Fprintf(&b, "# %s commands:\n", binName)
	seen := map[string]bool{}
	for _, w := range words {
		first := strings.Fields(w)[0]
		if seen[first] {
			continue
		}
		seen[first] = true
		fmt.Fprintf(&b, "#   %s\n", first)
	}
	return b.String()
}

func powershellCompletionScript(words []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Register-ArgumentCompleter -Native -CommandName %s -ScriptBlock {\n", binName)
	b.WriteString("  param($wordToComplete, $commandAst, $cursorPosition)\n")
	b.WriteString("  $commands = @(\n")
	seen := map[string]bool{}
	for _, w := range words {
		first := strings.Fields(w)[0]
		if seen[first] {
			continue
		}
		seen[first] = true
		fmt.Fprintf(&b, "    %q\n", first)
	}
	b.WriteString("  )\n")
	b.WriteString("  $commands | Where-Object { $_ -like \"$wordToComplete*\" } | ForEach-Object {\n")
	b.WriteString("    [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)\n")
	b.WriteString("  }\n")
	b.WriteString("}\n")
	return b.String()
}

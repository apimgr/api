package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// serverFlags lists every top-level flag this binary accepts, used as
// completion candidates for --shell completions/init.
var serverFlags = []string{
	"--help", "--version", "--status",
	"--mode", "--config", "--data", "--log", "--cache", "--backup", "--pid",
	"--address", "--port", "--baseurl", "--daemon", "--debug",
	"--color", "--lang", "--shell",
	"--service", "--maintenance", "--update",
}

// handleShellCommand implements `--shell completions|init|help [SHELL]`,
// per AI.md PART 8 shared flags.
func handleShellCommand(action, shell, binaryName string) {
	if shell == "" {
		shell = detectShell()
	}
	shell = normalizeShell(shell)

	switch action {
	case "help":
		printShellHelp(binaryName)
	case "completions":
		script, err := shellCompletionScript(binaryName, shell)
		if err != nil {
			fmt.Println("❌", err)
			os.Exit(1)
		}
		fmt.Print(script)
	case "init":
		if err := printShellInitSnippet(binaryName, shell); err != nil {
			fmt.Println("❌", err)
			os.Exit(1)
		}
	default:
		fmt.Printf("Unknown --shell action %q (expected completions, init, or help)\n", action)
		os.Exit(1)
	}
}

// detectShell infers the caller's shell from $SHELL when none is given
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

func printShellHelp(binaryName string) {
	fmt.Printf(`%s shell integration

Usage:
  %s --shell completions [SHELL]   Print a completion script to stdout
  %s --shell init [SHELL]          Print the line to add to your shell rc file
  %s --shell help                  Show this help

Supported SHELL values: bash, zsh, fish, sh, dash, ksh, powershell, pwsh
If SHELL is omitted, it is detected from $SHELL.
`, binaryName, binaryName, binaryName, binaryName)
}

func printShellInitSnippet(binaryName, shell string) error {
	switch shell {
	case "bash":
		fmt.Printf("source <(%s --shell completions bash)\n", binaryName)
	case "zsh":
		fmt.Printf("source <(%s --shell completions zsh)\n", binaryName)
	case "fish":
		fmt.Printf("%s --shell completions fish | source\n", binaryName)
	case "sh":
		fmt.Printf(". <(%s --shell completions sh)\n", binaryName)
	case "powershell":
		fmt.Printf("%s --shell completions powershell | Out-String | Invoke-Expression\n", binaryName)
	default:
		return fmt.Errorf("unsupported shell %q", shell)
	}
	return nil
}

func shellCompletionScript(binaryName, shell string) (string, error) {
	switch shell {
	case "bash":
		return bashServerCompletionScript(binaryName), nil
	case "zsh":
		return zshServerCompletionScript(binaryName), nil
	case "fish":
		return fishServerCompletionScript(binaryName), nil
	case "sh":
		return shServerCompletionScript(binaryName), nil
	case "powershell":
		return powershellServerCompletionScript(binaryName), nil
	default:
		return "", fmt.Errorf("unsupported shell %q (expected bash, zsh, fish, sh, dash, ksh, powershell, pwsh)", shell)
	}
}

func bashServerCompletionScript(binaryName string) string {
	fn := "_" + strings.ReplaceAll(binaryName, "-", "_") + "_completions"
	var b strings.Builder
	fmt.Fprintf(&b, "%s() {\n", fn)
	b.WriteString("  local cur\n")
	b.WriteString("  cur=\"${COMP_WORDS[COMP_CWORD]}\"\n")
	fmt.Fprintf(&b, "  COMPREPLY=($(compgen -W \"%s\" -- \"$cur\"))\n", strings.Join(serverFlags, " "))
	b.WriteString("}\n")
	fmt.Fprintf(&b, "complete -F %s %s\n", fn, binaryName)
	return b.String()
}

func zshServerCompletionScript(binaryName string) string {
	fn := "_" + strings.ReplaceAll(binaryName, "-", "_")
	var b strings.Builder
	fmt.Fprintf(&b, "#compdef %s\n\n", binaryName)
	fmt.Fprintf(&b, "%s() {\n", fn)
	b.WriteString("  local -a flags\n")
	b.WriteString("  flags=(\n")
	for _, f := range serverFlags {
		fmt.Fprintf(&b, "    %q\n", f)
	}
	b.WriteString("  )\n")
	b.WriteString("  _describe 'flag' flags\n")
	b.WriteString("}\n\n")
	fmt.Fprintf(&b, "compdef %s %s\n", fn, binaryName)
	return b.String()
}

func fishServerCompletionScript(binaryName string) string {
	var b strings.Builder
	for _, f := range serverFlags {
		name := strings.TrimPrefix(f, "--")
		fmt.Fprintf(&b, "complete -c %s -l %s\n", binaryName, name)
	}
	return b.String()
}

func shServerCompletionScript(binaryName string) string {
	var b strings.Builder
	b.WriteString("# POSIX sh has no native completion framework;\n")
	b.WriteString("# this script only lists valid flags.\n")
	fmt.Fprintf(&b, "# %s flags:\n", binaryName)
	for _, f := range serverFlags {
		fmt.Fprintf(&b, "#   %s\n", f)
	}
	return b.String()
}

func powershellServerCompletionScript(binaryName string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Register-ArgumentCompleter -Native -CommandName %s -ScriptBlock {\n", binaryName)
	b.WriteString("  param($wordToComplete, $commandAst, $cursorPosition)\n")
	b.WriteString("  $flags = @(\n")
	for _, f := range serverFlags {
		fmt.Fprintf(&b, "    %q\n", f)
	}
	b.WriteString("  )\n")
	b.WriteString("  $flags | Where-Object { $_ -like \"$wordToComplete*\" } | ForEach-Object {\n")
	b.WriteString("    [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)\n")
	b.WriteString("  }\n")
	b.WriteString("}\n")
	return b.String()
}

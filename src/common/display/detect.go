package display

import (
	"os"
	"strings"

	"golang.org/x/term"
)

func DetectDisplayEnv() DisplayEnv {
	env := DisplayEnv{
		IsTerminal:   term.IsTerminal(int(os.Stdout.Fd())),
		TerminalType: os.Getenv("TERM"),
		IsSSH:        os.Getenv("SSH_CLIENT") != "" || os.Getenv("SSH_TTY") != "",
		IsMosh:       os.Getenv("MOSH") != "" || strings.Contains(strings.ToLower(os.Getenv("TERM")), "mosh"),
		IsScreen:     os.Getenv("STY") != "" || os.Getenv("TMUX") != "",
	}
	if env.IsTerminal {
		if cols, rows, err := terminalDimensions(); err == nil {
			env.Cols, env.Rows = cols, rows
		}
	}
	env.detectPlatformDisplay()
	env.Mode = env.autoDetectDisplayMode()
	return env
}

func terminalDimensions() (int, int, error) { return term.GetSize(int(os.Stdout.Fd())) }

func CanUseANSI(env DisplayEnv) bool {
	return env.IsTerminal && !env.IsDumbTerminal() && os.Getenv("NO_COLOR") == ""
}

// Package banner renders the startup status in a terminal-width-aware form.
package banner

import (
	"bytes"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/apimgr/api/src/common/display"
	"github.com/apimgr/api/src/common/i18n"
	"github.com/apimgr/api/src/common/terminal"
)

// Config contains the application data needed to render a startup banner.
type Config struct {
	AppName     string
	Version     string
	AppMode     string
	Debug       bool
	URLs        []string
	StartupTime string
	Writer      io.Writer
	Lang        string
	Emojis      bool
	Colors      bool
	Terminal    display.DisplayEnv
}

// PrintStartup writes a responsive startup banner to the configured writer.
func PrintStartup(cfg Config) error {
	if cfg.Writer == nil {
		return nil
	}
	var rendered bytes.Buffer
	w := &rendered
	width := cfg.Terminal.Cols
	if width <= 0 {
		width = terminal.GetTerminalSize().Cols
	}
	if !cfg.Emojis || cfg.Terminal.IsDumbTerminal() {
		printPlain(w, cfg)
		return writeRendered(cfg.Writer, rendered.Bytes())
	}
	switch {
	case width >= 80:
		printFull(w, cfg)
	case width >= 60:
		printCompact(w, cfg)
	case width >= 40:
		printMinimal(w, cfg)
	default:
		printMicro(w, cfg)
	}
	return writeRendered(cfg.Writer, rendered.Bytes())
}

func writeRendered(writer io.Writer, rendered []byte) error {
	n, err := writer.Write(rendered)
	if err != nil {
		return fmt.Errorf("write startup banner: %w", err)
	}
	if n != len(rendered) {
		return io.ErrShortWrite
	}
	return nil
}

func printPlain(w io.Writer, cfg Config) {
	_, _ = fmt.Fprintf(w, "%s v%s\n", cfg.AppName, cfg.Version)
	_, _ = fmt.Fprintf(w, "%s: %s\n", i18n.Translate(cfg.Lang, "cli.running_in_mode_label"), cfg.AppMode)
	if cfg.Debug {
		_, _ = fmt.Fprintln(w, i18n.Translate(cfg.Lang, "cli.debugging_enabled"))
	}
	for _, address := range cfg.URLs {
		_, _ = fmt.Fprintf(w, "  %s\n", address)
	}
	if cfg.StartupTime != "" {
		_, _ = fmt.Fprintf(w, "  %s\n", i18n.TranslateFormat(cfg.Lang, "cli.server_started_on", map[string]string{"startup_datetime": cfg.StartupTime}))
	}
	_, _ = fmt.Fprintln(w)
}

func printFull(w io.Writer, cfg Config) {
	_, _ = fmt.Fprintf(w, "╭───────────────────────────────────────────────────────────╮\n│  %s\n├───────────────────────────────────────────────────────────┤\n", style(fmt.Sprintf("🚀 %s · 📦 %s", cfg.AppName, cfg.Version), "36", cfg))
	printMode(w, cfg, true, true)
	for _, address := range cfg.URLs {
		_, _ = fmt.Fprintf(w, "│  %s\n", style("🌐 "+address, "36", cfg))
	}
	if cfg.StartupTime != "" {
		_, _ = fmt.Fprintf(w, "│  ✅ %s\n", i18n.TranslateFormat(cfg.Lang, "cli.server_started_on", map[string]string{"startup_datetime": cfg.StartupTime}))
	}
	_, _ = fmt.Fprintln(w, "╰───────────────────────────────────────────────────────────╯")
}

func printCompact(w io.Writer, cfg Config) {
	_, _ = fmt.Fprintln(w, style(fmt.Sprintf("🚀 %s v%s", cfg.AppName, cfg.Version), "36", cfg))
	printMode(w, cfg, true, false)
	for _, address := range cfg.URLs {
		_, _ = fmt.Fprintln(w, style("🌐 "+address, "36", cfg))
	}
	if cfg.StartupTime != "" {
		_, _ = fmt.Fprintf(w, "✅ %s\n", i18n.TranslateFormat(cfg.Lang, "cli.server_started_on", map[string]string{"startup_datetime": cfg.StartupTime}))
	}
}

func printMinimal(w io.Writer, cfg Config) {
	_, _ = fmt.Fprintf(w, "%s %s\n%s\n", cfg.AppName, cfg.Version, cfg.AppMode)
	for _, address := range cfg.URLs {
		_, _ = fmt.Fprintln(w, hostPort(address))
	}
}

func printMicro(w io.Writer, cfg Config) {
	if len(cfg.URLs) == 0 {
		_, _ = fmt.Fprintln(w, cfg.AppName)
		return
	}
	_, _ = fmt.Fprintf(w, "%s %s\n", cfg.AppName, hostPort(cfg.URLs[0]))
}

func printMode(w io.Writer, cfg Config, icons, framed bool) {
	icon := ""
	if icons {
		switch strings.ToLower(cfg.AppMode) {
		case "development":
			icon = "🔧 "
		case "debug":
			icon = "🐛 "
		default:
			icon = "🔒 "
		}
	}
	debug := ""
	if cfg.Debug {
		debug = " [debugging]"
	}
	line := fmt.Sprintf("%s%s: %s%s", icon, i18n.Translate(cfg.Lang, "cli.running_in_mode_label"), cfg.AppMode, debug)
	if framed {
		_, _ = fmt.Fprintf(w, "│  %s\n", line)
		return
	}
	_, _ = fmt.Fprintln(w, line)
}

func hostPort(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	return u.Host
}

func style(text, code string, cfg Config) string {
	if !cfg.Colors || !display.CanUseANSI(cfg.Terminal) {
		return text
	}
	return "\033[" + code + "m" + text + "\033[0m"
}

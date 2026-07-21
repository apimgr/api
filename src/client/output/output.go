// Package output formats and prints API responses for the CLI, and
// implements the shared color/NO_COLOR rules from AI.md PART 32.
package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"

	"golang.org/x/term"
)

// Format is a CLI output format.
type Format string

const (
	// FormatJSON prints the response as pretty-printed JSON.
	FormatJSON Format = "json"
	// FormatTable prints the response as a best-effort table.
	FormatTable Format = "table"
	// FormatPlain prints the response as plain text.
	FormatPlain Format = "plain"
)

// ColorMode controls whether ANSI colors are emitted.
type ColorMode string

const (
	ColorAuto ColorMode = "auto"
	ColorYes  ColorMode = "yes"
	ColorNo   ColorMode = "no"
)

// ColorEnabled resolves the effective color mode, honoring --color and
// the NO_COLOR convention (https://no-color.org).
func ColorEnabled(mode ColorMode) bool {
	switch mode {
	case ColorYes:
		return true
	case ColorNo:
		return false
	default:
		if os.Getenv("NO_COLOR") != "" {
			return false
		}
		return term.IsTerminal(int(os.Stdout.Fd()))
	}
}

// Capture runs fn with os.Stdout temporarily redirected to an in-memory
// pipe and returns whatever fn printed, for callers (such as the TUI) that
// need a command's rendered output as a string rather than on the real
// terminal.
func Capture(fn func() error) (string, error) {
	orig := os.Stdout
	r, w, pipeErr := os.Pipe()
	if pipeErr != nil {
		return "", pipeErr
	}
	os.Stdout = w

	fnErr := fn()

	os.Stdout = orig
	w.Close()

	var buf bytes.Buffer
	if _, copyErr := io.Copy(&buf, r); copyErr != nil {
		return buf.String(), copyErr
	}
	return buf.String(), fnErr
}

// Print writes raw JSON response body to stdout in the requested format.
func Print(body []byte, format Format) error {
	switch format {
	case FormatJSON:
		return printJSON(body)
	case FormatPlain:
		return printPlain(body)
	default:
		return printTable(body)
	}
}

func printJSON(body []byte) error {
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		// Not JSON - print as-is.
		fmt.Println(string(bytes.TrimSpace(body)))
		return nil
	}
	pretty, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(pretty))
	return nil
}

func printPlain(body []byte) error {
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		fmt.Println(string(bytes.TrimSpace(body)))
		return nil
	}
	switch t := v.(type) {
	case map[string]any:
		if s, ok := singleValue(t); ok {
			fmt.Println(s)
			return nil
		}
	}
	fmt.Println(string(bytes.TrimSpace(body)))
	return nil
}

func printTable(body []byte) error {
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		fmt.Println(string(bytes.TrimSpace(body)))
		return nil
	}

	switch t := v.(type) {
	case map[string]any:
		printMapTable(t)
	case []any:
		printListTable(t)
	default:
		fmt.Println(string(bytes.TrimSpace(body)))
	}
	return nil
}

// singleValue returns the sole string/number field of a one-field map,
// used so `--output plain` can print e.g. {"uuid": "..."} as just the
// value.
func singleValue(m map[string]any) (string, bool) {
	if len(m) != 1 {
		return "", false
	}
	for _, v := range m {
		switch val := v.(type) {
		case string:
			return val, true
		case float64, bool:
			return fmt.Sprintf("%v", val), true
		}
	}
	return "", false
}

func printMapTable(m map[string]any) {
	keys := make([]string, 0, len(m))
	width := 0
	for k := range m {
		keys = append(keys, k)
		if len(k) > width {
			width = len(k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("%-*s  %v\n", width, k, m[k])
	}
}

func printListTable(list []any) {
	if len(list) == 0 {
		fmt.Println("(no results)")
		return
	}

	// Homogeneous list of scalars.
	if _, ok := list[0].(map[string]any); !ok {
		for _, item := range list {
			fmt.Println(item)
		}
		return
	}

	// List of objects: render columns from the union of keys, in the
	// order first seen.
	var columns []string
	seen := map[string]bool{}
	for _, item := range list {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		for _, k := range sortedKeys(row) {
			if !seen[k] {
				seen[k] = true
				columns = append(columns, k)
			}
		}
	}

	widths := make(map[string]int, len(columns))
	for _, c := range columns {
		widths[c] = len(c)
	}
	for _, item := range list {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		for _, c := range columns {
			if v, ok := row[c]; ok {
				if l := len(fmt.Sprintf("%v", v)); l > widths[c] {
					widths[c] = l
				}
			}
		}
	}

	for i, c := range columns {
		if i > 0 {
			fmt.Print("  ")
		}
		fmt.Printf("%-*s", widths[c], c)
	}
	fmt.Println()

	for _, item := range list {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		for i, c := range columns {
			if i > 0 {
				fmt.Print("  ")
			}
			val := ""
			if v, ok := row[c]; ok {
				val = fmt.Sprintf("%v", v)
			}
			fmt.Printf("%-*s", widths[c], val)
		}
		fmt.Println()
	}
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

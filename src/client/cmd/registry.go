package cmd

import (
	"fmt"

	"github.com/apimgr/api/src/client/api"
)

// Command is a single api-cli subcommand.
type Command struct {
	// Category groups commands under a top-level word, e.g. "text".
	Category string
	// Name is the subcommand word, e.g. "uuid".
	Name string
	// Usage is a short one-line usage string shown in help.
	Usage string
	// Desc describes what the command does.
	Desc string
	// Run executes the command against the given API client.
	Run func(c *api.Client, out *OutputOptions, args []string) error
}

// OutputOptions carries the resolved global flags a command needs to
// render its result.
type OutputOptions struct {
	Format string
}

var registry []Command

// register adds a command to the global registry. Called from each
// category file's init().
func register(cmd Command) {
	registry = append(registry, cmd)
}

// findCommand looks up a command by category and name.
func findCommand(category, name string) (Command, bool) {
	for _, c := range registry {
		if c.Category == category && c.Name == name {
			return c, true
		}
	}
	return Command{}, false
}

// categoryCommands returns every command registered under category, in
// registration order.
func categoryCommands(category string) []Command {
	var out []Command
	for _, c := range registry {
		if c.Category == category {
			out = append(out, c)
		}
	}
	return out
}

// categories returns the distinct set of registered categories, in
// first-seen order.
func categories() []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range registry {
		if !seen[c.Category] {
			seen[c.Category] = true
			out = append(out, c.Category)
		}
	}
	return out
}

// Categories is the exported form of categories(), for use by the tui
// package when building its category browser.
func Categories() []string {
	return categories()
}

// CategoryCommands is the exported form of categoryCommands(), for use by
// the tui package when building its command list.
func CategoryCommands(category string) []Command {
	return categoryCommands(category)
}

// FindCommand is the exported form of findCommand(), for use by the tui
// package when dispatching a selected command.
func FindCommand(category, name string) (Command, bool) {
	return findCommand(category, name)
}

// argAt returns args[i] or def if out of range.
func argAt(args []string, i int, def string) string {
	if i < len(args) {
		return args[i]
	}
	return def
}

// requireArg returns args[i], erroring with a usage message if missing.
func requireArg(args []string, i int, name string) (string, error) {
	if i >= len(args) {
		return "", fmt.Errorf("missing required argument: %s", name)
	}
	return args[i], nil
}

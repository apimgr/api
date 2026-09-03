package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withCleanRegistry snapshots the global registry, replaces it with an
// empty one for the duration of the test, and restores the original
// afterward so tests that register fake commands never leak into the real
// registry used by other tests (or by completions/help output).
func withCleanRegistry(t *testing.T) {
	t.Helper()
	orig := registry
	registry = nil
	t.Cleanup(func() { registry = orig })
}

// TestRegisterAndFindCommand covers the basic register -> findCommand round
// trip, plus the not-found boundary.
func TestRegisterAndFindCommand(t *testing.T) {
	withCleanRegistry(t)

	cmd := Command{Category: "fake", Name: "one", Usage: "fake one", Desc: "test command"}
	register(cmd)

	got, ok := findCommand("fake", "one")
	require.True(t, ok)
	assert.Equal(t, cmd.Desc, got.Desc)

	_, ok = findCommand("fake", "missing")
	assert.False(t, ok)

	_, ok = findCommand("missing-category", "one")
	assert.False(t, ok)
}

// TestFindCommand_EmptyRegistry verifies the boundary of looking up in an
// empty registry rather than erroring or panicking.
func TestFindCommand_EmptyRegistry(t *testing.T) {
	withCleanRegistry(t)

	_, ok := findCommand("anything", "anything")
	assert.False(t, ok)
}

// TestCategoryCommands_OrderAndFiltering verifies commands are returned
// only for the requested category and in registration order.
func TestCategoryCommands_OrderAndFiltering(t *testing.T) {
	withCleanRegistry(t)

	register(Command{Category: "a", Name: "first"})
	register(Command{Category: "b", Name: "other"})
	register(Command{Category: "a", Name: "second"})

	got := categoryCommands("a")
	require.Len(t, got, 2)
	assert.Equal(t, "first", got[0].Name)
	assert.Equal(t, "second", got[1].Name)

	assert.Empty(t, categoryCommands("nonexistent"))
}

// TestCategories_DistinctFirstSeenOrder verifies categories() dedupes and
// preserves first-seen ordering, matching the exported Categories().
func TestCategories_DistinctFirstSeenOrder(t *testing.T) {
	withCleanRegistry(t)

	register(Command{Category: "b", Name: "x"})
	register(Command{Category: "a", Name: "y"})
	register(Command{Category: "b", Name: "z"})

	assert.Equal(t, []string{"b", "a"}, categories())
	assert.Equal(t, []string{"b", "a"}, Categories())
}

// TestExportedWrappers verifies CategoryCommands/FindCommand delegate
// exactly to their unexported counterparts.
func TestExportedWrappers(t *testing.T) {
	withCleanRegistry(t)

	register(Command{Category: "cat", Name: "cmd", Desc: "desc"})

	assert.Equal(t, categoryCommands("cat"), CategoryCommands("cat"))

	got, ok := FindCommand("cat", "cmd")
	require.True(t, ok)
	assert.Equal(t, "desc", got.Desc)
}

// TestArgAt covers in-range, out-of-range, and empty-slice access.
func TestArgAt(t *testing.T) {
	tests := []struct {
		name string
		args []string
		i    int
		def  string
		want string
	}{
		{"in range", []string{"a", "b"}, 0, "z", "a"},
		{"last element", []string{"a", "b"}, 1, "z", "b"},
		{"out of range returns default", []string{"a"}, 5, "default", "default"},
		{"empty slice returns default", nil, 0, "default", "default"},
		{"negative-adjacent boundary at len", []string{"a"}, 1, "default", "default"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, argAt(tc.args, tc.i, tc.def))
		})
	}
}

// TestRequireArg covers the present and missing-argument branches, and
// verifies the error message names the missing argument.
func TestRequireArg(t *testing.T) {
	v, err := requireArg([]string{"password"}, 0, "password")
	require.NoError(t, err)
	assert.Equal(t, "password", v)

	_, err = requireArg([]string{}, 0, "password")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "password")

	_, err = requireArg([]string{"only-one"}, 1, "second")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "second")
}

// TestRealRegistry_CommandsAreWellFormed sanity-checks the actual registry
// populated by every category file's init() (crypto, datetime, network,
// raw, system, text): every command must have a non-empty category, name,
// and a non-nil Run function that satisfies the expected signature.
func TestRealRegistry_CommandsAreWellFormed(t *testing.T) {
	require.NotEmpty(t, registry, "expected category init() functions to have registered commands")

	seen := map[string]bool{}
	for _, c := range registry {
		assert.NotEmpty(t, c.Category)
		assert.NotEmpty(t, c.Name)
		assert.NotNil(t, c.Run)

		key := c.Category + " " + c.Name
		assert.False(t, seen[key], "duplicate command registered: %s", key)
		seen[key] = true
	}
}

// TestRealRegistry_KnownCommandsPresent spot-checks a handful of commands
// from each category file to make sure real registration actually
// happened, not just that the registry is non-empty.
func TestRealRegistry_KnownCommandsPresent(t *testing.T) {
	tests := []struct{ category, name string }{
		{"crypto", "bcrypt"},
		{"datetime", "now"},
		{"network", "ip"},
		{"raw", "get"},
		{"system", "health"},
		{"text", "uuid"},
	}
	for _, tc := range tests {
		t.Run(tc.category+" "+tc.name, func(t *testing.T) {
			_, ok := findCommand(tc.category, tc.name)
			assert.True(t, ok)
		})
	}
}

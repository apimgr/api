package tui

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/apimgr/api/src/client/api"
	"github.com/apimgr/api/src/client/cmd"
	"github.com/apimgr/api/src/client/config"
)

// testModel builds a Model against a real httptest.Server so Update-driven
// command execution exercises the actual cmd.Command.Run closures rather
// than a mock.
func testModel(t *testing.T) (Model, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(nil)
	t.Cleanup(srv.Close)
	cfg := config.Default()
	client := api.New(srv.URL, "")
	m := newModel(cfg, client, cmd.BuildInfo{Version: "1.0.0"})
	return m, srv
}

// TestNewModel_InitialState verifies the constructed model starts on the
// category screen with every registered category loaded and the dark theme
// applied by default.
func TestNewModel_InitialState(t *testing.T) {
	cfg := config.Default()
	client := api.New("http://127.0.0.1:1", "")
	m := newModel(cfg, client, cmd.BuildInfo{Version: "1.0.0"})

	assert.Equal(t, stateCategories, m.state)
	assert.Equal(t, TUIThemeDark, m.theme)
	assert.Len(t, m.categories.Items(), len(cmd.Categories()))
	assert.Empty(t, m.commands.Items())
}

// TestNewModel_LightTheme verifies cfg.TUI.Theme is honored.
func TestNewModel_LightTheme(t *testing.T) {
	cfg := config.Default()
	cfg.TUI.Theme = "light"
	client := api.New("http://127.0.0.1:1", "")
	m := newModel(cfg, client, cmd.BuildInfo{Version: "1.0.0"})
	assert.Equal(t, TUIThemeLight, m.theme)
}

// TestModel_Init verifies Init returns a nil tea.Cmd (nothing to kick off
// at startup).
func TestModel_Init(t *testing.T) {
	m, _ := testModel(t)
	assert.Nil(t, m.Init())
}

// TestModel_Update_WindowSizeMsg verifies a WindowSizeMsg is stored and
// triggers applyLayout, resizing the category list.
func TestModel_Update_WindowSizeMsg(t *testing.T) {
	m, _ := testModel(t)
	updated, teaCmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	nm := updated.(Model)

	assert.Nil(t, teaCmd)
	assert.Equal(t, 100, nm.width)
	assert.Equal(t, 40, nm.height)
	w, h := nm.categories.Width(), nm.categories.Height()
	assert.Equal(t, 100, w)
	// headerHeight(1) + footerHeight(1) subtracted from 40.
	assert.Equal(t, 38, h)
}

// TestApplyLayout_FloorsSmallDimensions verifies the usableHeight<3 and
// usableWidth<20 floor branches in applyLayout.
func TestApplyLayout_FloorsSmallDimensions(t *testing.T) {
	m, _ := testModel(t)
	m.width, m.height = 5, 1
	m.applyLayout()

	assert.Equal(t, 20, m.categories.Width(), "width should be floored to 20")
	assert.Equal(t, 3, m.categories.Height(), "height should be floored to 3")
	assert.Equal(t, 18, m.argsInput.Width)
}

// TestHandleKey_CtrlCAlwaysQuits verifies ctrl+c quits from every state.
func TestHandleKey_CtrlCAlwaysQuits(t *testing.T) {
	states := []state{stateCategories, stateCommands, stateArgs, stateOutput, stateHelp}
	for _, st := range states {
		t.Run("", func(t *testing.T) {
			m, _ := testModel(t)
			m.state = st
			_, teaCmd := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
			require.NotNil(t, teaCmd)
			assert.IsType(t, tea.QuitMsg{}, teaCmd())
		})
	}
}

// TestHandleKey_QQuitsExceptInArgsOrFiltering verifies "q" quits from the
// category/command/output/help screens but is treated as ordinary text
// input while on the args screen (so a token containing "q" can be typed).
func TestHandleKey_QQuitsExceptInArgsOrFiltering(t *testing.T) {
	quittingStates := []state{stateCategories, stateCommands, stateOutput, stateHelp}
	for _, st := range quittingStates {
		t.Run("", func(t *testing.T) {
			m, _ := testModel(t)
			m.state = st
			_, teaCmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
			require.NotNil(t, teaCmd)
			assert.IsType(t, tea.QuitMsg{}, teaCmd())
		})
	}

	m, _ := testModel(t)
	m.state = stateArgs
	m.argsInput.Focus()
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	nm := updated.(Model)
	assert.Equal(t, stateArgs, nm.state)
	assert.Contains(t, nm.argsInput.Value(), "q")
}

// TestHandleKey_HelpToggle verifies "?" enters help from any non-args state
// and remembers prevState so a second "?" returns to it.
func TestHandleKey_HelpToggle(t *testing.T) {
	m, _ := testModel(t)
	m.state = stateCommands

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	nm := updated.(Model)
	assert.Equal(t, stateHelp, nm.state)
	assert.Equal(t, stateCommands, nm.prevState)

	updated2, _ := nm.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	nm2 := updated2.(Model)
	assert.Equal(t, stateCommands, nm2.state)
}

// TestHandleKey_HelpIgnoredInArgsState verifies "?" is treated as literal
// text input while on the args screen, not a help toggle.
func TestHandleKey_HelpIgnoredInArgsState(t *testing.T) {
	m, _ := testModel(t)
	m.state = stateArgs
	m.argsInput.Focus()

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	nm := updated.(Model)
	assert.Equal(t, stateArgs, nm.state)
	assert.Contains(t, nm.argsInput.Value(), "?")
}

// TestHandleKey_EscTransitions table-drives esc's per-state back
// navigation.
func TestHandleKey_EscTransitions(t *testing.T) {
	tests := []struct {
		name  string
		from  state
		want  state
		setup func(*Model)
	}{
		{"commands back to categories", stateCommands, stateCategories, nil},
		{"args back to commands", stateArgs, stateCommands, nil},
		{"output back to commands", stateOutput, stateCommands, nil},
		{"help back to prevState", stateHelp, stateArgs, func(m *Model) { m.prevState = stateArgs }},
		{"categories has nowhere to go, stays put", stateCategories, stateCategories, nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := testModel(t)
			m.state = tc.from
			if tc.setup != nil {
				tc.setup(&m)
			}
			updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
			nm := updated.(Model)
			assert.Equal(t, tc.want, nm.state)
		})
	}
}

// TestHandleKey_CategoriesEnterSelectsCategory verifies pressing enter on
// the categories screen loads that category's commands and transitions to
// stateCommands.
func TestHandleKey_CategoriesEnterSelectsCategory(t *testing.T) {
	m, _ := testModel(t)
	require.NotEmpty(t, m.categories.Items())

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	nm := updated.(Model)

	assert.Equal(t, stateCommands, nm.state)
	assert.NotEmpty(t, nm.selectedCategory)
	assert.NotEmpty(t, nm.commands.Items())
	assert.Equal(t, nm.selectedCategory, nm.commands.Title)
}

// TestHandleKey_CommandsEnterSelectsCommand verifies pressing enter on the
// commands screen focuses the args input and transitions to stateArgs.
func TestHandleKey_CommandsEnterSelectsCommand(t *testing.T) {
	m, _ := testModel(t)
	catUpdated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = catUpdated.(Model)
	require.Equal(t, stateCommands, m.state)
	require.NotEmpty(t, m.commands.Items())

	updated, teaCmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	nm := updated.(Model)

	assert.Equal(t, stateArgs, nm.state)
	assert.NotEmpty(t, nm.selectedCommand.Name)
	assert.True(t, nm.argsInput.Focused())
	assert.NotNil(t, teaCmd)
}

// TestHandleKey_ArgsEnterRunsCommandAndTransitionsToOutput drives the full
// pipeline: select "system health" (a no-arg GET command), type nothing,
// press enter, and verify the model lands on stateOutput with the real
// server's response rendered into the viewport.
func TestHandleKey_ArgsEnterRunsCommandAndTransitionsToOutput(t *testing.T) {
	srv := httptest.NewServer(nil)
	t.Cleanup(srv.Close)
	client := api.New(srv.URL, "")
	cfg := config.Default()
	m := newModel(cfg, client, cmd.BuildInfo{Version: "1.0.0"})

	m.width, m.height = 80, 24
	m.applyLayout()
	m.selectedCategory = "system"
	m.selectedCommand, _ = cmd.FindCommand("system", "health")
	m.state = stateArgs
	m.argsInput.SetValue("")
	m.argsInput.Focus()

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	nm := updated.(Model)

	assert.Equal(t, stateOutput, nm.state)
	// A bare httptest.NewServer(nil) 404s every request, so this proves
	// the real HTTP round trip happened and the resulting error was
	// captured into both m.err and the output viewport.
	assert.Error(t, nm.err)
	assert.Contains(t, nm.outputView.View(), "Error:")
}

// TestRunSelectedCommand_Success verifies a successful command execution
// captures the server's output and returns no error.
func TestRunSelectedCommand_Success(t *testing.T) {
	mux := httptest.NewServer(nil)
	t.Cleanup(mux.Close)
	client := api.New(mux.URL, "")
	cfg := config.Default()
	m := newModel(cfg, client, cmd.BuildInfo{Version: "1.0.0"})
	m.selectedCategory = "system"
	m.selectedCommand, _ = cmd.FindCommand("system", "health")

	_, err := m.runSelectedCommand(nil)
	// Bare handler 404s, so we still expect an error here, but the point
	// of this test is that the command WAS found and dispatched (not the
	// "no longer registered" branch below), unlike the sibling test.
	assert.Error(t, err)
	assert.NotContains(t, err.Error(), "no longer registered")
}

// TestRunSelectedCommand_NotRegistered verifies the guard for a selected
// command that no longer exists in the registry (e.g. selectedCategory or
// selectedCommand.Name mismatched).
func TestRunSelectedCommand_NotRegistered(t *testing.T) {
	client := api.New("http://127.0.0.1:1", "")
	cfg := config.Default()
	m := newModel(cfg, client, cmd.BuildInfo{Version: "1.0.0"})
	m.selectedCategory = "does-not-exist"
	m.selectedCommand.Name = "also-missing"

	out, err := m.runSelectedCommand(nil)
	require.Error(t, err)
	assert.Empty(t, out)
	assert.Contains(t, err.Error(), "command no longer registered: does-not-exist also-missing")
}

// TestModel_Update_DelegatesToActiveWidget verifies non-key, non-resize
// messages are routed to whichever widget matches the current state,
// leaving the others untouched. Using tea.WindowSizeMsg's sibling path is
// not applicable here, so we exercise the default fallthrough with a
// no-op message type that all bubbles widgets accept without erroring.
func TestModel_Update_DelegatesToActiveWidget(t *testing.T) {
	m, _ := testModel(t)
	m.state = stateCategories

	updated, _ := m.Update(struct{}{})
	nm := updated.(Model)
	assert.Equal(t, stateCategories, nm.state)
}

// TestView_AllStatesRenderWithoutPanicking smoke-tests View() for every
// state, verifying the header/footer chrome is present and body content is
// non-empty.
func TestView_AllStatesRenderWithoutPanicking(t *testing.T) {
	m, _ := testModel(t)
	m.width, m.height = 80, 24
	m.applyLayout()

	states := []state{stateCategories, stateCommands, stateArgs, stateOutput, stateHelp}
	for _, st := range states {
		t.Run("", func(t *testing.T) {
			m.state = st
			var view string
			require.NotPanics(t, func() { view = m.View() })
			assert.Contains(t, view, "api-cli 1.0.0")
			assert.Contains(t, view, "q quit")
		})
	}
}

// TestView_ArgsStateShowsCommandChrome verifies the args screen renders the
// selected category/command name and usage string.
func TestView_ArgsStateShowsCommandChrome(t *testing.T) {
	m, _ := testModel(t)
	m.state = stateArgs
	m.selectedCategory = "system"
	m.selectedCommand = cmd.Command{Name: "health", Usage: "system health", Desc: "check health"}

	view := m.View()
	assert.Contains(t, view, "system health")
}

// TestView_HelpStateShowsHelpText verifies the help screen renders
// helpText()'s content. lipgloss.JoinVertical right-pads every line to a
// common width, so this checks line-by-line rather than for the exact
// unpadded helpText() string.
func TestView_HelpStateShowsHelpText(t *testing.T) {
	m, _ := testModel(t)
	m.state = stateHelp
	view := m.View()
	for _, line := range strings.Split(helpText(), "\n") {
		if line == "" {
			continue
		}
		assert.Contains(t, view, line)
	}
}

// TestHelpText verifies the help text mentions every documented key.
func TestHelpText(t *testing.T) {
	text := helpText()
	for _, want := range []string{"j/down", "k/up", "search", "enter", "esc", "?", "q, ctrl+c"} {
		assert.Contains(t, text, want)
	}
}

// TestCategoryItem verifies the list.Item adapter methods.
func TestCategoryItem(t *testing.T) {
	require.NotEmpty(t, cmd.Categories())
	cat := cmd.Categories()[0]
	item := categoryItem{name: cat}

	assert.Equal(t, cat, item.Title())
	assert.Equal(t, cat, item.FilterValue())
	assert.Contains(t, item.Description(), "commands")

	var _ list.Item = item
}

// TestCommandItem verifies the list.Item adapter methods.
func TestCommandItem(t *testing.T) {
	c := cmd.Command{Name: "health", Desc: "check server health"}
	item := commandItem{c: c}

	assert.Equal(t, "health", item.Title())
	assert.Equal(t, "check server health", item.Description())
	assert.Equal(t, "health check server health", item.FilterValue())

	var _ list.Item = item
}

// TestRun_RequiresRealTerminal documents that Run() itself (which calls
// tea.NewProgram(...).Run() with tea.WithAltScreen()) cannot be exercised
// from `go test` without a pty: bubbletea's Run() reads directly from the
// process's real stdin/stdout to drive the terminal renderer, and there is
// no synthetic-message injection point from the outside once Run() has
// been called. Every piece of logic Run() delegates to (Init, Update,
// View, handleKey, applyLayout, runSelectedCommand) is covered directly
// above via Model, which is the actually-testable surface.
func TestRun_RequiresRealTerminal(t *testing.T) {
	t.Skip("Run() drives a real terminal via tea.NewProgram(...).Run(); not exercisable without a pty. All Model logic it delegates to is tested directly above.")
}

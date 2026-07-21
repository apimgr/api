package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/apimgr/api/src/client/api"
	"github.com/apimgr/api/src/client/cmd"
	"github.com/apimgr/api/src/client/config"
	"github.com/apimgr/api/src/client/output"
)

// state is which screen of the TUI is active.
type state int

const (
	stateCategories state = iota
	stateCommands
	stateArgs
	stateOutput
	stateHelp
)

// categoryItem adapts a category name to list.Item.
type categoryItem struct{ name string }

func (i categoryItem) Title() string { return i.name }
func (i categoryItem) Description() string {
	return fmt.Sprintf("%d commands", len(cmd.CategoryCommands(i.name)))
}
func (i categoryItem) FilterValue() string { return i.name }

// commandItem adapts a cmd.Command to list.Item.
type commandItem struct{ c cmd.Command }

func (i commandItem) Title() string       { return i.c.Name }
func (i commandItem) Description() string { return i.c.Desc }
func (i commandItem) FilterValue() string { return i.c.Name + " " + i.c.Desc }

// Model is the bubbletea application model for api-cli's TUI.
type Model struct {
	state state
	theme TUITheme

	cfg    *config.CLIConfig
	client *api.Client
	build  cmd.BuildInfo

	categories list.Model
	commands   list.Model
	argsInput  textinput.Model
	outputView viewport.Model

	selectedCategory string
	selectedCommand  cmd.Command

	width, height int
	err           error
	prevState     state
}

// Run launches the TUI and returns a process exit code, per PART 32
// error-handling conventions. It is wired to cmd.TUILauncher by main.go.
func Run(cfg *config.CLIConfig, client *api.Client, build cmd.BuildInfo) int {
	m := newModel(cfg, client, build)
	p := tea.NewProgram(m, tea.WithAltScreen())
	finalModel, err := p.Run()
	if err != nil {
		fmt.Println("Error:", err)
		return cmd.ExitGeneral
	}
	if fm, ok := finalModel.(Model); ok && fm.err != nil {
		return cmd.ExitGeneral
	}
	return cmd.ExitSuccess
}

func newModel(cfg *config.CLIConfig, client *api.Client, build cmd.BuildInfo) Model {
	theme := themeByName(cfg.TUI.Theme)

	var catItems []list.Item
	for _, c := range cmd.Categories() {
		catItems = append(catItems, categoryItem{name: c})
	}

	categories := list.New(catItems, list.NewDefaultDelegate(), 0, 0)
	categories.Title = "api-cli"
	categories.Styles.Title = lipgloss.NewStyle().
		Background(theme.Primary).
		Foreground(theme.Background).
		Padding(0, 1)

	commands := list.New(nil, list.NewDefaultDelegate(), 0, 0)

	ti := textinput.New()
	ti.Placeholder = "arguments (space separated)"
	ti.CharLimit = 512

	return Model{
		state:      stateCategories,
		theme:      theme,
		cfg:        cfg,
		client:     client,
		build:      build,
		categories: categories,
		commands:   commands,
		argsInput:  ti,
		outputView: viewport.New(0, 0),
	}
}

// Init satisfies tea.Model.
func (m Model) Init() tea.Cmd {
	return nil
}

// Update satisfies tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.applyLayout()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	var teaCmd tea.Cmd
	switch m.state {
	case stateCategories:
		m.categories, teaCmd = m.categories.Update(msg)
	case stateCommands:
		m.commands, teaCmd = m.commands.Update(msg)
	case stateArgs:
		m.argsInput, teaCmd = m.argsInput.Update(msg)
	case stateOutput:
		m.outputView, teaCmd = m.outputView.Update(msg)
	}
	return m, teaCmd
}

func (m *Model) applyLayout() {
	// Reserve one line each for header and footer chrome, per the
	// PART 32 small-terminal viewport-management guidance.
	headerHeight := 1
	footerHeight := 1
	usableHeight := m.height - headerHeight - footerHeight
	if usableHeight < 3 {
		usableHeight = 3
	}
	usableWidth := m.width
	if usableWidth < 20 {
		usableWidth = 20
	}

	m.categories.SetSize(usableWidth, usableHeight)
	m.commands.SetSize(usableWidth, usableHeight)
	m.outputView.Width = usableWidth
	m.outputView.Height = usableHeight
	m.argsInput.Width = usableWidth - 2
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Global keys that apply regardless of screen and regardless of
	// whether the active widget is in filter-editing mode.
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "q":
		if m.state == stateArgs {
			break
		}
		if m.categories.FilterState() == list.Filtering || m.commands.FilterState() == list.Filtering {
			break
		}
		return m, tea.Quit
	case "?":
		if m.state == stateArgs {
			break
		}
		if m.state == stateHelp {
			m.state = m.prevState
			return m, nil
		}
		m.prevState = m.state
		m.state = stateHelp
		return m, nil
	case "esc":
		switch m.state {
		case stateCommands:
			m.state = stateCategories
			return m, nil
		case stateArgs, stateOutput:
			m.state = stateCommands
			return m, nil
		case stateHelp:
			m.state = m.prevState
			return m, nil
		}
	}

	switch m.state {
	case stateCategories:
		if msg.String() == "enter" {
			if it, ok := m.categories.SelectedItem().(categoryItem); ok {
				m.selectedCategory = it.name
				var items []list.Item
				for _, c := range cmd.CategoryCommands(it.name) {
					items = append(items, commandItem{c: c})
				}
				m.commands.SetItems(items)
				m.commands.Title = it.name
				m.state = stateCommands
			}
			return m, nil
		}
		var teaCmd tea.Cmd
		m.categories, teaCmd = m.categories.Update(msg)
		return m, teaCmd

	case stateCommands:
		if msg.String() == "enter" {
			if it, ok := m.commands.SelectedItem().(commandItem); ok {
				m.selectedCommand = it.c
				m.argsInput.SetValue("")
				m.argsInput.Focus()
				m.state = stateArgs
				return m, textinput.Blink
			}
			return m, nil
		}
		var teaCmd tea.Cmd
		m.commands, teaCmd = m.commands.Update(msg)
		return m, teaCmd

	case stateArgs:
		if msg.String() == "enter" {
			args := strings.Fields(m.argsInput.Value())
			m.argsInput.Blur()
			result, err := m.runSelectedCommand(args)
			m.err = err
			m.outputView.SetContent(result)
			m.state = stateOutput
			return m, nil
		}
		var teaCmd tea.Cmd
		m.argsInput, teaCmd = m.argsInput.Update(msg)
		return m, teaCmd

	case stateOutput:
		var teaCmd tea.Cmd
		m.outputView, teaCmd = m.outputView.Update(msg)
		return m, teaCmd
	}

	return m, nil
}

// runSelectedCommand executes the selected command's Run function and
// captures its rendered output as a string for the viewport.
func (m Model) runSelectedCommand(args []string) (string, error) {
	c, ok := cmd.FindCommand(m.selectedCategory, m.selectedCommand.Name)
	if !ok {
		return "", fmt.Errorf("command no longer registered: %s %s", m.selectedCategory, m.selectedCommand.Name)
	}

	captured, err := output.Capture(func() error {
		return c.Run(m.client, &cmd.OutputOptions{Format: "table"}, args)
	})
	if err != nil {
		return fmt.Sprintf("Error: %s", err), err
	}
	return captured, nil
}

// View satisfies tea.Model.
func (m Model) View() string {
	header := lipgloss.NewStyle().
		Foreground(m.theme.Accent).
		Bold(true).
		Render(fmt.Sprintf("api-cli %s", m.build.Version))

	footer := lipgloss.NewStyle().
		Foreground(m.theme.Muted).
		Render("q quit  ?  help  esc back  / search  enter select")

	var body string
	switch m.state {
	case stateCategories:
		body = m.categories.View()
	case stateCommands:
		body = m.commands.View()
	case stateArgs:
		body = fmt.Sprintf(
			"%s %s\n\n%s\n%s",
			m.selectedCategory, m.selectedCommand.Name,
			m.selectedCommand.Usage,
			m.argsInput.View(),
		)
	case stateOutput:
		body = m.outputView.View()
	case stateHelp:
		body = helpText()
	}

	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

func helpText() string {
	return strings.Join([]string{
		"api-cli TUI help",
		"",
		"j/down, k/up      move selection",
		"/                 search/filter list",
		"enter             select category/command, run command",
		"esc               go back",
		"?                 toggle this help",
		"q, ctrl+c         quit",
	}, "\n")
}

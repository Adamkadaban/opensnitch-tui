package placeholder

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/adamkadaban/opensnitch-tui/internal/theme"
	"github.com/adamkadaban/opensnitch-tui/internal/ui/view"
)

// Model renders temporary placeholder content for unfinished sections.
type Model struct {
	title   string
	message string
	theme   theme.Theme
	width   int
	height  int
}

// New creates a placeholder view with the provided title and description.
func New(title, message string, th theme.Theme) view.Model {
	return &Model{title: title, message: message, theme: th}
}

func (m *Model) Init() tea.Cmd { return nil }

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return m, nil
}

func (m *Model) View() string {
	return m.theme.Body.Copy().Width(m.width).Height(max(3, m.height)).Render(m.message)
}

func (m *Model) Title() string { return m.title }

func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

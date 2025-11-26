package root

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/adamkadaban/opensnitch-tui/internal/controller"
	"github.com/adamkadaban/opensnitch-tui/internal/keymap"
	"github.com/adamkadaban/opensnitch-tui/internal/state"
	"github.com/adamkadaban/opensnitch-tui/internal/theme"
	"github.com/adamkadaban/opensnitch-tui/internal/ui/prompt"
	"github.com/adamkadaban/opensnitch-tui/internal/ui/view"
	"github.com/adamkadaban/opensnitch-tui/internal/ui/views/alerts"
	"github.com/adamkadaban/opensnitch-tui/internal/ui/views/dashboard"
	"github.com/adamkadaban/opensnitch-tui/internal/ui/views/nodes"
	"github.com/adamkadaban/opensnitch-tui/internal/ui/views/rules"
	settingsview "github.com/adamkadaban/opensnitch-tui/internal/ui/views/settings"
)

// Options controls how the root model is assembled.
type Options struct {
	Theme    theme.Theme
	KeyMap   *keymap.Global
	Rules    controller.RuleManager
	Prompts  controller.PromptManager
	Settings controller.SettingsManager
}

// Model orchestrates routed Bubble Tea views and global UI chrome.
type Model struct {
	store  *state.Store
	sub    *state.Subscription
	keymap keymap.Global
	theme  theme.Theme
	prompt *prompt.Model

	views  map[state.ViewKind]view.Model
	order  []state.ViewKind
	active state.ViewKind

	width  int
	height int
}

// New builds the root Bubble Tea model.
func New(store *state.Store, opts Options) *Model {
	keyMap := keymap.DefaultGlobal()
	if opts.KeyMap != nil {
		keyMap = *opts.KeyMap
	}

	views := map[state.ViewKind]view.Model{
		state.ViewDashboard: dashboard.New(store, opts.Theme),
		state.ViewAlerts:    alerts.New(store, opts.Theme),
		state.ViewRules:     rules.New(store, opts.Theme, opts.Rules),
		state.ViewNodes:     nodes.New(store, opts.Theme),
		state.ViewSettings:  settingsview.New(store, opts.Theme, opts.Settings),
	}

	promptModel := prompt.New(store, opts.Theme, opts.Prompts)

	model := &Model{
		store:  store,
		keymap: keyMap,
		theme:  opts.Theme,
		prompt: promptModel,
		views:  views,
		order:  append([]state.ViewKind{}, state.DefaultViewOrder...),
		active: state.ViewDashboard,
	}
	if store != nil {
		model.sub = store.Subscribe()
	}
	return model
}

type storeChangeMsg struct{}

func (m *Model) Init() tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(m.views))
	for _, v := range m.views {
		cmds = append(cmds, v.Init())
	}
	if m.prompt != nil {
		cmds = append(cmds, m.prompt.Init())
	}
	cmds = append(cmds, waitForStoreChanges(m.sub))
	return tea.Batch(cmds...)
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case storeChangeMsg:
		return m, waitForStoreChanges(m.sub)
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		for _, v := range m.views {
			v.SetSize(msg.Width, max(1, msg.Height-2))
		}
		if m.prompt != nil {
			m.prompt.SetSize(msg.Width, max(1, msg.Height-2))
		}

	case tea.KeyMsg:
		if m.prompt != nil {
			if cmd, handled := m.prompt.Update(msg); handled {
				return m, cmd
			}
		}
		switch {
		case key.Matches(msg, m.keymap.Quit):
			return m, tea.Quit
		case key.Matches(msg, m.keymap.NextView):
			m.cycle(1)
		case key.Matches(msg, m.keymap.PrevView):
			m.cycle(-1)
		}

	case tea.QuitMsg:
		m.closeSubscription()
	}

	activeView := m.activeView()
	updated, cmd := activeView.Update(msg)
	if nextView, ok := updated.(view.Model); ok {
		m.views[m.active] = nextView
	}

	return m, cmd
}

func (m *Model) View() string {
	activeView := m.activeView()
	if activeView == nil {
		return ""
	}

	headline := lipgloss.JoinHorizontal(lipgloss.Top,
		m.theme.Title.Render("OpenSnitch TUI"),
		lipgloss.NewStyle().Padding(0, 1).Render(m.renderTabs()),
	)

	body := activeView.View()
	if m.prompt != nil {
		if overlay := m.prompt.View(); overlay != "" {
			body = overlay
		}
	}
	snapshot := m.store.Snapshot()
	footer := m.theme.Footer.Render(m.footerLine(snapshot))

	return lipgloss.JoinVertical(lipgloss.Left, headline, body, footer)
}

func (m *Model) activeView() view.Model {
	return m.views[m.active]
}

func (m *Model) cycle(delta int) {
	if len(m.order) == 0 {
		return
	}
	idx := indexOf(m.order, m.active)
	idx = (idx + delta) % len(m.order)
	if idx < 0 {
		idx += len(m.order)
	}
	m.active = m.order[idx]
	m.store.SetActiveView(m.active)
}

func (m *Model) closeSubscription() {
	if m.sub != nil {
		m.sub.Close()
		m.sub = nil
	}
}

func (m *Model) renderTabs() string {
	labels := make([]string, 0, len(m.order))
	for _, kind := range m.order {
		view := m.views[kind]
		if view == nil {
			continue
		}
		labels = append(labels, m.theme.RenderTab(view.Title(), kind == m.active))
	}
	return strings.Join(labels, " ")
}

func (m *Model) footerLine(snapshot state.Snapshot) string {
	nodes := len(snapshot.Nodes)
	line := fmt.Sprintf("View %s · Nodes %d · %s", titleCase(string(snapshot.ActiveView)), nodes, m.keymap.ShortHelp())
	if snapshot.LastError != "" {
		line = fmt.Sprintf("%s · %s", line, m.theme.Danger.Render(snapshot.LastError))
	}
	if !snapshot.Settings.AlertsInterrupt && len(snapshot.Prompts) > 0 && snapshot.ActiveView != state.ViewAlerts {
		indicator := m.theme.Danger.Render("● alerts pending")
		line = fmt.Sprintf("%s · %s", line, indicator)
	}
	return line
}

func indexOf(values []state.ViewKind, target state.ViewKind) int {
	for idx, value := range values {
		if value == target {
			return idx
		}
	}
	return 0
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func titleCase(value string) string {
	if value == "" {
		return value
	}
	return strings.ToUpper(value[:1]) + value[1:]
}

func waitForStoreChanges(sub *state.Subscription) tea.Cmd {
	if sub == nil {
		return nil
	}
	return func() tea.Msg {
		if _, ok := <-sub.Events(); !ok {
			return nil
		}
		return storeChangeMsg{}
	}
}

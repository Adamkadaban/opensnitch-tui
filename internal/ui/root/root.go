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
	"github.com/adamkadaban/opensnitch-tui/internal/ui/views/events"
	"github.com/adamkadaban/opensnitch-tui/internal/ui/views/firewall"
	"github.com/adamkadaban/opensnitch-tui/internal/ui/views/nodes"
	"github.com/adamkadaban/opensnitch-tui/internal/ui/views/rules"
	settingsview "github.com/adamkadaban/opensnitch-tui/internal/ui/views/settings"
	"github.com/adamkadaban/opensnitch-tui/internal/ui/views/tasks"
	"github.com/adamkadaban/opensnitch-tui/internal/util"
)

// Options controls how the root model is assembled.
type Options struct {
	Theme      theme.Theme
	KeyMap     *keymap.Global
	Rules      controller.RuleManager
	Firewall   controller.FirewallManager
	Tasks      controller.TaskManager
	NodeConfig controller.NodeConfigManager
	Prompts    controller.PromptManager
	Settings   controller.SettingsManager
}

// Model orchestrates routed Bubble Tea views and global UI chrome.
type Model struct {
	store     *state.Store
	sub       *state.Subscription
	keymap    keymap.Global
	theme     theme.Theme
	themeName string
	prompt    *prompt.Model

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
		state.ViewEvents:    events.New(store, opts.Theme),
		state.ViewRules:     rules.New(store, opts.Theme, opts.Rules),
		state.ViewFirewall:  firewall.New(store, opts.Theme, opts.Firewall),
		state.ViewTasks:     tasks.New(store, opts.Theme, opts.Tasks),
		state.ViewNodes:     nodes.New(store, opts.Theme, opts.NodeConfig),
		state.ViewSettings:  settingsview.New(store, opts.Theme, opts.Settings),
	}

	promptModel := prompt.New(store, opts.Theme, opts.Prompts)

	model := &Model{
		store:     store,
		keymap:    keyMap,
		theme:     opts.Theme,
		themeName: theme.Normalize(opts.Theme.Name),
		prompt:    promptModel,
		views:     views,
		order:     append([]state.ViewKind{}, state.DefaultViewOrder...),
		active:    state.ViewDashboard,
	}
	if store != nil {
		model.sub = store.Subscribe()
		model.applyTheme(theme.New(theme.Options{Name: store.Snapshot().Settings.ThemeName}))
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
	if m.prompt != nil {
		if cmd, handled := m.prompt.Update(msg); handled {
			return m, cmd
		}
	}

	switch msg := msg.(type) {
	case storeChangeMsg:
		m.onStoreChanged()
		cmds := []tea.Cmd{waitForStoreChanges(m.sub)}
		for kind, routedView := range m.views {
			updated, cmd := routedView.Update(msg)
			if nextView, ok := updated.(view.Model); ok {
				m.views[kind] = nextView
			}
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resize()

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keymap.Quit):
			m.closeSubscription()
			m.closeViews()
			return m, tea.Quit
		case key.Matches(msg, m.keymap.NextView):
			m.cycle(1)
		case key.Matches(msg, m.keymap.PrevView):
			m.cycle(-1)
		}

	case tea.QuitMsg:
		m.closeSubscription()
		m.closeViews()
	}

	activeView := m.activeView()
	updated, cmd := activeView.Update(msg)
	if nextView, ok := updated.(view.Model); ok {
		m.views[m.active] = nextView
	}

	cmds := []tea.Cmd{cmd}
	for kind, routedView := range m.views {
		if kind == m.active {
			continue
		}
		handler, ok := routedView.(view.MessageHandler)
		if !ok || !handler.HandlesMessage(msg) {
			continue
		}
		updated, routedCmd := routedView.Update(msg)
		if nextView, ok := updated.(view.Model); ok {
			m.views[kind] = nextView
		}
		cmds = append(cmds, routedCmd)
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) View() string {
	activeView := m.activeView()
	if activeView == nil {
		return ""
	}

	headline := m.renderHeadline()

	body := activeView.View()
	if m.prompt != nil {
		if overlay := m.prompt.View(); overlay != "" {
			body = overlay
		}
	}
	snapshot := m.store.Snapshot()
	footerTextWidth := max(1, m.width-m.theme.Footer.GetHorizontalFrameSize())
	footerLine := util.AnsiSlice(m.footerLine(snapshot), 0, footerTextWidth)
	footer := m.theme.Footer.Width(max(1, m.width)).Render(footerLine)

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

func (m *Model) closeViews() {
	for _, routedView := range m.views {
		if closer, ok := routedView.(view.Closer); ok {
			closer.Close()
		}
	}
}

func (m *Model) renderHeadline() string {
	title := m.theme.Title.Render("OpenSnitch TUI")
	gap := lipgloss.NewStyle().Padding(0, 1)
	available := m.width - lipgloss.Width(title) - gap.GetHorizontalFrameSize()
	tabs := m.renderTabs(available)
	if available <= 0 || lipgloss.Width(tabs) > available {
		return m.renderActiveTab(m.width)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, title, gap.Render(tabs))
}

func (m *Model) renderTabs(maxWidth int) string {
	for padding := 2; padding >= 0; padding-- {
		rendered := m.renderTabRow(padding)
		if lipgloss.Width(rendered) <= maxWidth {
			return rendered
		}
	}
	return m.renderActiveTab(maxWidth)
}

func (m *Model) renderTabRow(padding int) string {
	labels := make([]string, 0, len(m.order))
	for _, kind := range m.order {
		viewModel := m.views[kind]
		if viewModel == nil {
			continue
		}
		style := m.theme.TabInactive
		if kind == m.active {
			style = m.theme.TabActive
		}
		labels = append(labels, style.Padding(0, padding).Render(viewModel.Title()))
	}
	return strings.Join(labels, " ")
}

func (m *Model) renderActiveTab(maxWidth int) string {
	activeView := m.activeView()
	if activeView == nil || maxWidth <= 0 {
		return ""
	}
	padding := min(1, maxWidth/2)
	label := util.TruncateString(activeView.Title(), max(1, maxWidth-padding*2))
	return m.theme.TabActive.Padding(0, padding).Render(label)
}

func (m *Model) onStoreChanged() {
	if m.store == nil {
		return
	}
	snapshot := m.store.Snapshot()
	desired := theme.Normalize(snapshot.Settings.ThemeName)
	if desired == "" {
		desired = m.themeName
	}
	if desired == m.themeName {
		return
	}
	m.applyTheme(theme.New(theme.Options{Name: desired}))
}

func (m *Model) applyTheme(th theme.Theme) {
	if th.Name == "" {
		return
	}
	m.theme = th
	m.themeName = th.Name
	for _, v := range m.views {
		v.SetTheme(th)
	}
	if m.prompt != nil {
		m.prompt.SetTheme(th)
	}
	m.resize()
}

func (m *Model) resize() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	viewWidth := max(1, m.width)
	viewHeight := max(1, m.height-2)
	for _, v := range m.views {
		v.SetSize(viewWidth, viewHeight)
	}
	if m.prompt != nil {
		m.prompt.SetSize(m.width, max(1, m.height-2))
	}
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

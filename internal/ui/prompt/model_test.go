package prompt

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/adamkadaban/opensnitch-tui/internal/controller"
	"github.com/adamkadaban/opensnitch-tui/internal/state"
	"github.com/adamkadaban/opensnitch-tui/internal/theme"
	"github.com/adamkadaban/opensnitch-tui/internal/util"
)

type fakePromptController struct {
	mu        sync.Mutex
	decisions []controller.PromptDecision
	err       error
	paused    []string
	resumed   []string
}

func (f *fakePromptController) ResolvePrompt(decision controller.PromptDecision) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.decisions = append(f.decisions, decision)
	return f.err
}

func (f *fakePromptController) PausePrompt(promptID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paused = append(f.paused, promptID)
	return nil
}

func (f *fakePromptController) ResumePrompt(promptID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resumed = append(f.resumed, promptID)
	return nil
}

func fullPrompt(id string) state.Prompt {
	return state.Prompt{
		ID:       id,
		NodeID:   "local",
		NodeName: "local",
		Connection: state.Connection{
			Protocol:    "tcp",
			ProcessPath: "/usr/bin/curl",
			ProcessArgs: []string{"/usr/bin/curl", "https://example.com"},
			ProcessID:   1234,
			UserID:      1000,
			DstHost:     "example.com",
			DstIP:       "203.0.113.10",
			DstPort:     443,
			ProcessChecksums: map[string]string{
				string(controller.PromptTargetChecksumMD5): "d41d8cd98f00b204e9800998ecf8427e",
			},
		},
	}
}

func newPromptModel(t *testing.T, prompts ...state.Prompt) (*Model, *state.Store, *fakePromptController) {
	t.Helper()
	store := state.NewStore()
	for _, prompt := range prompts {
		store.AddPrompt(prompt)
	}
	ctrl := &fakePromptController{}
	model := New(store, theme.New(theme.Options{}), ctrl)
	model.SetSize(80, 40)
	_ = model.View()
	return model, store, ctrl
}

func key(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func specialKey(keyType tea.KeyType) tea.KeyMsg {
	return tea.KeyMsg{Type: keyType}
}

func runCmd(t *testing.T, model *Model, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatalf("expected command")
	}
	if _, handled := model.Update(cmd()); !handled {
		t.Fatalf("expected command result to be handled")
	}
}

func TestAdvancedToggleAndConditionAvailability(t *testing.T) {
	prompt := fullPrompt("p1")
	prompt.Connection.DstIP = ""
	prompt.Connection.ProcessChecksums = nil
	model, _, _ := newPromptModel(t, prompt)

	if _, handled := model.Update(key('v')); !handled {
		t.Fatalf("expected advanced toggle handled")
	}
	form := model.forms["p1"]
	if !form.advanced {
		t.Fatalf("expected advanced mode")
	}
	view := util.StripANSI(model.View())
	for _, text := range []string{"Extra matches:", "Destination IP (unavailable)", "MD5 checksum (unavailable)"} {
		if !strings.Contains(view, text) {
			t.Fatalf("expected %q in advanced view:\n%s", text, view)
		}
	}
	form.condition = 0
	model.focus = fieldConditions
	_, _ = model.Update(specialKey(tea.KeyRight))
	if form.selected[controller.PromptTargetDestinationIP] {
		t.Fatalf("unavailable destination IP was selected")
	}
}

func TestAdvancedConditionAvailability(t *testing.T) {
	conn := fullPrompt("p1").Connection
	options := extraConditionOptions(conn)
	if len(options) != 4 {
		t.Fatalf("expected four extra conditions, got %d", len(options))
	}
	for _, option := range options {
		if !option.available {
			t.Fatalf("expected %s to be available", option.value)
		}
	}
	conn.DstPort = 0
	conn.UserID = 0
	conn.ProcessChecksums = nil
	options = extraConditionOptions(conn)
	if options[1].available || options[3].available {
		t.Fatalf("port/checksum should be unavailable: %+v", options)
	}
	if !options[2].available {
		t.Fatalf("user ID 0/values must remain semantically available")
	}
}

func TestAdvancedSelectionDeduplicatesBaseTarget(t *testing.T) {
	model, _, _ := newPromptModel(t, fullPrompt("p1"))
	_, _ = model.Update(key('v'))
	form := model.forms["p1"]
	targets := targetOptionsFor(fullPrompt("p1").Connection)
	form.target = indexTarget(targets, controller.PromptTargetDestinationIP)
	form.condition = 0
	model.focus = fieldConditions

	_, _ = model.Update(specialKey(tea.KeyRight))
	if form.selected[controller.PromptTargetDestinationIP] {
		t.Fatalf("base target must not be selectable as an extra")
	}
	form.target = indexTarget(targets, controller.PromptTargetProcessPath)
	_, _ = model.Update(specialKey(tea.KeyRight))
	_, _ = model.Update(specialKey(tea.KeyRight))
	if form.selected[controller.PromptTargetDestinationIP] {
		t.Fatalf("second toggle should clear the selected condition")
	}
	_, _ = model.Update(specialKey(tea.KeyRight))
	preview := model.rulePreview(fullPrompt("p1").Connection, targets, form)
	if preview != "process.path + dest.ip" {
		t.Fatalf("unexpected deduplicated preview %q", preview)
	}
}

func TestTimedDurationSubmissionIsAsynchronous(t *testing.T) {
	model, _, ctrl := newPromptModel(t, fullPrompt("p1"))
	form := model.forms["p1"]
	model.focus = fieldDuration
	_, _ = model.Update(specialKey(tea.KeyRight))
	if durationOptions[form.duration].value != controller.PromptDuration30Seconds {
		t.Fatalf("expected 30 second duration")
	}
	cmd, handled := model.Update(specialKey(tea.KeyEnter))
	if !handled || cmd == nil {
		t.Fatalf("expected asynchronous submission command")
	}
	if len(ctrl.decisions) != 0 {
		t.Fatalf("controller called before command execution")
	}
	runCmd(t, model, cmd)
	if len(ctrl.decisions) != 1 || ctrl.decisions[0].Duration != controller.PromptDuration30Seconds {
		t.Fatalf("unexpected submitted decision: %+v", ctrl.decisions)
	}
}

func TestTimedDurationPresets(t *testing.T) {
	want := []controller.PromptDuration{
		controller.PromptDurationOnce,
		controller.PromptDuration30Seconds,
		controller.PromptDuration5Minutes,
		controller.PromptDuration15Minutes,
		controller.PromptDuration30Minutes,
		controller.PromptDuration1Hour,
		controller.PromptDuration12Hours,
		controller.PromptDurationUntilRestart,
		controller.PromptDurationAlways,
	}
	if len(durationOptions) != len(want) {
		t.Fatalf("duration option count = %d, want %d", len(durationOptions), len(want))
	}
	for i, duration := range want {
		if durationOptions[i].value != duration {
			t.Fatalf("duration %d = %s, want %s", i, durationOptions[i].value, duration)
		}
	}
}

func TestAdvancedSubmissionIncludesSelectedConditions(t *testing.T) {
	model, _, ctrl := newPromptModel(t, fullPrompt("p1"))
	_, _ = model.Update(key('v'))
	form := model.forms["p1"]
	form.selected[controller.PromptTargetDestinationIP] = true
	form.selected[controller.PromptTargetDestinationPort] = true
	form.selected[controller.PromptTargetUserID] = true
	form.selected[controller.PromptTargetChecksumMD5] = true
	model.focus = fieldAction

	cmd, _ := model.Update(key('a'))
	if cmd != nil {
		t.Fatalf("action shortcut should not submit")
	}
	cmd, _ = model.Update(specialKey(tea.KeyEnter))
	runCmd(t, model, cmd)
	if len(ctrl.decisions) != 1 {
		t.Fatalf("expected one decision")
	}
	got := ctrl.decisions[0]
	want := []controller.PromptTarget{
		controller.PromptTargetDestinationIP,
		controller.PromptTargetDestinationPort,
		controller.PromptTargetUserID,
		controller.PromptTargetChecksumMD5,
	}
	if len(got.Conditions) != len(want) {
		t.Fatalf("unexpected conditions: %+v", got.Conditions)
	}
	for i, condition := range got.Conditions {
		if condition.Target != want[i] {
			t.Fatalf("condition %d = %s, want %s", i, condition.Target, want[i])
		}
	}
}

func TestCommandSubmissionIncludesProcessPathWhenNeeded(t *testing.T) {
	prompt := fullPrompt("p1")
	prompt.Connection.ProcessArgs = []string{"curl", "https://example.com"}
	model, _, ctrl := newPromptModel(t, prompt)
	form := model.forms["p1"]
	targets := targetOptionsFor(prompt.Connection)
	form.target = indexTarget(targets, controller.PromptTargetProcessCmd)

	if preview := model.rulePreview(prompt.Connection, targets, form); preview != "process.command + process.path" {
		t.Fatalf("unexpected command preview %q", preview)
	}
	cmd, _ := model.Update(specialKey(tea.KeyEnter))
	runCmd(t, model, cmd)
	if len(ctrl.decisions) != 1 || len(ctrl.decisions[0].Conditions) != 1 ||
		ctrl.decisions[0].Conditions[0].Target != controller.PromptTargetProcessPath {
		t.Fatalf("command decision did not include process path: %+v", ctrl.decisions)
	}
}

func TestSubmissionErrorKeepsPromptAndShowsStatus(t *testing.T) {
	model, store, ctrl := newPromptModel(t, fullPrompt("p1"))
	ctrl.err = errors.New("validation failed")
	cmd, _ := model.Update(specialKey(tea.KeyEnter))
	runCmd(t, model, cmd)

	if len(store.Snapshot().Prompts) != 1 {
		t.Fatalf("validation error closed the prompt")
	}
	if !strings.Contains(util.StripANSI(model.View()), "validation failed") {
		t.Fatalf("expected validation error in prompt view")
	}
	if model.forms["p1"].submitting {
		t.Fatalf("form remained in submitting state")
	}
}

func TestQueuedPromptsKeepIndependentAdvancedState(t *testing.T) {
	model, _, _ := newPromptModel(t, fullPrompt("p1"), fullPrompt("p2"))
	_, _ = model.Update(key('v'))
	model.forms["p1"].selected[controller.PromptTargetDestinationIP] = true
	_, _ = model.Update(key(']'))
	_ = model.View()

	if model.activeID != "p2" || model.forms["p2"].advanced {
		t.Fatalf("second prompt inherited first prompt state")
	}
	_, _ = model.Update(key('v'))
	model.forms["p2"].selected[controller.PromptTargetDestinationPort] = true
	_, _ = model.Update(key('['))
	_ = model.View()
	if !model.forms["p1"].advanced || !model.forms["p1"].selected[controller.PromptTargetDestinationIP] {
		t.Fatalf("first prompt state was not preserved")
	}
	if model.forms["p1"].selected[controller.PromptTargetDestinationPort] {
		t.Fatalf("condition selection leaked between prompts")
	}
}

func TestInspectPreservesAdvancedSelections(t *testing.T) {
	model, _, ctrl := newPromptModel(t, fullPrompt("p1"))
	_, _ = model.Update(key('v'))
	model.forms["p1"].selected[controller.PromptTargetDestinationIP] = true
	if _, handled := model.Update(key('i')); !handled || !model.inspect {
		t.Fatalf("expected inspect mode")
	}
	if _, handled := model.Update(key('i')); !handled || model.inspect {
		t.Fatalf("expected return from inspect mode")
	}
	if !model.forms["p1"].advanced || !model.forms["p1"].selected[controller.PromptTargetDestinationIP] {
		t.Fatalf("inspect changed advanced state")
	}
	if len(ctrl.paused) != 1 || len(ctrl.resumed) != 1 {
		t.Fatalf("inspect did not preserve pause/resume behavior: paused=%v resumed=%v", ctrl.paused, ctrl.resumed)
	}
}

func TestAdvancedPromptBounds(t *testing.T) {
	prompt := fullPrompt("prompt-with-a-long-identifier")
	prompt.NodeName = strings.Repeat("node", 30)
	prompt.Connection.ProcessPath = "/" + strings.Repeat("very-long-path/", 20)
	prompt.Connection.ProcessArgs = []string{strings.Repeat("argument", 40)}
	for _, size := range []struct {
		width  int
		height int
	}{{120, 40}, {80, 40}} {
		t.Run(fmt.Sprintf("%dx%d", size.width, size.height), func(t *testing.T) {
			model, _, _ := newPromptModel(t, prompt)
			model.SetSize(size.width, size.height)
			_, _ = model.Update(key('v'))
			view := util.StripANSI(model.View())
			lines := strings.Split(view, "\n")
			if len(lines) > size.height {
				t.Fatalf("view height %d exceeds terminal height %d", len(lines), size.height)
			}
			for i, line := range lines {
				if util.RuneWidth(line) > size.width {
					t.Fatalf("line %d width %d exceeds terminal width %d: %q", i, util.RuneWidth(line), size.width, line)
				}
			}
		})
	}
}

func TestAdvancedConditionChecklistScrolls(t *testing.T) {
	model, _, _ := newPromptModel(t, fullPrompt("p1"))
	model.SetSize(80, 25)
	_, _ = model.Update(key('v'))
	model.focus = fieldConditions
	form := model.forms["p1"]
	for i := 0; i < 3; i++ {
		_, _ = model.Update(specialKey(tea.KeyDown))
	}
	if form.condition != 3 || form.conditionOffset == 0 {
		t.Fatalf("condition checklist did not scroll: condition=%d offset=%d", form.condition, form.conditionOffset)
	}
	if !strings.Contains(util.StripANSI(model.View()), "MD5 checksum") {
		t.Fatalf("scrolled condition was not visible")
	}
}

func TestActionShortcutsRemainAvailable(t *testing.T) {
	model, _, _ := newPromptModel(t, fullPrompt("p1"))
	form := model.forms["p1"]
	for keyRune, want := range map[rune]controller.PromptAction{
		'a': controller.PromptActionAllow,
		'd': controller.PromptActionDeny,
		'r': controller.PromptActionReject,
	} {
		_, _ = model.Update(key(keyRune))
		if got := actionOptions[form.action].value; got != want {
			t.Fatalf("%q selected %s, want %s", keyRune, got, want)
		}
	}
}

func TestAdvancedModeUsesArrowNavigationOnly(t *testing.T) {
	model, _, _ := newPromptModel(t, fullPrompt("p1"))
	_, _ = model.Update(key('v'))
	before := model.focus
	if _, handled := model.Update(key('j')); handled {
		t.Fatalf("vi navigation key must not be handled")
	}
	if model.focus != before {
		t.Fatalf("vi navigation key changed focus")
	}
}

func indexTarget(targets []targetOption, target controller.PromptTarget) int {
	for i, option := range targets {
		if option.value == target {
			return i
		}
	}
	return -1
}

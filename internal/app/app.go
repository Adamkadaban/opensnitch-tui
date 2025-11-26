package app

import (
	"context"
	"errors"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/sync/errgroup"

	"github.com/adamkadaban/opensnitch-tui/internal/config"
	"github.com/adamkadaban/opensnitch-tui/internal/daemon"
	"github.com/adamkadaban/opensnitch-tui/internal/keymap"
	"github.com/adamkadaban/opensnitch-tui/internal/settings"
	"github.com/adamkadaban/opensnitch-tui/internal/state"
	"github.com/adamkadaban/opensnitch-tui/internal/theme"
	root "github.com/adamkadaban/opensnitch-tui/internal/ui/root"
)

// Options control how the application is executed.
type Options struct {
	ConfigPath string
	Theme      string
	ListenAddr string
}

// Run loads configuration, prepares state, and starts the Bubble Tea program.
func Run(ctx context.Context, opts Options) error {
	configPath, err := config.ResolvePath(opts.ConfigPath)
	if err != nil {
		return fmt.Errorf("resolve config: %w", err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	cfg.DefaultPromptAction = config.NormalizePromptAction(cfg.DefaultPromptAction)
	cfg.DefaultPromptDuration = config.NormalizePromptDuration(cfg.DefaultPromptDuration)
	cfg.DefaultPromptTarget = config.NormalizePromptTarget(cfg.DefaultPromptTarget)

	palette := theme.New(theme.Options{Override: opts.Theme, Preferred: cfg.Theme})
	store := state.NewStore()
	store.SetNodes(configNodesToState(cfg.Nodes))
	store.SetSettings(state.Settings{
		DefaultPromptAction:   cfg.DefaultPromptAction,
		DefaultPromptDuration: cfg.DefaultPromptDuration,
		DefaultPromptTarget:   cfg.DefaultPromptTarget,
	})

	km := keymap.DefaultGlobal()
	daemonSrv := daemon.New(store, daemon.Options{
		ListenAddr:    opts.ListenAddr,
		ServerName:    "opensnitch-tui",
		ServerVersion: "dev",
	})

	settingsMgr := settings.NewManager(configPath, cfg)

	rootModel := root.New(store, root.Options{
		Theme:    palette,
		KeyMap:   &km,
		Rules:    daemonSrv,
		Prompts:  daemonSrv,
		Settings: settingsMgr,
	})

	prog := tea.NewProgram(rootModel, tea.WithAltScreen())

	runnerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	group, groupCtx := errgroup.WithContext(runnerCtx)
	group.Go(func() error {
		err := daemonSrv.Start(groupCtx)
		if err != nil && !errors.Is(err, context.Canceled) {
			prog.Quit()
		}
		return err
	})
	group.Go(func() error {
		defer cancel()
		_, err := prog.Run()
		return err
	})

	if err := group.Wait(); err != nil && !errors.Is(err, tea.ErrProgramKilled) && !errors.Is(err, context.Canceled) {
		return err
	}

	return nil
}

func configNodesToState(nodes []config.Node) []state.Node {
	result := make([]state.Node, 0, len(nodes))
	for idx, node := range nodes {
		id := node.ID
		if id == "" {
			id = fmt.Sprintf("node-%d", idx+1)
		}

		name := node.Name
		if name == "" {
			name = node.Address
		}

		result = append(result, state.Node{
			ID:      id,
			Name:    name,
			Address: node.Address,
			Status:  state.NodeStatusDisconnected,
			Message: "awaiting connection",
		})
	}
	return result
}

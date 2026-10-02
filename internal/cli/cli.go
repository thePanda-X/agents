// Package cli is the composition root. It wires concrete adapters, storage, and
// the TUI together; lower layers do not register their own dependencies.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"agents/internal/app"
	"agents/internal/harness"
	"agents/internal/storage"
	"agents/internal/tui"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/term"
)

const Version = "0.1.0"

type stringsFlag []string

func (s *stringsFlag) String() string         { return fmt.Sprint([]string(*s)) }
func (s *stringsFlag) Set(value string) error { *s = append(*s, value); return nil }

func Run(parent context.Context, args []string) error {
	flags := flag.NewFlagSet("agents", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	var cwd, harnessID, configDir, commandOverride, sessionID string
	var demoMode, resume, version, list, debug bool
	var roots, acpArgs stringsFlag
	flags.StringVar(&cwd, "cwd", ".", "workspace directory (defaults to the directory you launch agents from)")
	flags.StringVar(&harnessID, "harness", "", "registered harness (default from settings, initially opencode)")
	flags.StringVar(&configDir, "config-dir", "", "client settings/state directory")
	flags.StringVar(&commandOverride, "acp-command", "", "override the selected agent executable")
	flags.Var(&acpArgs, "acp-arg", "agent argument; repeat to replace the default argument list")
	flags.Var(&roots, "root", "additional workspace root; repeatable, capability-gated")
	flags.StringVar(&sessionID, "session", "", "load a saved session ID")
	flags.BoolVar(&resume, "resume", false, "load the last session for this workspace and harness")
	flags.BoolVar(&demoMode, "demo", false, "run the local ACP demo, without model calls or file edits")
	flags.BoolVar(&version, "version", false, "print version")
	flags.BoolVar(&list, "list-harnesses", false, "print registered harnesses")
	flags.BoolVar(&debug, "debug", false, "capture agent stderr in a private diagnostic file (may contain sensitive data)")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if version {
		fmt.Fprintln(os.Stdout, "agents", Version)
		return nil
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %v; use --cwd to select a workspace", flags.Args())
	}
	if configDir == "" {
		var err error
		configDir, err = storage.DefaultDirectory()
		if err != nil {
			return err
		}
	}
	store := &storage.Store{Directory: configDir}
	settings, snapshot, err := store.Load()
	if err != nil {
		return err
	}
	registry, err := newRegistry(settings.Agents)
	if err != nil {
		return err
	}
	if list {
		for _, descriptor := range registry.List() {
			fmt.Fprintf(os.Stdout, "%-16s %s\n", descriptor.ID, descriptor.Description)
		}
		return nil
	}
	if demoMode {
		harnessID = "demo"
	}
	if harnessID == "" {
		harnessID = settings.Harness
	}
	if harnessID == "" {
		harnessID = "opencode"
	}
	descriptor, err := registry.Lookup(harnessID)
	if err != nil {
		return err
	}
	workspace, err := canonicalDirectory(cwd)
	if err != nil {
		return err
	}
	for i, root := range roots {
		roots[i], err = canonicalDirectory(root)
		if err != nil {
			return err
		}
	}
	launch := descriptor.Launch
	if commandOverride != "" {
		launch.Command = commandOverride
	}
	if len(acpArgs) > 0 {
		launch.Args = acpArgs
	}
	var diagnostics io.Writer = io.Discard
	if debug {
		if err := os.MkdirAll(configDir, 0o700); err != nil {
			return err
		}
		file, err := os.OpenFile(filepath.Join(configDir, "agent.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		defer file.Close()
		diagnostics = file
	}
	var restore *harness.SessionInfo
	if resume {
		info := snapshot.Workspaces[storage.Key(workspace, harnessID)].Session
		if info.ID != "" {
			restore = &info
		}
	}
	if sessionID != "" {
		restore = &harness.SessionInfo{ID: sessionID, Workspace: workspace, AdditionalRoots: roots}
	}
	if !term.IsTerminal(os.Stdin.Fd()) || !term.IsTerminal(os.Stdout.Fd()) {
		return errors.New("an interactive terminal is required; use --help or --list-harnesses for non-interactive commands")
	}
	ctx, cancel := context.WithCancel(parent)
	controller := app.NewController(ctx)
	defer func() { cancel(); _ = controller.Close() }()
	model := tui.New(tui.Options{Context: ctx, Controller: controller, Registry: registry, Descriptor: descriptor, Agent: harness.Options{Workspace: workspace, AdditionalRoots: roots, Launch: launch, MCP: settings.MCP, Diagnostics: diagnostics}, Settings: settings, Snapshot: snapshot, Store: store, Restore: restore, Demo: harnessID == "demo"})
	program := tea.NewProgram(model, tea.WithContext(ctx))
	final, err := program.Run()
	if err != nil {
		return err
	}
	if result, ok := final.(*tui.Model); ok {
		settings, snapshot := result.Snapshot()
		if err := store.Save(settings, snapshot); err != nil {
			return err
		}
	}
	return nil
}

func canonicalDirectory(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("workspace %q: %w", path, err)
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workspace %q is not a directory", path)
	}
	return canonical, nil
}

package clientservices

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const MaxTerminalBytes = 1 << 20

type ExitStatus struct {
	Code   int
	Signal string
}

type TerminalSnapshot struct {
	Output    string
	Truncated bool
	Exit      *ExitStatus
}

type terminal struct {
	mu            sync.Mutex
	cmd           *exec.Cmd
	sessionID, id string
	limit         int
	output        []byte
	truncated     bool
	exit          *ExitStatus
	done          chan struct{}
	notify        chan struct{}
}

func (t *terminal) Write(p []byte) (int, error) {
	t.mu.Lock()
	t.output = append(t.output, p...)
	if len(t.output) > t.limit {
		start := len(t.output) - t.limit
		for start < len(t.output) && !utf8.RuneStart(t.output[start]) {
			start++
		}
		t.output = append([]byte(nil), t.output[start:]...)
		t.truncated = true
	}
	t.mu.Unlock()
	select {
	case t.notify <- struct{}{}:
	default:
	}
	return len(p), nil
}

func (t *terminal) snapshot() TerminalSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	return TerminalSnapshot{Output: strings.ToValidUTF8(string(t.output), "�"), Truncated: t.truncated, Exit: t.exit}
}

type Terminals struct {
	mu     sync.Mutex
	items  map[string]*terminal
	closed bool
	files  *FileSystem
	output func(sessionID, id, output string)
	wg     sync.WaitGroup
}

func NewTerminals(files *FileSystem, output func(string, string, string)) *Terminals {
	return &Terminals{items: make(map[string]*terminal), files: files, output: output}
}

func (m *Terminals) Create(sessionID, command string, args []string, env map[string]string, cwd string, limit uint64) (string, error) {
	if command == "" {
		return "", errors.New("terminal command cannot be empty")
	}
	dir, err := m.files.Directory(sessionID, cwd)
	if err != nil {
		return "", err
	}
	if limit == 0 || limit > MaxTerminalBytes {
		limit = MaxTerminalBytes
	}
	cmd := exec.Command(command, args...)
	cmd.Dir, cmd.Env = dir, MergeEnv(os.Environ(), env)
	// A descendant retaining stdout must not keep shutdown blocked forever.
	cmd.WaitDelay = time.Second
	isolateProcess(cmd)
	t := &terminal{cmd: cmd, sessionID: sessionID, id: rand.Text(), limit: int(limit), done: make(chan struct{}), notify: make(chan struct{}, 1)}
	cmd.Stdout, cmd.Stderr = t, t
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return "", errors.New("terminal manager is closed")
	}
	if len(m.items) >= 32 {
		return "", errors.New("too many live terminals (maximum 32)")
	}
	if err := cmd.Start(); err != nil {
		return "", err
	}
	m.items[t.id] = t
	m.wg.Add(2)
	go func() {
		defer m.wg.Done()
		err := cmd.Wait()
		// Clean up descendants while this process group still belongs to us.
		_ = killProcess(cmd)
		status := &ExitStatus{Code: 0}
		if err != nil {
			status.Code = cmd.ProcessState.ExitCode()
			if status.Code < 0 {
				status.Signal = "terminated"
			}
		}
		t.mu.Lock()
		t.exit = status
		t.mu.Unlock()
		close(t.done)
	}()
	go func() {
		defer m.wg.Done()
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()
		dirty := false
		for {
			select {
			case <-t.notify:
				dirty = true
			case <-ticker.C:
				if dirty {
					m.publish(t)
					dirty = false
				}
			case <-t.done:
				m.publish(t)
				return
			}
		}
	}()
	return t.id, nil
}

func (m *Terminals) publish(t *terminal) {
	if m.output != nil {
		m.output(t.sessionID, t.id, t.snapshot().Output)
	}
}

func (m *Terminals) get(sessionID, id string) (*terminal, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.items[id]
	if !ok || t.sessionID != sessionID {
		return nil, fmt.Errorf("unknown terminal %q for this session", id)
	}
	return t, nil
}

func (m *Terminals) Output(sessionID, id string) (TerminalSnapshot, error) {
	t, err := m.get(sessionID, id)
	if err != nil {
		return TerminalSnapshot{}, err
	}
	return t.snapshot(), nil
}

func (m *Terminals) Wait(ctx context.Context, sessionID, id string) (ExitStatus, error) {
	t, err := m.get(sessionID, id)
	if err != nil {
		return ExitStatus{}, err
	}
	select {
	case <-ctx.Done():
		return ExitStatus{}, ctx.Err()
	case <-t.done:
		return *t.snapshot().Exit, nil
	}
}

func (m *Terminals) Kill(sessionID, id string) error {
	t, err := m.get(sessionID, id)
	if err != nil {
		return err
	}
	select {
	case <-t.done:
		return nil
	default:
		return killProcess(t.cmd)
	}
}

func (m *Terminals) Release(sessionID, id string) error {
	t, err := m.get(sessionID, id)
	if err != nil {
		return err
	}
	select {
	case <-t.done:
	default:
		if err := killProcess(t.cmd); err != nil {
			return err
		}
	}
	<-t.done
	m.mu.Lock()
	delete(m.items, id)
	m.mu.Unlock()
	return nil
}

func (m *Terminals) Close() {
	m.mu.Lock()
	m.closed = true
	items := make([]*terminal, 0, len(m.items))
	for _, t := range m.items {
		items = append(items, t)
	}
	m.mu.Unlock()
	for _, t := range items {
		select {
		case <-t.done:
		default:
			_ = killProcess(t.cmd)
		}
	}
	m.wg.Wait()
	m.mu.Lock()
	clear(m.items)
	m.mu.Unlock()
}

// MergeEnv replaces existing entries instead of appending ambiguous duplicates.
func MergeEnv(base []string, overrides map[string]string) []string {
	result := make([]string, 0, len(base)+len(overrides))
	for _, entry := range base {
		key, _, ok := strings.Cut(entry, "=")
		if _, replace := overrides[key]; ok && replace {
			continue
		}
		result = append(result, entry)
	}
	for key, value := range overrides {
		result = append(result, key+"="+value)
	}
	return result
}

func (m *Terminals) CloseSession(sessionID string) {
	m.mu.Lock()
	var ids []string
	for id, terminal := range m.items {
		if terminal.sessionID == sessionID {
			ids = append(ids, id)
		}
	}
	m.mu.Unlock()
	for _, id := range ids {
		_ = m.Release(sessionID, id)
	}
}

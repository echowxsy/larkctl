package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// agentState holds per-chat agent state (claude session, working directory)
// and the named-workspace registry persisted to wsFile.

type chatState struct {
	Session string
	Cwd     string
}

type agentState struct {
	mu         sync.Mutex
	chats      map[string]*chatState
	defaultCwd string
	wsFile     string
	workspaces map[string]string
}

func newAgentState(defaultCwd, wsFile string) (*agentState, error) {
	st := &agentState{
		chats:      map[string]*chatState{},
		defaultCwd: defaultCwd,
		wsFile:     wsFile,
		workspaces: map[string]string{},
	}
	raw, err := os.ReadFile(wsFile)
	if err == nil {
		var stored struct {
			Workspaces map[string]string `json:"workspaces"`
		}
		if err := json.Unmarshal(raw, &stored); err != nil {
			return nil, fmt.Errorf("parse %s: %w", wsFile, err)
		}
		if stored.Workspaces != nil {
			st.workspaces = stored.Workspaces
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	return st, nil
}

// chat returns a snapshot of the chat's state with Cwd defaulted.
func (s *agentState) chat(chatID string) chatState {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.chats[chatID]
	if c == nil {
		return chatState{Cwd: s.defaultCwd}
	}
	out := *c
	if out.Cwd == "" {
		out.Cwd = s.defaultCwd
	}
	return out
}

func (s *agentState) ensureLocked(chatID string) *chatState {
	c := s.chats[chatID]
	if c == nil {
		c = &chatState{}
		s.chats[chatID] = c
	}
	return c
}

func (s *agentState) setSession(chatID, sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLocked(chatID).Session = sessionID
}

// setCwd validates dir and switches the chat's working directory, resetting
// its claude session (a session's context is bound to its directory).
func (s *agentState) setCwd(chatID, dir string) error {
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("需要绝对路径: %s", dir)
	}
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("目录不存在: %s", dir)
	}
	if !info.IsDir() {
		return fmt.Errorf("不是目录: %s", dir)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.ensureLocked(chatID)
	c.Cwd = dir
	c.Session = ""
	return nil
}

func (s *agentState) wsSave(name, path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.workspaces[name] = path
	return s.persistLocked()
}

func (s *agentState) wsRemove(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.workspaces[name]; !ok {
		return fmt.Errorf("工作区不存在: %s", name)
	}
	delete(s.workspaces, name)
	return s.persistLocked()
}

func (s *agentState) wsList() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]string, len(s.workspaces))
	for k, v := range s.workspaces {
		out[k] = v
	}
	return out
}

func (s *agentState) wsPath(name string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.workspaces[name]
	return p, ok
}

func (s *agentState) persistLocked() error {
	raw, err := json.MarshalIndent(map[string]any{"workspaces": s.workspaces}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.wsFile), 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.wsFile, raw, 0o644)
}

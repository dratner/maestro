// Package state provides persistent state storage for agents.
package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Transition represents a state transition event.
type Transition struct {
	TS   time.Time `json:"ts"`
	From string    `json:"from"`
	To   string    `json:"to"`
}

// AgentState represents the current state of an agent.
type AgentState struct {
	Version         string         `json:"version"`
	State           string         `json:"state"`
	LastTimestamp   time.Time      `json:"last_timestamp"`
	ContextSnapshot map[string]any `json:"context_snapshot"`
	Data            map[string]any `json:"data,omitempty"`

	// UI-specific fields.
	Plan        *string      `json:"plan,omitempty"`
	TaskContent *string      `json:"task_content,omitempty"`
	Transitions []Transition `json:"transitions,omitempty"`
}

// AppendTransition adds a new state transition to the agent state.
func (as *AgentState) AppendTransition(from, to string) {
	transition := Transition{
		From: from,
		To:   to,
		TS:   time.Now().UTC(),
	}
	as.Transitions = append(as.Transitions, transition)
}

// Store manages persistent state storage for agents.
type Store struct {
	baseDir string
}

// NewStore creates a new state store with the given base directory.
func NewStore(baseDir string) (*Store, error) {
	// Create base directory if it doesn't exist.
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create state directory %s: %w", baseDir, err)
	}

	return &Store{
		baseDir: baseDir,
	}, nil
}

// SaveState persists the current state for the given agent.
func (s *Store) SaveState(agentID, state string, data map[string]any) error {
	if agentID == "" {
		return fmt.Errorf("agentID cannot be empty")
	}

	if state == "" {
		return fmt.Errorf("state cannot be empty")
	}

	agentState := AgentState{
		Version:         "v1",
		State:           state,
		LastTimestamp:   time.Now().UTC(),
		ContextSnapshot: make(map[string]any),
		Data:            data,
	}

	// Add some basic context information.
	agentState.ContextSnapshot["agent_id"] = agentID
	agentState.ContextSnapshot["saved_at"] = agentState.LastTimestamp
	agentState.ContextSnapshot["state"] = state

	// Marshal to JSON.
	jsonData, err := json.MarshalIndent(agentState, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state for agent %s: %w", agentID, err)
	}

	// Write to file.
	filename := s.getStateFilename(agentID)
	if err := os.WriteFile(filename, jsonData, 0644); err != nil {
		return fmt.Errorf("failed to write state file for agent %s: %w", agentID, err)
	}

	return nil
}

// Load retrieves a value by ID into the provided destination.
func (s *Store) Load(id string, dest any) error {
	if id == "" {
		return fmt.Errorf("id cannot be empty")
	}

	filename := s.getStateFilename(id)

	// Check if file exists.
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		// Return empty state if file doesn't exist.
		return nil
	}

	// Read file.
	fileData, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("failed to read state file for id %s: %w", id, err)
	}

	// Unmarshal JSON into destination.
	if err := json.Unmarshal(fileData, dest); err != nil {
		return fmt.Errorf("failed to unmarshal state for id %s: %w", id, err)
	}

	return nil
}

// Save persists a value with the given ID.
func (s *Store) Save(id string, value any) error {
	if id == "" {
		return fmt.Errorf("id cannot be empty")
	}

	// Marshal to JSON.
	jsonData, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state for id %s: %w", id, err)
	}

	// Write to file.
	filename := s.getStateFilename(id)
	if err := os.WriteFile(filename, jsonData, 0644); err != nil {
		return fmt.Errorf("failed to write state file for id %s: %w", id, err)
	}

	return nil
}

// LoadState retrieves the persisted state for the given agent.
func (s *Store) LoadState(agentID string) (string, map[string]any, error) {
	if agentID == "" {
		return "", nil, fmt.Errorf("agentID cannot be empty")
	}

	filename := s.getStateFilename(agentID)

	// Check if file exists.
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		// Return empty state if file doesn't exist.
		return "", make(map[string]any), nil
	}

	// Read file.
	fileData, err := os.ReadFile(filename)
	if err != nil {
		return "", nil, fmt.Errorf("failed to read state file for agent %s: %w", agentID, err)
	}

	// Unmarshal JSON.
	var agentState AgentState
	if err := json.Unmarshal(fileData, &agentState); err != nil {
		return "", nil, fmt.Errorf("failed to unmarshal state for agent %s: %w", agentID, err)
	}

	// Return state and data.
	data := agentState.Data
	if data == nil {
		data = make(map[string]any)
	}

	return agentState.State, data, nil
}

// GetStateInfo returns metadata about the agent's persisted state.
func (s *Store) GetStateInfo(agentID string) (*AgentState, error) {
	if agentID == "" {
		return nil, fmt.Errorf("agentID cannot be empty")
	}

	filename := s.getStateFilename(agentID)

	// Check if file exists.
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return nil, fmt.Errorf("no state file found for agent %s", agentID)
	}

	// Read file.
	fileData, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read state file for agent %s: %w", agentID, err)
	}

	// Unmarshal JSON.
	var agentState AgentState
	if err := json.Unmarshal(fileData, &agentState); err != nil {
		return nil, fmt.Errorf("failed to unmarshal state for agent %s: %w", agentID, err)
	}

	return &agentState, nil
}

// DeleteState removes the persisted state for the given agent.
func (s *Store) DeleteState(agentID string) error {
	if agentID == "" {
		return fmt.Errorf("agentID cannot be empty")
	}

	filename := s.getStateFilename(agentID)

	// Check if file exists.
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		// File doesn't exist, nothing to delete.
		return nil
	}

	// Remove file.
	if err := os.Remove(filename); err != nil {
		return fmt.Errorf("failed to delete state file for agent %s: %w", agentID, err)
	}

	return nil
}

// ListAgents returns a list of agent IDs that have persisted state.
func (s *Store) ListAgents() ([]string, error) {
	// Read directory.
	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read state directory: %w", err)
	}

	var agentIDs []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		// Check if it matches our filename pattern: STATUS_<agentID>.json.
		if len(name) > 12 && name[:7] == "STATUS_" && name[len(name)-5:] == ".json" {
			agentID := name[7 : len(name)-5]
			agentIDs = append(agentIDs, agentID)
		}
	}

	return agentIDs, nil
}

// getStateFilename returns the filename for the given agent's state.
func (s *Store) getStateFilename(agentID string) string {
	return filepath.Join(s.baseDir, fmt.Sprintf("STATUS_%s.json", agentID))
}

// Global store instance (can be initialized later).
//
//nolint:gochecknoglobals // Global store is needed for convenience functions
var globalStore *Store

// InitGlobalStore initializes the global state store.
func InitGlobalStore(baseDir string) error {
	store, err := NewStore(baseDir)
	if err != nil {
		return err
	}
	globalStore = store
	return nil
}

// GetGlobalStore returns the global state store instance.
func GetGlobalStore() *Store {
	return globalStore
}

// SaveState is a convenience function using the global store.
func SaveState(agentID, state string, data map[string]any) error {
	if globalStore == nil {
		return fmt.Errorf("global store not initialized")
	}
	return globalStore.SaveState(agentID, state, data)
}

// LoadState is a convenience function using the global store.
func LoadState(agentID string) (string, map[string]any, error) {
	if globalStore == nil {
		return "", nil, fmt.Errorf("global store not initialized")
	}
	return globalStore.LoadState(agentID)
}

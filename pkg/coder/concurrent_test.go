package coder

import (
	"context"
	"sync"
	"testing"

	"orchestrator/pkg/config"
	"orchestrator/pkg/state"
)

func TestConcurrentCoders(t *testing.T) {
	const numCoders = 10

	// Create multiple coders concurrently.
	var wg sync.WaitGroup
	var mu sync.Mutex
	errors := make([]error, 0)

	for i := 0; i < numCoders; i++ {
		wg.Add(1)
		go func(_ int) {
			defer wg.Done()

			tempDir := t.TempDir()
			stateStore, err := state.NewStore(tempDir)
			if err != nil {
				mu.Lock()
				errors = append(errors, err)
				mu.Unlock()
				return
			}

			// Create coder - this will initialize local transitions.
			_, err = NewCoder("test-coder", stateStore, &config.ModelCfg{}, nil, tempDir, &config.Agent{}, nil, nil)
			if err != nil {
				mu.Lock()
				errors = append(errors, err)
				mu.Unlock()
				return
			}
		}(i)
	}

	wg.Wait()

	if len(errors) > 0 {
		t.Errorf("Got %d errors during concurrent coder creation: %v", len(errors), errors[0])
	}
}

func TestConcurrentTransitions(t *testing.T) {
	const numCoders = 5

	// Create coders and test concurrent transitions.
	var wg sync.WaitGroup
	var mu sync.Mutex
	errors := make([]error, 0)

	for i := 0; i < numCoders; i++ {
		wg.Add(1)
		go func(_ int) {
			defer wg.Done()

			tempDir := t.TempDir()
			stateStore, err := state.NewStore(tempDir)
			if err != nil {
				mu.Lock()
				errors = append(errors, err)
				mu.Unlock()
				return
			}

			driver, err := NewCoder("test-coder", stateStore, &config.ModelCfg{}, nil, tempDir, &config.Agent{}, nil, nil)
			if err != nil {
				mu.Lock()
				errors = append(errors, err)
				mu.Unlock()
				return
			}

			// Test a simple state transition.
			err = driver.BaseStateMachine.TransitionTo(context.Background(), StatePlanning, map[string]any{"test": "data"})
			if err != nil {
				mu.Lock()
				errors = append(errors, err)
				mu.Unlock()
				return
			}
		}(i)
	}

	wg.Wait()

	if len(errors) > 0 {
		t.Errorf("Got %d errors during concurrent transitions: %v", len(errors), errors[0])
	}
}

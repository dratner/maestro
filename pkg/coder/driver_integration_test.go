package coder

import (
	"context"
	"os"
	"testing"
	"time"

	"orchestrator/pkg/agent"
	"orchestrator/pkg/config"
	"orchestrator/pkg/state"
)

// TestCoderDriverHealthStoryIntegration tests the complete flow from PLANNING to DONE
func TestCoderDriverHealthStoryIntegration(t *testing.T) {
	// Create temp directory
	tempDir, err := os.MkdirTemp("", "coder-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create state store
	stateStore, err := state.NewStore(tempDir)
	if err != nil {
		t.Fatalf("Failed to create state store: %v", err)
	}

	// Create test config
	modelConfig := &config.ModelCfg{
		MaxContextTokens:  4096,
		MaxReplyTokens:    1024,
		CompactionBuffer:  512,
	}

	// Create driver in mock mode (no LLM client)  
	driver, err := NewCoderDriver("test-coder", stateStore, modelConfig, nil, tempDir)
	if err != nil {
		t.Fatalf("Failed to create driver: %v", err)
	}

	ctx := context.Background()

	// Initialize driver
	if err := driver.Initialize(ctx); err != nil {
		t.Fatalf("Failed to initialize driver: %v", err)
	}

	// Process /health endpoint task
	healthTask := "Create a /health endpoint that returns JSON with status:ok and timestamp"
	if err := driver.ProcessTask(ctx, healthTask); err != nil {
		t.Fatalf("Failed to process health task: %v", err)
	}

	// Verify final state is DONE
	finalState := driver.GetCurrentState()
	if finalState != agent.StateDone {
		// Get state data for debugging
		stateData := driver.GetStateData()
		t.Errorf("Expected final state to be DONE, got %s. State data: %+v", finalState, stateData)
	}

	// Verify state data contains expected values
	stateData := driver.GetStateData()
	
	// Check that planning was completed
	if _, exists := stateData["planning_completed_at"]; !exists {
		t.Error("Expected planning_completed_at to be set")
	}

	// Check that coding was completed
	if _, exists := stateData["coding_completed_at"]; !exists {
		t.Error("Expected coding_completed_at to be set")
	}

	// Check that testing was completed
	if _, exists := stateData["testing_completed_at"]; !exists {
		t.Error("Expected testing_completed_at to be set")
	}

	// Check that code review was completed
	if _, exists := stateData["code_review_completed_at"]; !exists {
		t.Error("Expected code_review_completed_at to be set")
	}

	// Verify the task content is preserved
	if taskContent, exists := stateData["task_content"]; !exists || taskContent != healthTask {
		t.Errorf("Expected task_content to be preserved, got %v", taskContent)
	}
}

// TestCoderDriverQuestionFlow tests the QUESTION state with origin tracking
func TestCoderDriverQuestionFlow(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "coder-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	stateStore, err := state.NewStore(tempDir)
	if err != nil {
		t.Fatalf("Failed to create state store: %v", err)
	}
	
	modelConfig := &config.ModelCfg{
		MaxContextTokens:  4096,
		MaxReplyTokens:    1024,
		CompactionBuffer:  512,
	}

	driver, err := NewCoderDriver("test-coder", stateStore, modelConfig, nil, tempDir)
	if err != nil {
		t.Fatalf("Failed to create driver: %v", err)
	}

	ctx := context.Background()

	if err := driver.Initialize(ctx); err != nil {
		t.Fatalf("Failed to initialize driver: %v", err)
	}

	// Process task that triggers a question
	questionTask := "I need help understanding this unclear requirement"
	if err := driver.ProcessTask(ctx, questionTask); err != nil {
		t.Fatalf("Failed to process question task: %v", err)
	}

	// Should be in QUESTION state
	currentState := driver.GetCurrentState()
	if currentState != StateQuestion.ToAgentState() {
		t.Errorf("Expected state to be QUESTION, got %s", currentState)
	}

	// Check that question data is set correctly
	stateData := driver.GetStateData()
	if origin, exists := stateData["question_origin"]; !exists || origin != "PLANNING" {
		t.Errorf("Expected question_origin to be PLANNING, got %v", origin)
	}

	// Simulate architect answer
	if err := driver.ProcessAnswer("Here's the clarification you need..."); err != nil {
		t.Fatalf("Failed to process answer: %v", err)
	}

	// Continue processing
	if err := driver.Run(ctx); err != nil {
		t.Fatalf("Failed to continue processing after answer: %v", err)
	}

	// Should have returned to PLANNING and then progressed
	finalState := driver.GetCurrentState()
	if finalState == StateQuestion.ToAgentState() {
		t.Error("Should have moved out of QUESTION state after receiving answer")
	}
}

// TestCoderDriverApprovalFlow tests the REQUEST→RESULT flow for approvals
func TestCoderDriverApprovalFlow(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "coder-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	stateStore, err := state.NewStore(tempDir)
	if err != nil {
		t.Fatalf("Failed to create state store: %v", err)
	}
	
	modelConfig := &config.ModelCfg{
		MaxContextTokens:  4096,
		MaxReplyTokens:    1024,
		CompactionBuffer:  512,
	}

	driver, err := NewCoderDriver("test-coder", stateStore, modelConfig, nil, tempDir)
	if err != nil {
		t.Fatalf("Failed to create driver: %v", err)
	}

	ctx := context.Background()

	if err := driver.Initialize(ctx); err != nil {
		t.Fatalf("Failed to initialize driver: %v", err)
	}

	// Manually set state to PLAN_REVIEW to test approval flow
	driver.SetStateData("task_content", "Create API endpoint")
	driver.SetStateData("plan", "Mock plan: Create REST API with proper error handling")
	if err := driver.TransitionTo(ctx, StatePlanReview.ToAgentState(), nil); err != nil {
		t.Fatalf("Failed to transition to PLAN_REVIEW: %v", err)
	}

	// Process the state (should create pending approval request)
	_, _, err = driver.ProcessState(ctx)
	if err != nil {
		t.Fatalf("Failed to process PLAN_REVIEW state: %v", err)
	}

	// Check that pending approval request exists
	hasPending, content, reason := driver.GetPendingApprovalRequest()
	if !hasPending {
		t.Error("Expected pending approval request")
	}
	if content == "" || reason == "" {
		t.Error("Expected approval request to have content and reason")
	}

	// Simulate architect approval
	if err := driver.ProcessApprovalResult("APPROVED", "plan"); err != nil {
		t.Fatalf("Failed to process approval result: %v", err)
	}

	// Continue processing
	if err := driver.Run(ctx); err != nil {
		t.Fatalf("Failed to continue after approval: %v", err)
	}

	// Should have moved to CODING state
	currentState := driver.GetCurrentState()
	if currentState != StateCoding.ToAgentState() {
		t.Errorf("Expected state to be CODING after plan approval, got %s", currentState)
	}
}

// TestCoderDriverFailureAndRetry tests failure scenarios and retry logic
func TestCoderDriverFailureAndRetry(t *testing.T) {
	modelConfig := &config.ModelCfg{
		MaxContextTokens:  4096,
		MaxReplyTokens:    1024,
		CompactionBuffer:  512,
	}

	testCases := []struct {
		name        string
		taskContent string
		expectFlow  []agent.State
	}{
		{
			name:        "Test failure and fix cycle",
			taskContent: "Create endpoint that should test fail initially",
			expectFlow:  []agent.State{StatePlanning.ToAgentState(), StatePlanReview.ToAgentState(), StateCoding.ToAgentState(), StateTesting.ToAgentState(), StateFixing.ToAgentState(), StateCoding.ToAgentState(), StateTesting.ToAgentState(), StateCodeReview.ToAgentState(), agent.StateDone},
		},
		{
			name:        "Normal successful flow",
			taskContent: "Create simple endpoint that works",
			expectFlow:  []agent.State{StatePlanning.ToAgentState(), StatePlanReview.ToAgentState(), StateCoding.ToAgentState(), StateTesting.ToAgentState(), StateCodeReview.ToAgentState(), agent.StateDone},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tempDir, err := os.MkdirTemp("", "coder-test")
			if err != nil {
				t.Fatalf("Failed to create temp dir: %v", err)
			}
			defer os.RemoveAll(tempDir)
			
			stateStore, err := state.NewStore(tempDir)
			if err != nil {
				t.Fatalf("Failed to create state store: %v", err)
			}
			
			driver, err := NewCoderDriver("test-coder", stateStore, modelConfig, nil, tempDir)
			if err != nil {
				t.Fatalf("Failed to create driver: %v", err)
			}

			ctx := context.Background()
			if err := driver.Initialize(ctx); err != nil {
				t.Fatalf("Failed to initialize driver: %v", err)
			}

			var stateTrace []agent.State
			stateTrace = append(stateTrace, driver.GetCurrentState())

			// Process the task
			if err := driver.ProcessTask(ctx, tc.taskContent); err != nil {
				t.Fatalf("Failed to process task: %v", err)
			}

			// Track state progression with timeout
			timeout := time.After(30 * time.Second)
			ticker := time.NewTicker(100 * time.Millisecond)
			defer ticker.Stop()

			for {
				select {
				case <-timeout:
					t.Fatalf("Test timed out, final state: %s, trace: %v", driver.GetCurrentState(), stateTrace)
				case <-ticker.C:
					currentState := driver.GetCurrentState()
					if len(stateTrace) == 0 || stateTrace[len(stateTrace)-1] != currentState {
						stateTrace = append(stateTrace, currentState)
					}
					
					if currentState == agent.StateDone || currentState == agent.StateError {
						goto testComplete
					}
				}
			}

		testComplete:
			finalState := driver.GetCurrentState()
			if finalState != agent.StateDone {
				t.Errorf("Expected final state DONE, got %s. State trace: %v", finalState, stateTrace)
			}

			t.Logf("State progression for %s: %v", tc.name, stateTrace)
		})
	}
}

// TestCoderDriverStateManagement tests the unified approval result management
func TestCoderDriverStateManagement(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "coder-state-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	stateStore, err := state.NewStore(tempDir)
	if err != nil {
		t.Fatalf("Failed to create state store: %v", err)
	}
	
	modelConfig := &config.ModelCfg{
		MaxContextTokens:  4096,
		MaxReplyTokens:    1024,
		CompactionBuffer:  512,
	}

	driver, err := NewCoderDriver("test-coder", stateStore, modelConfig, nil, tempDir)
	if err != nil {
		t.Fatalf("Failed to create driver: %v", err)
	}

	ctx := context.Background()
	if err := driver.Initialize(ctx); err != nil {
		t.Fatalf("Failed to initialize driver: %v", err)
	}

	// Test approval result processing
	err = driver.ProcessApprovalResult("APPROVED", "plan")
	if err != nil {
		t.Fatalf("Failed to process approval result: %v", err)
	}

	// Verify approval result is stored correctly
	stateData := driver.GetStateData()
	if approvalData, exists := stateData["plan_approval_result"]; exists {
		if result, ok := approvalData.(*ApprovalResult); ok {
			if result.Type != "plan" {
				t.Errorf("Expected approval type 'plan', got %s", result.Type)
			}
			if result.Status != "APPROVED" {
				t.Errorf("Expected approval status 'APPROVED', got %s", result.Status)
			}
			if result.Time.IsZero() {
				t.Error("Approval result should have a timestamp")
			}
		} else {
			t.Error("Approval result should be of type *ApprovalResult")
		}
	} else {
		t.Error("Approval result should be stored in state data")
	}

	t.Log("Approval result management working correctly")
}
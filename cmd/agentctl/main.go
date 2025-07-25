//nolint:cyclop // Package contains CLI parsing logic, complexity acceptable
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"orchestrator/pkg/agent"
	"orchestrator/pkg/bootstrap"
	"orchestrator/pkg/build"
	"orchestrator/pkg/coder"
	"orchestrator/pkg/config"
	"orchestrator/pkg/proto"
	"orchestrator/pkg/state"
)

// processWithApprovals handles the full workflow including auto-approvals in standalone mode.
func processWithApprovals(ctx context.Context, agent *coder.Coder, msg *proto.AgentMsg, isLiveMode bool) (*proto.AgentMsg, error) {
	maxIterations := 10 // Prevent infinite loops

	for i := 0; i < maxIterations; i++ {
		fmt.Fprintf(os.Stderr, "[DEBUG] Iteration %d, processing message type: %s\n", i, msg.Type)
		fmt.Fprintf(os.Stderr, "[DEBUG] Current agent state: %s\n", agent.GetCurrentState())

		// Process the story (for STORY messages) or step the state machine
		if msg.Type == proto.MsgTypeSTORY {
			taskContent, _ := msg.GetPayload("content")
			if taskContentStr, ok := taskContent.(string); ok {
				if err := agent.ProcessTask(ctx, taskContentStr); err != nil {
					return nil, fmt.Errorf("failed to process task: %w", err)
				}
			}
		}

		// Step the state machine.
		completed, err := agent.Step(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to step agent: %w", err)
		}

		fmt.Fprintf(os.Stderr, "[DEBUG] After step, new state: %s, completed: %v\n", agent.GetCurrentState(), completed)

		// Check if agent is done or completed.
		if completed || agent.GetCurrentState() == "DONE" {
			// Create a synthetic RESULT message for completion.
			result := proto.NewAgentMsg(proto.MsgTypeRESULT, agent.GetID(), "agentctl")
			result.SetPayload("status", "completed")
			return result, nil
		}

		// Handle pending approval requests in standalone mode.
		if isLiveMode {
			if hasPending, _, _, _, approvalType := agent.GetPendingApprovalRequest(); hasPending {
				fmt.Fprintf(os.Stderr, "[Auto-approving] %s request\n", approvalType)

				// Process approval directly.
				if err := agent.ProcessApprovalResult(context.Background(), "APPROVED", string(approvalType)); err != nil {
					return nil, fmt.Errorf("failed to process approval: %w", err)
				}

				// Clear the pending request.
				agent.ClearPendingApprovalRequest()

				// Continue with next iteration to process the approval.
				continue
			}
		}

		// Check for pending questions and auto-answer them.
		if isLiveMode {
			if hasPending, _, content, reason := agent.GetPendingQuestion(); hasPending {
				fmt.Fprintf(os.Stderr, "[Auto-answering] %s: %s\n", reason, content[:minInt(100, len(content))])

				// Provide a generic helpful answer.
				answer := "Please proceed with your best judgment. Focus on clean, well-documented code that follows best practices."
				if err := agent.ProcessAnswer(answer); err != nil {
					return nil, fmt.Errorf("failed to process answer: %w", err)
				}

				// Clear the pending question.
				agent.ClearPendingQuestion()

				// Continue with next iteration.
				continue
			}
		}

		// If we reach here without completion, continue the loop.
		// unless we've exceeded our iteration limit.
	}

	// If we've reached the maximum iterations, return an error.
	return nil, fmt.Errorf("exceeded maximum iterations (%d) without completion", maxIterations)
}

// Helper functions.
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// handleBootstrapDocker handles the bootstrap-docker command.
func handleBootstrapDocker() {
	// Get current directory.
	currentDir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get current directory: %v\n", err)
		os.Exit(1)
	}

	// Create bootstrap configuration.
	config := bootstrap.DefaultConfig()

	// Create artifact generator.
	generator := bootstrap.NewArtifactGenerator(currentDir, config)

	// Detect build backend.
	goBackend := &build.GoBackend{}
	nodeBackend := &build.NodeBackend{}
	pythonBackend := &build.PythonBackend{}

	var backend build.Backend
	if goBackend.Detect(currentDir) {
		backend = goBackend
	} else if nodeBackend.Detect(currentDir) {
		backend = nodeBackend
	} else if pythonBackend.Detect(currentDir) {
		backend = pythonBackend
	} else {
		// Default to a null backend.
		backend = &build.NullBackend{}
	}

	// Generate Dockerfile.
	fmt.Printf("Generating Dockerfile for %s backend...\n", backend.Name())
	if err := generator.GenerateDockerfile(backend); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to generate Dockerfile: %v\n", err)
		os.Exit(1)
	}

	// Generate .dockerignore.
	fmt.Printf("Generating .dockerignore...\n")
	if err := generator.GenerateDockerignore(backend); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to generate .dockerignore: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Successfully generated Dockerfile and .dockerignore for %s project\n", backend.Name())
}

//nolint:cyclop,maintidx // Main function with CLI parsing, complexity acceptable
func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s [bootstrap-docker|run] ...\n", os.Args[0])
		os.Exit(1)
	}

	// Handle bootstrap-docker command.
	if os.Args[1] == "bootstrap-docker" {
		handleBootstrapDocker()
		return
	}

	// Parse agent type - default to coder if not specified.
	agentType := string(agent.TypeCoder)
	if len(os.Args) >= 3 && os.Args[1] == "run" {
		if os.Args[2] == string(agent.TypeCoder) || os.Args[2] == string(agent.TypeArchitect) {
			agentType = os.Args[2]
		} else if os.Args[2] != "--input" {
			fmt.Fprintf(os.Stderr, "Usage: %s run [coder|architect] --input <file.json> [--workdir <dir>] [--cleanup]\n", os.Args[0])
			os.Exit(1)
		}
	} else if os.Args[1] != "run" {
		fmt.Fprintf(os.Stderr, "Usage: %s [bootstrap-docker|run] ...\n", os.Args[0])
		os.Exit(1)
	}

	var inputFile string
	var workDir string
	var cleanup = false

	// Determine starting index for flag parsing.
	startIdx := 2
	if len(os.Args) >= 3 && (os.Args[2] == string(agent.TypeCoder) || os.Args[2] == string(agent.TypeArchitect)) {
		startIdx = 3
	}

	// Simple flag parsing.
	for i := startIdx; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--input":
			if i+1 < len(os.Args) {
				inputFile = os.Args[i+1]
				i++
			}
		case "--workdir":
			if i+1 < len(os.Args) {
				workDir = os.Args[i+1]
				i++
			}
		case "--cleanup":
			cleanup = true
		}
	}

	if inputFile == "" {
		fmt.Fprintf(os.Stderr, "Error: --input is required\n")
		os.Exit(1)
	}

	// Mode auto-detection removed - always require API keys.

	// Set up workspace.
	if workDir == "" {
		tmpDir, err := os.MkdirTemp("", "agentctl-*")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create temp directory: %v\n", err)
			os.Exit(1)
		}
		workDir = tmpDir
		if cleanup {
			defer func() { _ = os.RemoveAll(workDir) }()
		} else {
			fmt.Fprintf(os.Stderr, "Working directory preserved at: %s\n", workDir)
		}
	}

	// Read and parse input.
	inputData, err := os.ReadFile(inputFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to read input file: %v\n", err)
		if cleanup && workDir != "" {
			_ = os.RemoveAll(workDir)
		}
		os.Exit(1) //nolint:gocritic // Explicit cleanup before exit
	}

	var inputMsg proto.AgentMsg
	if unmarshalErr := json.Unmarshal(inputData, &inputMsg); unmarshalErr != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse input JSON: %v\n", unmarshalErr)
		if cleanup && workDir != "" {
			_ = os.RemoveAll(workDir)
		}
		os.Exit(1)
	}

	// Create coder.
	stateStore, err := state.NewStore(filepath.Join(workDir, "state"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create state store: %v\n", err)
		if cleanup && workDir != "" {
			_ = os.RemoveAll(workDir)
		}
		os.Exit(1)
	}

	modelConfig := &config.ModelCfg{
		MaxContextTokens: 32000,
		MaxReplyTokens:   4096,
		CompactionBuffer: 1000,
	}

	// Create agent based on type with auto-detection.
	switch agentType {
	case string(agent.TypeCoder):
		var claudeAgent *coder.Coder

		// Auto-detect mode based on API key availability.
		apiKey := os.Getenv("ANTHROPIC_API_KEY")
		if apiKey != "" {
			fmt.Fprintf(os.Stderr, "ANTHROPIC_API_KEY detected, using live mode\n")

			// Create a minimal WorkspaceManager for agentctl (testing only)
			gitRunner := coder.NewDefaultGitRunner()
			workspaceManager := coder.NewWorkspaceManager(
				gitRunner,
				workDir,
				"", // No repo URL - agentctl runs standalone
				"main",
				".mirrors",
				"story-{STORY_ID}",
				"agentctl/{STORY_ID}",
			)

			// Create BuildService for MCP tools.
			buildService := build.NewBuildService()

			claudeAgent, err = coder.NewCoderWithClaude("agentctl-coder", "standalone-coder", workDir, stateStore, modelConfig, apiKey, workspaceManager, buildService)
		} else {
			fmt.Fprintf(os.Stderr, "No ANTHROPIC_API_KEY found, would use mock mode (but mocks removed)\n")
			fmt.Fprintf(os.Stderr, "Please set ANTHROPIC_API_KEY environment variable\n")
			if cleanup && workDir != "" {
				_ = os.RemoveAll(workDir)
			}
			os.Exit(1)
		}

		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create coder: %v\n", err)
			if cleanup && workDir != "" {
				_ = os.RemoveAll(workDir)
			}
			os.Exit(1)
		}

		// Process message with approval handling for standalone mode.
		ctx := context.Background()
		result, err := processWithApprovals(ctx, claudeAgent, &inputMsg, true) // Always live mode now
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to process message: %v\n", err)
			if cleanup && workDir != "" {
				_ = os.RemoveAll(workDir)
			}
			os.Exit(1)
		}

		// Output result.
		jsonData, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to marshal result: %v\n", err)
			if cleanup && workDir != "" {
				_ = os.RemoveAll(workDir)
			}
			os.Exit(1)
		}
		fmt.Println(string(jsonData))

	case string(agent.TypeArchitect):
		// TODO: Implement architect processing workflow - requires dispatcher and full setup
		fmt.Fprintf(os.Stderr, "Architect standalone mode not yet implemented\n")
		if cleanup && workDir != "" {
			_ = os.RemoveAll(workDir)
		}
		os.Exit(1)

	default:
		fmt.Fprintf(os.Stderr, "Invalid agent type '%s', must be 'coder' or 'architect'\n", agentType)
		if cleanup && workDir != "" {
			_ = os.RemoveAll(workDir)
		}
		os.Exit(1)
	}
}

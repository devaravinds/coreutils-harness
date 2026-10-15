package main

import (
	"context"
	"strings"
	"testing"
)

func TestRestrictedCommandsAreNotExposed(t *testing.T) {
	for _, tool := range coreutilsToolDefinitions() {
		command, ok := commandForTool(tool.Name)
		if !ok {
			t.Fatalf("tool %q does not map to a command", tool.Name)
		}
		if isRestrictedCommand(command) {
			t.Errorf("restricted command %q was exposed as a tool", command)
		}
	}
}

func TestRestrictedCommandReturnsExplicitResult(t *testing.T) {
	for command := range restrictedCommands {
		command := command
		t.Run(command, func(t *testing.T) {
			result, err := executeCoreutilsTool(context.Background(), coreutilsToolName(command), "not valid JSON")
			if err != nil {
				t.Fatalf("executeCoreutilsTool() returned unexpected error: %v", err)
			}
			if !strings.Contains(result, "restricted command") {
				t.Errorf("result %q does not identify a restricted command", result)
			}
		})
	}
}

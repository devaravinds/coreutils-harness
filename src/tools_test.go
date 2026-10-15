package main

import (
	"bufio"
	"context"
	"io"
	"strings"
	"testing"
)

func TestReadUserMessageReadsTrimmedLines(t *testing.T) {
	input := bufio.NewReader(strings.NewReader("  follow-up context  \n"))

	message, err := readUserMessage(input)
	if err != nil {
		t.Fatalf("readUserMessage() returned unexpected error: %v", err)
	}
	if message != "follow-up context" {
		t.Errorf("readUserMessage() = %q, want %q", message, "follow-up context")
	}
}

func TestReadUserMessageReturnsFinalUnterminatedLine(t *testing.T) {
	input := bufio.NewReader(strings.NewReader("end"))

	message, err := readUserMessage(input)
	if err != nil {
		t.Fatalf("readUserMessage() returned unexpected error: %v", err)
	}
	if message != "end" {
		t.Errorf("readUserMessage() = %q, want %q", message, "end")
	}
	if _, err := readUserMessage(input); err != io.EOF {
		t.Errorf("second read error = %v, want %v", err, io.EOF)
	}
}

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

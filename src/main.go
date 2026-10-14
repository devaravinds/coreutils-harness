package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/joho/godotenv"
)

const (
	defaultModel         = "gpt-6-luna"
	defaultMaxToolRounds = 32
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("load .env: %w", err)
	}

	task, err := userTask()
	if err != nil {
		return err
	}
	systemPrompt, err := loadSystemPrompt()
	if err != nil {
		return fmt.Errorf("load system prompt: %w", err)
	}

	model := strings.TrimSpace(os.Getenv("OPENAI_MODEL"))
	if model == "" {
		model = strings.TrimSpace(os.Getenv("DEFAULT_MODEL"))
	}
	if model == "" {
		model = defaultModel
	}

	maxToolRounds := defaultMaxToolRounds
	if value := strings.TrimSpace(os.Getenv("MAX_TOOL_ROUNDS")); value != "" {
		configuredRounds, err := strconv.Atoi(value)
		if err != nil || configuredRounds <= 0 {
			return fmt.Errorf("MAX_TOOL_ROUNDS must be a positive integer, got %q", value)
		}
		maxToolRounds = configuredRounds
	}
	client := NewClient()
	log.Printf("Ready to send your task to %s (with %d available tools)", model, len(coreutilsToolDefinitions()))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	request := ResponsesRequest{
		Model:           model,
		Input:           task,
		Instructions:    systemPrompt,
		Tools:           coreutilsToolDefinitions(),
		MaxOutputTokens: 2048,
	}

	for round := 0; round < maxToolRounds; round++ {
		log.Printf("Sending request to LLM (round %d)", round+1)
		response, err := client.CreateResponse(ctx, request)
		if err != nil {
			return err
		}
		log.Printf("Received LLM response (round %d)", round+1)

		calls := make([]ResponseOutputItem, 0)
		for _, item := range response.Output {
			if item.Type == "function_call" {
				calls = append(calls, item)
			}
		}

		// A response with no function calls is the model's signal that it is done.
		if len(calls) == 0 {
			log.Printf("LLM finished; displaying final response")
			fmt.Println(response.OutputText())
			return nil
		}
		log.Printf("LLM requested %d tool call(s)", len(calls))
		if strings.TrimSpace(response.ID) == "" {
			return errors.New("Responses API returned tool calls without a response ID")
		}

		outputs := make([]FunctionCallOutput, 0, len(calls))
		for _, call := range calls {
			result, callErr := executeCoreutilsTool(ctx, call.Name, call.Arguments)
			if callErr != nil {
				log.Printf("Tool %s failed: %v", call.Name, callErr)
				result = "tool error: " + callErr.Error()
			}
			log.Printf("Collected %d bytes of output from %s", len(result), call.Name)
			outputs = append(outputs, FunctionCallOutput{
				Type:   "function_call_output",
				CallID: call.CallID,
				Output: result,
			})
		}

		log.Printf("Sending tool results back to LLM")
		request = ResponsesRequest{
			Model:              model,
			Input:              outputs,
			PreviousResponseID: response.ID,
			Instructions:       systemPrompt,
			Tools:              coreutilsToolDefinitions(),
			MaxOutputTokens:    2048,
		}
	}

	return fmt.Errorf("stopped after %d tool rounds without a final response", maxToolRounds)
}

func userTask() (string, error) {
	if len(os.Args) > 1 {
		task := strings.TrimSpace(strings.Join(os.Args[1:], " "))
		if task != "" {
			return task, nil
		}
	}

	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", fmt.Errorf("read task from stdin: %w", err)
	}
	task := strings.TrimSpace(string(input))
	if task == "" {
		return "", errors.New("provide a task as command-line arguments or through stdin")
	}
	return task, nil
}

func loadSystemPrompt() (string, error) {
	candidates := []string{"system_prompt.txt"}
	if executable, err := os.Executable(); err == nil {
		executableDir := filepath.Dir(executable)
		candidates = append(candidates,
			filepath.Join(executableDir, "system_prompt.txt"),
			filepath.Join(executableDir, "..", "system_prompt.txt"),
		)
	}

	for _, path := range candidates {
		prompt, err := os.ReadFile(path)
		if err == nil {
			return string(prompt), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("read %s: %w", path, err)
		}
	}
	return "", errors.New("system_prompt.txt not found in the working directory or executable directory")
}

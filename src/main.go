package main

import (
	"bufio"
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

	input := bufio.NewReader(os.Stdin)
	task, err := userTask(input)
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

	for {
		finishedTurn := false
		for round := 0; round < maxToolRounds; round++ {
			log.Printf("Sending request to LLM (tool round %d)", round+1)
			response, err := client.CreateResponse(ctx, request)
			if err != nil {
				return err
			}
			log.Printf("Received LLM response (tool round %d)", round+1)

			calls := make([]ResponseOutputItem, 0)
			for _, item := range response.Output {
				if item.Type == "function_call" {
					calls = append(calls, item)
				}
			}

			// A response with no function calls is the model's signal that it is done for now.
			if len(calls) == 0 {
				log.Printf("LLM finished this turn; displaying response")
				fmt.Println(response.OutputText())
				fmt.Println("You can provide more context or ask another question, or type 'end' to end the conversation.")

				if strings.TrimSpace(response.ID) == "" {
					return errors.New("Responses API returned a response without an ID; cannot continue the conversation")
				}
				var message string
				for {
					fmt.Fprint(os.Stderr, "You: ")
					message, err = readUserMessage(input)
					if errors.Is(err, io.EOF) {
						return nil
					}
					if err != nil {
						return fmt.Errorf("read follow-up message: %w", err)
					}
					message = strings.TrimSpace(message)
					if message != "" {
						break
					}
				}
				if strings.EqualFold(message, "end") {
					log.Printf("Conversation ended by user")
					return nil
				}
				request = ResponsesRequest{
					Model:              model,
					Input:              message,
					PreviousResponseID: response.ID,
					Instructions:       systemPrompt,
					Tools:              coreutilsToolDefinitions(),
					MaxOutputTokens:    2048,
				}
				finishedTurn = true
				break
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
		if !finishedTurn {
			return fmt.Errorf("stopped after %d tool rounds without a final response", maxToolRounds)
		}
	}
}

func userTask(input *bufio.Reader) (string, error) {
	if len(os.Args) > 1 {
		task := strings.TrimSpace(strings.Join(os.Args[1:], " "))
		if task != "" {
			return task, nil
		}
	}

	task, err := readUserMessage(input)
	if errors.Is(err, io.EOF) {
		return "", errors.New("provide a task as command-line arguments or through stdin")
	}
	if err != nil {
		return "", fmt.Errorf("read task from stdin: %w", err)
	}
	if task == "" {
		return "", errors.New("provide a non-empty task as command-line arguments or through stdin")
	}
	return task, nil
}

func readUserMessage(input *bufio.Reader) (string, error) {
	message, err := input.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	if errors.Is(err, io.EOF) && message == "" {
		return "", io.EOF
	}
	return strings.TrimSpace(message), nil
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

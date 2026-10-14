package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	toolPrefix    = "coreutils_"
	maxToolOutput = 1 << 20
	toolTimeout   = 30 * time.Second
)

// coreutilsCommands is the GNU coreutils command set. Aliases and commands
// commonly provided by the package are listed as separate tools.
var coreutilsCommands = []string{
	"[", "arch", "b2sum", "base32", "base64", "basename", "basenc", "cat",
	"chcon", "chgrp", "chmod", "chown", "chroot", "cksum", "comm", "cp",
	"csplit", "cut", "date", "dd", "df", "dir", "dircolors", "dirname", "du",
	"echo", "env", "expand", "expr", "factor", "false", "fmt", "fold", "groups",
	"head", "hostid", "id", "install", "join", "link", "ln", "logname", "ls",
	"md5sum", "mkdir", "mkfifo", "mknod", "mktemp", "mv", "nice", "nl", "nohup",
	"nproc", "numfmt", "od", "paste", "pathchk", "pinky", "pr", "printenv",
	"printf", "ptx", "pwd", "readlink", "realpath", "rm", "rmdir", "runcon", "seq",
	"sha1sum", "sha224sum", "sha256sum", "sha384sum", "sha512sum", "shred", "shuf",
	"sleep", "sort", "split", "stat", "stdbuf", "stty", "sum", "sync", "tac",
	"tail", "tee", "test", "timeout", "touch", "tr", "true", "truncate", "tsort",
	"tty", "uname", "unexpand", "uniq", "unlink", "uptime", "users", "vdir", "wc",
	"who", "whoami", "yes",
}

// coreutilsToolName gives each command a unique Responses API function name.
func coreutilsToolName(command string) string {
	if command == "[" {
		return toolPrefix + "bracket"
	}
	return toolPrefix + strings.ReplaceAll(command, "-", "_")
}

// coreutilsToolDefinitions returns one function tool for each coreutils command.
func coreutilsToolDefinitions() []Tool {
	tools := make([]Tool, 0, len(coreutilsCommands))
	for _, command := range coreutilsCommands {
		tools = append(tools, Tool{
			Type:        "function",
			Name:        coreutilsToolName(command),
			Description: fmt.Sprintf("Run the coreutils `%s` command. Supply argv as separate strings; do not include the command name itself. stdin is optional input text passed to the command.", command),
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"args": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Command-line arguments, each as a separate string.",
					},
					"stdin": map[string]any{
						"type":        "string",
						"description": "Text to provide on standard input; use an empty string when not needed.",
					},
				},
				"required":             []string{"args", "stdin"},
				"additionalProperties": false,
			},
			Strict: true,
		})
	}
	return tools
}

type coreutilsArguments struct {
	Args  []string `json:"args"`
	Stdin string   `json:"stdin"`
}

// executeCoreutilsTool runs a single allowlisted coreutils command without a shell.
func executeCoreutilsTool(ctx context.Context, toolName, rawArguments string) (string, error) {
	command, ok := commandForTool(toolName)
	if !ok {
		return "", fmt.Errorf("unknown tool %q", toolName)
	}

	var arguments coreutilsArguments
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("decode arguments for %s: %w", toolName, err)
	}
	if arguments.Args == nil {
		arguments.Args = []string{}
	}
	quotedArgs := make([]string, len(arguments.Args))
	for index, argument := range arguments.Args {
		quotedArgs[index] = strconv.Quote(argument)
	}
	log.Printf("Using tool: %s %s (stdin: %d bytes)", command, strings.Join(quotedArgs, " "), len(arguments.Stdin))

	path, err := exec.LookPath(command)
	if err != nil {
		return "", fmt.Errorf("coreutils command %q is unavailable: %w", command, err)
	}

	commandContext, cancel := context.WithTimeout(ctx, toolTimeout)
	defer cancel()
	cmd := exec.CommandContext(commandContext, path, arguments.Args...)
	cmd.Stdin = strings.NewReader(arguments.Stdin)
	output := &limitedBuffer{limit: maxToolOutput}
	cmd.Stdout = output
	cmd.Stderr = output
	err = cmd.Run()

	result := output.String()
	if output.truncated {
		result += "\n[output truncated at 1 MiB]"
	}
	if commandContext.Err() == context.DeadlineExceeded {
		log.Printf("Tool %s timed out after %s", command, toolTimeout)
		return fmt.Sprintf("%s\n[command timed out after %s]", result, toolTimeout), nil
	}
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			log.Printf("Tool %s finished with exit code %d (%d output bytes)", command, exitError.ExitCode(), len(result))
			return fmt.Sprintf("%s\n[exit code: %d]", result, exitError.ExitCode()), nil
		}
		log.Printf("Tool %s could not complete: %v", command, err)
		return result, fmt.Errorf("run coreutils command %q: %w", command, err)
	}
	log.Printf("Tool %s finished successfully (%d output bytes)", command, len(result))
	return result, nil
}

func commandForTool(toolName string) (string, bool) {
	for _, command := range coreutilsCommands {
		if coreutilsToolName(command) == toolName {
			return command, true
		}
	}
	return "", false
}

type limitedBuffer struct {
	bytes.Buffer
	mutex     sync.Mutex
	limit     int
	truncated bool
}

func (buffer *limitedBuffer) Write(data []byte) (int, error) {
	buffer.mutex.Lock()
	defer buffer.mutex.Unlock()

	originalLength := len(data)
	remaining := buffer.limit - buffer.Len()
	if remaining > 0 {
		if len(data) > remaining {
			data = data[:remaining]
		}
		_, _ = buffer.Buffer.Write(data)
	}
	if len(data) < originalLength {
		buffer.truncated = true
	}
	return originalLength, nil
}

var _ io.Writer = (*limitedBuffer)(nil)

# coreutils-harness

A small Go command-line agent that sends a task to the OpenAI Responses API and lets the model use GNU coreutils commands to complete it. The program runs only commands from a fixed allowlist and invokes each command directly (without a shell).

## Requirements

- Go 1.20 or later
- GNU coreutils commands available on `PATH`
- An OpenAI API key with access to the configured model and the Responses API

## Configuration

The application loads a `.env` file from the current working directory when present. Environment variables already set in the process are also used. Configure:

| Variable | Required | Default | Purpose |
| --- | --- | --- | --- |
| `OPENAI_API_KEY` | Yes | — | API key used for authorization. |
| `OPENAI_MODEL` | No | `gpt-6-luna` | Model requested from the Responses API. |
| `OPENAI_BASE_URL` | No | `https://api.openai.com/v1` | API base URL; useful for compatible endpoints. |

For example, create a local `.env` file with your own key (do not commit it):

```dotenv
OPENAI_API_KEY=your-api-key-here
# Optional overrides:
# OPENAI_MODEL=your-model
# OPENAI_BASE_URL=https://api.openai.com/v1
```

The repository ignores `.env` files. Keep credentials private and out of source control.

## Build and run

From the repository root:

```sh
go build -o build/coreutils-harness ./src
./build/coreutils-harness "Summarize the files in the current directory"
```

Alternatively, run without creating a binary:

```sh
go run ./src "Count the lines in the Go source files"
```

A task may also be supplied on standard input when no task arguments are given:

```sh
printf '%s\n' 'Show disk usage for the current directory' | ./coreutils-harness
```

The task and any resulting command output are sent to the configured model as part of the Responses API conversation. The program prints the model's final text to standard output; progress and diagnostic messages are logged separately.

## Available commands and execution limits

The model can request the non-restricted GNU coreutils commands listed in `src/tools.go` (including file inspection, text processing, and checksums). The `[` command is exposed as `coreutils_bracket`. Commands that can modify or destroy files, alter host state, or launch executables outside the tool allowlist are restricted: they are not exposed as tools, and any direct request for one returns a `restricted command` result without running it. The exact installed executable for allowed commands is resolved through `PATH`; a command that is unavailable produces a tool error.

Each tool invocation has a 30-second timeout, and combined standard output and standard error are capped at 1 MiB. The agent allows up to 32 tool rounds and requests at most 2,048 output tokens per model response. Tool arguments are passed as separate process arguments, never through a shell.

**Safety note:** The restriction list is a command-level safeguard, not a sandbox. Allowed commands still run with the same access rights as the current process. Review tasks carefully and run the program with access only to files and systems you are comfortable allowing it to affect.

## Project layout

- `src/main.go` — task input, configuration loading, and agent/tool-call loop.
- `src/client.go` — OpenAI Responses API HTTP client and request/response types.
- `src/tools.go` — coreutils allowlist, tool definitions, and command execution.
- `system_prompt.txt` — embedded instructions sent with each model request.
- `go.mod`, `go.sum` — Go module and dependency metadata.

// Deku is a terminal-first coding agent that uses OpenAI-compatible models
// with built-in filesystem, search, Edit, command, and Git tools.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/hsrvms/deku/agent"
	"github.com/hsrvms/deku/config"
	"github.com/hsrvms/deku/provider"
	"github.com/hsrvms/deku/session"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, input io.Reader, output, errorOutput io.Writer) int {
	flags := flag.NewFlagSet("deku", flag.ContinueOnError)
	flags.SetOutput(errorOutput)
	resumeID := flags.String("resume", "", "resume an existing Session by ID")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		if err := writeError(errorOutput, "deku: unexpected argument %q\n", flags.Arg(0)); err != nil {
			return 1
		}
		return 2
	}

	cfg, err := config.Load()
	if err != nil {
		if writeErr := writeError(errorOutput, "deku: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}
	store, err := session.DefaultStore()
	if err != nil {
		if writeErr := writeError(errorOutput, "deku: initialize sessions: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}

	conversation, err := loadConversation(store, *resumeID)
	if err != nil {
		if writeErr := writeError(errorOutput, "deku: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}
	if err := writeError(errorOutput, "deku: session %s\n", conversation.ID); err != nil {
		return 1
	}

	model := provider.NewOpenAICompatible(cfg.Provider.Endpoint, cfg.Provider.APIKey)
	runner := agent.New(model, cfg.Provider.Model, conversation, output)
	return runConversation(runner, input, output, errorOutput)
}

func loadConversation(store *session.Store, resumeID string) (*session.Session, error) {
	if resumeID != "" {
		conversation, err := store.Resume(resumeID)
		if err != nil {
			return nil, fmt.Errorf("resume session: %w", err)
		}
		return conversation, nil
	}
	conversation, err := store.Create()
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	return conversation, nil
}

func runConversation(runner agent.Runner, input io.Reader, output, errorOutput io.Writer) int {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)
	for {
		if _, err := io.WriteString(output, "deku> "); err != nil {
			if writeErr := writeError(errorOutput, "deku: display prompt: %v\n", err); writeErr != nil {
				return 1
			}
			return 1
		}
		if !scanner.Scan() {
			break
		}
		request := strings.TrimSpace(scanner.Text())
		if request == "" {
			continue
		}

		if _, err := runner.Turn(context.Background(), request); err != nil {
			if writeErr := writeError(errorOutput, "deku: %v\n", err); writeErr != nil {
				return 1
			}
			continue
		}
		if _, err := io.WriteString(output, "\n"); err != nil {
			if writeErr := writeError(errorOutput, "deku: display response separator: %v\n", err); writeErr != nil {
				return 1
			}
			return 1
		}
	}
	if err := scanner.Err(); err != nil {
		if writeErr := writeError(errorOutput, "deku: read input: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}
	return 0
}

func writeError(output io.Writer, format string, args ...any) error {
	_, err := fmt.Fprintf(output, format, args...)
	return err
}

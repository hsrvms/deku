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
		fmt.Fprintf(errorOutput, "deku: unexpected argument %q\n", flags.Arg(0))
		return 2
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(errorOutput, "deku: %v\n", err)
		return 1
	}
	store, err := session.DefaultStore()
	if err != nil {
		fmt.Fprintf(errorOutput, "deku: initialize sessions: %v\n", err)
		return 1
	}

	conversation, err := loadConversation(store, *resumeID)
	if err != nil {
		fmt.Fprintf(errorOutput, "deku: %v\n", err)
		return 1
	}
	fmt.Fprintf(errorOutput, "deku: session %s\n", conversation.ID)

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
			fmt.Fprintf(errorOutput, "deku: display prompt: %v\n", err)
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
			fmt.Fprintf(errorOutput, "deku: %v\n", err)
			continue
		}
		if _, err := io.WriteString(output, "\n"); err != nil {
			fmt.Fprintf(errorOutput, "deku: display response separator: %v\n", err)
			return 1
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(errorOutput, "deku: read input: %v\n", err)
		return 1
	}
	return 0
}

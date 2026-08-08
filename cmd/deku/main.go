// Deku is a terminal-first coding agent that uses OpenAI-compatible models
// with built-in filesystem, search, Edit, command, and Git tools.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/hsrvms/deku/agent"
	"github.com/hsrvms/deku/approval"
	"github.com/hsrvms/deku/config"
	"github.com/hsrvms/deku/lineio"
	"github.com/hsrvms/deku/provider"
	"github.com/hsrvms/deku/repository"
	"github.com/hsrvms/deku/session"
	"github.com/hsrvms/deku/tool"
	"github.com/hsrvms/deku/version"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, input io.Reader, output, errorOutput io.Writer) int {
	flags := flag.NewFlagSet("deku", flag.ContinueOnError)
	flags.SetOutput(errorOutput)
	resumeID := flags.String("resume", "", "resume an existing Session by ID")
	showVersion := flags.Bool("version", false, "print the Deku version")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		if err := writeError(errorOutput, "deku: unexpected argument %q\n", flags.Arg(0)); err != nil {
			return 1
		}
		return 2
	}
	if *showVersion {
		if _, err := fmt.Fprintln(output, version.Current()); err != nil {
			return 1
		}
		return 0
	}

	projectRoot, err := repository.Root(".")
	if err != nil {
		if writeErr := writeError(errorOutput, "deku: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}
	cfg, err := config.Load(projectRoot)
	if err != nil {
		if writeErr := writeError(errorOutput, "deku: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}
	cfg, err = resolveProjectTrust(cfg, projectRoot, input, output, lineio.IsTerminal(input))
	if err != nil {
		if writeErr := writeError(errorOutput, "deku: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}
	if cfg.Project.Loaded {
		if err := writeError(errorOutput, "deku: project config loaded from %s/.deku\n", cfg.Project.Root); err != nil {
			return 1
		}
	} else if cfg.Project.Present {
		if err := writeError(errorOutput, "deku: project config found at %s/.deku but this project is not trusted; add %s to ~/.deku/trusted_projects.json to load it\n", cfg.Project.Root, cfg.Project.Root); err != nil {
			return 1
		}
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

	providers, err := provider.NewRegistry(cfg.Providers, cfg.Auth)
	if err != nil {
		if writeErr := writeError(errorOutput, "deku: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}
	selection := cfg.Selection
	if override, ok := conversation.LatestSelection(); ok {
		selection = override
	}
	if selection.IsZero() {
		if writeErr := writeError(errorOutput, "deku: no Provider or Model is selected: set defaultProvider and defaultModel in settings.json\n"); writeErr != nil {
			return 1
		}
		return 1
	}
	policy, err := approval.NewPolicyFromStrings(cfg.Approval.Tools, cfg.Approval.Defaults)
	if err != nil {
		if writeErr := writeError(errorOutput, "deku: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}
	registry, err := tool.NewRegistry(".")
	if err != nil {
		if writeErr := writeError(errorOutput, "deku: initialize tools: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}
	commitMode, err := repository.ParseMode(cfg.AgentCommits.Mode)
	if err != nil {
		if writeErr := writeError(errorOutput, "deku: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}
	var repo *repository.Repo
	if commitMode != repository.ModeOff {
		repo, err = repository.New(".")
		if err != nil {
			if writeErr := writeError(errorOutput, "deku: %v\n", err); writeErr != nil {
				return 1
			}
			return 1
		}
	}
	runner, err := agent.NewWithSelection(providers, selection, conversation, output, input, registry, policy, cfg.RepoMap.Exclude, repo, commitMode, cfg.AgentCommits.Validation)
	if err != nil {
		if writeErr := writeError(errorOutput, "deku: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}
	return runConversation(runner, providers, input, output, errorOutput)
}

// resolveProjectTrust handles the Project Trust decision for a repository that
// carries Project Config. When the user can answer (interactive terminal), it
// asks whether to trust the project; a yes answer records the decision through
// config.GrantTrust and reloads configuration so the Project Config applies. A
// no answer, or a non-interactive run, leaves the project untrusted and the
// configuration untouched — an untrusted repository is never trusted by
// default. The returned Config is the reloaded one when Trust was granted.
func resolveProjectTrust(cfg *config.Config, projectRoot string, input io.Reader, output io.Writer, interactive bool) (*config.Config, error) {
	if !cfg.Project.Present || cfg.Project.Trusted {
		return cfg, nil
	}
	if !interactive {
		return cfg, nil
	}
	granted, err := promptTrust(input, output, cfg.Project.Root)
	if err != nil {
		return nil, err
	}
	if !granted {
		return cfg, nil
	}
	if err := config.GrantTrust(cfg.Project.Root); err != nil {
		return nil, fmt.Errorf("grant project trust: %w", err)
	}
	return config.Load(projectRoot)
}

// promptTrust asks the user whether to grant Trust to the project at root,
// following the Approval prompt convention (y/yes or n/no, re-prompting on
// other input). An end of input declines: an untrusted repository is never
// trusted without explicit consent.
func promptTrust(input io.Reader, output io.Writer, root string) (bool, error) {
	if _, err := fmt.Fprintf(output, "deku: project config found at %s/.deku\nTrust this project? [y/N] ", root); err != nil {
		return false, fmt.Errorf("display trust prompt: %w", err)
	}
	br := bufio.NewReader(input)
	for {
		line, err := br.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return false, fmt.Errorf("read trust decision: %w", err)
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "y", "yes":
			return true, nil
		case "n", "no", "":
			return false, nil
		default:
			if _, err := io.WriteString(output, "Please answer y (trust) or n (ignore): "); err != nil {
				return false, fmt.Errorf("display trust prompt: %w", err)
			}
		}
	}
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

func runConversation(runner *agent.Agent, providers *provider.Registry, input io.Reader, output, errorOutput io.Writer) int {
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
		if strings.HasPrefix(request, "/") {
			if err := handleCommand(runner, providers, request, output); err != nil {
				if writeErr := writeError(errorOutput, "deku: %v\n", err); writeErr != nil {
					return 1
				}
			}
			continue
		}

		result, err := runner.Turn(context.Background(), request)
		if err != nil {
			if writeErr := writeError(errorOutput, "deku: %v\n", err); writeErr != nil {
				return 1
			}
			continue
		}
		if err := reportGitResult(output, errorOutput, result); err != nil {
			return 1
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

// handleCommand dispatches lines beginning with "/" before they become
// Turns. The /model command lists the Providers the Agent can authenticate
// to, or — with a Provider and Model argument — switches the active
// Selection for subsequent Turns and records the override in the Session.
func handleCommand(runner *agent.Agent, providers *provider.Registry, line string, output io.Writer) error {
	fields := strings.Fields(line)
	switch fields[0] {
	case "/model":
		switch len(fields) {
		case 1:
			return listSelectableModels(runner, providers, output)
		case 3:
			selection := provider.Selection{Provider: fields[1], Model: fields[2]}
			if err := runner.SetSelection(selection); err != nil {
				return err
			}
			_, err := fmt.Fprintf(output, "selection: %s / %s\n", selection.Provider, selection.Model)
			return err
		default:
			return fmt.Errorf("usage: /model [provider model]")
		}
	default:
		return fmt.Errorf("unknown command %q", fields[0])
	}
}

// listSelectableModels shows the current Selection and every Provider the
// Agent can authenticate to with its Models, so Selection is only offered
// from what works.
func listSelectableModels(runner *agent.Agent, providers *provider.Registry, output io.Writer) error {
	selection := runner.Selection()
	if _, err := fmt.Fprintf(output, "current selection: %s / %s\n", selection.Provider, selection.Model); err != nil {
		return err
	}
	entries := providers.Authenticatable()
	if len(entries) == 0 {
		_, err := io.WriteString(output, "no providers can authenticate; declare providers in models.json and credentials in auth.json\n")
		return err
	}
	for _, entry := range entries {
		if _, err := fmt.Fprintf(output, "%s: %s\n", entry.Name, strings.Join(entry.Models, ", ")); err != nil {
			return err
		}
	}
	return nil
}

// reportGitResult surfaces Validation outcomes and Git recoverability to the
// user. A successful Validation is never presented as proof that the repository
// is correct; a commit is a recoverable boundary, not a correctness guarantee.
func reportGitResult(output, errorOutput io.Writer, result agent.TurnResult) error {
	if result.StashRef != "" {
		if _, err := io.WriteString(output, "deku: stashed pre-existing work at "+result.StashRef+"\n"); err != nil {
			return err
		}
	}
	if result.Validation != nil {
		if result.Validation.Passed {
			if _, err := io.WriteString(output, "deku: validation passed ("+result.Validation.Command+")\n"); err != nil {
				return err
			}
		} else {
			if _, err := io.WriteString(output, "deku: validation failed ("+result.Validation.Command+"); work remains uncommitted\n"); err != nil {
				return err
			}
		}
	}
	if result.CommitID != "" {
		if _, err := io.WriteString(output, "deku: agent commit created "+result.CommitID+"\n"); err != nil {
			return err
		}
	}
	return nil
}

func writeError(output io.Writer, format string, args ...any) error {
	_, err := fmt.Fprintf(output, format, args...)
	return err
}

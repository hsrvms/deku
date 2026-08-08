// Package agent implements the core Agent loop that mediates between the user,
// the model, and the tools.
package agent

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/hsrvms/deku/activity"
	"github.com/hsrvms/deku/approval"
	"github.com/hsrvms/deku/lineio"
	"github.com/hsrvms/deku/prompt"
	"github.com/hsrvms/deku/provider"
	"github.com/hsrvms/deku/repomap"
	"github.com/hsrvms/deku/repository"
	"github.com/hsrvms/deku/session"
	"github.com/hsrvms/deku/tool"
)

// Runner is the public seam for driving a complete Turn.
type Runner interface {
	Turn(context.Context, string) (TurnResult, error)
}

// SelectionSource resolves the Adapter for a Selection. The provider
// Registry is the production implementation; Agent seam tests substitute a
// scripted source so each Selection runs against its own scripted Adapter.
type SelectionSource interface {
	Resolve(provider.Selection) (provider.Chat, error)
}

// TurnResult contains the completed model response and any usage reported by
// the Provider. When Git safety is active it also reports the Validation
// outcome and any Agent Commit or stash created during the Turn.
type TurnResult struct {
	Response   string
	Usage      *provider.Usage
	Validation *ValidationResult
	CommitID   string
	StashRef   string
}

// ValidationResult reports the repository Validation coordinate for a Turn
// once Git safety is active. It distinguishes a failing check from Git
// recoverability: a passed Validation is a precondition for an Agent Commit,
// never proof that the repository is correct.
type ValidationResult struct {
	Command string
	Passed  bool
	Output  string
}

// Agent owns one conversation and drives a multi-Step Turn at a time.
type Agent struct {
	provider      provider.Chat
	model         string
	source        SelectionSource
	selection     provider.Selection
	session       *session.Session
	output        io.Writer
	input         *bufio.Reader
	tools         *tool.Registry
	approval      approval.Decider
	toolErr       error
	repoMap       *repomap.Builder
	repoMapErr    error
	repo          *repository.Repo
	commitMode    repository.Mode
	validationCmd string
	initialized   bool
	stashRef      string
	sink          activity.Sink

	turnMu sync.Mutex
}

var _ Runner = (*Agent)(nil)

// New constructs an Agent rooted at the current working directory. The
// output writer receives TextDelta content and Approval prompts; a nil writer
// discards streamed output while still returning it in TurnResult. The input
// reader supplies synchronous Approval decisions; a nil reader defaults to
// standard input.
func New(p provider.Chat, model string, conversation *session.Session, output io.Writer, input io.Reader) *Agent {
	registry, err := tool.NewRegistry(".")
	return newAgent(p, model, conversation, output, input, registry, approval.DefaultPolicy(), nil, err, nil, nil, repository.ModeOff, "", nil)
}

// NewWithTools constructs an Agent with an explicit Tool registry. This is the
// test and embedding seam for choosing the repository being explored.
func NewWithTools(p provider.Chat, model string, conversation *session.Session, output io.Writer, input io.Reader, registry *tool.Registry) *Agent {
	return newAgent(p, model, conversation, output, input, registry, approval.DefaultPolicy(), nil, nil, nil, nil, repository.ModeOff, "", nil)
}

// NewWithPolicy constructs an Agent with an explicit Tool registry, a
// configured Approval policy, and repository-map exclusion patterns. The
// exclusion patterns are gitignore-style globs applied in addition to any
// .gitignore files when building the Repository Map on every Step.
func NewWithPolicy(p provider.Chat, model string, conversation *session.Session, output io.Writer, input io.Reader, registry *tool.Registry, policy approval.Policy, exclude []string) *Agent {
	return newAgent(p, model, conversation, output, input, registry, policy, nil, nil, exclude, nil, repository.ModeOff, "", nil)
}

// NewWithActivity constructs an Agent with an explicit Tool registry, Approval
// policy, repository-map exclusions, and an activity Sink. This is the test and
// embedding seam for observing the activity stream: a fake Sink records the
// deterministic indicator transitions and change events across a Turn.
func NewWithActivity(p provider.Chat, model string, conversation *session.Session, output io.Writer, input io.Reader, registry *tool.Registry, policy approval.Policy, exclude []string, sink activity.Sink) *Agent {
	return newAgent(p, model, conversation, output, input, registry, policy, nil, nil, exclude, nil, repository.ModeOff, "", sink)
}

// NewWithGit constructs an Agent with Git safety enabled. The Repository is a
// real Git working tree; mode is the Agent Commit configuration (off, ask, or
// on) and validation is the command run after a completed Turn before any
// Agent Commit. This is the production and test seam for dirty-tree handling,
// Checkpoints, stashes, Validation, external-change detection, and Agent
// Commit attribution.
func NewWithGit(p provider.Chat, model string, conversation *session.Session, output io.Writer, input io.Reader, registry *tool.Registry, policy approval.Policy, exclude []string, repo *repository.Repo, mode repository.Mode, validation string) *Agent {
	return newAgent(p, model, conversation, output, input, registry, policy, nil, nil, exclude, repo, mode, validation, nil)
}

// NewWithSelection constructs an Agent whose Adapter comes from a Selection
// resolved through source. The initial Selection is resolved immediately, so
// an unknown Provider, an unknown Model, or a Provider the Agent cannot
// authenticate to fails construction with an explicit error. Between Turns
// the caller changes the active Selection with SetSelection; the override is
// recorded in the Session and restored on resume.
func NewWithSelection(source SelectionSource, selection provider.Selection, conversation *session.Session, output io.Writer, input io.Reader, registry *tool.Registry, policy approval.Policy, exclude []string, repo *repository.Repo, mode repository.Mode, validation string) (*Agent, error) {
	if source == nil {
		return nil, errors.New("selection source is required")
	}
	adapter, err := source.Resolve(selection)
	if err != nil {
		return nil, err
	}
	agent := newAgent(adapter, selection.Model, conversation, output, input, registry, policy, nil, nil, exclude, repo, mode, validation, nil)
	agent.source = source
	agent.selection = selection
	return agent, nil
}

func newAgent(p provider.Chat, model string, conversation *session.Session, output io.Writer, input io.Reader, registry *tool.Registry, policy approval.Policy, gate approval.Decider, toolErr error, exclude []string, repo *repository.Repo, mode repository.Mode, validationCmd string, sink activity.Sink) *Agent {
	if output == nil {
		output = io.Discard
	}
	if input == nil {
		input = os.Stdin
	}
	var reader *bufio.Reader
	if shared, ok := input.(*bufio.Reader); ok {
		reader = shared
	} else {
		reader = bufio.NewReader(input)
	}
	if gate == nil {
		gate = approval.NewGate(policy, reader, output)
	}
	if mode != repository.ModeOn && mode != repository.ModeAsk {
		mode = repository.ModeOff
	}
	if repo == nil {
		mode = repository.ModeOff
	}
	if strings.TrimSpace(validationCmd) == "" {
		validationCmd = defaultValidationCommand
	}
	if sink == nil {
		sink = activity.Discard()
	}
	clone := &Agent{
		provider:      p,
		model:         model,
		session:       conversation,
		output:        output,
		input:         reader,
		tools:         registry,
		approval:      gate,
		toolErr:       toolErr,
		repo:          repo,
		commitMode:    mode,
		validationCmd: validationCmd,
		sink:          sink,
	}
	if registry != nil && toolErr == nil {
		builder, err := repomap.NewBuilder(registry.Root(), exclude)
		if err != nil {
			clone.repoMapErr = err
		} else {
			clone.repoMap = builder
		}
	}
	return clone
}

// defaultValidationCommand is the check run after a completed Turn before an
// Agent Commit. Callers may override it through configuration.
const defaultValidationCommand = "go test ./..."

// SetSelection changes the active Selection between Turns. The new Selection
// is resolved and validated first, then recorded in the Session transcript,
// and only then applied: a failed resolution leaves the active Selection
// untouched and records nothing. The change applies to subsequent Turns. An
// Agent constructed without a Selection source has a fixed Provider and
// cannot change Selection.
func (a *Agent) SetSelection(selection provider.Selection) error {
	if a == nil {
		return errors.New("agent is nil")
	}
	if a.source == nil {
		return errors.New("agent has no Selection source; construct it with NewWithSelection to change Selection")
	}
	if a.session == nil {
		return errors.New("agent session is required")
	}

	a.turnMu.Lock()
	defer a.turnMu.Unlock()

	adapter, err := a.source.Resolve(selection)
	if err != nil {
		return err
	}
	if err := a.session.RecordSelection(session.Selection{Provider: selection.Provider, Model: selection.Model}); err != nil {
		return fmt.Errorf("record selection: %w", err)
	}
	a.provider = adapter
	a.model = selection.Model
	a.selection = selection
	return nil
}

// Selection returns the Agent's active Selection. An Agent constructed
// without a Selection source has a fixed Provider and reports the zero
// Selection.
func (a *Agent) Selection() provider.Selection {
	if a == nil {
		return provider.Selection{}
	}
	a.turnMu.Lock()
	defer a.turnMu.Unlock()
	return a.selection
}

// Turn accepts one user request and drives Provider Steps until the model
// returns a response without Tool Calls.
func (a *Agent) Turn(ctx context.Context, request string) (TurnResult, error) {
	if a == nil {
		return TurnResult{}, errors.New("agent is nil")
	}
	if ctx == nil {
		return TurnResult{}, errors.New("agent context is nil")
	}
	if a.provider == nil {
		return TurnResult{}, errors.New("agent provider is required")
	}
	if strings.TrimSpace(a.model) == "" {
		return TurnResult{}, errors.New("agent model is required")
	}
	if a.session == nil {
		return TurnResult{}, errors.New("agent session is required")
	}
	if a.toolErr != nil {
		return TurnResult{}, fmt.Errorf("initialize tools: %w", a.toolErr)
	}
	if a.tools == nil {
		return TurnResult{}, errors.New("agent tools are required")
	}
	if a.repoMapErr != nil {
		return TurnResult{}, fmt.Errorf("initialize repository map: %w", a.repoMapErr)
	}
	request = strings.TrimSpace(request)
	if request == "" {
		return TurnResult{}, errors.New("agent request is required")
	}

	a.turnMu.Lock()
	defer a.turnMu.Unlock()

	if err := a.initialize(ctx); err != nil {
		return TurnResult{}, err
	}

	if err := a.session.Append(session.Message{Role: session.RoleUser, Content: request}); err != nil {
		return TurnResult{}, fmt.Errorf("record user request: %w", err)
	}

	var snapshot repository.Snapshot
	if a.gitEnabled() {
		a.tools.ResetTouched()
		var err error
		snapshot, err = a.repo.Snapshot()
		if err != nil {
			return TurnResult{}, fmt.Errorf("snapshot repository: %w", err)
		}
	}

	streamContext, cancel := context.WithCancel(ctx)
	defer cancel()
	var totalUsage *provider.Usage
	var finalResponse string
	for {
		if err := streamContext.Err(); err != nil {
			return TurnResult{}, fmt.Errorf("turn canceled: %w", err)
		}
		messages := providerMessages(a.session.Messages)
		systemPrompt, err := a.systemPrompt()
		if err != nil {
			return TurnResult{}, err
		}
		a.sink.Indicator(activity.Thinking)
		events, err := a.provider.Chat(
			streamContext,
			a.model,
			systemPrompt,
			messages,
			a.tools.Definitions(),
		)
		if err != nil {
			return TurnResult{}, fmt.Errorf("start provider step: %w", err)
		}
		if events == nil {
			return TurnResult{}, errors.New("provider returned a nil event stream")
		}

		response, calls, usage, err := a.consumeStep(events)
		if err != nil {
			return TurnResult{}, err
		}
		totalUsage = addUsage(totalUsage, usage)
		assistant := session.Message{Role: session.RoleAssistant, Content: response}
		for _, call := range calls {
			assistant.ToolCalls = append(assistant.ToolCalls, session.ToolCall{
				ID:        call.ID,
				Name:      call.Name,
				Arguments: call.Arguments,
			})
		}
		if err := a.session.Append(assistant); err != nil {
			return TurnResult{}, fmt.Errorf("record assistant response: %w", err)
		}
		if len(calls) == 0 {
			finalResponse = response
			break
		}

		for _, call := range calls {
			if strings.TrimSpace(call.ID) == "" {
				return TurnResult{}, fmt.Errorf("tool call %q has no ID", call.Name)
			}
			if strings.TrimSpace(call.Name) == "" {
				return TurnResult{}, errors.New("provider returned a tool call without a name")
			}
			content, err := a.runTool(streamContext, call)
			if err != nil {
				return TurnResult{}, err
			}
			if err := a.session.Append(session.Message{
				Role:       session.RoleTool,
				Name:       call.Name,
				ToolCallID: call.ID,
				Content:    content,
			}); err != nil {
				return TurnResult{}, fmt.Errorf("record tool result: %w", err)
			}
		}
	}

	result := TurnResult{
		Response: finalResponse,
		Usage:    totalUsage,
		StashRef: a.stashRef,
	}
	if a.gitEnabled() {
		if err := a.finishTurn(ctx, snapshot, request, &result); err != nil {
			return TurnResult{}, err
		}
	}
	return result, nil
}

// systemPrompt assembles the per-Step system prompt, refreshing the
// Repository Map so every Step sees the current repository structure.
func (a *Agent) systemPrompt() (string, error) {
	if a.repoMap == nil {
		return prompt.BuildSystemPrompt(""), nil
	}
	if a.repoMapErr != nil {
		return "", fmt.Errorf("build repository map: %w", a.repoMapErr)
	}
	repoMap, err := a.repoMap.Build()
	if err != nil {
		return "", fmt.Errorf("build repository map: %w", err)
	}
	return prompt.BuildSystemPrompt(repoMap), nil
}

// runTool gates a single Tool Call behind Approval and executes it, returning
// the normalized Tool Result content for the model. The call's Command Report
// is rendered before any Approval is sought; a call whose Report cannot be
// rendered is refused without executing, never approved blindly.
func (a *Agent) runTool(ctx context.Context, call provider.ToolCall) (string, error) {
	declared, tierErr := a.tools.Tier(call.Name)
	if tierErr != nil {
		return "tool error: " + tierErr.Error(), nil
	}
	report, reportErr := a.tools.Report(call.Name, call.Arguments)
	if reportErr != nil {
		return "tool error: " + reportErr.Error(), nil
	}
	if strings.TrimSpace(report) == "" {
		return fmt.Sprintf("tool error: the %s tool call has no Command Report; refusing to execute it", call.Name), nil
	}
	a.sink.Indicator(activity.AwaitingApproval)
	decision, err := a.approval.Decide(ctx, approval.Request{ToolName: call.Name, Declared: declared, Report: report})
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return "", fmt.Errorf("approve tool %q: %w", call.Name, contextErr)
		}
		return "", fmt.Errorf("approve tool %q: %w", call.Name, err)
	}
	if !decision.Approved {
		if err := writeOutput(a.output, fmt.Sprintf("Rejected the %s tool call; it did not execute.\n", call.Name)); err != nil {
			return "", fmt.Errorf("display tool rejection: %w", err)
		}
		return fmt.Sprintf("The user rejected the %s tool call; it did not execute.", call.Name), nil
	}
	a.sink.Indicator(activity.Working)
	before := a.tools.ChangeCount()
	content, toolErr := a.tools.Execute(ctx, call.Name, call.Arguments)
	if toolErr != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return "", fmt.Errorf("execute tool %q: %w", call.Name, contextErr)
		}
		content = "tool error: " + toolErr.Error()
	}
	if err := a.echoToolResult(call.Name, decision.Tier, content); err != nil {
		return "", err
	}
	for _, path := range a.tools.ChangesSince(before) {
		a.sink.Change(activity.Change{Tool: call.Name, Path: path})
	}
	return content, nil
}

// echoToolResult renders a Tool's normalized result to the terminal after
// execution, regardless of the Tool's tier, so the user sees what ran on
// their machine rather than only what the model reports back. The header
// names the Tool and its effective tier; the content is indented to keep it
// distinct from streamed model text.
func (a *Agent) echoToolResult(name string, tier approval.Tier, content string) error {
	var builder strings.Builder
	fmt.Fprintf(&builder, "Tool output (%s, %s):\n", name, tier)
	for _, line := range strings.Split(strings.TrimSuffix(content, "\n"), "\n") {
		builder.WriteString("  ")
		builder.WriteString(line)
		builder.WriteByte('\n')
	}
	return writeOutput(a.output, builder.String())
}

func (a *Agent) consumeStep(events <-chan provider.Event) (string, []provider.ToolCall, *provider.Usage, error) {
	var response strings.Builder
	calls := make(map[int]provider.ToolCall)
	var order []int
	seen := make(map[int]bool)
	var usage *provider.Usage
	done := false
	for event := range events {
		switch event := event.(type) {
		case provider.TextDelta:
			response.WriteString(event.Text)
			if err := writeOutput(a.output, event.Text); err != nil {
				return "", nil, nil, fmt.Errorf("display provider response: %w", err)
			}
		case provider.ToolCallDelta:
			call := calls[event.Index]
			call.Index = event.Index
			if event.ID != "" {
				call.ID = event.ID
			}
			if event.Name != "" {
				call.Name = event.Name
			}
			call.Arguments += event.Arguments
			calls[event.Index] = call
			if !seen[event.Index] {
				seen[event.Index] = true
				order = append(order, event.Index)
			}
		case provider.ToolCall:
			calls[event.Index] = event
			if !seen[event.Index] {
				seen[event.Index] = true
				order = append(order, event.Index)
			}
		case provider.Done:
			if done {
				return "", nil, nil, errors.New("provider returned multiple completion events")
			}
			done = true
			usage = event.Usage
		case provider.Error:
			if event.Err == nil {
				return "", nil, nil, errors.New("provider stream failed")
			}
			return "", nil, nil, fmt.Errorf("provider stream: %w", event.Err)
		}
	}
	if !done {
		return "", nil, nil, errors.New("provider stream ended without a completion event")
	}
	orderedCalls := make([]provider.ToolCall, 0, len(order))
	for _, index := range order {
		orderedCalls = append(orderedCalls, calls[index])
	}
	return response.String(), orderedCalls, usage, nil
}

func providerMessages(messages []session.Message) []provider.Message {
	converted := make([]provider.Message, 0, len(messages))
	for _, message := range messages {
		convertedMessage := provider.Message{
			Role:       message.Role,
			Content:    message.Content,
			Name:       message.Name,
			ToolCallID: message.ToolCallID,
		}
		for _, call := range message.ToolCalls {
			convertedMessage.ToolCalls = append(convertedMessage.ToolCalls, provider.ToolCall{
				ID:        call.ID,
				Name:      call.Name,
				Arguments: call.Arguments,
			})
		}
		converted = append(converted, convertedMessage)
	}
	return converted
}

func addUsage(total, step *provider.Usage) *provider.Usage {
	if total == nil && step == nil {
		return nil
	}
	if total == nil {
		total = &provider.Usage{}
	}
	if step != nil {
		total.PromptTokens += step.PromptTokens
		total.CompletionTokens += step.CompletionTokens
		total.TotalTokens += step.TotalTokens
	}
	return total
}

func writeOutput(output io.Writer, text string) error {
	if text == "" {
		return nil
	}
	written, err := io.WriteString(output, text)
	if err != nil {
		return err
	}
	if written != len(text) {
		return io.ErrShortWrite
	}
	return nil
}

// gitEnabled reports whether Git safety is active for this Agent. It is active
// only when a Repository is attached and the Agent Commit mode is on or ask; a
// dirty repository may reduce mode to off for the rest of the session.
func (a *Agent) gitEnabled() bool {
	return a.repo != nil && a.commitMode != repository.ModeOff
}

// initialize performs the one-time dirty-tree inspection at the start of a
// session. A dirty repository with Agent Commits enabled requires the user to
// choose a Checkpoint, a stash, continuing without commits, or cancellation,
// so pre-existing work is never silently committed or hidden.
func (a *Agent) initialize(ctx context.Context) error {
	if a.initialized || !a.gitEnabled() {
		return nil
	}
	state, err := a.repo.State()
	if err != nil {
		return fmt.Errorf("inspect repository: %w", err)
	}
	if state.Dirty() {
		action, err := a.chooseDirtyAction(ctx, state)
		if err != nil {
			return err
		}
		switch action {
		case dirtyCheckpoint:
			if _, err := a.repo.Checkpoint("deku: checkpoint pre-existing work before agent turn"); err != nil {
				return fmt.Errorf("create checkpoint: %w", err)
			}
		case dirtyStash:
			ref, err := a.repo.Stash(stashMessage())
			if err != nil {
				return fmt.Errorf("stash repository: %w", err)
			}
			a.stashRef = ref
		case dirtyContinue:
			a.commitMode = repository.ModeOff
		case dirtyCancel:
			return errors.New("canceled: repository has uncommitted changes; no Agent work was performed")
		}
	}
	a.initialized = true
	return nil
}

// dirtyAction is a user decision for a dirty repository at session start.
type dirtyAction int

// Dirty repository handling choices.
const (
	dirtyCheckpoint dirtyAction = iota
	dirtyStash
	dirtyContinue
	dirtyCancel
)

// chooseDirtyAction prompts the user to resolve a dirty repository when Agent
// Commits are enabled, re-prompting until a valid choice is entered.
func (a *Agent) chooseDirtyAction(ctx context.Context, state repository.State) (dirtyAction, error) {
	prompt := fmt.Sprintf(
		"The repository has uncommitted changes (staged: %d, unstaged: %d, untracked: %d). Agent Commits are enabled.\n"+
			"Choose how to proceed:\n"+
			"  1. Create a Checkpoint (commit existing work)\n"+
			"  2. Stash existing work\n"+
			"  3. Continue without Agent Commits\n"+
			"  4. Cancel\n"+
			"Enter choice [1-4]: ",
		len(state.Staged), len(state.Unstaged), len(state.Untracked))
	if _, err := io.WriteString(a.output, prompt); err != nil {
		return 0, fmt.Errorf("display repository prompt: %w", err)
	}
	for {
		line, err := a.readLine(ctx)
		if err != nil {
			return 0, err
		}
		switch strings.TrimSpace(line) {
		case "1":
			return dirtyCheckpoint, nil
		case "2":
			return dirtyStash, nil
		case "3":
			return dirtyContinue, nil
		case "4":
			return dirtyCancel, nil
		default:
			if _, err := io.WriteString(a.output, "Please enter 1, 2, 3, or 4: "); err != nil {
				return 0, fmt.Errorf("display repository prompt: %w", err)
			}
		}
	}
}

// stashMessage returns a recognizable, unique message for a Deku-created stash
// so the precise stash can be identified and restored later. A random suffix
// guarantees two stashes created in the same second never collide, so the
// stash is always findable by message and never mistaken for another.
func stashMessage() string {
	token := randomToken()
	return "deku: pre-existing work stashed before agent turn " + time.Now().UTC().Format("20060102T150405Z") + " " + token
}

// randomToken returns a short random hex token used to disambiguate stash
// messages. It falls back to a nanosecond timestamp only if the entropy source
// is unavailable, which is effectively never.
func randomToken() string {
	var suffix [6]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UTC().UnixNano())
	}
	return hex.EncodeToString(suffix[:])
}

// finishTurn attributes working-tree changes, runs Validation, and creates an
// Agent Commit when the mode and the user allow it. It runs only after a Turn
// completed without interruption or provider failure.
func (a *Agent) finishTurn(ctx context.Context, snapshot repository.Snapshot, request string, result *TurnResult) error {
	touched := a.tools.Touched()
	changed, err := a.repo.Changed(snapshot)
	if err != nil {
		return fmt.Errorf("detect repository changes: %w", err)
	}
	touchedSet := make(map[string]bool, len(touched))
	for _, path := range touched {
		touchedSet[path] = true
	}
	var agentOwned, external []string
	for _, path := range changed {
		if touchedSet[path] {
			agentOwned = append(agentOwned, path)
		} else {
			external = append(external, path)
		}
	}
	if len(external) > 0 {
		return fmt.Errorf("external repository changes detected during the Turn (%s); pausing without an Agent Commit", strings.Join(external, ", "))
	}
	if len(agentOwned) == 0 {
		return nil
	}

	validation, err := a.repo.Validate(ctx, a.validationCmd)
	if err != nil {
		return fmt.Errorf("run validation: %w", err)
	}
	result.Validation = &ValidationResult{
		Command: validation.Command,
		Passed:  validation.Passed,
		Output:  validation.Output,
	}
	if !validation.Passed {
		// Failed Validation leaves the Agent's work uncommitted for inspection.
		return nil
	}

	shouldCommit := false
	switch a.commitMode {
	case repository.ModeOn:
		shouldCommit = true
	case repository.ModeAsk:
		approved, err := a.askCommit(ctx)
		if err != nil {
			return err
		}
		shouldCommit = approved
	}
	if !shouldCommit {
		return nil
	}

	commitID, err := a.repo.Commit(agentOwned, commitMessage(request))
	if err != nil {
		return fmt.Errorf("create agent commit: %w", err)
	}
	result.CommitID = commitID
	return nil
}

// askCommit prompts the user whether to create an Agent Commit after a
// completed, validated Turn in ask mode.
func (a *Agent) askCommit(ctx context.Context) (bool, error) {
	if _, err := io.WriteString(a.output, "Validation passed. Create an Agent Commit for this Turn? [y/n] "); err != nil {
		return false, fmt.Errorf("display commit prompt: %w", err)
	}
	for {
		line, err := a.readLine(ctx)
		if err != nil {
			return false, err
		}
		switch strings.ToLower(line) {
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		default:
			if _, err := io.WriteString(a.output, "Please answer y (commit) or n (skip): "); err != nil {
				return false, fmt.Errorf("display commit prompt: %w", err)
			}
		}
	}
}

// commitMessage renders a recognizable Agent Commit message from the request.
func commitMessage(request string) string {
	summary := strings.TrimSpace(request)
	runes := []rune(summary)
	if len(runes) > 60 {
		return "deku: " + string(runes[:60])
	}
	return "deku: " + summary
}

// readLine reads the next non-empty line from the Agent's shared input reader,
// honoring context cancellation. The reader is shared with the Approval gate so
// buffered input is never double-buffered between the two.
func (a *Agent) readLine(ctx context.Context) (string, error) {
	type result struct {
		line string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		line, err := lineio.Scan(a.input)
		select {
		case ch <- result{line: line, err: err}:
		case <-ctx.Done():
		}
	}()
	select {
	case r := <-ch:
		return r.line, r.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

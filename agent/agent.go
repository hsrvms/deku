// Package agent implements the core Agent loop that mediates between the user,
// the model, and the tools.
package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/hsrvms/deku/approval"
	"github.com/hsrvms/deku/prompt"
	"github.com/hsrvms/deku/provider"
	"github.com/hsrvms/deku/repomap"
	"github.com/hsrvms/deku/session"
	"github.com/hsrvms/deku/tool"
)

// Runner is the public seam for driving a complete Turn.
type Runner interface {
	Turn(context.Context, string) (TurnResult, error)
}

// TurnResult contains the completed model response and any usage reported by
// the Provider.
type TurnResult struct {
	Response string
	Usage    *provider.Usage
}

// Agent owns one conversation and drives a multi-Step Turn at a time.
type Agent struct {
	provider   provider.Chat
	model      string
	session    *session.Session
	output     io.Writer
	tools      *tool.Registry
	approval   approval.Decider
	toolErr    error
	repoMap    *repomap.Builder
	repoMapErr error

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
	return newAgent(p, model, conversation, output, input, registry, nil, err, nil)
}

// NewWithTools constructs an Agent with an explicit Tool registry. This is the
// test and embedding seam for choosing the repository being explored.
func NewWithTools(p provider.Chat, model string, conversation *session.Session, output io.Writer, input io.Reader, registry *tool.Registry) *Agent {
	return newAgent(p, model, conversation, output, input, registry, nil, nil, nil)
}

// NewWithApproval constructs an Agent with an explicit Tool registry and a
// configured Approval policy. This is the production seam for wiring
// per-tool and per-tier overrides from configuration.
func NewWithApproval(p provider.Chat, model string, conversation *session.Session, output io.Writer, input io.Reader, registry *tool.Registry, policy approval.Policy) *Agent {
	return NewWithPolicy(p, model, conversation, output, input, registry, policy, nil)
}

// NewWithPolicy constructs an Agent with an explicit Tool registry, a
// configured Approval policy, and repository-map exclusion patterns. The
// exclusion patterns are gitignore-style globs applied in addition to any
// .gitignore files when building the Repository Map on every Step.
func NewWithPolicy(p provider.Chat, model string, conversation *session.Session, output io.Writer, input io.Reader, registry *tool.Registry, policy approval.Policy, exclude []string) *Agent {
	gate := approval.NewGate(policy, input, output)
	return newAgent(p, model, conversation, output, input, registry, gate, nil, exclude)
}

func newAgent(p provider.Chat, model string, conversation *session.Session, output io.Writer, input io.Reader, registry *tool.Registry, gate approval.Decider, toolErr error, exclude []string) *Agent {
	if output == nil {
		output = io.Discard
	}
	if gate == nil {
		gate = approval.NewGate(approval.DefaultPolicy(), input, output)
	}
	clone := &Agent{
		provider: p,
		model:    model,
		session:  conversation,
		output:   output,
		tools:    registry,
		approval: gate,
		toolErr:  toolErr,
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

	if err := a.session.Append(session.Message{Role: session.RoleUser, Content: request}); err != nil {
		return TurnResult{}, fmt.Errorf("record user request: %w", err)
	}

	streamContext, cancel := context.WithCancel(ctx)
	defer cancel()
	var totalUsage *provider.Usage
	for {
		if err := streamContext.Err(); err != nil {
			return TurnResult{}, fmt.Errorf("turn canceled: %w", err)
		}
		messages := providerMessages(a.session.Messages)
		systemPrompt, err := a.systemPrompt()
		if err != nil {
			return TurnResult{}, err
		}
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
			return TurnResult{Response: response, Usage: totalUsage}, nil
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
// the normalized Tool Result content for the model.
func (a *Agent) runTool(ctx context.Context, call provider.ToolCall) (string, error) {
	declared, tierErr := a.tools.Tier(call.Name)
	if tierErr != nil {
		return "tool error: " + tierErr.Error(), nil
	}
	decision, err := a.approval.Decide(ctx, call.Name, declared)
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return "", fmt.Errorf("approve tool %q: %w", call.Name, contextErr)
		}
		return "", fmt.Errorf("approve tool %q: %w", call.Name, err)
	}
	if !decision.Approved {
		return fmt.Sprintf("The user rejected the %s tool call; it did not execute.", call.Name), nil
	}
	content, toolErr := a.tools.Execute(ctx, call.Name, call.Arguments)
	if toolErr != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return "", fmt.Errorf("execute tool %q: %w", call.Name, contextErr)
		}
		content = "tool error: " + toolErr.Error()
	}
	return content, nil
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

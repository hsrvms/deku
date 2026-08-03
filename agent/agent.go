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

	"github.com/hsrvms/deku/prompt"
	"github.com/hsrvms/deku/provider"
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
	provider provider.Chat
	model    string
	session  *session.Session
	output   io.Writer
	tools    *tool.Registry
	toolErr  error

	turnMu sync.Mutex
}

var _ Runner = (*Agent)(nil)

// New constructs an Agent rooted at the current working directory. The
// output writer receives TextDelta content as it arrives; a nil writer
// discards streamed output while still returning it in TurnResult.
func New(p provider.Chat, model string, conversation *session.Session, output io.Writer) *Agent {
	registry, err := tool.NewRegistry(".")
	return newAgent(p, model, conversation, output, registry, err)
}

// NewWithTools constructs an Agent with an explicit Tool registry. This is the
// test and embedding seam for choosing the repository being explored.
func NewWithTools(p provider.Chat, model string, conversation *session.Session, output io.Writer, registry *tool.Registry) *Agent {
	return newAgent(p, model, conversation, output, registry, nil)
}

func newAgent(p provider.Chat, model string, conversation *session.Session, output io.Writer, registry *tool.Registry, toolErr error) *Agent {
	if output == nil {
		output = io.Discard
	}
	return &Agent{
		provider: p,
		model:    model,
		session:  conversation,
		output:   output,
		tools:    registry,
		toolErr:  toolErr,
	}
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
		events, err := a.provider.Chat(
			streamContext,
			a.model,
			prompt.BuildSystemPrompt(),
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
			content, toolErr := a.tools.Execute(streamContext, call.Name, call.Arguments)
			if toolErr != nil {
				if contextErr := streamContext.Err(); contextErr != nil {
					return TurnResult{}, fmt.Errorf("execute tool %q: %w", call.Name, contextErr)
				}
				content = "tool error: " + toolErr.Error()
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

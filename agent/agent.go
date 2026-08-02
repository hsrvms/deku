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

// Agent owns one conversation and drives one single-Step Turn at a time.
type Agent struct {
	provider provider.Chat
	model    string
	session  *session.Session
	output   io.Writer

	turnMu sync.Mutex
}

var _ Runner = (*Agent)(nil)

// New constructs an Agent. The output writer receives TextDelta content as it
// arrives; a nil writer discards streamed output while still returning it in
// TurnResult.
func New(p provider.Chat, model string, conversation *session.Session, output io.Writer) *Agent {
	if output == nil {
		output = io.Discard
	}
	return &Agent{
		provider: p,
		model:    model,
		session:  conversation,
		output:   output,
	}
}

// Turn accepts one user request, executes one Provider Step without tools,
// streams the response, and appends the completed conversation to the Session.
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
	request = strings.TrimSpace(request)
	if request == "" {
		return TurnResult{}, errors.New("agent request is required")
	}

	a.turnMu.Lock()
	defer a.turnMu.Unlock()

	if err := a.session.Append(session.Message{Role: session.RoleUser, Content: request}); err != nil {
		return TurnResult{}, fmt.Errorf("record user request: %w", err)
	}

	messages := make([]provider.Message, 0, len(a.session.Messages))
	for _, message := range a.session.Messages {
		messages = append(messages, provider.Message{
			Role:    message.Role,
			Content: message.Content,
		})
	}

	streamContext, cancel := context.WithCancel(ctx)
	defer cancel()
	events, err := a.provider.Chat(
		streamContext,
		a.model,
		prompt.BuildSystemPrompt(),
		messages,
		nil,
	)
	if err != nil {
		return TurnResult{}, fmt.Errorf("start provider step: %w", err)
	}
	if events == nil {
		return TurnResult{}, errors.New("provider returned a nil event stream")
	}

	var response strings.Builder
	var usage *provider.Usage
	done := false
	for event := range events {
		switch event := event.(type) {
		case provider.TextDelta:
			response.WriteString(event.Text)
			if err := writeOutput(a.output, event.Text); err != nil {
				return TurnResult{}, fmt.Errorf("display provider response: %w", err)
			}
		case provider.Done:
			if done {
				return TurnResult{}, errors.New("provider returned multiple completion events")
			}
			done = true
			usage = event.Usage
		case provider.Error:
			if event.Err == nil {
				return TurnResult{}, errors.New("provider stream failed")
			}
			return TurnResult{}, fmt.Errorf("provider stream: %w", event.Err)
		case provider.ToolCall, provider.ToolCallDelta:
			return TurnResult{}, errors.New("provider requested a tool, but tools are unavailable in this Turn")
		}
	}
	if !done {
		return TurnResult{}, errors.New("provider stream ended without a completion event")
	}

	responseText := response.String()
	if err := a.session.Append(session.Message{Role: session.RoleAssistant, Content: responseText}); err != nil {
		return TurnResult{}, fmt.Errorf("record assistant response: %w", err)
	}
	return TurnResult{Response: responseText, Usage: usage}, nil
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

// Package provider defines the adapter interface for translating Agent requests
// into model API wire formats and normalizing streaming responses into Events.
package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

// Message is a conversation message sent to a model.
//
// ToolCalls is populated for assistant messages that request tools. ToolCallID
// is populated for tool result messages.
type Message struct {
	Role       string
	Content    string
	Name       string
	ToolCallID string
	ToolCalls  []ToolCall
}

// ToolDefinition describes a function that the model may call.
type ToolDefinition struct {
	Type     string
	Function FunctionDefinition
}

// FunctionDefinition describes a function tool and its JSON input schema.
type FunctionDefinition struct {
	Name        string
	Description string
	Parameters  any
}

// Event is one unit of output from a Provider.
type Event interface {
	providerEvent()
}

// TextDelta is a fragment of model-generated text.
type TextDelta struct {
	Text string
}

// ToolCall is a complete model tool invocation.
type ToolCall struct {
	Index     int
	ID        string
	Name      string
	Arguments string
}

// ToolCallDelta is a fragment of a streamed model tool invocation. Any of ID,
// Name, and Arguments may be empty when that field was not present in the
// fragment.
type ToolCallDelta struct {
	Index     int
	ID        string
	Name      string
	Arguments string
}

// Usage contains token counts reported by the provider, when available.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// Done marks the successful end of a provider response.
type Done struct {
	Usage *Usage
}

// Error reports a failure after a streaming response has been established.
// Failures before a response is established are returned by Chat directly.
type Error struct {
	Err error
}

func (TextDelta) providerEvent()     {}
func (ToolCall) providerEvent()      {}
func (ToolCallDelta) providerEvent() {}
func (Done) providerEvent()          {}
func (Error) providerEvent()         {}

// Error implements error for convenient handling of an Error event.
func (e Error) Error() string {
	if e.Err == nil {
		return "provider error"
	}
	return e.Err.Error()
}

// Unwrap exposes the underlying provider error to errors.Is and errors.As.
func (e Error) Unwrap() error {
	return e.Err
}

// Chat is the provider contract used by the Agent. Implementations return a
// stream immediately after the HTTP response is accepted. Errors encountered
// while consuming that stream are delivered as Error events.
type Chat interface {
	Chat(ctx context.Context, model, system string, messages []Message, tools []ToolDefinition) (<-chan Event, error)
}

// OpenAICompatible implements Chat for APIs that expose the OpenAI chat
// completions endpoint and Server-Sent Events streaming format.
type OpenAICompatible struct {
	Endpoint string
	APIKey   string
	Client   *http.Client
}

// NewOpenAICompatible creates an OpenAI-compatible provider. Endpoint should
// identify the API root, such as https://api.openai.com/v1, or may already end
// in /chat/completions. Client defaults to http.DefaultClient.
func NewOpenAICompatible(endpoint, apiKey string) *OpenAICompatible {
	return &OpenAICompatible{
		Endpoint: endpoint,
		APIKey:   apiKey,
		Client:   http.DefaultClient,
	}
}

var _ Chat = (*OpenAICompatible)(nil)

// Chat sends one streaming chat completion request.
func (p *OpenAICompatible) Chat(ctx context.Context, model, system string, messages []Message, tools []ToolDefinition) (<-chan Event, error) {
	if p == nil {
		return nil, errors.New("provider is nil")
	}
	if ctx == nil {
		return nil, errors.New("provider context is nil")
	}
	if strings.TrimSpace(model) == "" {
		return nil, errors.New("provider model is required")
	}

	endpoint, err := completionEndpoint(p.Endpoint)
	if err != nil {
		return nil, err
	}

	body, err := json.Marshal(buildChatRequest(model, system, messages, tools))
	if err != nil {
		return nil, fmt.Errorf("encode provider request: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create provider request: %w", err)
	}
	request.Header.Set("Accept", "text/event-stream")
	request.Header.Set("Content-Type", "application/json")
	if p.APIKey != "" {
		request.Header.Set("Authorization", "Bearer "+p.APIKey)
	}

	client := p.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("provider request: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		providerErr := providerHTTPError(response)
		if closeErr := response.Body.Close(); closeErr != nil {
			return nil, errors.Join(providerErr, fmt.Errorf("close provider error response body: %w", closeErr))
		}
		return nil, providerErr
	}

	events := make(chan Event, 16)
	go consumeSSE(ctx, response.Body, events)
	return events, nil
}

type chatRequest struct {
	Model         string        `json:"model"`
	Messages      []wireMessage `json:"messages"`
	Tools         []wireTool    `json:"tools,omitempty"`
	Stream        bool          `json:"stream"`
	StreamOptions streamOptions `json:"stream_options"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type wireMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content,omitempty"`
	Name       string         `json:"name,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	ToolCalls  []wireToolCall `json:"tool_calls,omitempty"`
}

type wireTool struct {
	Type     string       `json:"type"`
	Function wireFunction `json:"function"`
}

type wireFunction struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
}

type wireToolCall struct {
	ID       string           `json:"id,omitempty"`
	Type     string           `json:"type"`
	Function wireToolFunction `json:"function"`
}

type wireToolFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

func buildChatRequest(model, system string, messages []Message, tools []ToolDefinition) chatRequest {
	wireMessages := make([]wireMessage, 0, len(messages)+1)
	if system != "" {
		wireMessages = append(wireMessages, wireMessage{Role: RoleSystem, Content: system})
	}
	for _, message := range messages {
		wireMessage := wireMessage{
			Role:       message.Role,
			Content:    message.Content,
			Name:       message.Name,
			ToolCallID: message.ToolCallID,
		}
		if len(message.ToolCalls) > 0 {
			wireMessage.ToolCalls = make([]wireToolCall, 0, len(message.ToolCalls))
			for _, call := range message.ToolCalls {
				wireMessage.ToolCalls = append(wireMessage.ToolCalls, wireToolCall{
					ID:   call.ID,
					Type: "function",
					Function: wireToolFunction{
						Name:      call.Name,
						Arguments: call.Arguments,
					},
				})
			}
		}
		wireMessages = append(wireMessages, wireMessage)
	}

	request := chatRequest{
		Model:         model,
		Messages:      wireMessages,
		Stream:        true,
		StreamOptions: streamOptions{IncludeUsage: true},
	}
	if len(tools) > 0 {
		request.Tools = make([]wireTool, 0, len(tools))
		for _, tool := range tools {
			toolType := tool.Type
			if toolType == "" {
				toolType = "function"
			}
			request.Tools = append(request.Tools, wireTool{
				Type: toolType,
				Function: wireFunction{
					Name:        tool.Function.Name,
					Description: tool.Function.Description,
					Parameters:  tool.Function.Parameters,
				},
			})
		}
	}
	return request
}

func completionEndpoint(endpoint string) (string, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return "", errors.New("provider endpoint is required")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("parse provider endpoint: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("provider endpoint must use http or https, got %q", parsed.Scheme)
	}
	if parsed.Host == "" {
		return "", errors.New("provider endpoint must include a host")
	}
	path := strings.TrimRight(parsed.Path, "/")
	if !strings.HasSuffix(path, "/chat/completions") {
		if path == "" {
			path = "/chat/completions"
		} else {
			path += "/chat/completions"
		}
	}
	parsed.Path = path
	parsed.RawPath = ""
	return parsed.String(), nil
}

const maxProviderErrorBody = 1 << 20

func providerHTTPError(response *http.Response) error {
	body, readErr := io.ReadAll(io.LimitReader(response.Body, maxProviderErrorBody+1))
	if readErr != nil {
		return fmt.Errorf("provider returned %s (read error body: %w)", response.Status, readErr)
	}
	if len(body) > maxProviderErrorBody {
		body = append(body[:maxProviderErrorBody], []byte("...<truncated>")...)
	}
	message := strings.TrimSpace(string(body))
	if message == "" {
		return fmt.Errorf("provider returned %s", response.Status)
	}
	return fmt.Errorf("provider returned %s: %s", response.Status, message)
}

type streamState struct {
	calls     map[int]*ToolCall
	completed map[int]bool
	usage     *Usage
}

type streamChunk struct {
	Choices []streamChoice  `json:"choices"`
	Usage   *Usage          `json:"usage,omitempty"`
	Error   *streamAPIError `json:"error,omitempty"`
}

type streamChoice struct {
	Index        int         `json:"index"`
	Delta        streamDelta `json:"delta"`
	FinishReason *string     `json:"finish_reason"`
}

type streamDelta struct {
	Content      string              `json:"content,omitempty"`
	ToolCalls    []streamToolCall    `json:"tool_calls,omitempty"`
	FunctionCall *streamFunctionCall `json:"function_call,omitempty"`
}

type streamToolCall struct {
	Index    int                `json:"index"`
	ID       string             `json:"id,omitempty"`
	Function streamFunctionCall `json:"function,omitempty"`
}

type streamFunctionCall struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type streamAPIError struct {
	Message string `json:"message"`
	Type    string `json:"type,omitempty"`
	Code    any    `json:"code,omitempty"`
}

func consumeSSE(ctx context.Context, body io.ReadCloser, events chan Event) {
	defer close(events)
	defer func() {
		if err := body.Close(); err != nil {
			sendStreamError(ctx, events, fmt.Errorf("close provider stream: %w", err))
		}
	}()

	state := streamState{
		calls:     make(map[int]*ToolCall),
		completed: make(map[int]bool),
	}
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 4096), 10<<20)
	var data strings.Builder
	terminal := false

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if data.Len() == 0 {
				continue
			}
			var err error
			terminal, err = processSSEData(ctx, strings.TrimSuffix(data.String(), "\n"), &state, events)
			data.Reset()
			if err != nil {
				sendStreamError(ctx, events, err)
				return
			}
			if terminal {
				return
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			value := strings.TrimPrefix(line, "data:")
			value = strings.TrimPrefix(value, " ")
			data.WriteString(value)
			data.WriteByte('\n')
			continue
		}
		field, _, hasFieldValue := strings.Cut(line, ":")
		if hasFieldValue && (field == "" || field == "event" || field == "id" || field == "retry") {
			// SSE metadata and keep-alive comments do not affect chat events.
			continue
		}
		// Ignore unknown SSE fields. The data field is the only field used
		// by the OpenAI-compatible protocol.
	}

	if ctx.Err() != nil {
		sendStreamError(ctx, events, ctx.Err())
		return
	}
	if err := scanner.Err(); err != nil {
		sendStreamError(ctx, events, fmt.Errorf("read SSE stream: %w", err))
		return
	}
	if data.Len() > 0 {
		var err error
		terminal, err = processSSEData(ctx, strings.TrimSuffix(data.String(), "\n"), &state, events)
		if err != nil {
			sendStreamError(ctx, events, err)
			return
		}
	}
	if terminal {
		return
	}
	sendStreamError(ctx, events, errors.New("provider stream ended before [DONE]"))
}

func processSSEData(ctx context.Context, data string, state *streamState, events chan Event) (bool, error) {
	data = strings.TrimSpace(data)
	if data == "" {
		return false, nil
	}
	if data == "[DONE]" {
		if err := emitCompletedToolCalls(ctx, state, events); err != nil {
			return false, err
		}
		if !emitEvent(ctx, events, Done{Usage: state.usage}) {
			return false, ctx.Err()
		}
		return true, nil
	}

	var chunk streamChunk
	if err := json.Unmarshal([]byte(data), &chunk); err != nil {
		return false, fmt.Errorf("decode SSE data: %w", err)
	}
	if chunk.Error != nil && chunk.Error.Message != "" {
		return false, fmt.Errorf("provider stream error: %s", chunk.Error.Message)
	}
	if chunk.Usage != nil {
		state.usage = chunk.Usage
	}

	for _, choice := range chunk.Choices {
		if choice.Delta.Content != "" {
			if !emitEvent(ctx, events, TextDelta{Text: choice.Delta.Content}) {
				return false, ctx.Err()
			}
		}
		for _, delta := range choice.Delta.ToolCalls {
			if err := emitToolCallDelta(ctx, state, events, delta); err != nil {
				return false, err
			}
		}
		if choice.Delta.FunctionCall != nil {
			delta := streamToolCall{
				Index:    0,
				Function: *choice.Delta.FunctionCall,
			}
			if err := emitToolCallDelta(ctx, state, events, delta); err != nil {
				return false, err
			}
		}
		if choice.FinishReason != nil && *choice.FinishReason == "tool_calls" {
			if err := emitCompletedToolCalls(ctx, state, events); err != nil {
				return false, err
			}
		}
	}
	return false, nil
}

func emitToolCallDelta(ctx context.Context, state *streamState, events chan Event, delta streamToolCall) error {
	call, ok := state.calls[delta.Index]
	if !ok {
		call = &ToolCall{Index: delta.Index}
		state.calls[delta.Index] = call
	}
	if delta.ID != "" {
		call.ID = delta.ID
	}
	if delta.Function.Name != "" {
		call.Name = delta.Function.Name
	}
	if delta.Function.Arguments != "" {
		call.Arguments += delta.Function.Arguments
	}
	if !emitEvent(ctx, events, ToolCallDelta{
		Index:     delta.Index,
		ID:        delta.ID,
		Name:      delta.Function.Name,
		Arguments: delta.Function.Arguments,
	}) {
		return ctx.Err()
	}
	return nil
}

func emitCompletedToolCalls(ctx context.Context, state *streamState, events chan Event) error {
	indices := make([]int, 0, len(state.calls))
	for index := range state.calls {
		if !state.completed[index] {
			indices = append(indices, index)
		}
	}
	sort.Ints(indices)
	for _, index := range indices {
		if !emitEvent(ctx, events, *state.calls[index]) {
			return ctx.Err()
		}
		state.completed[index] = true
	}
	return nil
}

func emitEvent(ctx context.Context, events chan Event, event Event) bool {
	select {
	case events <- event:
		return true
	case <-ctx.Done():
		return false
	}
}

func sendStreamError(ctx context.Context, events chan Event, err error) {
	if err == nil {
		return
	}
	// A cancellation error is still useful to the Agent. The channel is
	// buffered, so this does not normally block while the consumer unwinds.
	select {
	case events <- Error{Err: err}:
	case <-ctx.Done():
		select {
		case events <- Error{Err: err}:
		default:
		}
	}
}

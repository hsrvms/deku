package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOpenAICompatibleChatStreamsText(t *testing.T) {
	var request chatRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("request method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("request path = %s, want /v1/chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("authorization = %q, want Bearer test-key", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		writeSSE(t, w, `{"id":"1","choices":[{"index":0,"delta":{"role":"assistant","content":"hello"},"finish_reason":null}]}`)
		flusher.Flush()
		writeSSE(t, w, `{"id":"1","choices":[{"index":0,"delta":{"content":" world"},"finish_reason":null}]}`)
		flusher.Flush()
		writeSSE(t, w, `{"id":"1","choices":[],"usage":{"prompt_tokens":12,"completion_tokens":3,"total_tokens":15}}`)
		flusher.Flush()
		writeSSE(t, w, "[DONE]")
		flusher.Flush()
	}))
	defer server.Close()

	client := NewOpenAICompatible(server.URL+"/v1", "test-key")
	tools := []ToolDefinition{{
		Function: FunctionDefinition{
			Name:        "read_file",
			Description: "Read a file.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string"},
				},
			},
		},
	}}
	events, err := client.Chat(
		context.Background(),
		"test-model",
		"You are helpful.",
		[]Message{{Role: RoleUser, Content: "Say hello."}},
		tools,
	)
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	got := collectEvents(t, events)
	if len(got) != 3 {
		t.Fatalf("got %d events, want 3: %#v", len(got), got)
	}
	if text, ok := got[0].(TextDelta); !ok || text.Text != "hello" {
		t.Fatalf("first event = %#v, want TextDelta{Text: hello}", got[0])
	}
	if text, ok := got[1].(TextDelta); !ok || text.Text != " world" {
		t.Fatalf("second event = %#v, want TextDelta{Text: world}", got[1])
	}
	done, ok := got[2].(Done)
	if !ok {
		t.Fatalf("last event = %#v, want Done", got[2])
	}
	if done.Usage == nil || done.Usage.PromptTokens != 12 || done.Usage.CompletionTokens != 3 || done.Usage.TotalTokens != 15 {
		t.Errorf("done usage = %#v, want 12/3/15", done.Usage)
	}

	if request.Model != "test-model" {
		t.Errorf("request model = %q, want test-model", request.Model)
	}
	if !request.Stream {
		t.Error("request stream = false, want true")
	}
	if !request.StreamOptions.IncludeUsage {
		t.Error("request stream_options.include_usage = false, want true")
	}
	if len(request.Messages) != 2 {
		t.Fatalf("request messages = %d, want system and user messages", len(request.Messages))
	}
	if request.Messages[0].Role != RoleSystem || request.Messages[0].Content != "You are helpful." {
		t.Errorf("system message = %#v", request.Messages[0])
	}
	if request.Messages[1].Role != RoleUser || request.Messages[1].Content != "Say hello." {
		t.Errorf("user message = %#v", request.Messages[1])
	}
	if len(request.Tools) != 1 {
		t.Fatalf("request tools = %d, want 1", len(request.Tools))
	}
	if request.Tools[0].Type != "function" || request.Tools[0].Function.Name != "read_file" {
		t.Errorf("request tool = %#v, want function/read_file", request.Tools[0])
	}
}

func TestOpenAICompatibleChatNormalizesToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		writeSSE(t, w, `{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"main.go\""}}]},"finish_reason":null}]}`)
		flusher.Flush()
		writeSSE(t, w, `{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"}"}}]},"finish_reason":null}]}`)
		flusher.Flush()
		writeSSE(t, w, `{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`)
		flusher.Flush()
		writeSSE(t, w, "[DONE]")
		flusher.Flush()
	}))
	defer server.Close()

	events, err := NewOpenAICompatible(server.URL, "test-key").Chat(
		context.Background(),
		"test-model",
		"",
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	got := collectEvents(t, events)
	var deltas []ToolCallDelta
	var calls []ToolCall
	for _, event := range got {
		switch event := event.(type) {
		case ToolCallDelta:
			deltas = append(deltas, event)
		case ToolCall:
			calls = append(calls, event)
		}
	}
	if len(deltas) != 2 {
		t.Fatalf("tool call deltas = %d, want 2: %#v", len(deltas), deltas)
	}
	if len(calls) != 1 {
		t.Fatalf("complete tool calls = %d, want 1: %#v", len(calls), calls)
	}
	if got := calls[0]; got.ID != "call-1" || got.Name != "read_file" || got.Arguments != `{"path":"main.go"}` {
		t.Errorf("complete tool call = %#v", got)
	}
	if _, ok := got[len(got)-1].(Done); !ok {
		t.Errorf("last event = %#v, want Done", got[len(got)-1])
	}
}

func TestOpenAICompatibleChatReportsProviderStreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeSSE(t, w, `{"error":{"message":"upstream failed","type":"server_error"}}`)
	}))
	defer server.Close()

	events, err := NewOpenAICompatible(server.URL, "test-key").Chat(
		context.Background(), "test-model", "", nil, nil,
	)
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	got := collectEvents(t, events)
	if len(got) != 1 {
		t.Fatalf("got %d events, want one Error: %#v", len(got), got)
	}
	providerError, ok := got[0].(Error)
	if !ok {
		t.Fatalf("event = %#v, want Error", got[0])
	}
	if providerError.Err == nil || !strings.Contains(providerError.Err.Error(), "upstream failed") {
		t.Fatalf("error = %#v, want upstream provider error", providerError.Err)
	}
}

func TestOpenAICompatibleChatReportsMalformedStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {not valid json}\n\n"))
	}))
	defer server.Close()

	events, err := NewOpenAICompatible(server.URL, "test-key").Chat(
		context.Background(), "test-model", "", nil, nil,
	)
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	got := collectEvents(t, events)
	if len(got) != 1 {
		t.Fatalf("got %d events, want one Error: %#v", len(got), got)
	}
	providerError, ok := got[0].(Error)
	if !ok {
		t.Fatalf("event = %#v, want Error", got[0])
	}
	if providerError.Err == nil || !strings.Contains(providerError.Err.Error(), "decode SSE data") {
		t.Fatalf("error = %#v, want decode SSE data error", providerError.Err)
	}
}

func TestOpenAICompatibleChatReportsUnexpectedEnd(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[]}\n\n"))
	}))
	defer server.Close()

	events, err := NewOpenAICompatible(server.URL, "test-key").Chat(
		context.Background(), "test-model", "", nil, nil,
	)
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	got := collectEvents(t, events)
	if len(got) != 1 {
		t.Fatalf("got %d events, want one Error: %#v", len(got), got)
	}
	providerError, ok := got[0].(Error)
	if !ok {
		t.Fatalf("event = %#v, want Error", got[0])
	}
	if providerError.Err == nil || !strings.Contains(providerError.Err.Error(), "before [DONE]") {
		t.Fatalf("error = %#v, want unexpected-end error", providerError.Err)
	}
}

func TestOpenAICompatibleChatReturnsHTTPProviderError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"bad key"}}`, http.StatusUnauthorized)
	}))
	defer server.Close()

	events, err := NewOpenAICompatible(server.URL, "test-key").Chat(
		context.Background(), "test-model", "", nil, nil,
	)
	if err == nil {
		t.Fatal("Chat() error = nil, want provider HTTP error")
	}
	if events != nil {
		t.Fatalf("events = %#v, want nil on request error", events)
	}
	if !strings.Contains(err.Error(), "401 Unauthorized") || !strings.Contains(err.Error(), "bad key") {
		t.Fatalf("error = %v, want status and response body", err)
	}
}

func TestOpenAICompatibleChatCancellation(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		writeSSE(t, w, `{"choices":[{"index":0,"delta":{"content":"started"},"finish_reason":null}]}`)
		flusher.Flush()
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err := NewOpenAICompatible(server.URL, "test-key").Chat(
		ctx, "test-model", "", nil, nil,
	)
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	select {
	case event := <-events:
		if text, ok := event.(TextDelta); !ok || text.Text != "started" {
			t.Fatalf("first event = %#v, want started TextDelta", event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first event")
	}
	<-started
	cancel()

	got := collectEvents(t, events)
	if len(got) != 1 {
		t.Fatalf("got %d events after cancellation, want one Error: %#v", len(got), got)
	}
	providerError, ok := got[0].(Error)
	if !ok {
		t.Fatalf("event = %#v, want Error", got[0])
	}
	if !errors.Is(providerError.Err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", providerError.Err)
	}
}

func writeSSE(t *testing.T, w http.ResponseWriter, data string) {
	t.Helper()
	if _, err := w.Write([]byte("data: " + data + "\n\n")); err != nil {
		t.Errorf("write SSE event: %v", err)
	}
}

func collectEvents(t *testing.T, events <-chan Event) []Event {
	t.Helper()
	var got []Event
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return got
			}
			got = append(got, event)
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for provider events: %#v", got)
		}
	}
}

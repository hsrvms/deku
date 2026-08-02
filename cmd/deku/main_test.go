package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestRunStartsConversationAndStreamsResponse(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DEKU_PROVIDER_API_KEY", "test-key")
	t.Setenv("DEKU_PROVIDER_MODEL", "test-model")

	var request struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode provider request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hi there\"},\"finish_reason\":null}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()
	t.Setenv("DEKU_PROVIDER_ENDPOINT", server.URL)

	var stdout, stderr bytes.Buffer
	status := run(nil, strings.NewReader("hello\n"), &stdout, &stderr)
	if status != 0 {
		t.Fatalf("run() status = %d, stderr = %q", status, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Hi there") {
		t.Errorf("stdout = %q, want streamed response", stdout.String())
	}
	if len(request.Messages) != 2 || request.Messages[1].Role != "user" || request.Messages[1].Content != "hello" {
		t.Errorf("provider messages = %#v, want system and user messages", request.Messages)
	}
	if !strings.Contains(stderr.String(), "session") {
		t.Errorf("stderr = %q, want session identifier", stderr.String())
	}

	entries, err := os.ReadDir(home + "/.deku/sessions")
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("session files = %d, want one", len(entries))
	}
}

func TestRunResumesSessionHistory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DEKU_PROVIDER_API_KEY", "test-key")
	t.Setenv("DEKU_PROVIDER_MODEL", "test-model")

	var requests []struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode provider request: %v", err)
		}
		requests = append(requests, request)
		response := "first response"
		if len(requests) > 1 {
			response = "second response"
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":%q},\"finish_reason\":null}]}\n\n", response)
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()
	t.Setenv("DEKU_PROVIDER_ENDPOINT", server.URL)

	var firstOutput, firstErrors bytes.Buffer
	if status := run(nil, strings.NewReader("first request\n"), &firstOutput, &firstErrors); status != 0 {
		t.Fatalf("first run() status = %d, stderr = %q", status, firstErrors.String())
	}
	fields := strings.Fields(firstErrors.String())
	if len(fields) == 0 {
		t.Fatalf("first stderr = %q, want session ID", firstErrors.String())
	}
	sessionID := fields[len(fields)-1]

	var secondOutput, secondErrors bytes.Buffer
	if status := run([]string{"--resume", sessionID}, strings.NewReader("second request\n"), &secondOutput, &secondErrors); status != 0 {
		t.Fatalf("second run() status = %d, stderr = %q", status, secondErrors.String())
	}
	if !strings.Contains(secondOutput.String(), "second response") {
		t.Errorf("second stdout = %q, want resumed response", secondOutput.String())
	}
	if len(requests) != 2 {
		t.Fatalf("provider requests = %d, want 2", len(requests))
	}
	if len(requests[1].Messages) != 4 {
		t.Fatalf("resumed provider messages = %d, want 4 including system prompt", len(requests[1].Messages))
	}
	want := []string{"first request", "first response", "second request"}
	for index, content := range want {
		if requests[1].Messages[index+1].Content != content {
			t.Errorf("resumed message %d = %q, want %q", index, requests[1].Messages[index+1].Content, content)
		}
	}
}

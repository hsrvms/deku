package approval

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// request builds a Decision Request carrying a rendered Command Report, as the
// Agent would after asking the Tool to produce one.
func request(toolName string, declared Tier, report string) Request {
	return Request{ToolName: toolName, Declared: declared, Report: report}
}

func TestPolicyDefaults(t *testing.T) {
	policy := DefaultPolicy()
	if got := policy.Action(Read); got != Auto {
		t.Errorf("Read action = %q, want auto", got)
	}
	if got := policy.Action(Write); got != Prompt {
		t.Errorf("Write action = %q, want prompt", got)
	}
	if got := policy.Action(Destructive); got != Prompt {
		t.Errorf("Destructive action = %q, want prompt", got)
	}
	if got := policy.Action(Tier("unknown")); got != Prompt {
		t.Errorf("unknown tier action = %q, want prompt (fail-safe)", got)
	}
}

func TestPolicyEffectiveTierAppliesPerToolOverride(t *testing.T) {
	policy := NewPolicy(map[string]Tier{"edit": Destructive}, nil)
	if got := policy.EffectiveTier(Write, "edit"); got != Destructive {
		t.Errorf("edit effective tier = %q, want destructive override", got)
	}
	if got := policy.EffectiveTier(Write, "read"); got != Write {
		t.Errorf("read effective tier = %q, want declared write", got)
	}
}

func TestPolicyActionAppliesPerTierOverride(t *testing.T) {
	policy := NewPolicy(nil, map[Tier]Action{Write: Auto, Read: Prompt})
	if got := policy.Action(Write); got != Auto {
		t.Errorf("Write action = %q, want auto override", got)
	}
	if got := policy.Action(Read); got != Prompt {
		t.Errorf("Read action = %q, want prompt override", got)
	}
	if got := policy.Action(Destructive); got != Prompt {
		t.Errorf("Destructive action = %q, want default prompt", got)
	}
}

func TestNewPolicyFromStringsRejectsInvalidValues(t *testing.T) {
	if _, err := NewPolicyFromStrings(map[string]string{"edit": "explode"}, nil); err == nil {
		t.Error("invalid tool tier = nil error, want error")
	}
	if _, err := NewPolicyFromStrings(nil, map[string]string{"write": "sometimes"}); err == nil {
		t.Error("invalid tier action = nil error, want error")
	}
	if _, err := NewPolicyFromStrings(nil, map[string]string{"bogus": "auto"}); err == nil {
		t.Error("unknown tier key = nil error, want error")
	}
}

func TestNewPolicyFromStringsBuildsValidPolicy(t *testing.T) {
	policy, err := NewPolicyFromStrings(
		map[string]string{"edit": "destructive"},
		map[string]string{"write": "auto", "read": "prompt"},
	)
	if err != nil {
		t.Fatalf("NewPolicyFromStrings() error = %v", err)
	}
	if got := policy.EffectiveTier(Write, "edit"); got != Destructive {
		t.Errorf("edit effective tier = %q, want destructive", got)
	}
	if got := policy.Action(Write); got != Auto {
		t.Errorf("Write action = %q, want auto", got)
	}
	if got := policy.Action(Read); got != Prompt {
		t.Errorf("Read action = %q, want prompt", got)
	}
}

func TestGateAutoApprovesReadToolsWhileShowingReport(t *testing.T) {
	var prompt bytes.Buffer
	gate := NewGate(DefaultPolicy(), strings.NewReader(""), &prompt)
	decision, err := gate.Decide(context.Background(), request("read", Read, "Read: pkg/main.go"))
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if !decision.Approved {
		t.Errorf("read decision approved = false, want true")
	}
	if decision.Tier != Read {
		t.Errorf("read decision tier = %q, want read", decision.Tier)
	}
	if prompt.Len() == 0 {
		t.Error("read output = empty, want Command Report shown")
	}
	if !strings.Contains(prompt.String(), "Command Report:") || !strings.Contains(prompt.String(), "Read: pkg/main.go") {
		t.Errorf("read output = %q, want Command Report", prompt.String())
	}
	if strings.Contains(prompt.String(), "Approve?") {
		t.Errorf("read output = %q, want no y/n prompt for an auto-approved tool", prompt.String())
	}
}

func TestGatePromptsAndApprovesWriteToolWithReport(t *testing.T) {
	var prompt bytes.Buffer
	gate := NewGate(DefaultPolicy(), strings.NewReader("y\n"), &prompt)
	decision, err := gate.Decide(context.Background(), request("edit", Write, "Edit: main.go\n  replace \"func main() {}\" with \"func run() {}\""))
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if !decision.Approved {
		t.Errorf("write decision approved = false, want true")
	}
	if decision.Tier != Write {
		t.Errorf("write decision tier = %q, want write", decision.Tier)
	}
	if !strings.Contains(prompt.String(), "Approve?") || !strings.Contains(prompt.String(), "edit") {
		t.Errorf("write prompt = %q, want approval request for edit", prompt.String())
	}
	if !strings.Contains(prompt.String(), "Command Report:") || !strings.Contains(prompt.String(), `replace "func main() {}" with "func run() {}"`) {
		t.Errorf("write prompt = %q, want Command Report before the decision", prompt.String())
	}
}

func TestGatePromptsAndRejectsWriteTool(t *testing.T) {
	var prompt bytes.Buffer
	gate := NewGate(DefaultPolicy(), strings.NewReader("n\n"), &prompt)
	decision, err := gate.Decide(context.Background(), request("edit", Write, "Edit: main.go"))
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if decision.Approved {
		t.Errorf("write decision approved = true, want rejected")
	}
}

func TestGateWarnsAndPromptsDestructiveTool(t *testing.T) {
	var prompt bytes.Buffer
	gate := NewGate(DefaultPolicy(), strings.NewReader("y\n"), &prompt)
	decision, err := gate.Decide(context.Background(), request("command", Destructive, "Run: echo hello"))
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if !decision.Approved || decision.Tier != Destructive {
		t.Errorf("destructive decision = %#v, want approved destructive", decision)
	}
	if !strings.Contains(prompt.String(), "WARNING") {
		t.Errorf("destructive prompt = %q, want warning", prompt.String())
	}
	if !strings.Contains(prompt.String(), "Command Report:") || !strings.Contains(prompt.String(), "Run: echo hello") {
		t.Errorf("destructive prompt = %q, want Command Report", prompt.String())
	}
}

func TestGateRefusesBlankCommandReport(t *testing.T) {
	for _, tc := range []struct {
		name  string
		decl  Tier
		reply string
	}{
		{name: "gated write", decl: Write, reply: "y\n"},
		{name: "auto read", decl: Read, reply: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var prompt bytes.Buffer
			gate := NewGate(DefaultPolicy(), strings.NewReader(tc.reply), &prompt)
			_, err := gate.Decide(context.Background(), request("edit", tc.decl, "   "))
			if err == nil {
				t.Fatal("Decide() error = nil, want refusal for a blank Command Report")
			}
			if prompt.Len() != 0 {
				t.Errorf("output = %q, want no prompt for a refused call", prompt.String())
			}
		})
	}
}

func TestGateRepromptsOnInvalidInput(t *testing.T) {
	var prompt bytes.Buffer
	gate := NewGate(DefaultPolicy(), strings.NewReader("maybe\nyes\n"), &prompt)
	decision, err := gate.Decide(context.Background(), request("edit", Write, "Edit: main.go"))
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if !decision.Approved {
		t.Errorf("decision approved = false, want true after re-prompt")
	}
	if !strings.Contains(prompt.String(), "Please answer y (approve) or n (reject)") {
		t.Errorf("prompt = %q, want re-prompt hint", prompt.String())
	}
}

func TestGateReportsEndOfInput(t *testing.T) {
	var prompt bytes.Buffer
	gate := NewGate(DefaultPolicy(), strings.NewReader(""), &prompt)
	_, err := gate.Decide(context.Background(), request("edit", Write, "Edit: main.go"))
	if err == nil {
		t.Fatal("Decide() error = nil, want end-of-input error")
	}
}

func TestGateHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var prompt bytes.Buffer
	gate := NewGate(DefaultPolicy(), strings.NewReader("y\n"), &prompt)
	_, err := gate.Decide(ctx, request("edit", Write, "Edit: main.go"))
	if err == nil {
		t.Fatal("Decide() error = nil, want cancellation error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want wrapped context.Canceled", err)
	}
}

func TestGateHandlesSequentialPromptsOnSharedReader(t *testing.T) {
	var prompt bytes.Buffer
	gate := NewGate(DefaultPolicy(), strings.NewReader("y\ny\n"), &prompt)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for i := 0; i < 2; i++ {
		decision, err := gate.Decide(ctx, request("edit", Write, "Edit: main.go"))
		if err != nil {
			t.Fatalf("prompt %d: Decide() error = %v", i+1, err)
		}
		if !decision.Approved {
			t.Errorf("prompt %d: approved = false, want true", i+1)
		}
	}
}

func TestGateRejectsUnknownTierAndEmptyToolName(t *testing.T) {
	gate := NewGate(DefaultPolicy(), strings.NewReader("y\n"), &bytes.Buffer{})
	if _, err := gate.Decide(context.Background(), request("edit", Tier("bogus"), "Edit: main.go")); err == nil {
		t.Error("unknown declared tier = nil error, want error")
	}
	if _, err := gate.Decide(context.Background(), request("", Read, "Read: main.go")); err == nil {
		t.Error("empty tool name = nil error, want error")
	}
}

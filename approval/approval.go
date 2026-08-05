// Package approval classifies tool operations and gates execution behind
// synchronous user confirmation. Read tools run without a prompt; Write and
// Destructive tools pause the Agent until the user approves or rejects them.
package approval

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// Tier classifies the side effects a tool may have.
type Tier string

// Tool classification tiers.
const (
	Read        Tier = "read"
	Write       Tier = "write"
	Destructive Tier = "destructive"
)

// Valid reports whether t is a known classification tier.
func (t Tier) Valid() bool {
	switch t {
	case Read, Write, Destructive:
		return true
	default:
		return false
	}
}

// ParseTier converts a configuration value into a Tier. It rejects unknown
// tiers so configuration errors fail fast at startup.
func ParseTier(value string) (Tier, error) {
	tier := Tier(strings.ToLower(strings.TrimSpace(value)))
	if !tier.Valid() {
		return "", fmt.Errorf("unknown approval tier %q (want read, write, or destructive)", value)
	}
	return tier, nil
}

// Action is how a tier is enforced on a tool call.
type Action string

// Enforcement actions.
const (
	// Auto approves a tool call without prompting the user.
	Auto Action = "auto"
	// Prompt pauses the Agent and asks the user to approve or reject.
	Prompt Action = "prompt"
)

// ParseAction converts a configuration value into an Action. It rejects
// unknown actions so configuration errors fail fast at startup.
func ParseAction(value string) (Action, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "auto":
		return Auto, nil
	case "prompt":
		return Prompt, nil
	default:
		return "", fmt.Errorf("unknown approval action %q (want auto or prompt)", value)
	}
}

// defaults are the built-in tier classifications when no override applies.
var defaults = map[Tier]Action{
	Read:        Auto,
	Write:       Prompt,
	Destructive: Prompt,
}

// Policy resolves the effective tier and enforcement action for a tool name
// from per-tool and per-tier overrides. Absent overrides fall back to the
// built-in defaults.
type Policy struct {
	perTool map[string]Tier
	perTier map[Tier]Action
}

// DefaultPolicy returns the built-in policy used when no overrides apply:
// Read tools run unprompted; Write and Destructive tools prompt the user.
func DefaultPolicy() Policy {
	return NewPolicy(nil, nil)
}

// NewPolicy builds a Policy from explicit per-tool tier overrides and per-tier
// action overrides.
func NewPolicy(toolTiers map[string]Tier, tierActions map[Tier]Action) Policy {
	return Policy{perTool: toolTiers, perTier: tierActions}
}

// NewPolicyFromStrings builds a Policy from string overrides, validating every
// value so configuration errors are reported rather than silently ignored.
func NewPolicyFromStrings(toolTiers, tierActions map[string]string) (Policy, error) {
	perTool := make(map[string]Tier, len(toolTiers))
	for name, value := range toolTiers {
		tier, err := ParseTier(value)
		if err != nil {
			return Policy{}, fmt.Errorf("approval.tools.%s: %w", name, err)
		}
		perTool[name] = tier
	}
	perTier := make(map[Tier]Action, len(tierActions))
	for tierName, value := range tierActions {
		tier, err := ParseTier(tierName)
		if err != nil {
			return Policy{}, fmt.Errorf("approval.defaults.%s: %w", tierName, err)
		}
		action, err := ParseAction(value)
		if err != nil {
			return Policy{}, fmt.Errorf("approval.defaults.%s: %w", tierName, err)
		}
		perTier[tier] = action
	}
	return NewPolicy(perTool, perTier), nil
}

// EffectiveTier returns the tier enforced for toolName, applying the per-tool
// override when present and otherwise the tool's declared tier.
func (p Policy) EffectiveTier(declared Tier, toolName string) Tier {
	if override, ok := p.perTool[toolName]; ok {
		return override
	}
	return declared
}

// Action returns how tier is enforced, applying the per-tier override when
// present. Unknown tiers default to Prompt so an unclassified tool is never
// executed silently.
func (p Policy) Action(tier Tier) Action {
	if action, ok := p.perTier[tier]; ok {
		return action
	}
	if action, ok := defaults[tier]; ok {
		return action
	}
	return Prompt
}

// Decision records the outcome of an approval decision for one tool call.
type Decision struct {
	Tier     Tier
	Approved bool
}

// Decider is the synchronous approval gate the Agent relies on. Decide reports
// whether a tool call may proceed; it blocks until the user responds when the
// tool's effective tier requires Approval.
type Decider interface {
	Decide(ctx context.Context, toolName string, declared Tier) (Decision, error)
}

// Gate is the built-in Decider that prompts the user in the terminal.
type Gate struct {
	policy Policy
	input  io.Reader
	output io.Writer
}

// NewGate constructs a Gate that prompts the user. A nil input defaults to
// standard input; a nil output discards the prompt.
func NewGate(policy Policy, input io.Reader, output io.Writer) *Gate {
	if input == nil {
		input = os.Stdin
	}
	if output == nil {
		output = io.Discard
	}
	return &Gate{policy: policy, input: input, output: output}
}

var _ Decider = (*Gate)(nil)

// Decide applies the policy to a tool call and, when the effective tier
// requires prompting, asks the user to approve or reject before returning.
func (g *Gate) Decide(ctx context.Context, toolName string, declared Tier) (Decision, error) {
	if g == nil {
		return Decision{}, errors.New("approval gate is nil")
	}
	if ctx == nil {
		return Decision{}, errors.New("approval context is nil")
	}
	if strings.TrimSpace(toolName) == "" {
		return Decision{}, errors.New("approval tool name is required")
	}
	if !declared.Valid() {
		return Decision{}, fmt.Errorf("tool %q declares unknown tier %q", toolName, declared)
	}
	tier := g.policy.EffectiveTier(declared, toolName)
	if g.policy.Action(tier) == Auto {
		return Decision{Tier: tier, Approved: true}, nil
	}
	return g.prompt(ctx, toolName, tier)
}

// prompt requests a synchronous y/n decision from the user, re-prompting on
// unparseable input and honoring context cancellation.
func (g *Gate) prompt(ctx context.Context, toolName string, tier Tier) (Decision, error) {
	message := g.promptMessage(toolName, tier)
	if _, err := io.WriteString(g.output, message); err != nil {
		return Decision{}, fmt.Errorf("display approval prompt: %w", err)
	}

	lines := make(chan string)
	errs := make(chan error)
	go g.scanLines(ctx, lines, errs)

	for {
		select {
		case line := <-lines:
			switch strings.ToLower(strings.TrimSpace(line)) {
			case "y", "yes":
				return Decision{Tier: tier, Approved: true}, nil
			case "n", "no":
				return Decision{Tier: tier, Approved: false}, nil
			default:
				if _, err := io.WriteString(g.output, "Please answer y (approve) or n (reject): "); err != nil {
					return Decision{}, fmt.Errorf("display approval prompt: %w", err)
				}
			}
		case err, ok := <-errs:
			if !ok {
				return Decision{}, errors.New("approval input ended before a response")
			}
			return Decision{}, err
		case <-ctx.Done():
			return Decision{}, fmt.Errorf("approval: %w", ctx.Err())
		}
	}
}

// scanLines forwards non-empty input lines until the reader ends or the
// context is canceled.
func (g *Gate) scanLines(ctx context.Context, lines chan<- string, errs chan<- error) {
	scanner := bufio.NewScanner(g.input)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		select {
		case lines <- line:
		case <-ctx.Done():
			return
		}
	}
	if err := scanner.Err(); err != nil {
		select {
		case errs <- fmt.Errorf("read approval response: %w", err):
		case <-ctx.Done():
		}
		return
	}
	close(errs)
}

// promptMessage renders the user-facing request for a gated tool call.
func (g *Gate) promptMessage(toolName string, tier Tier) string {
	if tier == Destructive {
		return fmt.Sprintf("WARNING: the %s tool is destructive. Approve? [y/n] ", toolName)
	}
	return fmt.Sprintf("The %s tool is classified as %s. Approve? [y/n] ", toolName, tier)
}

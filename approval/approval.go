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

	"github.com/hsrvms/deku/lineio"
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

// Request describes one gated Tool Call awaiting an Approval decision. It
// carries the Tool name, the tier the Tool declares, and the rendered Command
// Report: the concrete action the call will take (the exact command, the Edit
// changes, or the Write path). Approval is never sought for a call without a
// rendered Report, so a decision is never given blindly.
type Request struct {
	ToolName string
	Declared Tier
	Report   string
}

// Decider is the synchronous approval gate the Agent relies on. Decide reports
// whether a tool call may proceed; it blocks until the user responds when the
// tool's effective tier requires Approval.
type Decider interface {
	Decide(ctx context.Context, request Request) (Decision, error)
}

// Gate is the built-in Decider that prompts the user in the terminal. It holds
// one persistent line reader so buffered input is preserved across prompts and
// no scanner is shared or leaked between Approval decisions.
type Gate struct {
	policy Policy
	input  io.Reader
	output io.Writer
	br     *bufio.Reader
	color  bool
}

// NewGate constructs a Gate that prompts the user. A nil input defaults to
// standard input; a nil output discards the prompt. The Command Report is
// styled with color only when output is a terminal, so captured or piped
// output stays plain.
func NewGate(policy Policy, input io.Reader, output io.Writer) *Gate {
	if input == nil {
		input = os.Stdin
	}
	if output == nil {
		output = io.Discard
	}
	return &Gate{policy: policy, input: input, output: output, color: isTerminal(output)}
}

var _ Decider = (*Gate)(nil)

// Decide applies the policy to a tool call and, when the effective tier
// requires prompting, asks the user to approve or reject before returning. The
// call's Command Report is displayed in both cases; a call without a rendered
// Report is refused rather than approved blindly.
func (g *Gate) Decide(ctx context.Context, request Request) (Decision, error) {
	if g == nil {
		return Decision{}, errors.New("approval gate is nil")
	}
	if ctx == nil {
		return Decision{}, errors.New("approval context is nil")
	}
	if strings.TrimSpace(request.ToolName) == "" {
		return Decision{}, errors.New("approval tool name is required")
	}
	if !request.Declared.Valid() {
		return Decision{}, fmt.Errorf("tool %q declares unknown tier %q", request.ToolName, request.Declared)
	}
	if strings.TrimSpace(request.Report) == "" {
		return Decision{}, fmt.Errorf("tool %q has no Command Report; refusing to approve blindly", request.ToolName)
	}
	tier := g.policy.EffectiveTier(request.Declared, request.ToolName)
	if g.policy.Action(tier) == Auto {
		if err := g.displayReport(request.ToolName, tier, request.Report); err != nil {
			return Decision{}, err
		}
		return Decision{Tier: tier, Approved: true}, nil
	}
	return g.prompt(ctx, request, tier)
}

// prompt requests a synchronous y/n decision from the user, re-prompting on
// unparseable input and honoring context cancellation.
func (g *Gate) prompt(ctx context.Context, request Request, tier Tier) (Decision, error) {
	message := g.promptMessage(request, tier)
	if _, err := io.WriteString(g.output, message); err != nil {
		return Decision{}, fmt.Errorf("display approval prompt: %w", err)
	}

	for {
		line, err := g.readLine(ctx)
		if err != nil {
			return Decision{}, err
		}
		switch strings.ToLower(line) {
		case "y", "yes":
			return Decision{Tier: tier, Approved: true}, nil
		case "n", "no":
			return Decision{Tier: tier, Approved: false}, nil
		default:
			if _, err := io.WriteString(g.output, "Please answer y (approve) or n (reject): "); err != nil {
				return Decision{}, fmt.Errorf("display approval prompt: %w", err)
			}
		}
	}
}

// reader returns the Gate's persistent line reader, reusing a shared reader
// when one was supplied so the caller and the Gate never double-buffer input.
func (g *Gate) reader() *bufio.Reader {
	if g.br == nil {
		if shared, ok := g.input.(*bufio.Reader); ok {
			g.br = shared
		} else {
			g.br = bufio.NewReader(g.input)
		}
	}
	return g.br
}

// readLine reads the next non-empty line from the persistent reader, returning
// when a line is available or the context is canceled. The read happens in a
// short-lived goroutine that exits after one line, so a cancelled prompt never
// leaves a reader permanently consuming input.
func (g *Gate) readLine(ctx context.Context) (string, error) {
	type result struct {
		line string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		line, err := lineio.Scan(g.reader())
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

// displayReport shows the Command Report of an auto-approved call without a
// decision prompt, so Read Tools stay efficient without losing visibility.
func (g *Gate) displayReport(toolName string, tier Tier, report string) error {
	if _, err := io.WriteString(g.output, g.reportMessage(toolName, tier, report)); err != nil {
		return fmt.Errorf("display command report: %w", err)
	}
	return nil
}

// promptMessage renders the user-facing request for a gated tool call: the
// tier line, the Command Report, and the y/n decision.
func (g *Gate) promptMessage(request Request, tier Tier) string {
	var builder strings.Builder
	if tier == Destructive {
		if g.color {
			builder.WriteString(ansiRed)
			builder.WriteString(ansiBold)
		}
		builder.WriteString("WARNING: ")
		if g.color {
			builder.WriteString(ansiReset)
		}
	}
	builder.WriteString(g.reportMessage(request.ToolName, tier, request.Report))
	if g.color {
		builder.WriteString(ansiBold)
	}
	builder.WriteString("Approve? [y/n] ")
	if g.color {
		builder.WriteString(ansiReset)
	}
	return builder.String()
}

// reportMessage renders the Command Report block for a tool call: the tool
// name and its effective tier, followed by the indented Report lines. When the
// output is a terminal, the header is colored and diff lines are red for
// removals (-) and green for additions (+), so a multi-line Report scans like
// code instead of raw text.
func (g *Gate) reportMessage(toolName string, tier Tier, report string) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "The %s tool is classified as %s.\n", toolName, tier)
	if g.color {
		builder.WriteString(ansiCyan)
	}
	builder.WriteString("Command Report:")
	if g.color {
		builder.WriteString(ansiReset)
	}
	builder.WriteByte('\n')
	for _, line := range strings.Split(strings.TrimSuffix(report, "\n"), "\n") {
		builder.WriteString("  ")
		if g.color {
			switch {
			case strings.HasPrefix(line, "-"):
				builder.WriteString(ansiRed)
			case strings.HasPrefix(line, "+"):
				builder.WriteString(ansiGreen)
			}
		}
		builder.WriteString(line)
		if g.color {
			builder.WriteString(ansiReset)
		}
		builder.WriteByte('\n')
	}
	return builder.String()
}

// ANSI escape sequences styling the Command Report when output is a terminal.
// Reports stay plain when output is captured or piped.
const (
	ansiReset = "\x1b[0m"
	ansiBold  = "\x1b[1m"
	ansiRed   = "\x1b[31m"
	ansiGreen = "\x1b[32m"
	ansiCyan  = "\x1b[36m"
)

// isTerminal reports whether w is a terminal character device. The Gate styles
// its Command Report only for a terminal so captured or piped output stays
// plain.
func isTerminal(w io.Writer) bool {
	file, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

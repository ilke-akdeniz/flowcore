package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/mike-akdeniz/flowcore"
)

// ClaudeChecker asks a model to decide an agent step.
//
// This file is the only place in the application that knows a model exists.
// FlowCore never calls one, and structurally cannot: it holds an opaque subject
// reference and not the subject, so it could not build this prompt without either
// storing releases itself or calling back into this application. Both would
// invert the relationship between a library and its caller.
//
// What crosses back into the library is the same thing a human's click produces —
// an action id, a completer, and a remark.
type ClaudeChecker struct {
	client anthropic.Client
	model  string
}

func NewClaudeChecker() *ClaudeChecker {
	return &ClaudeChecker{client: anthropic.NewClient(), model: "claude-opus-5"}
}

func (c *ClaudeChecker) Mode() string { return "claude (" + c.model + ")" }

// instructions are per-agent. The agent reference is an opaque string to
// FlowCore; here it is the key that selects a job.
var instructions = map[string]string{
	"agent:diff-risk@v1": `You assess the deployment risk of a software release from its metadata.

High risk means the change touches authentication, session handling, data migration,
or is otherwise not safely reversible. Everything else is low risk. Size alone is not
risk: a large additive change is low risk, and a small change to an auth path is not.`,

	"agent:changelog@v1": `You check whether a release's changelog honestly describes what the release does.

It is accurate when a user reading only the changelog would not be surprised by the
deploy. It is a mismatch when the changelog omits or understates something users will
notice — especially anything described as internal that has user-visible effects.`,
}

func (c *ClaudeChecker) Check(ctx context.Context, request CheckRequest) (Verdict, error) {
	instruction, ok := instructions[request.Agent]
	if !ok {
		return Verdict{}, fmt.Errorf("no instructions for %s", request.Agent)
	}

	names := make([]string, 0, len(request.Actions))
	for _, action := range request.Actions {
		names = append(names, action.Name)
	}

	// The available actions come from FlowCore, so the model is choosing from the
	// workflow as it was defined — not from a list hard-coded here that could
	// drift away from the definition.
	system := instruction + "\n\nReply in exactly this form and nothing else:\n\n" +
		"ACTION: <one of: " + strings.Join(names, ", ") + ">\n" +
		"FINDING: <two or three sentences: what you found, or that you found nothing>"

	response, err := c.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     c.model,
		MaxTokens: 1024,
		System:    []anthropic.TextBlockParam{{Text: system}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(describe(request.Release))),
		},
	})
	if err != nil {
		return Verdict{}, fmt.Errorf("asking %s: %w", c.model, err)
	}

	var text strings.Builder
	for _, block := range response.Content {
		if paragraph, ok := block.AsAny().(anthropic.TextBlock); ok {
			text.WriteString(paragraph.Text)
		}
	}

	return parseVerdict(text.String(), request.Actions)
}

func describe(release Release) string {
	return fmt.Sprintf(
		"Version: %s\nCommit: %s\nTitle: %s\nChangelog: %s\nDiff: %s",
		release.Version, release.Commit, release.Title, release.Changelog, release.DiffStat)
}

// parseVerdict reads the model's answer and checks it against what the step
// actually offers.
//
// The validation is not defensive clutter: a model asked to pick from a list will
// occasionally return something adjacent, and the alternative to catching it here
// is a foreign-key violation from the database with nothing explaining why.
func parseVerdict(text string, actions []flowcore.Action) (Verdict, error) {
	var chosen, finding string

	for _, line := range strings.Split(text, "\n") {
		switch {
		case strings.HasPrefix(line, "ACTION:"):
			chosen = strings.TrimSpace(strings.TrimPrefix(line, "ACTION:"))
		case strings.HasPrefix(line, "FINDING:"):
			finding = strings.TrimSpace(strings.TrimPrefix(line, "FINDING:"))
		}
	}

	if chosen == "" {
		return Verdict{}, fmt.Errorf("no ACTION line in the reply: %q", truncate(text, 200))
	}

	actionID, err := actionNamed(actions, chosen)
	if err != nil {
		return Verdict{}, err
	}

	return Verdict{ActionID: actionID, Remark: finding}, nil
}

func truncate(text string, limit int) string {
	if len(text) <= limit {
		return text
	}

	return text[:limit] + "…"
}

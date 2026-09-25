package app

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mike-akdeniz/flowcore"
)

// App is the client: everything on this side of the boundary, holding the two
// FlowCore entry points it calls.
//
// Catalog and Engine are the entire library surface. Everything else in this
// package — sessions, the subject store, seeding, later the agent dispatcher —
// exists because FlowCore deliberately does none of it.
type App struct {
	Config   Config
	Catalog  *flowcore.Catalog
	Engine   *flowcore.Engine
	Sessions *SessionStore
	// Dispatcher runs agent steps off the web request. Set by New.
	Dispatcher *Dispatcher
}

func New(config Config, pool *pgxpool.Pool, logger *slog.Logger) *App {
	application := &App{
		Config:   config,
		Catalog:  flowcore.NewCatalog(pool),
		Engine:   flowcore.NewEngine(pool),
		Sessions: NewSessionStore(),
	}
	application.Dispatcher = NewDispatcher(application, chooseChecker(logger), logger)

	return application
}

// chooseChecker decides whether agent steps consult a model or a script.
//
// Detect and switch, per decision 5: with a key the findings are real, without
// one they are pre-written against the seeded releases. Everything else on the
// path — the queue, the worker, the CompleteStep call, the remark on the visit —
// is identical, so someone who clones this with nothing configured still sees the
// whole application work.
func chooseChecker(logger *slog.Logger) Checker {
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		logger.Info("agent steps use canned findings",
			"reason", "ANTHROPIC_API_KEY is not set")

		return CannedChecker{}
	}

	checker := NewClaudeChecker()
	logger.Info("agent steps call a model", "mode", checker.Mode())

	return checker
}

// AgentReferences are the assignees this application dispatches automatically.
//
// Derived from the seeded definition rather than listed separately, so the two
// cannot drift. Once a visitor can define their own steps, this has to collect
// agent assignees from their definitions too.
func (a *App) AgentReferences() []string {
	var references []string

	for _, step := range releaseApprovalDefinition().Steps {
		if IsAgent(step.AssigneeID) {
			references = append(references, step.AssigneeID)
		}
	}

	return references
}

// StartJanitor expires idle sessions and deletes the definitions they created.
// It does nothing when the TTL is zero, which is the default, so running locally
// never loses work you built.
func (a *App) StartJanitor(ctx context.Context, logger *slog.Logger) {
	if a.Config.SessionTTL == 0 {
		logger.Info("session janitor disabled", "reason", "CLIENT_SESSION_TTL is 0")

		return
	}

	go func() {
		ticker := time.NewTicker(a.Config.SessionTTL / 4)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				a.sweep(ctx, logger)
			}
		}
	}()
}

// sweep deletes the definitions of expired sessions. FlowCore's cascades remove
// each definition's statuses, steps and actions; the runs started from it are
// instance-side and go with them.
func (a *App) sweep(ctx context.Context, logger *slog.Logger) {
	for _, session := range a.Sessions.Expired(a.Config.SessionTTL) {
		for _, definitionID := range session.DefinitionIDs {
			if err := a.Catalog.DeleteWorkflowDefinition(ctx, definitionID); err != nil {
				logger.Warn("expiring session", "session", session.ID, "definition", definitionID, "err", err)
			}
		}

		logger.Info("session expired", "session", session.ID, "definitions", len(session.DefinitionIDs))
	}
}

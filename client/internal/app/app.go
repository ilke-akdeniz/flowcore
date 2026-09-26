package app

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mike-akdeniz/flowcore"
	"github.com/mike-akdeniz/flowcore/client/internal/store"
)

// App is the client: everything on this side of the boundary, holding the two
// FlowCore entry points it calls.
//
// Catalog and Engine are the entire library surface. Everything else in this
// package — sessions, the subject store, seeding, later the agent dispatcher —
// exists because FlowCore deliberately does none of it.
type App struct {
	Config  Config
	Catalog *flowcore.Catalog
	Engine  *flowcore.Engine
	// Store is the console's own data — submissions, documents, the roster, and
	// which workflow serves which kind of submission. None of it is the library's.
	Store *store.Store
	// Dispatcher runs agent steps off the web request. Set by New.
	Dispatcher *Dispatcher
}

func New(config Config, pool *pgxpool.Pool, logger *slog.Logger) *App {
	application := &App{
		Config:  config,
		Catalog: flowcore.NewCatalog(pool),
		Engine:  flowcore.NewEngine(pool),
		Store:   store.New(pool),
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

	for _, definition := range seededDefinitions() {
		for _, step := range definition.Steps {
			if IsAgent(step.AssigneeID) {
				references = append(references, step.AssigneeID)
			}
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
	// Read the registry before the session row goes: deleting it cascades through
	// the console's tables, and the definition ids would go with them.
	expiring, err := a.expiringDefinitions(ctx)
	if err != nil {
		logger.Warn("sweep", "err", err)

		return
	}

	for sessionID, definitionIDs := range expiring {
		for _, definitionID := range definitionIDs {
			if err := a.Catalog.DeleteWorkflowDefinition(ctx, definitionID); err != nil {
				logger.Warn("expiring session", "session", sessionID, "definition", definitionID, "err", err)
			}
		}

		logger.Info("session expired", "session", sessionID, "definitions", len(definitionIDs))
	}
}

func (a *App) expiringDefinitions(ctx context.Context) (map[string][]uuid.UUID, error) {
	sessions, err := a.Store.ExpiredSessions(ctx, a.Config.SessionTTL)
	if err != nil {
		return nil, err
	}

	expiring := make(map[string][]uuid.UUID, len(sessions))
	for _, sessionID := range sessions {
		registered, err := a.Store.RegisteredWorkflows(ctx, sessionID)
		if err != nil {
			return nil, err
		}

		for _, workflow := range registered {
			expiring[sessionID] = append(expiring[sessionID], workflow.FlowcoreDefinitionID)
		}
	}

	return expiring, nil
}

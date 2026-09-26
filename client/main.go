// Command client is FlowCore's reference client: a working application built on
// the library, meant to be run, read, lifted from, or forked as the starting
// point for a real one.
//
// It exists because a boundary is invisible from one side. FlowCore's central
// claims — that references are opaque, that the library never calls a model,
// that the caller owns dispatch, that subjects live elsewhere — cannot be read
// from the API alone. Each becomes legible only when something is shown doing
// the other half, and this is that something.
//
//	docker compose up -d
//	go run .
//
// See README.md.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mike-akdeniz/flowcore"
	"github.com/mike-akdeniz/flowcore/client/internal/api"
	"github.com/mike-akdeniz/flowcore/client/internal/app"
	"github.com/mike-akdeniz/flowcore/client/internal/store"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	if err := run(logger); err != nil {
		logger.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	config, err := app.LoadConfig()
	if err != nil {
		return err
	}

	pool, err := pgxpool.New(ctx, config.DatabaseURL)
	if err != nil {
		return err
	}

	defer pool.Close()

	// The library ships its own schema and applies it, so the client needs no
	// migration tool of its own and no SQL file to keep in step.
	if err := flowcore.Migrate(ctx, pool); err != nil {
		return err
	}

	// The console's own schema, in its own Postgres schema with its own migration
	// history. Two independent sets of tables in one database.
	if err := store.Migrate(ctx, pool); err != nil {
		return err
	}

	logger.Info("schema applied", "schemas", "flowcore, console")

	application := app.New(config, pool, logger)

	// The cast is shared across every session, so it is seeded once rather than
	// copied per visitor. Idempotent, so it runs on every boot.
	if err := application.Store.SeedStaff(ctx); err != nil {
		return err
	}

	application.StartJanitor(ctx, logger)
	application.Dispatcher.Start(ctx)

	built, err := assets()
	if err != nil {
		return err
	}

	server := api.NewServer(application, logger, built)

	httpServer := &http.Server{
		Addr:              config.Addr,
		Handler:           server.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			logger.Warn("shutdown", "err", err)
		}
	}()

	logger.Info("listening", "addr", config.Addr)

	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	return nil
}

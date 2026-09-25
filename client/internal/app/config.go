// Package app is the client half of the boundary: everything FlowCore
// deliberately does not do. Identity resolution, subject storage, session
// scoping, and (later) dispatching agent steps all live here.
//
// Nothing in this package is part of the library. That is the point — reading it
// beside the flowcore calls it makes is what shows where the line falls.
package app

import (
	"fmt"
	"os"
	"time"
)

// Config is the client's whole configuration surface.
type Config struct {
	DatabaseURL string
	Addr        string
	// SessionTTL is how long an idle session survives. Zero means never expire
	// and no janitor runs, which is the default so that running locally never
	// loses work you built. It is set when deploying publicly, where sessions
	// accumulate from strangers.
	//
	// Defaulting to "never" fails in the safe direction: a forgotten setting on a
	// deploy grows the database slowly, where the reverse deletes a local user's
	// workflows overnight.
	SessionTTL time.Duration
}

// LoadConfig reads the environment, falling back to values that work against the
// docker-compose Postgres beside this file.
func LoadConfig() (Config, error) {
	config := Config{
		DatabaseURL: environmentOr("CLIENT_DATABASE_URL",
			"postgres://flowcore:flowcore@localhost:5433/flowcore_client?sslmode=disable"),
		Addr: environmentOr("CLIENT_ADDR", ":8080"),
	}

	raw := environmentOr("CLIENT_SESSION_TTL", "0")
	if raw != "0" {
		ttl, err := time.ParseDuration(raw)
		if err != nil {
			return Config{}, fmt.Errorf("CLIENT_SESSION_TTL: %w", err)
		}

		config.SessionTTL = ttl
	}

	return config, nil
}

func environmentOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}

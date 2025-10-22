package interfaces

import (
	"context"
)

// EventSource abstracts a concrete provider of events (built-in or external).
// Implementations should be safe to Start once and Stop once; Events() must
// return a stable channel for the lifetime of the source.
type EventSource interface {
	Start(ctx context.Context) error
	Events() <-chan Event
	Stop() error
	Name() string
}

package event

import (
	"context"
	"testing"
	"time"

	"github.com/kagent-dev/khook/internal/interfaces"
)

type stubSource struct {
	name string
	ch   chan interfaces.Event
}

func (s *stubSource) Start(ctx context.Context) error { return nil }
func (s *stubSource) Events() <-chan interfaces.Event { return s.ch }
func (s *stubSource) Stop() error                     { close(s.ch); return nil }
func (s *stubSource) Name() string                    { return s.name }

func TestMultiplexerFanIn(t *testing.T) {
	s1 := &stubSource{name: "s1", ch: make(chan interfaces.Event, 1)}
	s2 := &stubSource{name: "s2", ch: make(chan interfaces.Event, 1)}
	mux := NewMultiplexer([]interfaces.EventSource{s1, s2})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out, err := mux.WatchEvents(ctx)
	if err != nil {
		t.Fatalf("WatchEvents error: %v", err)
	}

	e1 := interfaces.Event{Type: "pod-restart", ResourceName: "a"}
	e2 := interfaces.Event{Type: "probe-failed", ResourceName: "b"}
	s1.ch <- e1
	s2.ch <- e2

	got1 := <-out
	got2 := <-out
	if got1.Type == got2.Type && got1.ResourceName == got2.ResourceName {
		t.Fatalf("expected two distinct events")
	}

	// ensure shutdown OK
	cancel()
	time.Sleep(50 * time.Millisecond)
}

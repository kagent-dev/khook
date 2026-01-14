package event

import (
	"context"
	"sync"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/log"

	v1alpha2 "github.com/kagent-dev/khook/api/v1alpha2"
	"github.com/kagent-dev/khook/internal/interfaces"
)

// Multiplexer aggregates multiple EventSources and exposes them as a single
// EventWatcher-compatible stream. Filtering remains in the processor.
type Multiplexer struct {
	sources []interfaces.EventSource
	outCh   chan interfaces.Event
	cancel  context.CancelFunc
	logger  logr.Logger
}

func NewMultiplexer(sources []interfaces.EventSource) *Multiplexer {
	return &Multiplexer{
		sources: sources,
		outCh:   make(chan interfaces.Event, 256),
		logger:  log.Log.WithName("event-multiplexer"),
	}
}

// Start all sources and begin fan-in.
func (m *Multiplexer) Start(ctx context.Context) error {
	if m.cancel != nil {
		return nil
	}
	ctxRun, cancel := context.WithCancel(ctx)
	m.cancel = cancel

	var wg sync.WaitGroup
	wg.Add(len(m.sources))
	for _, src := range m.sources {
		s := src
		go func() {
			defer wg.Done()
			if err := s.Start(ctxRun); err != nil {
				m.logger.Error(err, "failed to start event source", "source", s.Name())
				return
			}
			for {
				select {
				case <-ctxRun.Done():
					return
				case ev, ok := <-s.Events():
					if !ok {
						return
					}
					select {
					case m.outCh <- ev:
					case <-ctxRun.Done():
						return
					}
				}
			}
		}()
	}

	// Close outCh when all readers exit
	go func() {
		wg.Wait()
		close(m.outCh)
	}()
	return nil
}

// Stop all sources.
func (m *Multiplexer) Stop() error {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	for _, s := range m.sources {
		_ = s.Stop()
	}
	return nil
}

// WatchEvents exposes the multiplexed channel. Compatible with EventWatcher.
func (m *Multiplexer) WatchEvents(ctx context.Context) (<-chan interfaces.Event, error) {
	if err := m.Start(ctx); err != nil {
		return nil, err
	}
	return m.outCh, nil
}

// FilterEvent is a passthrough; filtering is handled in the processor.
func (m *Multiplexer) FilterEvent(event interfaces.Event, hooks []*v1alpha2.Hook) []interfaces.EventMatch {
	return nil
}

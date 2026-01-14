package event

import (
	"context"
	"crypto/tls"
	"io"
	"testing"
	"time"

	v1alpha2 "github.com/kagent-dev/khook/api/v1alpha2"
	"github.com/kagent-dev/khook/internal/interfaces"
)

type fakeDialer struct {
	events []interfaces.Event
}

func (f *fakeDialer) Stream(ctx context.Context, address string, _ *tls.Config, _ string, _ string, onEvent func(e interfaces.Event)) error {
	for _, e := range f.events {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		onEvent(e)
	}
	// simulate end of stream
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(10 * time.Millisecond):
		return io.EOF
	}
}

func TestExternalGRPCEventSourceStreams(t *testing.T) {
	spec := v1alpha2.ExternalEventSourceSpec{Endpoint: v1alpha2.ExternalEndpoint{Address: "example:1234"}}
	events := []interfaces.Event{{Type: "pod-restart"}, {Type: "probe-failed"}}
	src := NewExternalGRPCEventSource("ext", "default", spec, &fakeDialer{events: events})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := src.Start(ctx); err != nil {
		t.Fatalf("start error: %v", err)
	}
	got := []interfaces.Event{}
	for i := 0; i < 2; i++ {
		select {
		case e := <-src.Events():
			got = append(got, e)
		case <-time.After(time.Second):
			t.Fatal("timeout")
		}
	}
	if got[0].Type == got[1].Type {
		t.Fatalf("expected distinct events")
	}
}

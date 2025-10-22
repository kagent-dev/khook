package event

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"time"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/log"

	v1alpha2 "github.com/kagent-dev/khook/api/v1alpha2"
	"github.com/kagent-dev/khook/internal/interfaces"
)

// ExternalGRPCEventSource streams events from an external provider via gRPC.
// NOTE: The actual gRPC client code is intentionally abstracted behind a dialer
// to avoid adding heavy deps until wired up.
type ExternalGRPCEventSource struct {
	name      string
	namespace string
	spec      v1alpha2.ExternalEventSourceSpec
	outCh     chan interfaces.Event
	cancel    context.CancelFunc
	logger    logr.Logger
	dialer    GRPCDialer
}

type GRPCDialer interface {
	// Stream connects and streams events until EOF or context cancel.
	// config is an opaque JSON string containing provider-specific configuration.
	Stream(ctx context.Context, address string, tlsConfig *tls.Config, authHeader string, config string, onEvent func(e interfaces.Event)) error
}

func NewExternalGRPCEventSource(name, namespace string, spec v1alpha2.ExternalEventSourceSpec, dialer GRPCDialer) *ExternalGRPCEventSource {
	return &ExternalGRPCEventSource{
		name:      name,
		namespace: namespace,
		spec:      spec,
		outCh:     make(chan interfaces.Event, 256),
		logger:    log.Log.WithName("external-grpc-source").WithValues("name", name, "namespace", namespace),
		dialer:    dialer,
	}
}

func (s *ExternalGRPCEventSource) Name() string                    { return fmt.Sprintf("external-%s", s.name) }
func (s *ExternalGRPCEventSource) Events() <-chan interfaces.Event { return s.outCh }

func (s *ExternalGRPCEventSource) Start(ctx context.Context) error {
	if s.cancel != nil {
		return nil
	}
	ctxRun, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	if s.dialer == nil {
		s.logger.Info("No gRPC dialer configured; external source disabled", "source", s.name)
		go func() {
			<-ctxRun.Done()
			close(s.outCh)
		}()
		return nil
	}
	go s.run(ctxRun)
	return nil
}

func (s *ExternalGRPCEventSource) Stop() error {
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	return nil
}

func (s *ExternalGRPCEventSource) run(ctx context.Context) {
	defer close(s.outCh)

	backoff := time.Second * 2
	maxBackoff := time.Second * 60
	if s.spec.Connection != nil && s.spec.Connection.ReconnectBackoff != nil {
		if s.spec.Connection.ReconnectBackoff.InitialSeconds > 0 {
			backoff = time.Duration(s.spec.Connection.ReconnectBackoff.InitialSeconds) * time.Second
		}
		if s.spec.Connection.ReconnectBackoff.MaxSeconds > 0 {
			maxBackoff = time.Duration(s.spec.Connection.ReconnectBackoff.MaxSeconds) * time.Second
		}
	}

	for {
		if err := s.connectAndStream(ctx); err != nil {
			if err == io.EOF || ctx.Err() != nil {
				return
			}
			s.logger.Error(err, "stream error, will reconnect")
		}
		// backoff
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		// exponential growth up to max
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

func (s *ExternalGRPCEventSource) connectAndStream(ctx context.Context) error {
	// Defensive nil checks
	if s == nil {
		panic("ExternalGRPCEventSource is nil - this should never happen")
	}
	if s.dialer == nil {
		return fmt.Errorf("dialer is nil")
	}
	if s.spec.Endpoint.Address == "" {
		return fmt.Errorf("endpoint address is empty")
	}

	tlsConfig, err := s.buildTLS()
	if err != nil {
		return err
	}
	authHeader := s.buildAuth()

	// Extract config JSON from the ExternalEventSource spec
	var configJSON string
	if s.spec.Config != nil {
		configJSON = s.spec.Config.JSON
	}

	return s.dialer.Stream(ctx, s.spec.Endpoint.Address, tlsConfig, authHeader, configJSON, func(e interfaces.Event) {
		select {
		case s.outCh <- e:
		case <-ctx.Done():
		}
	})
}

func (s *ExternalGRPCEventSource) buildTLS() (*tls.Config, error) {
	if s.spec.Endpoint.TLS == nil || !s.spec.Endpoint.TLS.Enabled {
		return nil, nil
	}
	// Controller will provide CA/cert/key via mounted secrets; wiring omitted here.
	if s.spec.Endpoint.TLS.SecretRef == nil {
		return nil, fmt.Errorf("tls.enabled is true but secretRef is nil")
	}
	// Placeholder: empty pool/cert to satisfy signature; real impl will load from filesystem/secret.
	pool := x509.NewCertPool()
	_ = pool
	return &tls.Config{MinVersion: tls.VersionTLS12}, nil
}

func (s *ExternalGRPCEventSource) buildAuth() string {
	if s.spec.Endpoint.Auth == nil {
		return ""
	}
	switch s.spec.Endpoint.Auth.Type {
	case "BearerToken":
		if s.spec.Endpoint.Auth.SecretRef != nil {
			return "Bearer <token>" // actual token retrieval omitted
		}
	case "Basic":
		if s.spec.Endpoint.Auth.SecretRef != nil {
			return "Basic <creds>"
		}
	}
	return ""
}

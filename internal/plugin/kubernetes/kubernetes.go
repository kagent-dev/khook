package kubernetes

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	eventsv1 "k8s.io/api/events/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kagent-dev/khook/internal/plugin"
)

// KubernetesEventSource implements the EventSource interface for Kubernetes events
type KubernetesEventSource struct {
	client    kubernetes.Interface
	namespace string
	logger    logr.Logger
	stopCh    chan struct{}
	eventCh   chan plugin.Event
	ctx       context.Context
	cancel    context.CancelFunc
}

// NewKubernetesEventSource creates a new Kubernetes event source
func NewKubernetesEventSource() plugin.EventSource {
	return &KubernetesEventSource{
		logger:  log.Log.WithName("kubernetes-plugin"),
		stopCh:  make(chan struct{}),
		eventCh: make(chan plugin.Event, 100),
	}
}

// Name returns the name of the event source
func (k *KubernetesEventSource) Name() string {
	return "kubernetes"
}

// Version returns the version of the event source
func (k *KubernetesEventSource) Version() string {
	return "1.0.0"
}

// Initialize sets up the Kubernetes event source with configuration
func (k *KubernetesEventSource) Initialize(ctx context.Context, config map[string]interface{}) error {
	k.logger.Info("Initializing Kubernetes event source", "config", config)

	// Extract namespace from config, default to "default"
	namespace := "default"
	if ns, ok := config["namespace"].(string); ok && ns != "" {
		namespace = ns
	}

	// Validate namespace
	if err := k.validateNamespace(namespace); err != nil {
		return fmt.Errorf("invalid namespace: %w", err)
	}

	k.namespace = namespace

	// Create Kubernetes client
	var client kubernetes.Interface
	if clientInterface, ok := config["client"]; ok {
		if kubeClient, ok := clientInterface.(kubernetes.Interface); ok {
			client = kubeClient
		} else {
			return fmt.Errorf("provided client is not a kubernetes.Interface")
		}
	} else {
		// Create client from in-cluster config or kubeconfig
		config, err := rest.InClusterConfig()
		if err != nil {
			return fmt.Errorf("failed to get in-cluster config: %w", err)
		}

		client, err = kubernetes.NewForConfig(config)
		if err != nil {
			return fmt.Errorf("failed to create Kubernetes client: %w", err)
		}
	}

	k.client = client
	k.ctx, k.cancel = context.WithCancel(ctx)

	k.logger.Info("Successfully initialized Kubernetes event source", "namespace", k.namespace)
	return nil
}

// WatchEvents returns a channel of events from Kubernetes
func (k *KubernetesEventSource) WatchEvents(ctx context.Context) (<-chan plugin.Event, error) {
	if k.client == nil {
		return nil, fmt.Errorf("event source not initialized")
	}

	k.logger.Info("Starting Kubernetes event watching", "namespace", k.namespace)

	// Create a field selector to watch for events
	fieldSelector := fields.Everything()

	// Create a watch for events using the events.k8s.io/v1 API
	watchlist := metav1.ListOptions{
		FieldSelector: fieldSelector.String(),
	}

	k.logger.V(1).Info("Creating EventsV1 watcher", "fieldSelector", fieldSelector.String(), "namespace", k.namespace)
	watcher, err := k.client.EventsV1().Events(k.namespace).Watch(ctx, watchlist)
	if err != nil {
		return nil, fmt.Errorf("failed to create event watcher: %w", err)
	}
	k.logger.Info("EventsV1 watcher established", "namespace", k.namespace)

	go func() {
		defer watcher.Stop()
		defer close(k.eventCh)

		for {
			select {
			case <-ctx.Done():
				k.logger.Info("Context cancelled, stopping Kubernetes event watcher")
				return
			case <-k.stopCh:
				k.logger.Info("Stop signal received, stopping Kubernetes event watcher")
				return
			case event, ok := <-watcher.ResultChan():
				if !ok {
					k.logger.Info("Kubernetes event watcher channel closed")
					return
				}

				if event.Type == watch.Added || event.Type == watch.Modified {
					if k8sEvent, ok := event.Object.(*eventsv1.Event); ok {
						k.logger.V(2).Info("Received Kubernetes event",
							"watchType", event.Type,
							"namespace", k8sEvent.Namespace,
							"regarding.kind", k8sEvent.Regarding.Kind,
							"regarding.name", k8sEvent.Regarding.Name,
							"reason", k8sEvent.Reason,
							"type", k8sEvent.Type,
							"note", k8sEvent.Note)

						// Staleness filter: ignore events older than 15 minutes without recent occurrence
						cutoff := time.Now().Add(-15 * time.Minute)
						lastTime := k8sEvent.CreationTimestamp.Time
						if !k8sEvent.EventTime.IsZero() {
							lastTime = k8sEvent.EventTime.Time
						}
						if k8sEvent.Series != nil && !k8sEvent.Series.LastObservedTime.IsZero() {
							lastTime = k8sEvent.Series.LastObservedTime.Time
						}
						if lastTime.Before(cutoff) {
							k.logger.V(1).Info("Ignoring stale event (>15m)",
								"namespace", k8sEvent.Namespace,
								"regarding.name", k8sEvent.Regarding.Name,
								"reason", k8sEvent.Reason,
								"lastTime", lastTime)
							continue
						}

						if mappedEvent := k.mapKubernetesEvent(k8sEvent); mappedEvent != nil {
							k.logger.Info("Discovered interesting event",
								"eventType", mappedEvent.Type,
								"resource", mappedEvent.ResourceName,
								"reason", mappedEvent.Reason,
								"namespace", mappedEvent.Namespace)
							select {
							case k.eventCh <- *mappedEvent:
								k.logger.V(2).Info("Queued event for processing",
									"eventType", mappedEvent.Type,
									"resource", mappedEvent.ResourceName)
							case <-ctx.Done():
								return
							case <-k.stopCh:
								return
							}
						} else {
							k.logger.V(3).Info("Ignoring event (no mapping)",
								"namespace", k8sEvent.Namespace,
								"regarding.kind", k8sEvent.Regarding.Kind,
								"regarding.name", k8sEvent.Regarding.Name,
								"reason", k8sEvent.Reason,
								"type", k8sEvent.Type)
						}
					}
				}
			}
		}
	}()

	return k.eventCh, nil
}

// SupportedEventTypes returns the list of event types this source can provide
func (k *KubernetesEventSource) SupportedEventTypes() []string {
	return []string{
		"pod-restart",
		"oom-kill",
		"pod-pending",
		"probe-failed",
	}
}

// Stop gracefully shuts down the event source
func (k *KubernetesEventSource) Stop() error {
	k.logger.Info("Stopping Kubernetes event source")
	if k.cancel != nil {
		k.cancel()
	}
	close(k.stopCh)
	return nil
}

// validateNamespace validates the Kubernetes namespace name
func (k *KubernetesEventSource) validateNamespace(namespace string) error {
	if namespace == "" {
		return fmt.Errorf("namespace cannot be empty")
	}

	if len(namespace) > 63 {
		return fmt.Errorf("namespace name too long: %d characters (max 63)", len(namespace))
	}

	// Basic namespace name validation (Kubernetes naming rules)
	for _, r := range namespace {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-') {
			return fmt.Errorf("namespace name contains invalid character '%c', only lowercase alphanumeric and hyphens allowed", r)
		}
	}

	if namespace[0] == '-' || namespace[len(namespace)-1] == '-' {
		return fmt.Errorf("namespace name cannot start or end with a hyphen")
	}

	return nil
}

// mapKubernetesEvent converts a Kubernetes event to our internal Event type
func (k *KubernetesEventSource) mapKubernetesEvent(k8sEvent *eventsv1.Event) *plugin.Event {
	eventType := k.mapEventType(k8sEvent)
	if eventType == "" {
		// This event type is not one we're interested in
		k.logger.V(3).Info("Event not mapped to internal type",
			"namespace", k8sEvent.Namespace,
			"regarding.kind", k8sEvent.Regarding.Kind,
			"regarding.name", k8sEvent.Regarding.Name,
			"reason", k8sEvent.Reason,
			"type", k8sEvent.Type,
			"note", k8sEvent.Note)
		return nil
	}

	// Get timestamp - prefer eventTime, fall back to creationTimestamp
	timestamp := k8sEvent.CreationTimestamp.Time
	if !k8sEvent.EventTime.IsZero() {
		timestamp = k8sEvent.EventTime.Time
	}

	// Handle deprecated fields for backward compatibility
	count := "1"
	if k8sEvent.DeprecatedCount != 0 {
		count = fmt.Sprintf("%d", k8sEvent.DeprecatedCount)
	}

	event := &plugin.Event{
		Type:         eventType,
		ResourceName: k8sEvent.Regarding.Name,
		Timestamp:    timestamp,
		Namespace:    k8sEvent.Namespace,
		Reason:       k8sEvent.Reason,
		Message:      k8sEvent.Note,
		Source:       "kubernetes",
		Metadata: map[string]interface{}{
			"kind":                k8sEvent.Regarding.Kind,
			"apiVersion":          k8sEvent.Regarding.APIVersion,
			"count":               count,
			"type":                k8sEvent.Type,
			"reportingController": k8sEvent.ReportingController,
			"reportingInstance":   k8sEvent.ReportingInstance,
			"uid":                 string(k8sEvent.UID),
		},
	}

	k.logger.V(1).Info("Mapped Kubernetes event",
		"eventType", event.Type,
		"resource", event.ResourceName,
		"reason", event.Reason,
		"type", k8sEvent.Type,
		"note", k8sEvent.Note)

	return event
}

// mapEventType maps Kubernetes event reasons to our event types
func (k *KubernetesEventSource) mapEventType(k8sEvent *eventsv1.Event) string {
	// Ignore Normal events entirely; only act on warnings/errors
	if strings.ToLower(k8sEvent.Type) == "normal" {
		return ""
	}
	// Map based on the regarding object kind and event reason
	switch k8sEvent.Regarding.Kind {
	case "Pod":
		return k.mapPodEventType(k8sEvent)
	default:
		return ""
	}
}

// mapPodEventType maps pod-related events to our event types
func (k *KubernetesEventSource) mapPodEventType(k8sEvent *eventsv1.Event) string {
	reason := strings.ToLower(k8sEvent.Reason)
	message := strings.ToLower(k8sEvent.Note)
	eventType := strings.ToLower(k8sEvent.Type)

	switch {
	// OOM Kill events
	case reason == "oomkilling" || reason == "oomkilled":
		return "oom-kill"
	case reason == "killing" || reason == "killed":
		// Check if it's an OOM kill based on message
		if strings.Contains(message, "oom") || strings.Contains(message, "out of memory") {
			return "oom-kill"
		}
		return "pod-restart"

	// Container restart events (BackOff is the most common)
	case reason == "backoff":
		// "Back-off restarting failed container" indicates restart issues
		return "pod-restart"
	case reason == "failed" && strings.Contains(message, "container"):
		return "pod-restart"

	// Pod scheduling issues
	case reason == "failedscheduling":
		return "pod-pending"
	case reason == "pending" || (eventType == "warning" && strings.Contains(message, "pending")):
		return "pod-pending"

	// Probe failures
	case reason == "unhealthy":
		// Probe failures typically have "Liveness probe failed", "Readiness probe failed", etc.
		if strings.Contains(message, "liveness") || strings.Contains(message, "readiness") || strings.Contains(message, "startup") {
			return "probe-failed"
		}
	case strings.Contains(reason, "probe") && eventType == "warning":
		return "probe-failed"

	// Additional restart-related events
	case reason == "started" && strings.Contains(message, "container"):
		// This could indicate a restart, but we might want to be more selective
		return ""
	case reason == "created" && eventType == "normal":
		// Normal creation events, not necessarily restarts
		return ""

	default:
		return ""
	}

	return ""
}

package event

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	eventsv1 "k8s.io/api/events/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kagent/hook-controller/internal/interfaces"
)

// Watcher implements the EventWatcher interface
type Watcher struct {
	client    kubernetes.Interface
	namespace string
	logger    logr.Logger
	stopCh    chan struct{}
	eventCh   chan interfaces.Event
}

// NewWatcher creates a new EventWatcher instance
func NewWatcher(client kubernetes.Interface, namespace string) interfaces.EventWatcher {
	return &Watcher{
		client:    client,
		namespace: namespace,
		logger:    log.Log.WithName("event-watcher"),
		stopCh:    make(chan struct{}),
		eventCh:   make(chan interfaces.Event, 100),
	}
}

// Start begins the event watching process
func (w *Watcher) Start(ctx context.Context) error {
	w.logger.Info("Starting event watcher", "namespace", w.namespace)

	// Create a field selector to watch for events
	fieldSelector := fields.Everything()

	// Create a watch for events using the events.k8s.io/v1 API
	watchlist := metav1.ListOptions{
		FieldSelector: fieldSelector.String(),
	}

	w.logger.V(1).Info("Creating EventsV1 watcher", "fieldSelector", fieldSelector.String(), "namespace", w.namespace)
	watcher, err := w.client.EventsV1().Events(w.namespace).Watch(ctx, watchlist)
	if err != nil {
		return fmt.Errorf("failed to create event watcher: %w", err)
	}
	w.logger.Info("EventsV1 watcher established", "namespace", w.namespace)

	go func() {
		defer watcher.Stop()
		defer close(w.eventCh)

		for {
			select {
			case <-ctx.Done():
				w.logger.Info("Context cancelled, stopping event watcher")
				return
			case <-w.stopCh:
				w.logger.Info("Stop signal received, stopping event watcher")
				return
			case event, ok := <-watcher.ResultChan():
				if !ok {
					w.logger.Info("Event watcher channel closed")
					return
				}

				if event.Type == watch.Added || event.Type == watch.Modified {
					if k8sEvent, ok := event.Object.(*eventsv1.Event); ok {
						w.logger.V(2).Info("Received Kubernetes event",
							"watchType", event.Type,
							"namespace", k8sEvent.Namespace,
							"regarding.kind", k8sEvent.Regarding.Kind,
							"regarding.name", k8sEvent.Regarding.Name,
							"reason", k8sEvent.Reason,
							"type", k8sEvent.Type,
							"note", k8sEvent.Note,
							"reportingController", k8sEvent.ReportingController)
						if mappedEvent := w.mapKubernetesEvent(k8sEvent); mappedEvent != nil {
							w.logger.Info("Discovered interesting event",
								"eventType", mappedEvent.Type,
								"resource", mappedEvent.ResourceName,
								"reason", mappedEvent.Reason,
								"namespace", mappedEvent.Namespace)
							select {
							case w.eventCh <- *mappedEvent:
								w.logger.V(2).Info("Queued event for processing",
									"eventType", mappedEvent.Type,
									"resource", mappedEvent.ResourceName)
							case <-ctx.Done():
								return
							case <-w.stopCh:
								return
							}
						} else {
							w.logger.V(3).Info("Ignoring event (no mapping)",
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

	return nil
}

// Stop gracefully stops the event watcher
func (w *Watcher) Stop() error {
	w.logger.Info("Stopping event watcher")
	close(w.stopCh)
	return nil
}

// WatchEvents returns a channel of events that match the specified types
func (w *Watcher) WatchEvents(ctx context.Context, eventTypes []string) (<-chan interfaces.Event, error) {
	if err := w.Start(ctx); err != nil {
		return nil, err
	}

	// Create a filtered channel that only sends events matching the specified types
	filteredCh := make(chan interfaces.Event, 100)

	w.logger.Info("Starting filtered event stream", "watchTypes", eventTypes)
	go func() {
		defer close(filteredCh)

		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-w.eventCh:
				if !ok {
					return
				}

				// Check if this event type is in our watch list
				for _, eventType := range eventTypes {
					if event.Type == eventType {
						w.logger.V(1).Info("Event matches filter",
							"eventType", event.Type,
							"resource", event.ResourceName,
							"reason", event.Reason)
						select {
						case filteredCh <- event:
						case <-ctx.Done():
							return
						}
						break
					} else {
						w.logger.V(3).Info("Event does not match filter",
							"wanted", eventType,
							"got", event.Type,
							"resource", event.ResourceName,
							"reason", event.Reason)
					}
				}
			}
		}
	}()

	return filteredCh, nil
}

// FilterEvent matches an event against hook configurations and returns matches
func (w *Watcher) FilterEvent(event interfaces.Event, hooks []interface{}) []interfaces.EventMatch {
	var matches []interfaces.EventMatch

	// This will be implemented when we have the actual hook processing logic
	// For now, return empty matches
	w.logger.V(1).Info("Filtered event", "eventType", event.Type, "matches", len(matches))
	return matches
}

// mapKubernetesEvent converts a Kubernetes event to our internal Event type
func (w *Watcher) mapKubernetesEvent(k8sEvent *eventsv1.Event) *interfaces.Event {
	eventType := w.mapEventType(k8sEvent)
	if eventType == "" {
		// This event type is not one we're interested in
		w.logger.V(3).Info("Event not mapped to internal type",
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

	event := &interfaces.Event{
		Type:         eventType,
		ResourceName: k8sEvent.Regarding.Name,
		Timestamp:    timestamp,
		Namespace:    k8sEvent.Namespace,
		Reason:       k8sEvent.Reason,
		Message:      k8sEvent.Note,
		UID:          string(k8sEvent.UID),
		Metadata: map[string]string{
			"kind":                k8sEvent.Regarding.Kind,
			"apiVersion":          k8sEvent.Regarding.APIVersion,
			"count":               count,
			"type":                k8sEvent.Type,
			"reportingController": k8sEvent.ReportingController,
			"reportingInstance":   k8sEvent.ReportingInstance,
		},
	}

	w.logger.V(1).Info("Mapped Kubernetes event",
		"eventType", event.Type,
		"resource", event.ResourceName,
		"reason", event.Reason,
		"type", k8sEvent.Type,
		"note", k8sEvent.Note)

	return event
}

// mapEventType maps Kubernetes event reasons to our event types
func (w *Watcher) mapEventType(k8sEvent *eventsv1.Event) string {
	// Map based on the regarding object kind and event reason
	switch k8sEvent.Regarding.Kind {
	case "Pod":
		return w.mapPodEventType(k8sEvent)
	default:
		return ""
	}
}

// mapPodEventType maps pod-related events to our event types
func (w *Watcher) mapPodEventType(k8sEvent *eventsv1.Event) string {
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

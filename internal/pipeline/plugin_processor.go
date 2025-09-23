package pipeline

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kagent-dev/khook/api/v1alpha2"
	"github.com/kagent-dev/khook/internal/errors"
	"github.com/kagent-dev/khook/internal/event"
	"github.com/kagent-dev/khook/internal/interfaces"
	"github.com/kagent-dev/khook/internal/plugin"
)

// ProcessorConfig holds configuration for the PluginProcessor
type ProcessorConfig struct {
	CleanupInterval    time.Duration
	StatusInterval     time.Duration
	EventChannelBuffer int
}

// DefaultProcessorConfig provides sensible defaults
var DefaultProcessorConfig = ProcessorConfig{
	CleanupInterval:    5 * time.Minute,
	StatusInterval:     1 * time.Minute,
	EventChannelBuffer: 1000,
}

// PluginProcessor handles event processing using the plugin system
type PluginProcessor struct {
	pluginManager        *plugin.Manager
	mappingLoader        *event.MappingLoader
	deduplicationManager interfaces.DeduplicationManager
	kagentClient         interfaces.KagentClient
	statusManager        interfaces.StatusManager
	logger               logr.Logger
	ctx                  context.Context
	cancel               context.CancelFunc
	eventChannels        map[string]<-chan plugin.Event
	mu                   sync.RWMutex
	config               ProcessorConfig
}

// NewPluginProcessor creates a new plugin-aware event processor
func NewPluginProcessor(
	pluginManager *plugin.Manager,
	mappingLoader *event.MappingLoader,
	deduplicationManager interfaces.DeduplicationManager,
	kagentClient interfaces.KagentClient,
	statusManager interfaces.StatusManager,
) *PluginProcessor {
	return NewPluginProcessorWithConfig(
		pluginManager,
		mappingLoader,
		deduplicationManager,
		kagentClient,
		statusManager,
		DefaultProcessorConfig,
	)
}

// NewPluginProcessorWithConfig creates a new plugin-aware event processor with custom config
func NewPluginProcessorWithConfig(
	pluginManager *plugin.Manager,
	mappingLoader *event.MappingLoader,
	deduplicationManager interfaces.DeduplicationManager,
	kagentClient interfaces.KagentClient,
	statusManager interfaces.StatusManager,
	config ProcessorConfig,
) *PluginProcessor {
	ctx, cancel := context.WithCancel(context.Background())
	return &PluginProcessor{
		pluginManager:        pluginManager,
		mappingLoader:        mappingLoader,
		deduplicationManager: deduplicationManager,
		kagentClient:         kagentClient,
		statusManager:        statusManager,
		logger:               log.Log.WithName("plugin-processor"),
		ctx:                  ctx,
		cancel:               cancel,
		eventChannels:        make(map[string]<-chan plugin.Event),
		config:               config,
	}
}

// StartEventProcessing starts processing events from all active plugins
func (pp *PluginProcessor) StartEventProcessing(ctx context.Context, hooks []*v1alpha2.Hook) error {
	pp.logger.Info("Starting plugin-based event processing", "hookCount", len(hooks))

	// Get all active plugins
	activePlugins := pp.pluginManager.GetActivePlugins()
	if len(activePlugins) == 0 {
		return fmt.Errorf("no active plugins found")
	}

	pp.logger.Info("Found active plugins", "count", len(activePlugins))

	// Start event watching for each active plugin
	for pluginName, loadedPlugin := range activePlugins {
		if err := pp.startPluginEventWatching(ctx, pluginName, loadedPlugin); err != nil {
			pp.logger.Error(err, "Failed to start event watching for plugin", "plugin", pluginName)
			continue
		}
	}

	// Start the main event processing loop
	return pp.processEventsFromPlugins(ctx, hooks)
}

// startPluginEventWatching starts event watching for a specific plugin
func (pp *PluginProcessor) startPluginEventWatching(ctx context.Context, pluginName string, loadedPlugin *plugin.LoadedPlugin) error {
	pp.logger.Info("Starting event watching for plugin", "plugin", pluginName)

	// Start the plugin if not already started
	if err := pp.pluginManager.StartPlugin(pluginName); err != nil {
		return fmt.Errorf("failed to start plugin %s: %w", pluginName, err)
	}

	// Get the event channel for this plugin
	eventChannels := pp.pluginManager.GetEventChannels()
	if eventCh, exists := eventChannels[pluginName]; exists {
		pp.mu.Lock()
		pp.eventChannels[pluginName] = eventCh
		pp.mu.Unlock()
		pp.logger.Info("Successfully started event watching for plugin", "plugin", pluginName)
	} else {
		return fmt.Errorf("no event channel found for plugin %s", pluginName)
	}

	return nil
}

// processEventsFromPlugins processes events from all plugin channels
func (pp *PluginProcessor) processEventsFromPlugins(ctx context.Context, hooks []*v1alpha2.Hook) error {
	pp.logger.Info("Starting event processing loop")

	// Set up periodic cleanup and status updates
	cleanupTicker := time.NewTicker(pp.config.CleanupInterval)
	statusTicker := time.NewTicker(pp.config.StatusInterval)
	defer cleanupTicker.Stop()
	defer statusTicker.Stop()

	// Create a merged channel for all plugin events
	mergedEventCh := pp.createMergedEventChannel(ctx)

	for {
		select {
		case <-ctx.Done():
			pp.logger.Info("Event processing stopped due to context cancellation")
			return ctx.Err()

		case pluginEvent, ok := <-mergedEventCh:
			if !ok {
				pp.logger.Info("Merged event channel closed, stopping processing")
				return nil
			}

			// Convert plugin event to interfaces.Event for compatibility
			interfaceEvent := pp.convertPluginEventToInterface(pluginEvent)

			// Apply event mapping if available
			mappedEvent := pp.applyEventMapping(interfaceEvent, pluginEvent.Source)
			if mappedEvent == nil {
				pp.logger.V(2).Info("Event filtered out by mapping",
					"eventType", interfaceEvent.Type,
					"source", pluginEvent.Source)
				continue
			}

			// Process the event
			if err := pp.ProcessEvent(ctx, *mappedEvent, hooks); err != nil {
				pp.logger.Error(err, "Failed to process event",
					"eventType", mappedEvent.Type,
					"resourceName", mappedEvent.ResourceName,
					"source", pluginEvent.Source)
				// Continue processing other events
			}

		case <-cleanupTicker.C:
			// Periodic cleanup of expired events
			if err := pp.CleanupExpiredEvents(ctx, hooks); err != nil {
				pp.logger.Error(err, "Failed to cleanup expired events")
			}

		case <-statusTicker.C:
			// Periodic status updates
			if err := pp.UpdateHookStatuses(ctx, hooks); err != nil {
				pp.logger.Error(err, "Failed to update hook statuses")
			}
		}
	}
}

// createMergedEventChannel creates a single channel that merges events from all plugin channels
func (pp *PluginProcessor) createMergedEventChannel(ctx context.Context) <-chan plugin.Event {
	mergedCh := make(chan plugin.Event, pp.config.EventChannelBuffer)

	pp.mu.RLock()
	eventChannels := make(map[string]<-chan plugin.Event)
	for name, ch := range pp.eventChannels {
		eventChannels[name] = ch
	}
	pp.mu.RUnlock()

	// Start goroutines to forward events from each plugin channel to the merged channel
	var wg sync.WaitGroup
	for pluginName, eventCh := range eventChannels {
		wg.Add(1)
		go func(name string, ch <-chan plugin.Event) {
			defer wg.Done()
			pp.logger.V(2).Info("Starting event forwarding goroutine", "plugin", name)

			for {
				select {
				case <-ctx.Done():
					pp.logger.V(2).Info("Event forwarding stopped for plugin", "plugin", name)
					return
				case event, ok := <-ch:
					if !ok {
						pp.logger.Info("Event channel closed for plugin", "plugin", name)
						return
					}
					select {
					case mergedCh <- event:
						pp.logger.V(3).Info("Forwarded event from plugin",
							"plugin", name,
							"eventType", event.Type,
							"resource", event.ResourceName)
					case <-ctx.Done():
						return
					}
				}
			}
		}(pluginName, eventCh)
	}

	// Close merged channel when all plugin channels are closed
	go func() {
		wg.Wait()
		close(mergedCh)
		pp.logger.Info("Merged event channel closed")
	}()

	return mergedCh
}

// convertPluginEventToInterface converts a plugin.Event to interfaces.Event for compatibility
func (pp *PluginProcessor) convertPluginEventToInterface(pluginEvent plugin.Event) interfaces.Event {
	// Convert metadata from map[string]interface{} to map[string]string
	metadata := make(map[string]string)
	for key, value := range pluginEvent.Metadata {
		if strValue, ok := value.(string); ok {
			metadata[key] = strValue
		} else {
			metadata[key] = fmt.Sprintf("%v", value)
		}
	}

	return interfaces.Event{
		Type:         pluginEvent.Type,
		ResourceName: pluginEvent.ResourceName,
		Timestamp:    pluginEvent.Timestamp,
		Namespace:    pluginEvent.Namespace,
		Reason:       pluginEvent.Reason,
		Message:      pluginEvent.Message,
		UID:          "", // Plugin events don't have UID in the same way
		Metadata:     metadata,
	}
}

// applyEventMapping applies event mapping configuration to filter and transform events
func (pp *PluginProcessor) applyEventMapping(event interfaces.Event, source string) *interfaces.Event {

	// Get mapping for this event
	mapping, exists := pp.mappingLoader.GetMapping(source, event.Type)
	if !exists {
		pp.logger.V(2).Info("No mapping found for event",
			"source", source,
			"eventType", event.Type)
		return nil // Filter out unmapped events
	}

	// Check if mapping is enabled
	if !mapping.Enabled {
		pp.logger.V(2).Info("Event mapping is disabled",
			"source", source,
			"eventType", event.Type)
		return nil // Filter out disabled events
	}

	// Apply mapping transformation
	mappedEvent := event
	mappedEvent.Type = mapping.InternalType

	// Add mapping tags to metadata
	if mapping.Tags != nil {
		for key, value := range mapping.Tags {
			mappedEvent.Metadata[fmt.Sprintf("mapping.%s", key)] = value
		}
	}

	// Add severity to metadata
	if mapping.Severity != "" {
		mappedEvent.Metadata["mapping.severity"] = mapping.Severity
	}

	pp.logger.V(1).Info("Applied event mapping",
		"source", source,
		"originalType", event.Type,
		"mappedType", mappedEvent.Type,
		"severity", mapping.Severity)

	return &mappedEvent
}

// ProcessEvent processes a single event (reusing logic from original processor)
func (pp *PluginProcessor) ProcessEvent(ctx context.Context, event interfaces.Event, hooks []*v1alpha2.Hook) error {
	pp.logger.Info("Processing event",
		"eventType", event.Type,
		"resourceName", event.ResourceName,
		"namespace", event.Namespace,
		"hookCount", len(hooks))

	// Find matching hooks and configurations for this event
	matches := pp.findEventMatches(event, hooks)
	if len(matches) == 0 {
		pp.logger.V(1).Info("No matching hooks found for event",
			"eventType", event.Type,
			"resourceName", event.ResourceName)
		return nil
	}

	pp.logger.Info("Found matching hooks for event",
		"eventType", event.Type,
		"resourceName", event.ResourceName,
		"matchCount", len(matches))

	// Process each match
	processingErrors := errors.NewProcessingErrors("event processing")
	for _, match := range matches {
		if err := pp.processEventMatch(ctx, match); err != nil {
			pp.logger.Error(err, "Failed to process event match",
				"hook", match.Hook.Name,
				"eventType", event.Type,
				"resourceName", event.ResourceName,
				"agentRef", match.Configuration.AgentRef)

			processingErrors.AddWithContext(err, fmt.Sprintf("hook %s/%s",
				match.Hook.Namespace, match.Hook.Name))
			// Continue processing other matches even if one fails
		}
	}

	return processingErrors.ToError()
}

// findEventMatches finds all hook configurations that match the given event
func (pp *PluginProcessor) findEventMatches(event interfaces.Event, hooks []*v1alpha2.Hook) []EventMatch {
	var matches []EventMatch

	for _, hook := range hooks {
		for _, config := range hook.Spec.EventConfigurations {
			if config.EventType == event.Type {
				matches = append(matches, EventMatch{
					Hook:          hook,
					Configuration: config,
					Event:         event,
				})
			}
		}
	}

	return matches
}

// processEventMatch processes a single event match through the complete pipeline
func (pp *PluginProcessor) processEventMatch(ctx context.Context, match EventMatch) error {
	hookRef := types.NamespacedName{
		Namespace: match.Hook.Namespace,
		Name:      match.Hook.Name,
	}

	// Check deduplication - should we process this event?
	if !pp.deduplicationManager.ShouldProcessEvent(hookRef, match.Event) {
		pp.logger.V(1).Info("Event ignored due to deduplication",
			"hook", hookRef,
			"eventType", match.Event.Type,
			"resourceName", match.Event.ResourceName)

		// Record that we ignored a duplicate event
		if err := pp.statusManager.RecordDuplicateEvent(ctx, match.Hook, match.Event); err != nil {
			pp.logger.Error(err, "Failed to record duplicate event", "hook", hookRef)
		}
		return nil
	}

	// Record the event in deduplication manager
	if err := pp.deduplicationManager.RecordEvent(hookRef, match.Event); err != nil {
		return fmt.Errorf("failed to record event in deduplication manager: %w", err)
	}

	agentRefNs := match.Hook.Namespace
	if match.Configuration.AgentRef.Namespace != nil {
		agentRefNs = *match.Configuration.AgentRef.Namespace
	}
	agentRef := types.NamespacedName{
		Name:      match.Configuration.AgentRef.Name,
		Namespace: agentRefNs,
	}

	// Record that the event is firing
	if err := pp.statusManager.RecordEventFiring(ctx, match.Hook, match.Event, agentRef); err != nil {
		pp.logger.Error(err, "Failed to record event firing", "hook", hookRef)
		// Continue processing even if status recording fails
	}

	// Create agent request with event context
	agentRequest := pp.createAgentRequest(match, agentRef)

	// Call the Kagent agent
	response, err := pp.kagentClient.CallAgent(ctx, agentRequest)
	if err != nil {
		// Record the failure
		if statusErr := pp.statusManager.RecordAgentCallFailure(ctx, match.Hook, match.Event, agentRef, err); statusErr != nil {
			pp.logger.Error(statusErr, "Failed to record agent call failure", "hook", hookRef)
		}
		return fmt.Errorf("failed to call agent %s: %w", agentRef.Name, err)
	}

	// Record successful agent call
	if err := pp.statusManager.RecordAgentCallSuccess(ctx, match.Hook, match.Event, agentRef, response.RequestId); err != nil {
		pp.logger.Error(err, "Failed to record agent call success", "hook", hookRef)
		// Continue even if status recording fails
	}

	// Mark event as notified to suppress re-sending within suppression window
	pp.deduplicationManager.MarkNotified(hookRef, match.Event)

	pp.logger.Info("Successfully processed event match",
		"hook", hookRef,
		"eventType", match.Event.Type,
		"resourceName", match.Event.ResourceName,
		"agentRef", agentRef,
		"requestId", response.RequestId)

	return nil
}

// createAgentRequest creates an agent request from an event match
func (pp *PluginProcessor) createAgentRequest(match EventMatch, agentRef types.NamespacedName) interfaces.AgentRequest {
	// Expand prompt template with event context (reuse from original processor)
	processor := &Processor{logger: pp.logger}
	prompt := processor.expandPromptTemplate(match.Configuration.Prompt, match.Event)

	return interfaces.AgentRequest{
		AgentRef:     agentRef,
		Prompt:       prompt,
		EventName:    match.Event.Type,
		EventTime:    match.Event.Timestamp,
		ResourceName: match.Event.ResourceName,
		Context: map[string]interface{}{
			"namespace":     match.Event.Namespace,
			"reason":        match.Event.Reason,
			"message":       match.Event.Message,
			"uid":           match.Event.UID,
			"metadata":      match.Event.Metadata,
			"hookName":      match.Hook.Name,
			"hookNamespace": match.Hook.Namespace,
		},
	}
}

// UpdateHookStatuses updates the status of all hooks with their current active events
func (pp *PluginProcessor) UpdateHookStatuses(ctx context.Context, hooks []*v1alpha2.Hook) error {
	pp.logger.Info("Updating hook statuses", "hookCount", len(hooks))

	processingErrors := errors.NewProcessingErrors("hook status updates")

	for _, hook := range hooks {
		hookRef := types.NamespacedName{
			Namespace: hook.Namespace,
			Name:      hook.Name,
		}

		// Get active events for this hook with current status
		activeEvents := pp.deduplicationManager.GetActiveEventsWithStatus(hookRef)

		// Update the hook status
		if err := pp.statusManager.UpdateHookStatus(ctx, hook, activeEvents); err != nil {
			pp.logger.Error(err, "Failed to update hook status", "hook", hookRef)
			processingErrors.AddWithContext(err, fmt.Sprintf("hook %s", hookRef))
			// Continue updating other hooks even if one fails
			continue
		}

		pp.logger.V(1).Info("Updated hook status",
			"hook", hookRef,
			"activeEventsCount", len(activeEvents))
	}

	return processingErrors.ToError()
}

// CleanupExpiredEvents cleans up expired events for all hooks
func (pp *PluginProcessor) CleanupExpiredEvents(ctx context.Context, hooks []*v1alpha2.Hook) error {
	pp.logger.V(1).Info("Cleaning up expired events", "hookCount", len(hooks))

	processingErrors := errors.NewProcessingErrors("expired events cleanup")

	for _, hook := range hooks {
		hookRef := types.NamespacedName{
			Namespace: hook.Namespace,
			Name:      hook.Name,
		}

		if err := pp.deduplicationManager.CleanupExpiredEvents(hookRef); err != nil {
			pp.logger.Error(err, "Failed to cleanup expired events", "hook", hookRef)
			processingErrors.AddWithContext(err, fmt.Sprintf("hook %s", hookRef))
			// Continue cleaning up other hooks even if one fails
		}
	}

	return processingErrors.ToError()
}

// Stop gracefully stops the plugin processor
func (pp *PluginProcessor) Stop() error {
	pp.logger.Info("Stopping plugin processor")
	pp.cancel()
	return nil
}

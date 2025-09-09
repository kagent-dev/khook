package pipeline

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"text/template"
	"time"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/antweiss/khook/api/v1alpha2"
	"github.com/antweiss/khook/internal/interfaces"
)

// Processor handles the complete event processing pipeline
type Processor struct {
	eventWatcher         interfaces.EventWatcher
	deduplicationManager interfaces.DeduplicationManager
	kagentClient         interfaces.KagentClient
	statusManager        interfaces.StatusManager
	logger               logr.Logger
}

// NewProcessor creates a new event processing pipeline
func NewProcessor(
	eventWatcher interfaces.EventWatcher,
	deduplicationManager interfaces.DeduplicationManager,
	kagentClient interfaces.KagentClient,
	statusManager interfaces.StatusManager,
) *Processor {
	return &Processor{
		eventWatcher:         eventWatcher,
		deduplicationManager: deduplicationManager,
		kagentClient:         kagentClient,
		statusManager:        statusManager,
		logger:               log.Log.WithName("event-processor"),
	}
}

// ProcessEvent processes a single event against all provided hooks
func (p *Processor) ProcessEvent(ctx context.Context, event interfaces.Event, hooks []*v1alpha2.Hook) error {
	p.logger.Info("Processing event",
		"eventType", event.Type,
		"resourceName", event.ResourceName,
		"namespace", event.Namespace,
		"hookCount", len(hooks))

	// Find matching hooks and configurations for this event
	matches := p.findEventMatches(event, hooks)
	if len(matches) == 0 {
		p.logger.V(1).Info("No matching hooks found for event",
			"eventType", event.Type,
			"resourceName", event.ResourceName)
		return nil
	}

	p.logger.Info("Found matching hooks for event",
		"eventType", event.Type,
		"resourceName", event.ResourceName,
		"matchCount", len(matches))

	// Process each match
	var lastError error
	for _, match := range matches {
		if err := p.processEventMatch(ctx, match); err != nil {
			p.logger.Error(err, "Failed to process event match",
				"hook", match.Hook.Name,
				"eventType", event.Type,
				"resourceName", event.ResourceName,
				"agentId", match.Configuration.AgentRef.Name)
			lastError = err
			// Continue processing other matches even if one fails
			continue
		}
	}

	return lastError
}

// EventMatch represents a matched event with its hook and configuration
type EventMatch struct {
	Hook          *v1alpha2.Hook
	Configuration v1alpha2.EventConfiguration
	Event         interfaces.Event
}

// findEventMatches finds all hook configurations that match the given event
func (p *Processor) findEventMatches(event interfaces.Event, hooks []*v1alpha2.Hook) []EventMatch {
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
func (p *Processor) processEventMatch(ctx context.Context, match EventMatch) error {
	hookName := fmt.Sprintf("%s/%s", match.Hook.Namespace, match.Hook.Name)

	// Check deduplication - should we process this event?
	if !p.deduplicationManager.ShouldProcessEvent(hookName, match.Event) {
		p.logger.V(1).Info("Event ignored due to deduplication",
			"hook", hookName,
			"eventType", match.Event.Type,
			"resourceName", match.Event.ResourceName)

		// Record that we ignored a duplicate event
		if err := p.statusManager.RecordDuplicateEvent(ctx, match.Hook, match.Event); err != nil {
			p.logger.Error(err, "Failed to record duplicate event", "hook", hookName)
		}
		return nil
	}

	// Record the event in deduplication manager
	if err := p.deduplicationManager.RecordEvent(hookName, match.Event); err != nil {
		return fmt.Errorf("failed to record event in deduplication manager: %w", err)
	}

	// Record that the event is firing
	if err := p.statusManager.RecordEventFiring(ctx, match.Hook, match.Event, match.Configuration.AgentRef.Name); err != nil {
		p.logger.Error(err, "Failed to record event firing", "hook", hookName)
		// Continue processing even if status recording fails
	}

	// Create agent request with event context
	agentRequest := p.createAgentRequest(match)

	// Call the Kagent agent
	response, err := p.kagentClient.CallAgent(ctx, agentRequest)
	if err != nil {
		// Record the failure
		if statusErr := p.statusManager.RecordAgentCallFailure(ctx, match.Hook, match.Event, match.Configuration.AgentRef.Name, err); statusErr != nil {
			p.logger.Error(statusErr, "Failed to record agent call failure", "hook", hookName)
		}
		return fmt.Errorf("failed to call agent %s: %w", match.Configuration.AgentRef.Name, err)
	}

	// Record successful agent call
	if err := p.statusManager.RecordAgentCallSuccess(ctx, match.Hook, match.Event, match.Configuration.AgentRef.Name, response.RequestId); err != nil {
		p.logger.Error(err, "Failed to record agent call success", "hook", hookName)
		// Continue even if status recording fails
	}

	// Mark event as notified to suppress re-sending within suppression window
	p.deduplicationManager.MarkNotified(hookName, match.Event)

	p.logger.Info("Successfully processed event match",
		"hook", hookName,
		"eventType", match.Event.Type,
		"resourceName", match.Event.ResourceName,
		"agentId", match.Configuration.AgentRef.Name,
		"requestId", response.RequestId)

	return nil
}

// createAgentRequest creates an agent request from an event match
func (p *Processor) createAgentRequest(match EventMatch) interfaces.AgentRequest {
	// Expand prompt template with event context
	prompt := p.expandPromptTemplate(match.Configuration.Prompt, match.Event)

	return interfaces.AgentRequest{
		AgentId:      match.Configuration.AgentRef.Name,
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

// expandPromptTemplate expands template variables in the prompt using Go's text/template
func (p *Processor) expandPromptTemplate(templateStr string, event interfaces.Event) string {
	// Validate template for security
	if err := p.validateTemplate(templateStr); err != nil {
		p.logger.Error(err, "Template validation failed, using original template",
			"template", templateStr,
			"eventType", event.Type)
		return templateStr
	}

	// First, try to expand known placeholders using the original manual method
	// This ensures backward compatibility for unknown placeholders
	result := p.expandKnownPlaceholders(templateStr, event)

	// Check if there are still unexpanded template placeholders
	// If so, skip text/template processing to maintain backward compatibility
	if strings.Contains(result, "{{") && strings.Contains(result, "}}") {
		p.logger.V(2).Info("Template contains unknown placeholders, skipping advanced processing",
			"template", result)
		return result
	}

	// Then try to use text/template for more advanced templating
	// This allows for complex template expressions while maintaining backward compatibility
	result = p.expandWithTextTemplate(result, event)

	return result
}

// validateTemplate performs security validation on template strings
func (p *Processor) validateTemplate(templateStr string) error {
	if templateStr == "" {
		return fmt.Errorf("template cannot be empty")
	}

	if len(templateStr) > 10000 {
		return fmt.Errorf("template too long: %d characters (max 10000)", len(templateStr))
	}

	// Check for potentially dangerous template constructs
	dangerousPatterns := []string{
		"{{/*",       // block comments that might hide malicious code
		"{{define",   // template definitions
		"{{template", // template calls
		"{{call",     // function calls
		"{{data",     // data access
		"{{urlquery", // URL encoding
		"{{print",    // print function
		"{{printf",   // printf function
		"{{println",  // println function
	}

	for _, pattern := range dangerousPatterns {
		if strings.Contains(templateStr, pattern) {
			return fmt.Errorf("template contains potentially dangerous construct: %s", pattern)
		}
	}

	// Validate bracket matching
	openCount := strings.Count(templateStr, "{{")
	closeCount := strings.Count(templateStr, "}}")

	if openCount != closeCount {
		return fmt.Errorf("template has unmatched brackets: %d opens, %d closes", openCount, closeCount)
	}

	return nil
}

// expandKnownPlaceholders handles the original manual placeholder replacement
func (p *Processor) expandKnownPlaceholders(template string, event interfaces.Event) string {
	expanded := template

	replacements := map[string]string{
		"{{.EventType}}":    event.Type,
		"{{.ResourceName}}": event.ResourceName,
		"{{.Namespace}}":    event.Namespace,
		"{{.Reason}}":       event.Reason,
		"{{.Message}}":      event.Message,
		"{{.Timestamp}}":    event.Timestamp.Format(time.RFC3339),
		"{{.EventTime}}":    event.Timestamp.Format(time.RFC3339),
		"{{.EventMessage}}": event.Message,
	}

	for placeholder, value := range replacements {
		expanded = strings.ReplaceAll(expanded, placeholder, value)
	}

	return expanded
}

// expandWithTextTemplate attempts to use text/template for advanced features
func (p *Processor) expandWithTextTemplate(templateStr string, event interfaces.Event) string {
	// Create template data for advanced templating
	templateData := map[string]interface{}{
		"EventType":    event.Type,
		"ResourceName": event.ResourceName,
		"Namespace":    event.Namespace,
		"Reason":       event.Reason,
		"Message":      event.Message,
		"Timestamp":    event.Timestamp.Format(time.RFC3339),
		"EventTime":    event.Timestamp.Format(time.RFC3339),
		"EventMessage": event.Message,
		"Event":        event, // Full event access for advanced templating
	}

	// Try to parse and execute the template
	tmpl, err := template.New("prompt").Parse(templateStr)
	if err != nil {
		// If parsing fails, return the original string (likely already processed)
		p.logger.V(3).Info("Template parsing failed, using already expanded template",
			"template", templateStr,
			"error", err.Error())
		return templateStr
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, templateData); err != nil {
		// If execution fails, return the original string
		p.logger.V(3).Info("Template execution failed, using already expanded template",
			"template", templateStr,
			"error", err.Error())
		return templateStr
	}

	result := buf.String()
	p.logger.V(2).Info("Advanced template expansion completed",
		"originalLength", len(templateStr),
		"expandedLength", len(result))

	return result
}

// UpdateHookStatuses updates the status of all hooks with their current active events
func (p *Processor) UpdateHookStatuses(ctx context.Context, hooks []*v1alpha2.Hook) error {
	p.logger.Info("Updating hook statuses", "hookCount", len(hooks))

	for _, hook := range hooks {
		hookName := fmt.Sprintf("%s/%s", hook.Namespace, hook.Name)

		// Get active events for this hook with current status
		activeEvents := p.deduplicationManager.GetActiveEventsWithStatus(hookName)

		// Update the hook status
		if err := p.statusManager.UpdateHookStatus(ctx, hook, activeEvents); err != nil {
			p.logger.Error(err, "Failed to update hook status", "hook", hookName)
			// Continue updating other hooks even if one fails
			continue
		}

		p.logger.V(1).Info("Updated hook status",
			"hook", hookName,
			"activeEventsCount", len(activeEvents))
	}

	return nil
}

// CleanupExpiredEvents cleans up expired events for all hooks
func (p *Processor) CleanupExpiredEvents(ctx context.Context, hooks []*v1alpha2.Hook) error {
	p.logger.V(1).Info("Cleaning up expired events", "hookCount", len(hooks))

	for _, hook := range hooks {
		hookName := fmt.Sprintf("%s/%s", hook.Namespace, hook.Name)

		if err := p.deduplicationManager.CleanupExpiredEvents(hookName); err != nil {
			p.logger.Error(err, "Failed to cleanup expired events", "hook", hookName)
			// Continue cleaning up other hooks even if one fails
			continue
		}
	}

	return nil
}

// ProcessEventWorkflow handles the complete event processing workflow
func (p *Processor) ProcessEventWorkflow(ctx context.Context, eventTypes []string, hooks []*v1alpha2.Hook) error {
	p.logger.Info("Starting event processing workflow",
		"eventTypes", eventTypes,
		"hookCount", len(hooks))

	// Start watching for events (filtering is done by the processor)
	eventCh, err := p.eventWatcher.WatchEvents(ctx)
	if err != nil {
		return fmt.Errorf("failed to start event watching: %w", err)
	}

	// Set up periodic cleanup and status updates
	cleanupTicker := time.NewTicker(5 * time.Minute)
	statusTicker := time.NewTicker(1 * time.Minute)
	defer cleanupTicker.Stop()
	defer statusTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.logger.Info("Event processing workflow stopped due to context cancellation")
			return ctx.Err()

		case event, ok := <-eventCh:
			if !ok {
				p.logger.Info("Event channel closed, stopping workflow")
				return nil
			}

			// Process the event
			if err := p.ProcessEvent(ctx, event, hooks); err != nil {
				p.logger.Error(err, "Failed to process event",
					"eventType", event.Type,
					"resourceName", event.ResourceName)
				// Continue processing other events
			}

		case <-cleanupTicker.C:
			// Periodic cleanup of expired events
			if err := p.CleanupExpiredEvents(ctx, hooks); err != nil {
				p.logger.Error(err, "Failed to cleanup expired events")
			}

		case <-statusTicker.C:
			// Periodic status updates
			if err := p.UpdateHookStatuses(ctx, hooks); err != nil {
				p.logger.Error(err, "Failed to update hook statuses")
			}
		}
	}
}

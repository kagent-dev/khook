package workflow

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/log"

	kagentv1alpha2 "github.com/kagent-dev/khook/api/v1alpha2"
	"github.com/kagent-dev/khook/internal/event"
	"github.com/kagent-dev/khook/internal/interfaces"
	"github.com/kagent-dev/khook/internal/pipeline"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// WorkflowManager manages per-namespace event processing workflows
type WorkflowManager struct {
	k8sClient     kubernetes.Interface
	ctrlClient    client.Client
	dedupManager  interfaces.DeduplicationManager
	kagentClient  interfaces.KagentClient
	statusManager interfaces.StatusManager
	eventRecorder interfaces.EventRecorder
	sreServer     interface{}
	logger        logr.Logger
}

// NewWorkflowManager creates a new workflow manager
func NewWorkflowManager(
	k8sClient kubernetes.Interface,
	ctrlClient client.Client,
	dedupManager interfaces.DeduplicationManager,
	kagentClient interfaces.KagentClient,
	statusManager interfaces.StatusManager,
	eventRecorder interfaces.EventRecorder,
	sreServer interface{},
) *WorkflowManager {
	return &WorkflowManager{
		k8sClient:     k8sClient,
		ctrlClient:    ctrlClient,
		dedupManager:  dedupManager,
		kagentClient:  kagentClient,
		statusManager: statusManager,
		eventRecorder: eventRecorder,
		sreServer:     sreServer,
		logger:        log.Log.WithName("workflow-manager"),
	}
}

// NamespaceState tracks per-namespace workflow state
type NamespaceState struct {
	Cancel    context.CancelFunc
	Signature string
}

// StartNamespaceWorkflow starts a workflow for a specific namespace
func (wm *WorkflowManager) StartNamespaceWorkflow(
	ctx context.Context,
	namespace string,
	hooks []*kagentv1alpha2.Hook,
	signature string,
) (*NamespaceState, error) {

	ctxNS, cancel := context.WithCancel(ctx)
	state := &NamespaceState{
		Cancel:    cancel,
		Signature: signature,
	}

	eventTypes := wm.uniqueEventTypes(hooks)
	wm.logger.Info("Starting namespace workflow",
		"namespace", namespace,
		"hookCount", len(hooks),
		"eventTypes", eventTypes)

	go wm.runNamespaceWorkflow(ctxNS, namespace, hooks, eventTypes)

	return state, nil
}

// StopNamespaceWorkflow stops a namespace workflow
func (wm *WorkflowManager) StopNamespaceWorkflow(namespace string, state *NamespaceState) {
	wm.logger.Info("Stopping namespace workflow", "namespace", namespace)
	state.Cancel()
}

// runNamespaceWorkflow runs the actual workflow for a namespace
func (wm *WorkflowManager) runNamespaceWorkflow(
	ctx context.Context,
	namespace string,
	hooks []*kagentv1alpha2.Hook,
	eventTypes []string,
) {
	defer func() {
		if r := recover(); r != nil {
			wm.logger.Error(fmt.Errorf("namespace workflow panic: %v", r),
				"namespace workflow panicked", "namespace", namespace)
		}
	}()

	wm.logger.Info("Namespace workflow started", "namespace", namespace)

	watcher := event.NewWatcher(wm.k8sClient, namespace)
	processor := pipeline.NewProcessor(watcher, wm.dedupManager, wm.kagentClient, wm.statusManager, wm.sreServer)

	if err := processor.ProcessEventWorkflow(ctx, eventTypes, hooks); err != nil {
		wm.logger.Error(err, "Namespace workflow exited with error", "namespace", namespace)
	} else {
		wm.logger.Info("Namespace workflow finished", "namespace", namespace)
	}
}

// uniqueEventTypes extracts unique event types from hooks
func (wm *WorkflowManager) uniqueEventTypes(hooks []*kagentv1alpha2.Hook) []string {
	set := map[string]struct{}{}
	for _, h := range hooks {
		for _, ec := range h.Spec.EventConfigurations {
			set[ec.EventType] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for t := range set {
		out = append(out, t)
	}
	return out
}

// CalculateSignature creates a signature for hook changes detection
func (wm *WorkflowManager) CalculateSignature(hooks []*kagentv1alpha2.Hook) string {
	parts := make([]string, 0, len(hooks))
	for _, h := range hooks {
		cfgs := make([]string, 0, len(h.Spec.EventConfigurations))
		for _, ec := range h.Spec.EventConfigurations {
			cfgs = append(cfgs, ec.EventType+"|"+ec.AgentRef.Name+"|"+ec.Prompt)
		}
		parts = append(parts, h.Namespace+"/"+h.Name+"@"+strings.Join(cfgs, ";"))
	}
	return strings.Join(parts, ",")
}

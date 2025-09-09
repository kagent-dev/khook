package v1alpha2

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func init() {
	SchemeBuilder.Register(&Hook{}, &HookList{})
}

// HookSpec defines the desired state of Hook
type HookSpec struct {
	// EventConfigurations defines the list of event configurations to monitor
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	EventConfigurations []EventConfiguration `json:"eventConfigurations"`
}

// EventConfiguration defines a single event type configuration
type EventConfiguration struct {
	// EventType specifies the type of Kubernetes event to monitor
	// +kubebuilder:validation:Enum=pod-restart;pod-pending;oom-kill;probe-failed
	// +kubebuilder:validation:Required
	EventType string `json:"eventType"`

	// AgentId specifies the Kagent agent to call when this event occurs
	// +kubebuilder:validation:Required
	AgentRef ObjectReference `json:"agentRef"`

	// Prompt specifies the prompt template to send to the agent
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Prompt string `json:"prompt"`
}

type ObjectReference struct {
	// Name of the referent.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#names
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Namespace of the referent.
	// If unspecified, the namespace of the Hook will be used.
	// +kubebuilder:validation:Optional
	Namespace *string `json:"namespace,omitempty"`
}

// HookStatus defines the observed state of Hook
type HookStatus struct {
	// ActiveEvents contains the list of currently active events
	ActiveEvents []ActiveEventStatus `json:"activeEvents,omitempty"`

	// LastUpdated indicates when the status was last updated
	LastUpdated metav1.Time `json:"lastUpdated,omitempty"`
}

// Validate validates the Hook resource
func (h *Hook) Validate() error {
	if len(h.Spec.EventConfigurations) == 0 {
		return fmt.Errorf("at least one event configuration is required")
	}

	if len(h.Spec.EventConfigurations) > 50 {
		return fmt.Errorf("too many event configurations: %d (max 50)", len(h.Spec.EventConfigurations))
	}

	for i, config := range h.Spec.EventConfigurations {
		if err := h.validateEventConfiguration(config, i); err != nil {
			return err
		}
	}

	return nil
}

// validateEventConfiguration validates a single event configuration
func (h *Hook) validateEventConfiguration(config EventConfiguration, index int) error {
	// Validate EventType
	validEventTypes := map[string]bool{
		"pod-restart":  true,
		"pod-pending":  true,
		"oom-kill":     true,
		"probe-failed": true,
	}

	if !validEventTypes[config.EventType] {
		return fmt.Errorf("event configuration %d: invalid event type '%s', must be one of: pod-restart, pod-pending, oom-kill, probe-failed", index, config.EventType)
	}

	// Validate AgentId
	if strings.TrimSpace(config.AgentRef.Name) == "" {
		return fmt.Errorf("event configuration %d: agentRef.name cannot be empty", index)
	}

	if len(config.AgentRef.Name) > 100 {
		return fmt.Errorf("event configuration %d: agentId too long: %d characters (max 100)", index, len(config.AgentRef.Name))
	}

	// Validate agent ID format (alphanumeric, hyphens, underscores only)
	for _, r := range config.AgentRef.Name {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_') {
			return fmt.Errorf("event configuration %d: agentId contains invalid character '%c', only alphanumeric, hyphens, and underscores allowed", index, r)
		}
	}

	// Validate Prompt
	if strings.TrimSpace(config.Prompt) == "" {
		return fmt.Errorf("event configuration %d: prompt cannot be empty", index)
	}

	if len(config.Prompt) > 10000 {
		return fmt.Errorf("event configuration %d: prompt too long: %d characters (max 10000)", index, len(config.Prompt))
	}

	// Validate template constructs
	if err := h.validatePromptTemplate(config.Prompt, index); err != nil {
		return err
	}

	return nil
}

// validatePromptTemplate validates the prompt template for security and correctness
func (h *Hook) validatePromptTemplate(prompt string, index int) error {
	if prompt == "" {
		return fmt.Errorf("event configuration %d: prompt cannot be empty", index)
	}

	// Check for balanced brackets
	openCount := strings.Count(prompt, "{{")
	closeCount := strings.Count(prompt, "}}")

	if openCount != closeCount {
		return fmt.Errorf("event configuration %d: prompt has unmatched template brackets: %d opens, %d closes", index, openCount, closeCount)
	}

	// Check for potentially dangerous template constructs
	dangerousPatterns := []string{
		"{{/*",       // block comments
		"{{define",   // template definitions
		"{{template", // template calls
		"{{call",     // function calls
		"{{data",     // data access
		"{{urlquery", // URL encoding functions
		"{{print",    // print functions
		"{{printf",   // printf functions
		"{{println",  // println functions
		"{{js",       // JavaScript execution
		"{{html",     // HTML escaping (could be abused)
	}

	for _, pattern := range dangerousPatterns {
		if strings.Contains(prompt, pattern) {
			return fmt.Errorf("event configuration %d: prompt contains potentially dangerous template construct: %s", index, pattern)
		}
	}

	return nil
}

// ActiveEventStatus represents the status of an active event
type ActiveEventStatus struct {
	// EventType is the type of the active event
	// +kubebuilder:validation:Required
	EventType string `json:"eventType"`

	// ResourceName is the name of the Kubernetes resource involved
	// +kubebuilder:validation:Required
	ResourceName string `json:"resourceName"`

	// FirstSeen is when the event was first observed
	// +kubebuilder:validation:Required
	FirstSeen metav1.Time `json:"firstSeen"`

	// LastSeen is when the event was last observed
	// +kubebuilder:validation:Required
	LastSeen metav1.Time `json:"lastSeen"`

	// Status indicates whether the event is firing or resolved
	// +kubebuilder:validation:Enum=firing;resolved
	// +kubebuilder:validation:Required
	Status string `json:"status"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:webhook:path=/validate-kagent-dev-v1alpha2-hook,mutating=false,failurePolicy=fail,sideEffects=None,groups=kagent.dev,resources=hooks,verbs=create;update,versions=v1alpha2,name=vhook.kb.io,admissionReviewVersions=v1

// Hook is the Schema for the hooks API
type Hook struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   HookSpec   `json:"spec,omitempty"`
	Status HookStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// HookList contains a list of Hook
type HookList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Hook `json:"items"`
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Hook) DeepCopyInto(out *Hook) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Hook.
func (in *Hook) DeepCopy() *Hook {
	if in == nil {
		return nil
	}
	out := new(Hook)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *Hook) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *HookList) DeepCopyInto(out *HookList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]Hook, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new HookList.
func (in *HookList) DeepCopy() *HookList {
	if in == nil {
		return nil
	}
	out := new(HookList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *HookList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *HookSpec) DeepCopyInto(out *HookSpec) {
	*out = *in
	if in.EventConfigurations != nil {
		in, out := &in.EventConfigurations, &out.EventConfigurations
		*out = make([]EventConfiguration, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new HookSpec.
func (in *HookSpec) DeepCopy() *HookSpec {
	if in == nil {
		return nil
	}
	out := new(HookSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *HookStatus) DeepCopyInto(out *HookStatus) {
	*out = *in
	if in.ActiveEvents != nil {
		in, out := &in.ActiveEvents, &out.ActiveEvents
		*out = make([]ActiveEventStatus, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	in.LastUpdated.DeepCopyInto(&out.LastUpdated)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new HookStatus.
func (in *HookStatus) DeepCopy() *HookStatus {
	if in == nil {
		return nil
	}
	out := new(HookStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *EventConfiguration) DeepCopyInto(out *EventConfiguration) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new EventConfiguration.
func (in *EventConfiguration) DeepCopy() *EventConfiguration {
	if in == nil {
		return nil
	}
	out := new(EventConfiguration)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ActiveEventStatus) DeepCopyInto(out *ActiveEventStatus) {
	*out = *in
	in.FirstSeen.DeepCopyInto(&out.FirstSeen)
	in.LastSeen.DeepCopyInto(&out.LastSeen)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ActiveEventStatus.
func (in *ActiveEventStatus) DeepCopy() *ActiveEventStatus {
	if in == nil {
		return nil
	}
	out := new(ActiveEventStatus)
	in.DeepCopyInto(out)
	return out
}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *Hook) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	hook, ok := obj.(*Hook)
	if !ok {
		return nil, fmt.Errorf("expected a Hook object, got %T", obj)
	}
	return validateHook(hook)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *Hook) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	hook, ok := newObj.(*Hook)
	if !ok {
		return nil, fmt.Errorf("expected a Hook object, got %T", newObj)
	}
	return validateHook(hook)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *Hook) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	// Allow all deletions
	return nil, nil
}

// validateHook performs validation logic for Hook resources
func validateHook(hook *Hook) (admission.Warnings, error) {
	var allErrs []string
	var warnings admission.Warnings

	// Validate that eventConfigurations is not empty
	if len(hook.Spec.EventConfigurations) == 0 {
		allErrs = append(allErrs, "spec.eventConfigurations cannot be empty")
	}

	// Validate each event configuration
	eventTypes := make(map[string]bool)
	for i, config := range hook.Spec.EventConfigurations {
		// Check for duplicate event types
		if eventTypes[config.EventType] {
			allErrs = append(allErrs, fmt.Sprintf("spec.eventConfigurations[%d]: duplicate eventType '%s'", i, config.EventType))
		}
		eventTypes[config.EventType] = true

		// Validate event type
		if !isValidEventType(config.EventType) {
			allErrs = append(allErrs, fmt.Sprintf("spec.eventConfigurations[%d].eventType: invalid event type '%s', must be one of: pod-restart, pod-pending, oom-kill, probe-failed", i, config.EventType))
		}

		// Validate agentId is not empty
		if strings.TrimSpace(config.AgentRef.Name) == "" {
			allErrs = append(allErrs, fmt.Sprintf("spec.eventConfigurations[%d].agentId: cannot be empty", i))
		}

		// Validate prompt is not empty
		if strings.TrimSpace(config.Prompt) == "" {
			allErrs = append(allErrs, fmt.Sprintf("spec.eventConfigurations[%d].prompt: cannot be empty", i))
		}

		// Warn about potentially long prompts
		if len(config.Prompt) > 1000 {
			warnings = append(warnings, fmt.Sprintf("spec.eventConfigurations[%d].prompt: prompt is very long (%d characters), consider shortening for better performance", i, len(config.Prompt)))
		}
	}

	if len(allErrs) > 0 {
		return warnings, fmt.Errorf("validation failed: %s", strings.Join(allErrs, "; "))
	}

	return warnings, nil
}

// isValidEventType checks if the provided event type is valid
func isValidEventType(eventType string) bool {
	validTypes := map[string]bool{
		"pod-restart":  true,
		"pod-pending":  true,
		"oom-kill":     true,
		"probe-failed": true,
	}
	return validTypes[eventType]
}

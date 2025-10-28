package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kagentv1alpha2 "github.com/kagent-dev/khook/api/v1alpha2"
)

func TestCalculateSignature(t *testing.T) {
	wm := &WorkflowManager{}

	tests := []struct {
		name      string
		hooks     []*kagentv1alpha2.Hook
		signature string
	}{
		{
			name: "single event form",
			hooks: []*kagentv1alpha2.Hook{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-hook",
						Namespace: "default",
					},
					Spec: kagentv1alpha2.HookSpec{
						EventConfigurations: []kagentv1alpha2.EventConfiguration{
							{
								EventType: "pod-restart",
								AgentRef: kagentv1alpha2.ObjectReference{
									Name: "test-agent",
								},
								Prompt: "Test prompt",
							},
						},
					},
				},
			},
			signature: "default/test-hook@pod-restart|test-agent|Test prompt",
		},
		{
			name: "grouped source form",
			hooks: []*kagentv1alpha2.Hook{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-hook",
						Namespace: "default",
					},
					Spec: kagentv1alpha2.HookSpec{
						EventConfigurations: []kagentv1alpha2.EventConfiguration{
							{
								Source: &kagentv1alpha2.EventSourceEvents{
									Name: "redis-events",
									Events: []kagentv1alpha2.SourceEvent{
										{
											EventType: "create-issue",
											AgentRef: kagentv1alpha2.ObjectReference{
												Name: "jira-agent",
											},
											Prompt: "Create issue",
										},
									},
								},
							},
						},
					},
				},
			},
			signature: "default/test-hook@redis-events:create-issue|jira-agent|Create issue",
		},
		{
			name: "grouped source form with multiple events",
			hooks: []*kagentv1alpha2.Hook{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-hook",
						Namespace: "default",
					},
					Spec: kagentv1alpha2.HookSpec{
						EventConfigurations: []kagentv1alpha2.EventConfiguration{
							{
								Source: &kagentv1alpha2.EventSourceEvents{
									Name: "redis-events",
									Events: []kagentv1alpha2.SourceEvent{
										{
											EventType: "create-issue",
											AgentRef: kagentv1alpha2.ObjectReference{
												Name: "jira-agent",
											},
											Prompt: "Create issue",
										},
										{
											EventType: "resolve-issue",
											AgentRef: kagentv1alpha2.ObjectReference{
												Name: "jira-agent",
											},
											Prompt: "Resolve issue",
										},
									},
								},
							},
						},
					},
				},
			},
			signature: "default/test-hook@redis-events:create-issue|jira-agent|Create issue;redis-events:resolve-issue|jira-agent|Resolve issue",
		},
		{
			name: "prompt change should change signature",
			hooks: []*kagentv1alpha2.Hook{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-hook",
						Namespace: "default",
					},
					Spec: kagentv1alpha2.HookSpec{
						EventConfigurations: []kagentv1alpha2.EventConfiguration{
							{
								Source: &kagentv1alpha2.EventSourceEvents{
									Name: "redis-events",
									Events: []kagentv1alpha2.SourceEvent{
										{
											EventType: "create-issue",
											AgentRef: kagentv1alpha2.ObjectReference{
												Name: "jira-agent",
											},
											Prompt: "Create issue with details",
										},
									},
								},
							},
						},
					},
				},
			},
			signature: "default/test-hook@redis-events:create-issue|jira-agent|Create issue with details",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sig := wm.CalculateSignature(tt.hooks)
			assert.Equal(t, tt.signature, sig)
		})
	}
}

func TestCalculateSignatureChangesOnPromptUpdate(t *testing.T) {
	wm := &WorkflowManager{}

	hook := &kagentv1alpha2.Hook{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-hook",
			Namespace: "default",
		},
		Spec: kagentv1alpha2.HookSpec{
			EventConfigurations: []kagentv1alpha2.EventConfiguration{
				{
					Source: &kagentv1alpha2.EventSourceEvents{
						Name: "redis-events",
						Events: []kagentv1alpha2.SourceEvent{
							{
								EventType: "create-issue",
								AgentRef: kagentv1alpha2.ObjectReference{
									Name: "jira-agent",
								},
								Prompt: "Create an issue for this event data.",
							},
						},
					},
				},
			},
		},
	}

	sig1 := wm.CalculateSignature([]*kagentv1alpha2.Hook{hook})

	// Update the prompt
	hook.Spec.EventConfigurations[0].Source.Events[0].Prompt = "Create an issue for this event data. Include all context and details."

	sig2 := wm.CalculateSignature([]*kagentv1alpha2.Hook{hook})

	// Signatures should be different
	assert.NotEqual(t, sig1, sig2, "Signature should change when prompt is updated")
}


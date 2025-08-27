/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha2

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestHookValidation(t *testing.T) {
	hook := &Hook{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-hook",
			Namespace: "default",
		},
		Spec: HookSpec{
			EventConfigurations: []EventConfiguration{
				{
					EventType: "pod-restart",
					AgentId:   "agent-123",
					Prompt:    "Pod has restarted",
				},
			},
		},
	}

	// Test ValidateCreate
	_, err := hook.ValidateCreate(context.Background(), hook)
	if err != nil {
		t.Errorf("ValidateCreate() unexpected error = %v", err)
	}

	// Test ValidateUpdate
	_, err = hook.ValidateUpdate(context.Background(), hook, hook)
	if err != nil {
		t.Errorf("ValidateUpdate() unexpected error = %v", err)
	}

	// Test ValidateDelete
	_, err = hook.ValidateDelete(context.Background(), hook)
	if err != nil {
		t.Errorf("ValidateDelete() unexpected error = %v", err)
	}
}

func TestHookDeepCopy(t *testing.T) {
	original := &Hook{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-hook",
			Namespace: "default",
		},
		Spec: HookSpec{
			EventConfigurations: []EventConfiguration{
				{
					EventType: "pod-restart",
					AgentId:   "agent-123",
					Prompt:    "Pod has restarted",
				},
			},
		},
		Status: HookStatus{
			ActiveEvents: []ActiveEventStatus{
				{
					EventType:    "pod-restart",
					ResourceName: "test-pod",
					FirstSeen:    metav1.Now(),
					LastSeen:     metav1.Now(),
					Status:       "firing",
				},
			},
			LastUpdated: metav1.Now(),
		},
	}

	// Test DeepCopy
	copied := original.DeepCopy()
	if copied == nil {
		t.Error("DeepCopy() returned nil")
		return
	}

	if copied.Name != original.Name {
		t.Errorf("DeepCopy() name mismatch: got %v, want %v", copied.Name, original.Name)
	}

	if len(copied.Spec.EventConfigurations) != len(original.Spec.EventConfigurations) {
		t.Errorf("DeepCopy() event configurations length mismatch: got %v, want %v",
			len(copied.Spec.EventConfigurations), len(original.Spec.EventConfigurations))
	}

	// Test DeepCopyObject
	obj := original.DeepCopyObject()
	if obj == nil {
		t.Error("DeepCopyObject() returned nil")
		return
	}

	hookObj, ok := obj.(*Hook)
	if !ok {
		t.Errorf("DeepCopyObject() returned wrong type: got %T, want *Hook", obj)
		return
	}

	if hookObj.Name != original.Name {
		t.Errorf("DeepCopyObject() name mismatch: got %v, want %v", hookObj.Name, original.Name)
	}
}

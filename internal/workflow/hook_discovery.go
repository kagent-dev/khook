package workflow

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	kagentv1alpha2 "github.com/kagent-dev/khook/api/v1alpha2"
)

// HookDiscoveryService handles cluster-wide discovery of Hook resources
type HookDiscoveryService struct {
	client client.Client
}

// NewHookDiscoveryService creates a new hook discovery service
func NewHookDiscoveryService(client client.Client) *HookDiscoveryService {
	return &HookDiscoveryService{client: client}
}

// DiscoverHooks discovers all Hook resources cluster-wide and groups them by namespace
func (s *HookDiscoveryService) DiscoverHooks(ctx context.Context) (map[string][]*kagentv1alpha2.Hook, error) {
	var hookList kagentv1alpha2.HookList
	if err := s.client.List(ctx, &hookList, &client.ListOptions{}); err != nil {
		return nil, fmt.Errorf("failed to list hooks: %w", err)
	}

	byNS := map[string][]*kagentv1alpha2.Hook{}
	for i := range hookList.Items {
		h := hookList.Items[i]
		ns := h.Namespace
		byNS[ns] = append(byNS[ns], &h)
	}

	return byNS, nil
}

// GetHookCount returns the total number of hooks discovered
func (s *HookDiscoveryService) GetHookCount(hooksByNamespace map[string][]*kagentv1alpha2.Hook) int {
	count := 0
	for _, hooks := range hooksByNamespace {
		count += len(hooks)
	}
	return count
}

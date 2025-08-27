package controller

import (
	"context"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kagentv1alpha2 "github.com/kagent/hook-controller/api/v1alpha2"
	"github.com/kagent/hook-controller/internal/config"
	"github.com/kagent/hook-controller/internal/interfaces"
)

// HookReconciler reconciles a Hook object
type HookReconciler struct {
	client.Client
	Scheme               *runtime.Scheme
	Log                  logr.Logger
	Config               *config.Config
	EventWatcher         interfaces.EventWatcher
	KagentClient         interfaces.KagentClient
	DeduplicationManager interfaces.DeduplicationManager
}

//+kubebuilder:rbac:groups=kagent.dev,resources=hooks,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=kagent.dev,resources=hooks/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=kagent.dev,resources=hooks/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=events,verbs=get;list;watch;create;update;patch

// Reconcile is part of the main kubernetes reconciliation loop
func (r *HookReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("hook", req.NamespacedName)

	// TODO: Implement reconciliation logic in task 6
	log.Info("Reconciling Hook", "name", req.Name, "namespace", req.Namespace)

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager
func (r *HookReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kagentv1alpha2.Hook{}).
		Complete(r)
}

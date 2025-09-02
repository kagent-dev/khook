package main

import (
	"context"
	"flag"
	"os"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	kubeclient "k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	kagentv1alpha2 "github.com/kagent/hook-controller/api/v1alpha2"
	kclient "github.com/kagent/hook-controller/internal/client"
	"github.com/kagent/hook-controller/internal/config"
	ddup "github.com/kagent/hook-controller/internal/deduplication"
	"github.com/kagent/hook-controller/internal/event"
	"github.com/kagent/hook-controller/internal/pipeline"
	"github.com/kagent/hook-controller/internal/status"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(kagentv1alpha2.AddToScheme(scheme))
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var configFile string

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false, "Enable leader election for controller manager.")
	flag.StringVar(&configFile, "config", "", "The controller will load its initial configuration from this file.")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// Load configuration
	_, err := config.Load(configFile)
	if err != nil {
		setupLog.Error(err, "unable to load configuration")
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "khook",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// Setup health checks
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	// Add workflow runner to discover hooks across namespaces and watch events
	if err := mgr.Add(newWorkflowRunner(mgr)); err != nil {
		setupLog.Error(err, "unable to add workflow runner")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

// workflowRunner discovers Hook resources cluster-wide and starts per-namespace event processing workflows
type workflowRunner struct {
	client crclient.Client
	mgr    ctrl.Manager
}

func newWorkflowRunner(mgr ctrl.Manager) *workflowRunner {
	return &workflowRunner{client: mgr.GetClient(), mgr: mgr}
}

func (w *workflowRunner) NeedLeaderElection() bool { return true }

func (w *workflowRunner) Start(ctx context.Context) error {
	logger := log.Log.WithName("workflow-runner")
	logger.Info("Starting workflow runner")

	cfg := ctrl.GetConfigOrDie()
	k8s, err := kubeclient.NewForConfig(cfg)
	if err != nil {
		logger.Error(err, "failed to create kubernetes clientset")
		return err
	}

	dedup := ddup.NewManager()
	kagentCli, err := kclient.NewClientFromEnv(log.Log.WithName("kagent-client"))
	if err != nil {
		logger.Error(err, "failed to initialize Kagent client from env")
		return err
	}

	// nsState tracks per-namespace workflow state:
	// - cancel: cancels the running workflow goroutine for the namespace
	// - signature: hash of current hooks in the namespace to detect changes
	type nsState struct {
		cancel    context.CancelFunc
		signature string
	}
	nsRunners := map[string]*nsState{}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	sync := func() error {
		logger.V(1).Info("Listing hooks cluster-wide")
		var hookList kagentv1alpha2.HookList
		if err := w.client.List(ctx, &hookList, &crclient.ListOptions{}); err != nil {
			logger.Error(err, "failed to list hooks")
			return err
		}
		logger.Info("Listed hooks", "count", len(hookList.Items))

		byNS := map[string][]*kagentv1alpha2.Hook{}
		for i := range hookList.Items {
			h := hookList.Items[i]
			ns := h.Namespace
			byNS[ns] = append(byNS[ns], &h)
		}

		for ns, hooks := range byNS {
			logger.Info("Namespace hooks discovered", "namespace", ns, "hookCount", len(hooks))
			sig := signatureForHooks(hooks)
			if st, ok := nsRunners[ns]; ok {
				if st.signature == sig {
					logger.V(1).Info("No changes in hooks; keeping workflow running", "namespace", ns)
					continue
				}
				logger.Info("Restarting namespace workflow due to hook changes", "namespace", ns)
				st.cancel()
				delete(nsRunners, ns)
			}

			ctxNS, cancel := context.WithCancel(ctx)
			nsRunners[ns] = &nsState{cancel: cancel, signature: sig}
			types := uniqueEventTypes(hooks)
			logger.Info("Computed event types for namespace", "namespace", ns, "eventTypes", types)

			// Build status manager using manager client and recorder
			statusMgr := status.NewManager(w.mgr.GetClient(), w.mgr.GetEventRecorderFor("khook"))

			go func(namespace string, hooks []*kagentv1alpha2.Hook, eventTypes []string) {
				logger.Info("Starting namespace workflow", "namespace", namespace, "hookCount", len(hooks), "eventTypes", eventTypes)
				watcher := event.NewWatcher(k8s, namespace)
				processor := pipeline.NewProcessor(watcher, dedup, kagentCli, statusMgr)
				if err := processor.ProcessEventWorkflow(ctxNS, eventTypes, hooks); err != nil {
					logger.Error(err, "namespace workflow exited with error", "namespace", namespace)
				} else {
					logger.Info("Namespace workflow finished", "namespace", namespace)
				}
			}(ns, hooks, types)
		}

		if len(byNS) == 0 {
			logger.Info("No hooks found; no workflows started")
		}

		for ns, st := range nsRunners {
			if _, ok := byNS[ns]; !ok {
				logger.Info("Stopping namespace workflow (no hooks)", "namespace", ns)
				st.cancel()
				delete(nsRunners, ns)
			}
		}
		return nil
	}

	_ = sync()
	for {
		select {
		case <-ctx.Done():
			logger.Info("Stopping workflow runner")
			for ns, st := range nsRunners {
				logger.Info("Stopping namespace workflow", "namespace", ns)
				st.cancel()
			}
			return nil
		case <-ticker.C:
			_ = sync()
		}
	}
}

func uniqueEventTypes(hooks []*kagentv1alpha2.Hook) []string {
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

func signatureForHooks(hooks []*kagentv1alpha2.Hook) string {
	parts := make([]string, 0, len(hooks))
	for _, h := range hooks {
		cfgs := make([]string, 0, len(h.Spec.EventConfigurations))
		for _, ec := range h.Spec.EventConfigurations {
			cfgs = append(cfgs, ec.EventType+"|"+ec.AgentId+"|"+ec.Prompt)
		}
		parts = append(parts, h.Namespace+"/"+h.Name+"@"+strings.Join(cfgs, ";"))
	}
	return strings.Join(parts, ",")
}

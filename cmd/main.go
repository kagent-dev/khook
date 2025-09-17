package main

import (
	"context"
	"flag"
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	kubeclient "k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	kagentv1alpha2 "github.com/kagent-dev/khook/api/v1alpha2"
	kclient "github.com/kagent-dev/khook/internal/client"
	"github.com/kagent-dev/khook/internal/config"
	"github.com/kagent-dev/khook/internal/workflow"
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

	// Add workflow coordinator to manage hooks and event processing
	if err := mgr.Add(newWorkflowCoordinator(mgr)); err != nil {
		setupLog.Error(err, "unable to add workflow coordinator")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

// workflowCoordinator manages the complete workflow lifecycle using proper services
type workflowCoordinator struct {
	mgr ctrl.Manager
}

func newWorkflowCoordinator(mgr ctrl.Manager) *workflowCoordinator {
	return &workflowCoordinator{mgr: mgr}
}

func (w *workflowCoordinator) NeedLeaderElection() bool { return true }

func (w *workflowCoordinator) Start(ctx context.Context) error {
	logger := log.Log.WithName("workflow-coordinator")
	logger.Info("Starting workflow coordinator")

	// Get Kubernetes clients
	cfg := ctrl.GetConfigOrDie()
	k8s, err := kubeclient.NewForConfig(cfg)
	if err != nil {
		logger.Error(err, "failed to create kubernetes clientset")
		return err
	}

	// Initialize Kagent client
	kagentCli, err := kclient.NewClientFromEnv(log.Log.WithName("kagent-client"))
	if err != nil {
		logger.Error(err, "failed to initialize Kagent client from env")
		return err
	}

	// Create workflow coordinator
	eventRecorder := w.mgr.GetEventRecorderFor("khook")
	coordinator := workflow.NewCoordinator(k8s, w.mgr.GetClient(), kagentCli, eventRecorder)

	// Start the coordinator
	return coordinator.Start(ctx)
}

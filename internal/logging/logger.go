package logging

import (
	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// SetupLogger configures the controller-runtime logger
func SetupLogger(level string, format string) logr.Logger {
	opts := zap.Options{
		Development: format != "json",
	}

	// Set log level based on configuration
	// Note: controller-runtime zap options handle level configuration differently
	// This is a simplified version for the basic setup

	logger := zap.New(zap.UseFlagOptions(&opts))
	ctrl.SetLogger(logger)

	return logger
}

// NewLogger creates a new logger with the given name
func NewLogger(name string) logr.Logger {
	return ctrl.Log.WithName(name)
}

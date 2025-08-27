# Deduplication Manager

This directory contains the DeduplicationManager implementation for event deduplication and state management.

## Overview

The DeduplicationManager provides in-memory storage and logic for tracking active Kubernetes events to prevent duplicate processing within a configurable timeout window (10 minutes by default).

## Components

### Manager
- `manager.go`: Core implementation of the DeduplicationManager interface
- `manager_test.go`: Comprehensive unit tests including time-based scenarios and concurrency tests

## Key Features

- **Event Deduplication**: Prevents processing of duplicate events within the timeout window
- **Timeout Management**: Automatically resolves events after 10 minutes
- **Thread Safety**: Uses mutex locks for concurrent access
- **Memory Efficient**: Automatically cleans up expired events
- **Status Tracking**: Tracks event status (firing/resolved) with timestamps

## Usage

```go
manager := deduplication.NewManager()

// Check if event should be processed
if manager.ShouldProcessEvent("hook-name", event) {
    // Process the event
    err := manager.RecordEvent("hook-name", event)
    if err != nil {
        // Handle error
    }
}

// Get active events for status reporting
activeEvents := manager.GetActiveEvents("hook-name")

// Cleanup expired events
err := manager.CleanupExpiredEvents("hook-name")
```

## Testing

Run tests with:
```bash
go test ./internal/deduplication -v
go test ./internal/deduplication -bench=.
```
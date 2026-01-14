## Multi-Source Event Ingestion (Built-in and External gRPC) - Design

### Context
Khook currently ingests Kubernetes Events via a single watcher (`internal/event/watcher.go`) and matches them to `Hook` specs within the event processing pipeline (`internal/pipeline/processor.go`). To support additional event sources we will introduce a first-class, extensible event source model:

- Built-in sources are implemented in-tree as Go types that satisfy a common interface.
- External sources are defined as CRDs and consumed via a well-defined gRPC contract maintained out-of-tree.

This keeps core source types co-evolving with Khook while enabling third-parties to provide new sources without being compiled into the controller.

### Goals
- Add an extensible abstraction for multiple event sources.
- Support both built-in (compiled-in) and external (gRPC) event sources.
- Allow `Hook` to reference either a built-in event source or an external one.
- Maintain backward compatibility with existing `Hook` specs using `eventType`.
- Provide observability, resilience, and secure connectivity for external sources.

### Non-Goals
- Replacing existing pipeline/dedup/agent integration.
- Implementing every possible built-in source in this iteration (we’ll start with the existing Kubernetes Events source and structure for adding more).

### High-Level Architecture
- Introduce an `EventSource` interface. Each built-in source implements it. An `ExternalGRPCEventSource` implements it by connecting to an external service.
- Add an `EventMultiplexer` that aggregates any number of `EventSource`s into a single `EventWatcher`-compatible facade so the processing pipeline remains unchanged.
- Add a new CRD `ExternalEventSource` that configures the endpoint, TLS/auth, and options for external providers.
- Extend `Hook` spec so each `eventConfigurations[]` item is either:
  - a single built-in event using top-level `eventType`, `agentRef`, `prompt`, or
  - a grouped `source` with `name`, `namespace`, and `events[]` where each event has `eventType`, `agentRef`, `prompt`.
- `eventType` remains the matching key; external providers should emit supported event types.

### Interfaces
We introduce `EventSource` and a multiplexer that implements the existing `EventWatcher` contract. This minimizes changes to `internal/pipeline/processor.go`.

```go
// New, in internal/interfaces/eventsources.go
type EventSource interface {
    // Start should be idempotent; it begins producing events into an internal channel.
    Start(ctx context.Context) error
    // Events returns a read-only channel of events for this source.
    Events() <-chan Event
    // Stop gracefully stops the source (cancel context or internal stop channel).
    Stop() error
    // Name is a stable identifier for logging/metrics.
    Name() string
}

// New, in internal/event/multiplexer.go
// Multiplexer aggregates N EventSources and exposes them as a single EventWatcher.
type Multiplexer struct {
    sources []interfaces.EventSource
}

// Satisfy existing EventWatcher to avoid refactoring the pipeline.
func (m *Multiplexer) WatchEvents(ctx context.Context) (<-chan interfaces.Event, error) { /* merge fan-in */ }
func (m *Multiplexer) Start(ctx context.Context) error { /* start all */ }
func (m *Multiplexer) Stop() error { /* stop all */ }
func (m *Multiplexer) FilterEvent(event interfaces.Event, hooks []*v1alpha2.Hook) []interfaces.EventMatch { /* optional passthrough */ return nil }
```

Built-in sources will be created in-tree. The current Kubernetes Events watcher becomes one such `EventSource` implementation (thin adaptation of `Watcher`).

### External gRPC Event Source
We will support external providers over gRPC with a streaming contract that is purpose-built for events (inspired by KEDA’s external scaler pattern). Khook will open and maintain a long-lived stream to the external endpoint and translate incoming messages into `interfaces.Event` instances.

#### Protocol (proposed)
```proto
// pkg: externalevents.v1
syntax = "proto3";

package externalevents.v1;

message WatchRequest {
  // Namespace scoping (optional). Empty means cluster-wide if allowed by policy.
  string namespace = 1;
  // Arbitrary configuration payload (JSON-encoded string or map) from the CRD.
  string config_json = 2;
  // Optional per-hook hints (e.g., event types). Aggregated by Khook for efficiency.
  repeated string hints = 3;
}

message EventMessage {
  string type = 1;            // e.g., "pod-restart" or arbitrary external type
  string resource_name = 2;
  string namespace = 3;
  string reason = 4;
  string message = 5;
  string uid = 6;
  map<string, string> metadata = 7;
  int64 timestamp_unix = 8;   // seconds since epoch
}

service ExternalEventSource {
  rpc StreamEvents(WatchRequest) returns (stream EventMessage);
}
```

Notes:
- We keep a single server stream per configured ExternalEventSource (and optionally per namespace) with server push of events.
- If the upstream doesn’t support server-streaming, providers can buffer and ‘push’ polled events through the stream.
- `hints` in `WatchRequest` are derived automatically by Khook from all declared events: top-level single events and every `source.events[*].eventType`. Hooks do not configure hints directly.

#### Resilience
- Exponential backoff reconnects with jitter.
- Per-source circuit breaking and health metrics.
- Optional heartbeats (e.g., periodic empty `EventMessage` or explicit health stream) to detect dead connections.

#### Security
- TLS and optional mTLS support (CA bundle, client cert/key via Kubernetes Secret).
- Per-source token or basic auth via Secret reference.
- Namespaced scoping by default; cross-namespace and cluster-wide may require elevated RBAC and explicit allow flags.

### CRD Changes

#### 1) New CRD: ExternalEventSource
Group/Version: `kagent.dev/v1alpha2`
Kind: `ExternalEventSource`

```yaml
apiVersion: kagent.dev/v1alpha2
kind: ExternalEventSource
metadata:
  name: my-external-source
  namespace: default
spec:
  endpoint:
    address: "externalevents.default.svc:8080"  # host:port
    tls:
      enabled: true
      # optional mTLS + custom CA; single Secret holds CA, cert, and key
      secretRef:
        name: my-tls
        caKey: ca.crt
        certKey: tls.crt
        keyKey: tls.key
    # optional per-connection auth
    auth:
      type: BearerToken # or Basic
      secretRef: { name: external-auth, key: token }

  # Optional JSON configuration string passed to the provider
  config:
    json: |
      { "filter": { "kinds": ["Pod"], "reasons": ["BackOff"] } }

  # Connection policy
  connection:
    namespaceScope: true   # restrict stream to this namespace
    reconnectBackoff:
      initialSeconds: 2
      maxSeconds: 60

status:
  conditions:
  - type: Ready
    status: "True"
    reason: Connected
    message: "Stream established"
  lastConnectedTime: "2025-10-09T10:00:00Z"
```

Validation highlights:
- `address` required; scheme optional (defaults to TLS when `tls.enabled: true`).
- A single `tls.secretRef` is used; it must exist in the same namespace as the `ExternalEventSource` and contain keys for CA, certificate, and private key as configured.

Controller responsibilities (new):
- Reconcile `ExternalEventSource` objects; manage gRPC connections; maintain `Ready` condition and last connection info.
- Provide events to the `EventMultiplexer` via the `ExternalGRPCEventSource` implementation.

#### 2) Extend Hook spec to reference sources
We retain the current single-event schema for built-in sources and add a grouped `source` form for external (or named) sources to define multiple events. Each item in `eventConfigurations[]` must be either a single built-in event or a grouped `source` with an `events[]` list. External providers MUST emit supported event types.

```yaml
apiVersion: kagent.dev/v1alpha2
kind: Hook
spec:
  eventConfigurations:
  - # built-in Kubernetes source (default)
    eventType: pod-restart
    agentRef: { name: kagent }
    prompt: "Pod {{.ResourceName}} restarted"

  - # external gRPC source via CRD reference: multiple events grouped
    source:
      name: my-external-source
      namespace: default
      events:
      - eventType: pod-restart
        agentRef: { name: kagent }
        prompt: "External restart: {{.ResourceName}}"
      - eventType: probe-failed
        agentRef: { name: kagent }
        prompt: "Probe failed on {{.ResourceName}}"
```

API changes (additive):
- In `api/v1alpha2/hook_types.go`, make `EventConfiguration` a union-like struct supporting either a single built-in event or a grouped source:
  - Single built-in event: `EventType`, `AgentRef`, `Prompt`
  - Grouped source: `Source *EventSourceEvents` with `Name`, `Namespace`, `Events []SourceEvent{ EventType, AgentRef, Prompt }`
- Validation: exactly one form per list item. All `eventType` values must be in the supported set.

Example shape (Go):
```go
type EventConfiguration struct {
    // Single built-in event form
    EventType  string          `json:"eventType,omitempty"`
    AgentRef   ObjectReference `json:"agentRef,omitempty"`
    Prompt     string          `json:"prompt,omitempty"`

    // Grouped source form
    Source *EventSourceEvents `json:"source,omitempty"`
}

type EventSourceEvents struct {
    Name      string        `json:"name"`
    Namespace string        `json:"namespace,omitempty"`
    Events    []SourceEvent `json:"events"`
}

type SourceEvent struct {
    EventType string          `json:"eventType"`
    AgentRef  ObjectReference `json:"agentRef"`
    Prompt    string          `json:"prompt"`
}
```

### Controller and Workflow Changes
- `internal/workflow`: During workflow startup per namespace, build a set of sources from:
  - Existing Kubernetes Events source (built-in), optionally filtered by hooks.
  - Any referenced `ExternalEventSource` objects (namespace-scoped) required by hooks.
- Instantiate an `EventMultiplexer` with all required `EventSource`s and pass it to the pipeline.
- The pipeline remains unchanged; it receives a single stream and finds matches per event.

### Built-in Sources (initial set)
- `kubernetes-events` (existing watcher adapted to `EventSource`).
- Future: `kube-state-metrics`, `prometheus-alerts`, etc., may be added similarly.

### Observability
- Per-source metrics: connection status, reconnects, events per second, errors.
- Structured logs include `source.name` and `source.type`.
- Expose readiness state of `ExternalEventSource` via CR status conditions.

### Security Considerations
- All external connections use TLS by default when `tls.enabled` is true; support mTLS via a single Secret containing CA, cert, and key.
- Secrets stay in the same namespace as the `ExternalEventSource` to prevent privilege escalation.
- Optional allowlist of external hostnames in the controller config.
- Namespace scoping enforced: controllers only request events for their namespace unless explicitly configured.

### Backward Compatibility
- Hooks using `eventType` keep working, mapped to the built-in Kubernetes Events source with the same event type mapping as today.
- Validation ensures that new fields are optional; old manifests remain valid.

### Failure Modes and Recovery
- External stream drops: auto-reconnect with backoff and jitter.
- Provider overload: backpressure via channel buffering; drop policy and metrics to surface issues.
- CRD/Secret updates: reconcile triggers a reconnect with updated settings.

### Implementation Plan (Phased)
1) API and CRD
   - Add grouped `source.events[]` form to `EventConfiguration`; keep single built-in `eventType` form.
   - Introduce `ExternalEventSource` CRD, manifests, and validation.
2) Interfaces and Core
   - Add `EventSource` interface and `EventMultiplexer` that implements `EventWatcher`.
   - Adapt existing Kubernetes watcher into a built-in `EventSource`.
3) External gRPC Client
   - Implement `ExternalGRPCEventSource` with TLS/mTLS, auth, reconnection, metrics.
   - Controller for `ExternalEventSource` to maintain status conditions.
4) Wiring
   - Update workflow manager to construct sources based on hooks and existing CRDs in the namespace.
   - Keep `Processor` unchanged (consumes the multiplexed stream).
5) Docs & Examples
   - Add examples for `ExternalEventSource` and `Hook` referencing it.
   - Troubleshooting guide for connection issues.

### Open Questions
- Should we support fan-out multiplexing per `Hook` vs per `Namespace`? Initial approach: per-namespace shared streams for efficiency.
- Do we need request-scoped auth per hook? Initial approach: connection-level auth on the external source.
- Should we support batch/event envelope compression in gRPC? Defer until needed.



# Redis Event Source for khook

A standalone gRPC server that streams events from Redis Pub/Sub to khook.

## Overview

This external event source implementation connects to Redis and subscribes to Pub/Sub channels, forwarding messages as events to khook via gRPC streaming.

## Architecture

```
┌─────────────────┐         ┌──────────────────────┐         ┌────────────┐
│                 │         │                      │         │            │
│  Redis Pub/Sub  │────────>│  Redis Event Source  │────────>│   khook    │
│                 │ PUBSUB  │    (gRPC Server)     │  gRPC   │            │
└─────────────────┘         └──────────────────────┘         └────────────┘
```

The Redis Event Source:
- Subscribes to one or more Redis Pub/Sub channels
- Receives JSON-formatted event messages
- Streams them to khook via the ExternalEventSource gRPC protocol

## Event Message Format

Events published to Redis channels must be JSON objects with the following structure:

```json
{
  "type": "name-change",
  "resource_name": "my-resource",
  "namespace": "default",
  "reason": "NameChanged",
  "message": "Resource name was changed",
  "uid": "unique-id",
  "metadata": {
    "key": "value"
  },
  "timestamp_unix": 1729091066
}
```

## Configuration

### Command Line Flags

- `--port` - gRPC server port (default: 8080)
- `--redis-addr` - Redis connection string (required, format: `redis://host:port` or `rediss://host:port`)
- `--tls-cert` - Path to TLS certificate file (optional)
- `--tls-key` - Path to TLS key file (optional)

### Channel Configuration

Channels are configured via the `config_json` field in the `ExternalEventSource` CRD:

```yaml
apiVersion: kagent.dev/v1alpha2
kind: ExternalEventSource
metadata:
  name: redis-events
  namespace: default
spec:
  endpoint:
    address: "redis-event-source.default.svc:8080"
    tls:
      enabled: false
  config:
    json: '{"channels": ["khook:events", "app:alerts", "system:events"]}'
```

If no channels are specified, the service defaults to subscribing to `khook:events`.

## Building

### Local Build

```bash
cd examples/redis-event-source
go mod download
go build -o redis-event-source ./cmd
```

### Docker Build

```bash
docker build -t redis-event-source:latest -f examples/redis-event-source/Dockerfile .
```

## Running

### Locally

```bash
./redis-event-source --redis-addr redis://localhost:6379 --port 8080
```

### With TLS

```bash
./redis-event-source \
  --redis-addr redis://localhost:6379 \
  --port 8080 \
  --tls-cert /path/to/cert.pem \
  --tls-key /path/to/key.pem
```

### In Kubernetes

```bash
kubectl apply -f examples/redis-event-source/deploy/
```

## Deployment

See the `deploy/` directory for Kubernetes manifests:

- `deployment.yaml` - Deployment for the Redis Event Source
- `service.yaml` - Service exposing the gRPC server
- `external-event-source.yaml` - ExternalEventSource CRD instance

## Testing

### Publish a Test Event

```bash
kubectl exec -n redis <redis-pod> -- redis-cli PUBLISH "khook:events" '{
  "type": "name-change",
  "resource_name": "test-resource",
  "namespace": "default",
  "reason": "NameChanged",
  "message": "Test event from Redis",
  "uid": "test-123",
  "metadata": {"test": "true"},
  "timestamp_unix": '$(date +%s)'
}'
```

### Verify Event Reception

Check khook logs for event processing:

```bash
kubectl logs -n kagent -l app.kubernetes.io/name=khook | grep "name-change"
```

## Development

### Prerequisites

- Go 1.24+
- Protocol Buffers compiler (`protoc`)
- Go gRPC plugins

### Generate gRPC Code

From the repository root:

```bash
protoc --go_out=. --go_opt=paths=source_relative \
  --go-grpc_out=. --go-grpc_opt=paths=source_relative \
  api/proto/externalevents/v1/external_events.proto
```

## Troubleshooting

### Connection Issues

Check that:
1. Redis is accessible at the specified address
2. The gRPC server port is accessible from khook
3. TLS configuration matches between client and server

### No Events Received

Verify that:
1. Messages are being published to the correct Redis channel
2. The channel name matches the configuration
3. Message format is valid JSON matching the expected schema

### Logs

Enable debug logging:

```bash
./redis-event-source --redis-addr redis://localhost:6379 --port 8080 2>&1 | tee redis-source.log
```

## License

See the main khook repository license.

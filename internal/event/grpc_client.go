package event

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/kagent-dev/khook/api/proto/externalevents/v1"
	"github.com/kagent-dev/khook/internal/interfaces"
)

// GRPCClient implements the GRPCDialer interface by connecting to an external
// gRPC event source server that implements the ExternalEventSource service.
type GRPCClient struct{}

func (c *GRPCClient) Stream(ctx context.Context, address string, tlsConfig *tls.Config, authHeader string, config string, onEvent func(e interfaces.Event)) error {
	if address == "" {
		return fmt.Errorf("grpc client: empty address")
	}

	// Prepare dial options
	var opts []grpc.DialOption

	if tlsConfig != nil {
		// Use provided TLS config
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)))
	} else {
		// Use insecure connection
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	// TODO: Add auth header as gRPC metadata if provided
	// if authHeader != "" {
	//     opts = append(opts, grpc.WithPerRPCCredentials(customAuth{authHeader}))
	// }

	// Connect to the gRPC server
	conn, err := grpc.NewClient(address, opts...)
	if err != nil {
		return fmt.Errorf("grpc client: failed to dial: %w", err)
	}
	defer conn.Close()

	// Create client
	client := pb.NewExternalEventSourceClient(conn)

	// Create watch request
	req := &pb.WatchRequest{
		ConfigJson: config,
		// Hints could be populated from hook event types if needed
		Hints: []string{},
	}

	// Start streaming
	stream, err := client.StreamEvents(ctx, req)
	if err != nil {
		return fmt.Errorf("grpc client: failed to stream events: %w", err)
	}

	// Receive events
	for {
		event, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("grpc client: stream error: %w", err)
		}

		// Convert protobuf event to internal event
		ts := time.Now()
		if event.TimestampUnix != 0 {
			ts = time.Unix(event.TimestampUnix, 0)
		}

		// Convert metadata from map[string]string to map[string]interface{}
		metadata := make(map[string]interface{})
		for k, v := range event.Metadata {
			metadata[k] = v
		}

		onEvent(interfaces.Event{
			Type:         event.Type,
			ResourceName: event.ResourceName,
			Namespace:    event.Namespace,
			Reason:       event.Reason,
			Message:      event.Message,
			UID:          event.Uid,
			Metadata:     metadata,
			Timestamp:    ts,
		})
	}
}

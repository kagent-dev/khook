package main

import (
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	redis "github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	pb "github.com/kagent-dev/khook/examples/redis-event-source/proto/externalevents/v1"
)

type server struct {
	pb.UnimplementedExternalEventSourceServer
	redisAddr string
}

type redisEventMessage struct {
	Type          string            `json:"type"`
	ResourceName  string            `json:"resource_name"`
	Namespace     string            `json:"namespace"`
	Reason        string            `json:"reason"`
	Message       string            `json:"message"`
	UID           string            `json:"uid"`
	Metadata      map[string]string `json:"metadata"`
	TimestampUnix int64             `json:"timestamp_unix"`
}

func (s *server) StreamEvents(req *pb.WatchRequest, stream pb.ExternalEventSource_StreamEventsServer) error {
	ctx := stream.Context()

	// Handle nil request
	if req == nil {
		log.Printf("StreamEvents called with nil request, using defaults")
		req = &pb.WatchRequest{
			Namespace:  "",
			ConfigJson: "",
		}
	}

	log.Printf("StreamEvents called: namespace=%s, config=%s", req.Namespace, req.ConfigJson)

	// Parse Redis address
	u, err := url.Parse(s.redisAddr)
	if err != nil {
		return fmt.Errorf("invalid redis address: %w", err)
	}

	// Configure Redis client
	opts := &redis.Options{
		Addr: u.Host,
	}
	if pw, ok := u.User.Password(); ok {
		opts.Password = pw
	}
	if u.Scheme == "rediss" {
		opts.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}

	rdb := redis.NewClient(opts)
	defer rdb.Close()

	// Test connection
	if err := rdb.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis connection failed: %w", err)
	}
	log.Printf("Connected to Redis at %s", opts.Addr)

	// Parse config to extract channel list
	channels := []string{"khook:events"} // default
	if req.ConfigJson != "" {
		var cfg map[string]interface{}
		if err := json.Unmarshal([]byte(req.ConfigJson), &cfg); err == nil {
			if channelList, ok := cfg["channels"].([]interface{}); ok {
				channels = nil // clear default if config specifies channels
				for _, ch := range channelList {
					if chStr, ok := ch.(string); ok {
						channels = append(channels, chStr)
					}
				}
			}
		}
	}
	if len(channels) == 0 {
		channels = []string{"khook:events"} // fallback
	}

	log.Printf("Subscribing to Redis channels: %v", channels)
	pubsub := rdb.Subscribe(ctx, channels...)
	defer pubsub.Close()

	ch := pubsub.Channel()
	for {
		select {
		case <-ctx.Done():
			log.Printf("Stream context done: %v", ctx.Err())
			return ctx.Err()
		case msg, ok := <-ch:
			if !ok {
				log.Printf("Redis channel closed")
				return fmt.Errorf("redis channel closed")
			}

			// First unmarshal to generic map to capture all fields
			var raw map[string]interface{}
			if err := json.Unmarshal([]byte(msg.Payload), &raw); err != nil {
				log.Printf("Failed to unmarshal event: %v, payload: %s", err, msg.Payload)
				continue
			}

			// Then unmarshal to structured type
			var em redisEventMessage
			if err := json.Unmarshal([]byte(msg.Payload), &em); err != nil {
				log.Printf("Failed to unmarshal event to struct: %v", err)
				continue
			}

			// Auto-populate metadata with unknown fields
			// Known fields that shouldn't go into metadata
			knownFields := map[string]bool{
				"type": true, "resource_name": true, "namespace": true,
				"reason": true, "message": true, "uid": true,
				"metadata": true, "timestamp_unix": true,
			}

			// Initialize metadata if nil
			if em.Metadata == nil {
				em.Metadata = make(map[string]string)
			}

			// Move unknown fields to metadata
			for key, value := range raw {
				if !knownFields[key] {
					// Strip common prefixes like "Metadata." or "metadata."
					// This handles cases where users send {"Metadata.field": "value"}
					cleanKey := key
					if strings.HasPrefix(strings.ToLower(key), "metadata.") {
						cleanKey = key[9:] // Remove "Metadata." or "metadata." prefix
					}

					// Convert value to string
					if strVal, ok := value.(string); ok {
						em.Metadata[cleanKey] = strVal
					} else if value != nil {
						// Convert other types to JSON string
						jsonBytes, _ := json.Marshal(value)
						em.Metadata[cleanKey] = string(jsonBytes)
					}
				}
			}

			// Map to protobuf EventMessage
			ts := time.Now().Unix()
			if em.TimestampUnix != 0 {
				ts = em.TimestampUnix
			}

			event := &pb.EventMessage{
				Type:          em.Type,
				ResourceName:  em.ResourceName,
				Namespace:     em.Namespace,
				Reason:        em.Reason,
				Message:       em.Message,
				Uid:           em.UID,
				Metadata:      em.Metadata,
				TimestampUnix: ts,
			}

			log.Printf("Streaming event: type=%s, resource=%s, namespace=%s, metadata=%+v", event.Type, event.ResourceName, event.Namespace, event.Metadata)
			if err := stream.Send(event); err != nil {
				log.Printf("Failed to send event: %v", err)
				return err
			}
		}
	}
}

func main() {
	var (
		port      = flag.Int("port", 8080, "gRPC server port")
		redisAddr = flag.String("redis-addr", "redis://localhost:6379", "Redis address (redis://host:port or rediss://host:port)")
		tlsCert   = flag.String("tls-cert", "", "Path to TLS certificate file")
		tlsKey    = flag.String("tls-key", "", "Path to TLS key file")
	)
	flag.Parse()

	// Parse and validate Redis address
	if !strings.HasPrefix(*redisAddr, "redis://") && !strings.HasPrefix(*redisAddr, "rediss://") {
		log.Fatalf("Invalid redis-addr: must start with redis:// or rediss://")
	}

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	var opts []grpc.ServerOption

	// Configure TLS if cert and key are provided
	if *tlsCert != "" && *tlsKey != "" {
		creds, err := credentials.NewServerTLSFromFile(*tlsCert, *tlsKey)
		if err != nil {
			log.Fatalf("Failed to load TLS credentials: %v", err)
		}
		opts = append(opts, grpc.Creds(creds))
		log.Printf("TLS enabled")
	}

	grpcServer := grpc.NewServer(opts...)
	pb.RegisterExternalEventSourceServer(grpcServer, &server{redisAddr: *redisAddr})

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		log.Printf("Received signal %v, shutting down gracefully", sig)
		grpcServer.GracefulStop()
	}()

	log.Printf("Redis Event Source gRPC server listening on :%d", *port)
	log.Printf("Redis address: %s", *redisAddr)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}

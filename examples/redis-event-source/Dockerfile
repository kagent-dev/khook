FROM golang:1.24.6-alpine AS builder

WORKDIR /app

# Copy only the redis-event-source module
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build the redis-event-source binary
RUN CGO_ENABLED=0 GOOS=linux go build -o redis-event-source ./cmd

FROM alpine:latest
RUN apk --no-cache add ca-certificates

WORKDIR /root/

COPY --from=builder /app/redis-event-source .

EXPOSE 50051

CMD ["./redis-event-source"]

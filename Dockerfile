# Build the manager binary
ARG BUILDPLATFORM
FROM --platform=$BUILDPLATFORM golang:1.24 AS builder
ARG TARGETARCH
ARG TARGETPLATFORM
# This is used to print the build platform in the logs
ARG BUILDPLATFORM


WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum

# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN --mount=type=cache,target=/root/go/pkg/mod,rw      \
    --mount=type=cache,target=/root/.cache/go-build,rw \
     go mod download

# Copy the go source
COPY cmd/ cmd/
COPY api/ api/
COPY internal/ internal/

ARG LDFLAGS
RUN --mount=type=cache,target=/root/go/pkg/mod,rw             \
    --mount=type=cache,target=/root/.cache/go-build,rw        \
    echo "Building on $BUILDPLATFORM -> linux/$TARGETARCH" && \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -ldflags "$LDFLAGS" -o manager cmd/main.go

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/manager .
USER 65532:65532

ARG VERSION

LABEL org.opencontainers.image.source=https://github.com/kagent-dev/khook
LABEL org.opencontainers.image.description="Khook is the controller for running hooks for agents."
LABEL org.opencontainers.image.authors="Kagent Creators 🤖"
LABEL org.opencontainers.image.version="$VERSION"

ENTRYPOINT ["/manager"]
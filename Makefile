# Image URL to use all building/pushing image targets

# Image configuration
DOCKER_REGISTRY ?= localhost:5001
BASE_IMAGE_REGISTRY ?= ghcr.io
DOCKER_REPO ?= kagent-dev/khook
HELM_REPO ?= oci://ghcr.io/kagent-dev
HELM_DIST_FOLDER ?= dist

BUILD_DATE := $(shell date -u '+%Y-%m-%d')
GIT_COMMIT := $(shell git rev-parse --short HEAD || echo "unknown")
VERSION ?= $(shell git describe --tags --always 2>/dev/null | grep v || echo "v0.0.0-$(GIT_COMMIT)")

# Local architecture detection to build for the current platform
LOCALARCH ?= $(shell uname -m | sed 's/x86_64/amd64/' | sed 's/aarch64/arm64/')


# Docker buildx configuration
BUILDKIT_VERSION = v0.23.0
BUILDX_NO_DEFAULT_ATTESTATIONS=1
BUILDX_BUILDER_NAME ?= kagent-builder-$(BUILDKIT_VERSION)

DOCKER_BUILDER ?= docker buildx
DOCKER_BUILD_ARGS ?= --push --platform linux/$(LOCALARCH)
KIND_CLUSTER_NAME ?= kagent

DOCKER_IMAGE ?= khook

IMG ?= $(DOCKER_REGISTRY)/$(DOCKER_REPO)/$(DOCKER_IMAGE):$(VERSION)


# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# Setting SHELL to bash allows bash commands to be executed by recipes.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: all
all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk commands is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test: fmt vet ## Run tests.
	go test ./... -coverprofile cover.out

##@ Build

.PHONY: build
build: fmt vet ## Build manager binary.
	go build -o bin/manager cmd/main.go

.PHONY: run
run: fmt vet ## Run a controller from your host.
	go run ./cmd/main.go

.PHONY: docker-build
docker-build:
	$(DOCKER_BUILDER) build --build-arg VERSION=$(VERSION) $(DOCKER_BUILD_ARGS) -t $(IMG) .

##@ Deployment

.PHONY: create-kind-cluster
create-kind-cluster:
	bash ./scripts/kind/setup-kind.sh
	bash ./scripts/kind/setup-metallb.sh

.PHONY: delete-kind-cluster
delete-kind-cluster:
	kind delete cluster --name $(KIND_CLUSTER_NAME)

.PHONY: install
install: ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	kubectl apply -f config/crd/bases

.PHONY: uninstall
uninstall: ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config.
	kubectl delete -f config/crd/bases

.PHONY: deploy
deploy: ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	kubectl apply -k config/default

.PHONY: undeploy
undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config.
	kubectl delete -k config/default

.PHONY: deploy-samples
deploy-samples: ## Deploy sample Hook resources.
	kubectl apply -f config/samples/

.PHONY: undeploy-samples
undeploy-samples: ## Remove sample Hook resources.
	kubectl delete -f config/samples/

.PHONY: kustomize-build
kustomize-build: ## Build kustomized manifests.
	kubectl kustomize config/default

##@ Helm

.PHONY: helm-cleanup
helm-cleanup:
	rm -f ./$(HELM_DIST_FOLDER)/*.tgz

.PHONY: helm-version
helm-version: 
	VERSION=$(VERSION) envsubst < helm/khook-crds/Chart-template.yaml > helm/khook-crds/Chart.yaml
	VERSION=$(VERSION) envsubst < helm/khook/Chart-template.yaml > helm/khook/Chart.yaml
	helm dependency update helm/khook
	helm dependency update helm/khook-crds
	helm package -d $(HELM_DIST_FOLDER) helm/khook-crds
	helm package -d $(HELM_DIST_FOLDER) helm/khook

.PHONY: helm-lint
helm-lint: ## Lint Helm chart.
	helm lint helm/khook

.PHONY: helm-template
helm-template: ## Generate Helm templates.
	helm template khook helm/khook

.PHONY: helm-install
helm-install: ## Install Helm chart.
	helm install khook helm/khook \
		--namespace kagent \
		--create-namespace

.PHONY: helm-upgrade
helm-upgrade: ## Upgrade Helm chart.
	helm upgrade khook helm/khook \
		--namespace kagent

.PHONY: helm-uninstall
helm-uninstall: ## Uninstall Helm chart.
	helm uninstall khook --namespace kagent
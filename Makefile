# Image URL to use all building/pushing image targets
IMG ?= kagent/hook-controller:latest
DOCKER_REGISTRY ?= otomato
DOCKER_IMAGE ?= khook
GIT_HASH ?= $(shell git rev-parse --short HEAD)
DOCKER_TAG ?= $(GIT_HASH)

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
docker-build: ## Build docker image with the manager.
	docker build -t ${IMG} .

.PHONY: docker-push
docker-push: ## Push docker image with the manager.
	docker push ${IMG}

.PHONY: docker-build-hash
docker-build-hash: ## Build docker image with git hash tag.
	docker build -t $(DOCKER_REGISTRY)/$(DOCKER_IMAGE):$(DOCKER_TAG) .
	docker tag $(DOCKER_REGISTRY)/$(DOCKER_IMAGE):$(DOCKER_TAG) $(DOCKER_REGISTRY)/$(DOCKER_IMAGE):latest

.PHONY: docker-push-hash
docker-push-hash: docker-build-hash ## Build and push docker image with git hash tag to Docker Hub.
	docker push $(DOCKER_REGISTRY)/$(DOCKER_IMAGE):$(DOCKER_TAG)
	docker push $(DOCKER_REGISTRY)/$(DOCKER_IMAGE):latest

.PHONY: docker-login
docker-login: ## Login to Docker Hub (requires DOCKER_USERNAME and DOCKER_PASSWORD env vars).
	@echo "$$DOCKER_PASSWORD" | docker login -u "$$DOCKER_USERNAME" --password-stdin

##@ Deployment

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

.PHONY: helm-lint
helm-lint: ## Lint Helm chart.
	helm lint charts/kagent-hook-controller

.PHONY: helm-template
helm-template: ## Generate Helm templates.
	helm template khook charts/kagent-hook-controller

.PHONY: helm-install
helm-install: ## Install Helm chart.
	helm install khook charts/kagent-hook-controller \
		--namespace kagent-system \
		--create-namespace

.PHONY: helm-upgrade
helm-upgrade: ## Upgrade Helm chart.
	helm upgrade khook charts/kagent-hook-controller \
		--namespace kagent-system

.PHONY: helm-uninstall
helm-uninstall: ## Uninstall Helm chart.
	helm uninstall khook --namespace kagent-system
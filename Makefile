SHELL := $(shell which bash)

MODULE  := github.com/0x0BSoD/mcp-k8s
BIN     := bin

# Image registry prefix — override on the command line:
#   make docker IMAGE_REPO=ghcr.io/myorg
IMAGE_REPO    ?= ttl.sh/mcp-k8s
IMAGE_TAG     ?= 1h

AGENT_IMAGE   := $(IMAGE_REPO)/cluster-agent:$(IMAGE_TAG)
COORD_IMAGE   := $(IMAGE_REPO)/coordinator:$(IMAGE_TAG)

AGENT_CMD     := ./cmd/cluster-agent
COORD_CMD     := ./cmd/coordinator

GO_FLAGS      := -trimpath
LDFLAGS       := -s -w

.DEFAULT_GOAL := help

# ── Help ───────────────────────────────────────────────────────────────────────
.PHONY: help
help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n\nTargets:\n"} \
	     /^[a-zA-Z0-9\/_-]+:.*##/ { printf "  \033[36m%-28s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

# ── Build ──────────────────────────────────────────────────────────────────────
.PHONY: build
build: build/cluster-agent build/coordinator ## Build both binaries into bin/

.PHONY: build/cluster-agent
build/cluster-agent: ## Build the cluster-agent binary
	@mkdir -p $(BIN)
	go build $(GO_FLAGS) -ldflags "$(LDFLAGS)" -o $(BIN)/cluster-agent $(AGENT_CMD)

.PHONY: build/coordinator
build/coordinator: ## Build the coordinator binary
	@mkdir -p $(BIN)
	go build $(GO_FLAGS) -ldflags "$(LDFLAGS)" -o $(BIN)/coordinator $(COORD_CMD)

# ── Test ───────────────────────────────────────────────────────────────────────
.PHONY: test
test: ## Run all tests
	go test ./...

.PHONY: test/verbose
test/verbose: ## Run all tests with verbose output
	go test -v ./...

.PHONY: test/cover
test/cover: ## Run tests and open HTML coverage report
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out

# ── Code quality ───────────────────────────────────────────────────────────────
.PHONY: lint
lint: ## Run golangci-lint
	golangci-lint run ./...

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: fmt
fmt: ## Run gofmt and fix files in place
	gofmt -w -l .

.PHONY: tidy
tidy: ## Run go mod tidy
	go mod tidy

# ── Proto ──────────────────────────────────────────────────────────────────────
.PHONY: proto
proto: ## Regenerate gRPC code from proto/clusteragent.proto
	./proto/gen.sh

# ── Docker ─────────────────────────────────────────────────────────────────────
.PHONY: docker
docker: docker/cluster-agent docker/coordinator ## Build both Docker images

.PHONY: docker/cluster-agent
docker/cluster-agent: ## Build cluster-agent Docker image
	docker build \
		--build-arg CMD=cluster-agent \
		-t $(AGENT_IMAGE) .

.PHONY: docker/coordinator
docker/coordinator: ## Build coordinator Docker image
	docker build \
		--build-arg CMD=coordinator \
		-t $(COORD_IMAGE) .

.PHONY: docker/push
docker/push: ## Push both images to IMAGE_REPO
	docker push $(AGENT_IMAGE)
	docker push $(COORD_IMAGE)

# ── Deploy ─────────────────────────────────────────────────────────────────────
.PHONY: deploy
deploy: ## Apply all Kubernetes manifests (namespace first, then components)
	kubectl apply -f deploy/namespace.yaml
	kubectl apply -f deploy/cluster-agent/
	kubectl apply -f deploy/coordinator/

.PHONY: undeploy
undeploy: ## Delete all Kubernetes manifests
	kubectl delete -f deploy/coordinator/  --ignore-not-found
	kubectl delete -f deploy/cluster-agent/ --ignore-not-found
	kubectl delete -f deploy/namespace.yaml --ignore-not-found

# ── Local run ──────────────────────────────────────────────────────────────────
.PHONY: run/cluster-agent
run/cluster-agent: build/cluster-agent ## Run cluster-agent locally (requires KUBECONFIG + MCP_CLUSTER_NAME)
	$(BIN)/cluster-agent

.PHONY: run/coordinator
run/coordinator: build/coordinator ## Run coordinator locally
	$(BIN)/coordinator

# ── Clean ──────────────────────────────────────────────────────────────────────
.PHONY: clean
clean: ## Remove build artifacts
	rm -rf $(BIN) coverage.out

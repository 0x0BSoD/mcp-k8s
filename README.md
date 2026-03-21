# mcp-k8s

Kubernetes observability tool that watches cluster events, enriches them with ownership context, and answers questions about cluster health — optionally with LLM-narrated explanations.

## Architecture

```
┌─────────────────────────────────┐        ┌──────────────────────────────┐
│          Coordinator            │  gRPC  │        Cluster Agent         │
│  (central brain, HTTP API)      │◄──────►│  (in-cluster sensor)         │
│                                 │        │                              │
│  ┌──────────┐  ┌────────────┐   │        │  ┌────────┐  ┌───────────┐   │
│  │  Planner │  │ Correlator │   │        │  │ Watcher│  │   Store   │   │
│  └──────────┘  └────────────┘   │        │  └────────┘  └───────────┘   │
│  ┌──────────┐  ┌────────────┐   │        │  ┌──────────────────────┐    │
│  │ Executor │  │ LLM client │   │        │  │      Enricher        │    │
│  └──────────┘  └────────────┘   │        │  └──────────────────────┘    │
└─────────────────────────────────┘        └──────────────────────────────┘
```

- **Cluster Agent** — runs inside each cluster. Watches Kubernetes Events via informers, enriches them with owner chains (Pod→ReplicaSet→Deployment), stores them in a ring buffer, and exposes a gRPC API.
- **Coordinator** — runs outside clusters (or in one central cluster). Receives HTTP queries, builds a rule-based execution plan, fans out gRPC calls to registered cluster agents, correlates the results, and returns structured JSON — optionally with an LLM-narrated explanation.

The layer boundary is a strict typed gRPC contract. The coordinator never asks the agent to think; it only asks for data.

## Features

- **Event timeline reconstruction** — deduplicates and sequences burst episodes separately (up to 5 per key)
- **Owner chain enrichment** — traces Pod→RS→Deployment and surfaces the workload root cause
- **Object snapshots** — container states, restart counts, last termination reasons, workload conditions
- **Container logs** — last N lines fetched on demand
- **Pod metrics** — CPU (millicores) and memory (bytes) via metrics-server; gracefully disabled when metrics-server is absent
- **Causal correlation** — links downstream events to their root causes across a namespace
- **LLM narration** — optional Ollama integration: natural language question → structured intent → pipeline → narrated answer

## API

### POST /query — structured query

```json
{
  "cluster_name": "prod",
  "namespace": "default",
  "intent": "ExplainRestarts",
  "since_minutes": 60,
  "mode": "incident-summary"
}
```

**Intents:** `NamespaceOverview` | `ExplainObjectIssue` | `ExplainRestarts` | `BuildTimeline`

**Modes:** `technical` (default) | `concise` | `incident-summary`

For `ExplainObjectIssue`, also provide `object_kind` and `object_name`.

### POST /ask — natural language query (requires Ollama)

```json
{
  "cluster": "prod",
  "namespace": "default",
  "question": "why are the redis pods restarting?",
  "mode": "concise"
}
```

The LLM extracts structured intent from the question, runs the same planner/executor/correlator pipeline as `/query`, then narrates the findings. Response includes both `answer` (narrative string) and the full structured response object.

Returns `503` when `OLLAMA_BASE_URL` is not configured.

### POST /registry — register a cluster agent

```json
{
  "cluster_name": "prod",
  "grpc_endpoint": "mcp-cluster-agent.mcp-system.svc:50051"
}
```

Requires `Authorization: Bearer <token>` when `MCP_REGISTRY_TOKEN` is set.

### DELETE /registry/{cluster}

Unregister a cluster. Same auth requirement.

### GET /registry

List all registered clusters.

### GET /health

Returns `{"status": "ok"}`.

---

## Response shape

```json
{
  "cluster": "prod",
  "mode": "technical",
  "namespace_summary": [...],
  "timeline": [...],
  "root_causes": [...],
  "affected_objects": [...],
  "snapshots": [...],
  "container_logs": [...],
  "pod_metrics": [...],
  "summary": "...",
  "warnings": [],
  "answer": "..."
}
```

`warnings` is non-empty when some steps failed but partial data was collected. HTTP status is `207 Multi-Status` when all steps failed and nothing was collected.

---

## Configuration

### Cluster Agent

| Env var | Default | Description |
|---|---|---|
| `MCP_CLUSTER_NAME` | _(required)_ | Name used to identify this cluster |
| `MCP_GRPC_ADDR` | `:50051` | gRPC listen address |
| `MCP_WATCH_NAMESPACE` | `""` (all) | Restrict event watch to one namespace |
| `MCP_STORE_MAX_SIZE` | `10000` | Ring buffer capacity |

### Coordinator

| Env var | Default | Description |
|---|---|---|
| `MCP_HTTP_ADDR` | `:8080` | HTTP listen address |
| `MCP_REGISTRY_TOKEN` | `""` (no auth) | Shared secret for registry mutations |
| `MCP_REGISTRY_FILE` | `""` (in-memory) | Path for persistent cluster registry |
| `OLLAMA_BASE_URL` | `""` (disabled) | Ollama base URL, e.g. `http://localhost:11434` |
| `OLLAMA_MODEL` | `llama3` | Model name to use with Ollama |

---

## Build

```sh
make build          # build both binaries into bin/
make test           # run all tests
make proto          # regenerate gRPC code from proto/clusteragent.proto
make docker         # build both Docker images
```

Requires Go 1.21+, `protoc`, `protoc-gen-go`, and `protoc-gen-go-grpc` for proto generation.

---

## Deploy

```sh
make deploy         # kubectl apply deploy/namespace.yaml, cluster-agent/, coordinator/
make undeploy       # tear it all down
```

Everything lands in the `mcp-system` namespace. Deploy one cluster agent per cluster and one coordinator anywhere with network access to all agents.

After deploying, register each agent with the coordinator:

```sh
curl -s -X POST http://<coordinator>:8080/registry \
  -H 'Content-Type: application/json' \
  -d '{"cluster_name":"prod","grpc_endpoint":"mcp-cluster-agent.mcp-system.svc:50051"}'
```

---

## Local development

```sh
# Terminal 1 — cluster agent (needs KUBECONFIG pointing at a real cluster)
MCP_CLUSTER_NAME=local ./bin/cluster-agent

# Terminal 2 — coordinator
./bin/coordinator

# Register the local agent
curl -s -X POST http://localhost:8080/registry \
  -H 'Content-Type: application/json' \
  -d '{"cluster_name":"local","grpc_endpoint":"localhost:50051"}'

# Structured query
curl -s -X POST http://localhost:8080/query \
  -H 'Content-Type: application/json' \
  -d '{"cluster_name":"local","namespace":"payments","intent":"ExplainRestarts","since_minutes":30,"mode":"incident-summary"}' | jq

# Natural language query (requires Ollama)
OLLAMA_BASE_URL=http://localhost:11434 ./bin/coordinator &
curl -s -X POST http://localhost:8080/ask \
  -H 'Content-Type: application/json' \
  -d '{"cluster":"local","question":"why are pods crashing in the payments namespace?"}' | jq .answer
```

---

## gRPC methods

| Method | Description |
|---|---|
| `ListEvents` | Events from the store, optionally filtered by namespace/reason/time |
| `GetEventsForObject` | All events for a specific kind+name |
| `GetNamespaceSummary` | Grouped event counts per reason for a namespace |
| `ResolveOwnerChain` | Owner reference chain from a resource to its workload root |
| `GetObjectSnapshot` | Current state of an object (phase, container statuses, conditions) |
| `GetContainerLogs` | Last N log lines for a container |
| `GetPodMetrics` | CPU/memory usage for pods via metrics-server |

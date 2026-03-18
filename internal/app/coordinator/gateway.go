package coordinator

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/0x0BSoD/mcp-k8s/pkg/correlation"
	"github.com/0x0BSoD/mcp-k8s/pkg/planner"
)

// QueryRequest is the JSON body for POST /query.
type QueryRequest struct {
	ClusterName  string `json:"cluster_name"`
	Namespace    string `json:"namespace"`
	ObjectKind   string `json:"object_kind,omitempty"`
	ObjectName   string `json:"object_name,omitempty"`
	Reason       string `json:"reason,omitempty"`
	SinceMinutes int    `json:"since_minutes,omitempty"` // 0 = no bound
	Limit        int    `json:"limit,omitempty"`
	Intent       string `json:"intent,omitempty"` // "NamespaceOverview" | "ExplainObjectIssue" | "ExplainRestarts" | "BuildTimeline"
	Mode         string `json:"mode,omitempty"`   // "technical" | "concise" | "incident-summary"
}

// RegisterRequest is the JSON body for POST /registry.
type RegisterRequest struct {
	ClusterName  string            `json:"cluster_name"`
	GRPCEndpoint string            `json:"grpc_endpoint"`
	Labels       map[string]string `json:"labels,omitempty"`
}

type gateway struct {
	registry *Registry
	executor *Executor
	planner  *planner.Planner
	corr     *correlation.Correlator
	builder  *ResponseBuilder
}

func newGateway(r *Registry, e *Executor, p *planner.Planner, c *correlation.Correlator, b *ResponseBuilder) *gateway {
	return &gateway{registry: r, executor: e, planner: p, corr: c, builder: b}
}

func (g *gateway) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", g.handleHealth)
	mux.HandleFunc("POST /query", g.handleQuery)
	mux.HandleFunc("POST /registry", g.handleRegister)
	mux.HandleFunc("DELETE /registry/{cluster}", g.handleUnregister)
	mux.HandleFunc("GET /registry", g.handleListRegistry)
	return mux
}

// ── Handlers ───────────────────────────────────────────────────────────────────

func (g *gateway) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (g *gateway) handleQuery(w http.ResponseWriter, r *http.Request) {
	var req QueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.ClusterName == "" {
		writeError(w, http.StatusBadRequest, "cluster_name is required")
		return
	}

	q := planner.Query{
		ClusterName: req.ClusterName,
		Namespace:   req.Namespace,
		ObjectKind:  req.ObjectKind,
		ObjectName:  req.ObjectName,
		Reason:      req.Reason,
		Limit:       req.Limit,
		Intent:      parseIntent(req.Intent),
	}
	if req.SinceMinutes > 0 {
		q.Since = time.Now().Add(-time.Duration(req.SinceMinutes) * time.Minute)
	}

	plan, err := g.planner.Plan(q)
	if err != nil {
		writeError(w, http.StatusBadRequest, "plan error: "+err.Error())
		return
	}

	result, err := g.executor.Execute(r.Context(), plan)
	if err != nil {
		slog.Error("execution failed", "cluster", req.ClusterName, "err", err)
		writeError(w, http.StatusBadGateway, "execution error: "+err.Error())
		return
	}

	timeline := g.corr.Correlate(result.Events)
	mode := parseMode(req.Mode)
	resp := g.builder.Build(req.ClusterName, mode, timeline, result)

	status := http.StatusOK
	if len(result.StepErrors) > 0 && len(result.Events) == 0 && len(result.Snapshots) == 0 {
		// All steps failed, nothing collected — signal partial/failed response.
		status = http.StatusMultiStatus
	}
	writeJSON(w, status, resp)
}

func (g *gateway) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if req.ClusterName == "" || req.GRPCEndpoint == "" {
		writeError(w, http.StatusBadRequest, "cluster_name and grpc_endpoint are required")
		return
	}

	g.registry.Register(ClusterEntry{
		ClusterName:  req.ClusterName,
		GRPCEndpoint: req.GRPCEndpoint,
		Labels:       req.Labels,
	})

	slog.Info("cluster registered", "cluster", req.ClusterName, "endpoint", req.GRPCEndpoint)
	writeJSON(w, http.StatusOK, map[string]string{"status": "registered"})
}

func (g *gateway) handleUnregister(w http.ResponseWriter, r *http.Request) {
	cluster := r.PathValue("cluster")
	g.registry.Unregister(cluster)
	slog.Info("cluster unregistered", "cluster", cluster)
	writeJSON(w, http.StatusOK, map[string]string{"status": "unregistered"})
}

func (g *gateway) handleListRegistry(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, g.registry.List())
}

// ── Helpers ────────────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func parseIntent(s string) planner.Intent {
	switch s {
	case "NamespaceOverview":
		return planner.IntentNamespaceOverview
	case "ExplainObjectIssue":
		return planner.IntentExplainObjectIssue
	case "ExplainRestarts":
		return planner.IntentExplainRestarts
	case "BuildTimeline":
		return planner.IntentBuildTimeline
	default:
		return planner.IntentUnknown
	}
}

func parseMode(s string) ResponseMode {
	switch s {
	case "concise":
		return ResponseModeConcise
	case "incident-summary":
		return ResponseModeIncidentSummary
	default:
		return ResponseModeTechnical
	}
}

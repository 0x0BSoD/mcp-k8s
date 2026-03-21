package coordinator

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/0x0BSoD/mcp-k8s/pkg/correlation"
	"github.com/0x0BSoD/mcp-k8s/pkg/llm"
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
	token    string     // required Bearer token for registry mutations; empty = no auth
	llm      llm.Client // nil when LLM is disabled
}

func newGateway(r *Registry, e *Executor, p *planner.Planner, c *correlation.Correlator, b *ResponseBuilder, token string, lc llm.Client) *gateway {
	return &gateway{registry: r, executor: e, planner: p, corr: c, builder: b, token: token, llm: lc}
}

func (g *gateway) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", g.handleHealth)
	mux.HandleFunc("POST /query", g.handleQuery)
	mux.HandleFunc("POST /ask", g.handleAsk)
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

// AskRequest is the JSON body for POST /ask.
type AskRequest struct {
	ClusterName string `json:"cluster"`
	Question    string `json:"question"`
	// Namespace is an optional hint; the LLM may override it.
	Namespace string `json:"namespace,omitempty"`
	Mode      string `json:"mode,omitempty"` // "technical" | "concise" | "incident-summary"
}

// AskResponse extends Response with the LLM-narrated answer.
type AskResponse struct {
	Response
	Answer string `json:"answer,omitempty"`
}

func (g *gateway) handleAsk(w http.ResponseWriter, r *http.Request) {
	if g.llm == nil {
		writeError(w, http.StatusServiceUnavailable, "LLM not configured (set OLLAMA_BASE_URL)")
		return
	}

	var req AskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if req.ClusterName == "" {
		writeError(w, http.StatusBadRequest, "cluster is required")
		return
	}
	if req.Question == "" {
		writeError(w, http.StatusBadRequest, "question is required")
		return
	}

	// Step 1: LLM extracts structured intent from the natural language question.
	intentJSON, err := g.llm.Chat(r.Context(), llm.ExtractIntentSystem, llm.ExtractIntentUser(req.Question))
	if err != nil {
		slog.Error("LLM intent extraction failed", "err", err)
		writeError(w, http.StatusBadGateway, "LLM error: "+err.Error())
		return
	}

	q, err := parseIntentJSON(intentJSON, req.ClusterName, req.Namespace)
	if err != nil {
		slog.Warn("intent JSON parse failed, falling back to heuristics", "raw", intentJSON, "err", err)
		// Fall back to a broad namespace overview.
		q = planner.Query{
			ClusterName: req.ClusterName,
			Namespace:   req.Namespace,
			Intent:      planner.IntentNamespaceOverview,
		}
	}

	// Step 2: Execute the plan.
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

	// Step 3: Correlate and build structured response.
	timeline := g.corr.Correlate(result.Events)
	mode := parseMode(req.Mode)
	structured := g.builder.Build(req.ClusterName, mode, timeline, result)

	// Step 4: LLM narrates the answer.
	narrative, err := g.llm.Summarize(r.Context(), llm.SummarizeTimeline(req.Question, timeline))
	if err != nil {
		slog.Warn("LLM narration failed, returning structured data only", "err", err)
		narrative = structured.Summary // fall back to rule-based summary
	}

	status := http.StatusOK
	if len(result.StepErrors) > 0 && len(result.Events) == 0 && len(result.Snapshots) == 0 {
		status = http.StatusMultiStatus
	}
	writeJSON(w, status, AskResponse{Response: structured, Answer: narrative})
}

func (g *gateway) handleRegister(w http.ResponseWriter, r *http.Request) {
	if !g.requireToken(w, r) {
		return
	}
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
	if !g.requireToken(w, r) {
		return
	}
	cluster := r.PathValue("cluster")
	g.registry.Unregister(cluster)
	slog.Info("cluster unregistered", "cluster", cluster)
	writeJSON(w, http.StatusOK, map[string]string{"status": "unregistered"})
}

func (g *gateway) handleListRegistry(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, g.registry.List())
}

// ── Helpers ────────────────────────────────────────────────────────────────────

// requireToken checks the Authorization: Bearer header when a token is
// configured. Returns true if the request is allowed to proceed.
func (g *gateway) requireToken(w http.ResponseWriter, r *http.Request) bool {
	if g.token == "" {
		return true // no auth configured
	}
	auth := r.Header.Get("Authorization")
	if strings.TrimPrefix(auth, "Bearer ") != g.token {
		writeError(w, http.StatusUnauthorized, "invalid or missing token")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// intentPayload is the JSON shape the LLM returns for intent extraction.
type intentPayload struct {
	Namespace    string `json:"namespace"`
	ObjectKind   string `json:"object_kind"`
	ObjectName   string `json:"object_name"`
	Reason       string `json:"reason"`
	SinceMinutes int    `json:"since_minutes"`
	Intent       string `json:"intent"`
}

// parseIntentJSON unmarshals the LLM's JSON reply into a planner.Query.
// clusterName and fallbackNS are used when the LLM omits them.
func parseIntentJSON(raw, clusterName, fallbackNS string) (planner.Query, error) {
	// Strip optional markdown code fences the LLM may add.
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	var p intentPayload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return planner.Query{}, err
	}

	ns := p.Namespace
	if ns == "" {
		ns = fallbackNS
	}

	q := planner.Query{
		ClusterName: clusterName,
		Namespace:   ns,
		ObjectKind:  p.ObjectKind,
		ObjectName:  p.ObjectName,
		Reason:      p.Reason,
		Intent:      parseIntent(p.Intent),
	}
	if p.SinceMinutes > 0 {
		q.Since = time.Now().Add(-time.Duration(p.SinceMinutes) * time.Minute)
	}
	return q, nil
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

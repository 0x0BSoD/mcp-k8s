package planner

import "fmt"

// Planner converts a Query into an ordered Plan of cluster-agent calls.
// It is rule-based: no LLM, no randomness. Given the same Query it always
// produces the same Plan.
type Planner struct{}

func New() *Planner { return &Planner{} }

// Plan builds an execution plan for the given query.
// Returns an error only when the query is structurally invalid (missing
// required fields for the chosen intent).
func (p *Planner) Plan(q Query) (Plan, error) {
	if q.ClusterName == "" {
		return Plan{}, fmt.Errorf("ClusterName is required")
	}

	var steps []Step
	var err error

	switch q.Intent {
	case IntentNamespaceOverview:
		steps = p.namespaceOverview(q)

	case IntentExplainObjectIssue:
		steps, err = p.explainObjectIssue(q)

	case IntentExplainRestarts:
		steps = p.explainRestarts(q)

	case IntentBuildTimeline:
		steps = p.buildTimeline(q)

	default:
		// Heuristic fallback based on what params are present.
		steps, err = p.heuristic(q)
	}

	if err != nil {
		return Plan{}, err
	}
	return Plan{ClusterName: q.ClusterName, Steps: steps}, nil
}

// ── Intent handlers ────────────────────────────────────────────────────────────

// namespaceOverview: summary first (cheap), then full event list.
func (p *Planner) namespaceOverview(q Query) []Step {
	return []Step{
		{Method: MethodGetNamespaceSummary, Namespace: q.Namespace, Since: q.Since},
		{Method: MethodListEvents, Namespace: q.Namespace, Since: q.Since, Limit: q.Limit},
	}
}

// explainObjectIssue: events → owner chain → current snapshot.
// Order matters: events give context, owner chain shows blast radius,
// snapshot gives the current state at query time.
func (p *Planner) explainObjectIssue(q Query) ([]Step, error) {
	if q.ObjectKind == "" || q.ObjectName == "" {
		return nil, fmt.Errorf("IntentExplainObjectIssue requires ObjectKind and ObjectName")
	}
	return []Step{
		{
			Method:    MethodGetEventsForObject,
			Namespace: q.Namespace,
			Kind:      q.ObjectKind,
			Name:      q.ObjectName,
		},
		{
			Method:    MethodResolveOwnerChain,
			Namespace: q.Namespace,
			Kind:      q.ObjectKind,
			Name:      q.ObjectName,
		},
		{
			Method:    MethodGetObjectSnapshot,
			Namespace: q.Namespace,
			Kind:      q.ObjectKind,
			Name:      q.ObjectName,
		},
	}, nil
}

// explainRestarts: namespace summary first, then targeted event queries for
// the most common restart-related reasons.
func (p *Planner) explainRestarts(q Query) []Step {
	restartReasons := []string{"BackOff", "OOMKilling", "Failed", "Unhealthy", "FailedMount"}
	steps := make([]Step, 0, 1+len(restartReasons))

	steps = append(steps, Step{
		Method:    MethodGetNamespaceSummary,
		Namespace: q.Namespace,
		Since:     q.Since,
	})
	for _, reason := range restartReasons {
		steps = append(steps, Step{
			Method:    MethodListEvents,
			Namespace: q.Namespace,
			Reason:    reason,
			Since:     q.Since,
		})
	}
	return steps
}

// buildTimeline: a single paginated ListEvents call, ordered by time.
func (p *Planner) buildTimeline(q Query) []Step {
	return []Step{
		{
			Method:    MethodListEvents,
			Namespace: q.Namespace,
			Reason:    q.Reason,
			Since:     q.Since,
			Limit:     q.Limit,
		},
	}
}

// heuristic falls back to object investigation if object params are present,
// namespace overview if only namespace is present, or returns an error.
func (p *Planner) heuristic(q Query) ([]Step, error) {
	if q.ObjectKind != "" && q.ObjectName != "" {
		return p.explainObjectIssue(q)
	}
	if q.Namespace != "" {
		return p.namespaceOverview(q), nil
	}
	return nil, fmt.Errorf("cannot build plan: provide at least a Namespace or ObjectKind+ObjectName")
}

package llm

import (
	"fmt"
	"strings"
	"time"

	"github.com/0x0BSoD/mcp-k8s/pkg/correlation"
)

// ExtractIntentSystem is the system prompt for the intent extraction Chat call.
const ExtractIntentSystem = `You are a Kubernetes observability assistant.
Extract structured query parameters from the user's question and reply with a
JSON object only — no explanation, no markdown fences.

JSON schema:
{
  "namespace":    string,  // Kubernetes namespace; empty = all
  "object_kind":  string,  // e.g. "Pod", "Deployment"; empty if not specified
  "object_name":  string,  // specific resource name; empty if not specified
  "reason":       string,  // Kubernetes event Reason filter; empty if not specified
  "since_minutes": number, // look-back window; 0 = no bound; default 60
  "intent":       string   // one of: NamespaceOverview | ExplainObjectIssue | ExplainRestarts | BuildTimeline
}

Intent rules:
- NamespaceOverview  — broad "what's wrong in <namespace>" questions
- ExplainObjectIssue — questions about a specific named resource
- ExplainRestarts    — questions about pod restarts / CrashLoopBackOff
- BuildTimeline      — questions asking for an event timeline or history`

// ExtractIntentUser builds the user message for intent extraction.
func ExtractIntentUser(question string) string {
	return question
}

// SummarizeTimeline builds a prompt asking the LLM to narrate a Timeline.
func SummarizeTimeline(question string, tl correlation.Timeline) string {
	if len(tl.Events) == 0 {
		return fmt.Sprintf(
			"The user asked: %q\n\nNo Kubernetes events were found for this query. "+
				"Reply with a brief, helpful message explaining that no issues were detected.",
			question,
		)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "The user asked: %q\n\n", question)
	fmt.Fprintf(&sb, "Here is a structured summary of relevant Kubernetes events:\n\n")

	if len(tl.RootCauses) > 0 {
		sb.WriteString("ROOT CAUSES:\n")
		for _, rc := range tl.RootCauses {
			fmt.Fprintf(&sb, "  [%s] %s/%s — %s: %s\n",
				rc.LastSeen.UTC().Format(time.RFC3339),
				rc.InvolvedObjectKind, rc.InvolvedObjectName,
				rc.Reason, rc.Message,
			)
		}
		sb.WriteString("\n")
	}

	sb.WriteString("EVENT TIMELINE (chronological):\n")
	for _, e := range tl.Events {
		marker := ""
		if e.IsRootCause {
			marker = " [ROOT CAUSE]"
		} else if e.CausedBy != "" {
			marker = fmt.Sprintf(" [caused by %s]", e.CausedBy)
		}
		fmt.Fprintf(&sb, "  [%s] %s/%s — %s: %s (count=%d)%s\n",
			e.LastSeen.UTC().Format(time.RFC3339),
			e.InvolvedObjectKind, e.InvolvedObjectName,
			e.Reason, e.Message, e.Count, marker,
		)
	}

	sb.WriteString("\nUsing only the information above, provide a clear, concise diagnosis. ")
	sb.WriteString("Explain the root cause(s), the downstream effects, and suggest a remediation step if obvious. ")
	sb.WriteString("Do not invent information not present in the events.")

	return sb.String()
}

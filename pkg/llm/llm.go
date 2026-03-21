package llm

import "context"

// Client is the minimal interface every LLM backend must satisfy.
type Client interface {
	// Summarize sends a single prompt and returns the model's completion.
	Summarize(ctx context.Context, prompt string) (string, error)

	// Chat sends a system + user message pair and returns the model's reply.
	Chat(ctx context.Context, system, user string) (string, error)
}

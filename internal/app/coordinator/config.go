package coordinator

import "os"

type Config struct {
	HTTPAddr string
	// RegistryToken is the shared secret required for POST/DELETE /registry.
	// Empty means no auth (dev mode).
	RegistryToken string
	// RegistryFile is the path where cluster registrations are persisted.
	// Empty means in-memory only (lost on restart).
	RegistryFile string
	// OllamaBaseURL is the base URL for the Ollama HTTP API.
	// Empty disables LLM features.
	OllamaBaseURL string
	// OllamaModel is the model name to use with Ollama (default: llama3).
	OllamaModel string
}

func fromEnv() Config {
	return Config{
		HTTPAddr:      envOrDefault("MCP_HTTP_ADDR", ":8080"),
		RegistryToken: os.Getenv("MCP_REGISTRY_TOKEN"),
		RegistryFile:  os.Getenv("MCP_REGISTRY_FILE"),
		OllamaBaseURL: os.Getenv("OLLAMA_BASE_URL"),
		OllamaModel:   envOrDefault("OLLAMA_MODEL", "llama3"),
	}
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

package coordinator

import "os"

type Config struct {
	HTTPAddr string
}

func fromEnv() Config {
	return Config{
		HTTPAddr: envOrDefault("MCP_HTTP_ADDR", ":8080"),
	}
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

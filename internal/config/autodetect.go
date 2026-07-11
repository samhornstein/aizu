package config

import (
	"net/http"
	"time"
)

// wellKnownModelServers are probed in order when no model credential is
// configured. host.docker.internal reaches the host from inside the aizu
// container (see extra_hosts in docker-compose.yml); the localhost entries
// cover running Aizu natively via `make run`.
var wellKnownModelServers = []string{
	"http://host.docker.internal:11434/v1", // Ollama
	"http://host.docker.internal:8080/v1",  // llama.cpp (llama-server)
	"http://host.docker.internal:1234/v1",  // LM Studio
	"http://localhost:11434/v1",
	"http://localhost:8080/v1",
	"http://localhost:1234/v1",
}

// AutodetectModelServer probes well-known local OpenAI-compatible servers
// and returns the first base URL whose /models endpoint answers, or "".
// Kept out of Load(), which promises to perform no network calls.
func AutodetectModelServer() string {
	client := &http.Client{Timeout: 2 * time.Second}
	for _, base := range wellKnownModelServers {
		resp, err := client.Get(base + "/models")
		if err != nil {
			continue
		}
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return base
		}
	}
	return ""
}

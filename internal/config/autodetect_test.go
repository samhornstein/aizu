package config

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAutodetectFindsRunningServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("path = %q, want /v1/models", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	orig := wellKnownModelServers
	defer func() { wellKnownModelServers = orig }()
	wellKnownModelServers = []string{
		"http://127.0.0.1:1", // nothing listens here
		srv.URL + "/v1",
	}

	if got := AutodetectModelServer(); got != srv.URL+"/v1" {
		t.Errorf("AutodetectModelServer() = %q, want %q", got, srv.URL+"/v1")
	}
}

func TestAutodetectNothingRunning(t *testing.T) {
	orig := wellKnownModelServers
	defer func() { wellKnownModelServers = orig }()
	wellKnownModelServers = []string{"http://127.0.0.1:1"}

	if got := AutodetectModelServer(); got != "" {
		t.Errorf("AutodetectModelServer() = %q, want empty", got)
	}
}

func TestAutodetectSkipsNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	orig := wellKnownModelServers
	defer func() { wellKnownModelServers = orig }()
	wellKnownModelServers = []string{srv.URL + "/v1"}

	if got := AutodetectModelServer(); got != "" {
		t.Errorf("AutodetectModelServer() = %q, want empty for non-200 response", got)
	}
}

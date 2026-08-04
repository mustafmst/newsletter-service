package main

import (
	"net/http"
	"testing"
	"time"

	"github.com/pmstowski/newsletter/internal/config"
)

func TestNewHTTPServerUsesProductionTimeouts(t *testing.T) {
	handler := http.NewServeMux()
	server := newHTTPServer(config.Config{HTTPAddr: ":8080"}, handler)

	if server.Addr != ":8080" {
		t.Fatalf("Addr = %q, want :8080", server.Addr)
	}
	if server.Handler != handler {
		t.Fatal("Handler was not preserved")
	}
	if server.ReadHeaderTimeout != 10*time.Second {
		t.Fatalf("ReadHeaderTimeout = %s", server.ReadHeaderTimeout)
	}
	if server.ReadTimeout != 15*time.Second {
		t.Fatalf("ReadTimeout = %s", server.ReadTimeout)
	}
	if server.WriteTimeout != 30*time.Second {
		t.Fatalf("WriteTimeout = %s", server.WriteTimeout)
	}
	if server.IdleTimeout != 60*time.Second {
		t.Fatalf("IdleTimeout = %s", server.IdleTimeout)
	}
}

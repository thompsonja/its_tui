package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
)

// apiBase returns the API base URL from the API_BASE env var, defaulting to
// http://localhost:9001.
func apiBase() string {
	if b := os.Getenv("API_BASE"); b != "" {
		return b
	}
	return "http://localhost:9001"
}

func TestHelloEndpoint(t *testing.T) {
	if os.Getenv("API_BASE") == "" {
		t.Skip("API_BASE not set")
	}
	url := fmt.Sprintf("%s/hello", apiBase())
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	msg, ok := body["message"]
	if !ok {
		t.Fatal("response missing 'message' field")
	}
	if msg != "hello world" && msg != "hello universe" {
		t.Fatalf("unexpected message: %q", msg)
	}
	t.Logf("message: %s", msg)
}

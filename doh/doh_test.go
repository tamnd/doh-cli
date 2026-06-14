package doh_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tamnd/doh-cli/doh"
)

// makeTestServer creates an httptest.Server that returns a canned JSON response body.
func makeTestServer(body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
}

func newTestClient(baseURL string) *doh.Client {
	c := doh.NewClient()
	c.Rate = 0
	c.Retries = 0
	c.BaseURL = baseURL
	return c
}

func TestResolveA(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"Status": 0,
		"Question": []map[string]any{{"name": "google.com.", "type": 1}},
		"Answer": []map[string]any{
			{"name": "google.com.", "type": 1, "TTL": 215, "data": "142.251.12.113"},
			{"name": "google.com.", "type": 1, "TTL": 215, "data": "142.251.12.138"},
		},
	})
	srv := makeTestServer(string(body))
	defer srv.Close()

	c := newTestClient(srv.URL)
	records, err := c.Resolve(context.Background(), "google.com", "A")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("got %d records, want 2", len(records))
	}
	if records[0].Type != "A" {
		t.Errorf("Type = %q, want A", records[0].Type)
	}
	if records[0].Data != "142.251.12.113" {
		t.Errorf("Data = %q, want 142.251.12.113", records[0].Data)
	}
}

func TestResolveMX(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"Status": 0,
		"Question": []map[string]any{{"name": "google.com.", "type": 15}},
		"Answer": []map[string]any{
			{"name": "google.com.", "type": 15, "TTL": 300, "data": "10 smtp.google.com."},
		},
	})
	srv := makeTestServer(string(body))
	defer srv.Close()

	c := newTestClient(srv.URL)
	records, err := c.Resolve(context.Background(), "google.com", "MX")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("got %d records, want 1", len(records))
	}
	if records[0].Type != "MX" {
		t.Errorf("Type = %q, want MX", records[0].Type)
	}
	if records[0].Data != "10 smtp.google.com." {
		t.Errorf("Data = %q, want 10 smtp.google.com.", records[0].Data)
	}
}

func TestResolveNXDOMAIN(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"Status":   3,
		"Question": []map[string]any{{"name": "nxdomain.example.invalid.", "type": 1}},
	})
	srv := makeTestServer(string(body))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.Resolve(context.Background(), "nxdomain.example.invalid", "A")
	if err == nil {
		t.Fatal("expected error for NXDOMAIN, got nil")
	}
	if !strings.Contains(err.Error(), "NXDOMAIN") {
		t.Errorf("error = %q, want NXDOMAIN in message", err.Error())
	}
}

func TestGetRetriesOn503(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("recovered"))
	}))
	defer srv.Close()

	c := doh.NewClient()
	c.Rate = 0
	c.Retries = 5

	start := time.Now()
	body, err := c.Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "recovered" {
		t.Errorf("body = %q after retries", body)
	}
	if hits != 3 {
		t.Errorf("server saw %d hits, want 3", hits)
	}
	if time.Since(start) < 500*time.Millisecond {
		t.Error("retries did not back off")
	}
}

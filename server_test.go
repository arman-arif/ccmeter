package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestLocalOnlyBlocksRebinding(t *testing.T) {
	h := localOnly("127.0.0.1:7878", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	name, _ := os.Hostname()
	for host, want := range map[string]int{
		"127.0.0.1:7878":        200,
		"localhost:7878":        200,
		"LOCALHOST.:7878":       200,
		"[::1]:7878":            200,
		"192.168.1.20:7878":     200,
		name + ":7878":          200,
		"evil.example.com":      403,
		"evil.example.com:7878": 403,
		"localhost.evil.com":    403,
	} {
		req := httptest.NewRequest("GET", "/api/summary", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Errorf("Host %q: got %d, want %d", host, rec.Code, want)
		}
	}

	// A domain given with -addr is allowed.
	h = localOnly("meter.lan:7878", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	req := httptest.NewRequest("GET", "/", nil)
	req.Host = "meter.lan:7878"
	rec := httptest.NewRecorder()
	if h.ServeHTTP(rec, req); rec.Code != 200 {
		t.Errorf("-addr host: got %d", rec.Code)
	}
}

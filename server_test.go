package main

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
	"time"
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

func TestWebHandlerRoutes(t *testing.T) {
	h := webHandler(fstest.MapFS{
		"index.html":      {Data: []byte("<html>app</html>")},
		"assets/app-1.js": {Data: []byte("js")},
	})
	for _, c := range []struct {
		method, path string
		code         int
		body         string
		cache        string
	}{
		{"GET", "/", 200, "app", "no-cache"},
		{"GET", "/accounts/work", 200, "app", "no-cache"},
		{"GET", "/accounts/a%2Fb?days=7", 200, "app", "no-cache"},
		{"GET", "/no/such/page", 200, "app", "no-cache"},
		{"GET", "/assets/app-1.js", 200, "js", "immutable"},
		{"GET", "/assets/missing.js", 404, "", ""},
		{"GET", "/api/nope", 404, "error", ""},
		{"POST", "/api/summary", 404, "error", ""},
		{"POST", "/accounts/work", 405, "error", ""},
	} {
		req := httptest.NewRequest(c.method, c.path, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != c.code {
			t.Errorf("%s %s: code %d, want %d", c.method, c.path, rec.Code, c.code)
		}
		if !strings.Contains(rec.Body.String(), c.body) {
			t.Errorf("%s %s: body %q, want %q", c.method, c.path, rec.Body, c.body)
		}
		if !strings.Contains(rec.Header().Get("Cache-Control"), c.cache) {
			t.Errorf("%s %s: Cache-Control %q, want %q", c.method, c.path, rec.Header().Get("Cache-Control"), c.cache)
		}
	}
}

// The committed build must be complete, since `go install` doesn't run npm.
func TestEmbeddedDashboardIsComplete(t *testing.T) {
	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		t.Fatal(err)
	}
	index, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		t.Fatalf("web/index.html: %v (run make web)", err)
	}
	refs := regexp.MustCompile(`(?:src|href)="/(assets/[^"]+)"`).FindAllSubmatch(index, -1)
	if len(refs) < 2 {
		t.Fatalf("expected script and style references in index.html, found %d", len(refs))
	}
	for _, m := range refs {
		if _, err := fs.Stat(sub, string(m[1])); err != nil {
			t.Errorf("index.html refers to %s: %v (run make web)", m[1], err)
		}
	}
}

func TestAccountRouteFindsNamesWithSlashes(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	s := &server{}
	s.applyLocked(Config{Accounts: []AccountConfig{{Name: "team/a b", Dir: t.TempDir()}}})
	h := http.NewServeMux()
	h.HandleFunc("GET /api/accounts/{name}", s.handleDetail)
	for path, want := range map[string]int{
		"/api/accounts/" + url.PathEscape("team/a b") + "?days=7": 200,
		"/api/accounts/nobody": 404,
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != want {
			t.Errorf("%s: %d, want %d: %s", path, rec.Code, want, rec.Body)
		}
		if want != 200 {
			continue
		}
		var d AccountDetail
		if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil {
			t.Fatal(err)
		}
		if d.Name != "team/a b" || d.Range.Days != 7 || len(d.Range.Daily) != 7 {
			t.Errorf("detail: name %q, days %d, %d daily points", d.Name, d.Range.Days, len(d.Range.Daily))
		}
		if want := time.Now().Format("2006-01-02"); d.Range.To != want {
			t.Errorf("range ends %s, want %s", d.Range.To, want)
		}
	}
}

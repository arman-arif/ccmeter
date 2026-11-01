package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHookConfigDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	got := hookConfigDir(hookInput{TranscriptPath: "/data/claude-work/projects/-src-app/abc.jsonl"})
	if got != "/data/claude-work" {
		t.Errorf("from transcript: %s", got)
	}
	if got := hookConfigDir(hookInput{}); got != filepath.Join(home, ".claude") {
		t.Errorf("default: %s", got)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", "/env/dir")
	if got := hookConfigDir(hookInput{TranscriptPath: "/data/claude-work/projects/p/x.jsonl"}); got != "/env/dir" {
		t.Errorf("env should win: %s", got)
	}
}

func TestHookInstallMerge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	os.WriteFile(path, []byte(`{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"notify.sh"}]}]}}`), 0o644)
	a := AccountConfig{Name: "w", Dir: dir}
	if res, err := installHook(a, "/bin/ccmeter hook"); err != nil || res != "installed" {
		t.Fatal(res, err)
	}
	if res, _ := installHook(a, "/bin/ccmeter hook"); res != "already set" {
		t.Errorf("second install: %s", res)
	}
	if res, _ := installHook(a, "/new/ccmeter hook"); res != "installed" {
		t.Errorf("path change should reinstall: %s", res)
	}
	b, _ := os.ReadFile(path)
	if strings.Count(string(b), "ccmeter hook") != 2 || !strings.Contains(string(b), "notify.sh") {
		t.Fatalf("bad merge:\n%s", b)
	}
	if res, _ := uninstallHook(a); res != "removed" {
		t.Errorf("uninstall: %s", res)
	}
	b, _ = os.ReadFile(path)
	if strings.Contains(string(b), "ccmeter") || !strings.Contains(string(b), "notify.sh") || strings.Contains(string(b), "SessionStart") {
		t.Fatalf("bad uninstall:\n%s", b)
	}
}

func TestRefreshLimitsFromEndpoint(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer tok" || r.Header.Get("anthropic-beta") != usageBeta {
			w.WriteHeader(401)
			return
		}
		w.Write([]byte(`{"five_hour":{"utilization":42,"resets_at":"2099-01-01T00:00:00Z"},"seven_day":{"utilization":7,"resets_at":null}}`))
	}))
	defer srv.Close()
	t.Setenv("CCMETER_USAGE_URL", srv.URL)

	dir := t.TempDir()
	exp := time.Now().Add(time.Hour).UnixMilli()
	creds, _ := json.Marshal(map[string]any{"claudeAiOauth": map[string]any{"accessToken": "tok", "expiresAt": exp}})
	os.WriteFile(filepath.Join(dir, ".credentials.json"), creds, 0o600)

	pl, err := refreshLimits(dir, false)
	if err != nil || pl == nil || pl.FiveHour.UsedPct != 42 || pl.SevenDay.UsedPct != 7 || pl.Source != "usage-endpoint" {
		t.Fatalf("got %+v, %v", pl, err)
	}
	if _, err := refreshLimits(dir, false); err != nil || calls != 1 {
		t.Errorf("second call should be throttled (calls=%d, err=%v)", calls, err)
	}
	if fetchDue(dir, time.Now()) {
		t.Error("fetch should not be due right after one")
	}

	// Expired login: no request is made.
	creds, _ = json.Marshal(map[string]any{"claudeAiOauth": map[string]any{"accessToken": "tok", "expiresAt": 1000}})
	os.WriteFile(filepath.Join(dir, ".credentials.json"), creds, 0o600)
	if _, err := refreshLimits(dir, true); err == nil || !strings.Contains(err.Error(), "expired") || calls != 1 {
		t.Errorf("expired token: err=%v calls=%d", err, calls)
	}
}

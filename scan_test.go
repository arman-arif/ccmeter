package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeLog(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanRecordsProjectAndSession(t *testing.T) {
	dir := t.TempDir()
	ts := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	usage := `"usage":{"input_tokens":1,"output_tokens":2}`

	writeLog(t, filepath.Join(dir, "projects", "-home-me-app", "abc.jsonl"),
		`{"type":"assistant","timestamp":"`+ts+`","requestId":"r1","cwd":"/home/me/app","sessionId":"s1","message":{"id":"m1","model":"claude-sonnet-4-5",`+usage+`}}`+"\n"+
			// The same response logged again must still count once.
			`{"type":"assistant","timestamp":"`+ts+`","requestId":"r1","cwd":"/home/me/app","sessionId":"s1","message":{"id":"m1","model":"claude-sonnet-4-5",`+usage+`}}`+"\n")
	// Older logs have no cwd or sessionId: fall back to the log's location.
	writeLog(t, filepath.Join(dir, "projects", "-home-me-old", "def.jsonl"),
		`{"type":"assistant","timestamp":"`+ts+`","requestId":"r2","message":{"id":"m2","model":"claude-sonnet-4-5",`+usage+`}}`+"\n")

	a := newAccount(AccountConfig{Name: "x", Dir: dir})
	a.Scan(time.Now().Add(-historyWindow))
	_, es, _, _, _ := a.snapshot()
	if len(es) != 2 {
		t.Fatalf("got %d entries, want 2", len(es))
	}
	got := map[string]string{}
	for _, e := range es {
		got[e.Project] = e.Session
	}
	if got["/home/me/app"] != "s1" {
		t.Errorf("line with cwd and sessionId: %v", got)
	}
	if got["-home-me-old"] != "def" {
		t.Errorf("line without them: %v", got)
	}
}

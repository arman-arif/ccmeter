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

	line := func(req, id, extra string) string {
		return `{"type":"assistant","timestamp":"` + ts + `","requestId":"` + req + `",` + extra +
			`"message":{"id":"` + id + `","model":"claude-sonnet-4-5",` + usage + `}}` + "\n"
	}

	// A session that runs `cd` changes cwd partway: it's still one project, and
	// the real path is the cwd that encodes to the folder's name, wherever it comes.
	writeLog(t, filepath.Join(dir, "projects", "-home-me-my-app", "abc.jsonl"),
		line("r1", "m1", `"cwd":"/home/me/my-app/web","sessionId":"s1",`)+
			line("r2", "m2", `"cwd":"/home/me/my-app","sessionId":"s1",`)+
			line("r3", "m3", `"cwd":"/tmp/elsewhere","sessionId":"s1",`)+
			// The same response logged again must still count once.
			line("r3", "m3", `"cwd":"/tmp/elsewhere","sessionId":"s1",`))
	// Subagent logs sit deeper in the same project folder.
	writeLog(t, filepath.Join(dir, "projects", "-home-me-my-app", "abc", "subagents", "agent-1.jsonl"),
		line("r4", "m4", `"cwd":"/home/me/my-app","sessionId":"s1",`))
	// Older logs have no cwd or sessionId: fall back to the log's location.
	writeLog(t, filepath.Join(dir, "projects", "-home-me-old", "def.jsonl"), line("r5", "m5", ""))
	// A cwd that doesn't encode to the folder name is still better than nothing.
	writeLog(t, filepath.Join(dir, "projects", "-other", "ghi.jsonl"), line("r6", "m6", `"cwd":"/first/seen","sessionId":"s3",`))

	a := newAccount(AccountConfig{Name: "x", Dir: dir})
	a.Scan(time.Now().Add(-historyWindow))
	_, es, _, _, _ := a.snapshot()
	if len(es) != 6 {
		t.Fatalf("got %d entries, want 6", len(es))
	}
	perProject := map[string]int{}
	sessions := map[string]string{}
	for _, e := range es {
		perProject[e.Project]++
		sessions[e.Project] = e.Session
	}
	if perProject["-home-me-my-app"] != 4 || sessions["-home-me-my-app"] != "s1" {
		t.Errorf("one project despite cd and subagents: %v %v", perProject, sessions)
	}
	if sessions["-home-me-old"] != "def" {
		t.Errorf("line without cwd or sessionId: %v", sessions)
	}

	paths := a.projectPaths()
	if paths["-home-me-my-app"] != "/home/me/my-app" {
		t.Errorf("path of a project whose cwd moved: %q", paths["-home-me-my-app"])
	}
	if p, ok := paths["-home-me-old"]; ok {
		t.Errorf("no cwd anywhere should leave no path, got %q", p)
	}
	if paths["-other"] != "/first/seen" {
		t.Errorf("first cwd as a fallback: %q", paths["-other"])
	}
}

func TestProjectSlug(t *testing.T) {
	for in, want := range map[string]string{
		"/home/me/my_app.v2": "-home-me-my-app-v2",
		"/home/me/.claude":   "-home-me--claude",
		`C:\Users\me\app`:    "C--Users-me-app",
		"/home/me/café":      "-home-me-caf-",
	} {
		if got := projectSlug(in); got != want {
			t.Errorf("projectSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

package ccmeter

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func usageLine(id, model string, at time.Time, in, out int) string {
	return fmt.Sprintf(`{"type":"assistant","timestamp":%q,"requestId":"r%s","sessionId":"s","message":{"id":"m%s","model":%q,"usage":{"input_tokens":%d,"output_tokens":%d}}}`+"\n",
		at.UTC().Format(time.RFC3339), id, id, model, in, out)
}

// trackedScan is what a dashboard start does: a new account reading the logs.
func trackedScan(dir string) *Account {
	a := newTrackedAccount(AccountConfig{Name: "x", Dir: dir})
	a.Scan(time.Now().Add(-historyWindow))
	return a
}

func allTimeOf(t *testing.T, a *Account) *AllTime {
	t.Helper()
	at, _ := a.allTimeUsage(newPricer(nil))
	if at == nil {
		t.Fatal("no all-time usage")
	}
	return at
}

func TestAllTimeOutlivesDeletedLogs(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	const model = "claude-sonnet-4-5" // $3 in, $15 out per million
	now := time.Now()
	old := now.AddDate(0, 0, -60)
	oldLog := filepath.Join(dir, "projects", "p", "old.jsonl")
	writeLog(t, oldLog, usageLine("1", model, old, 1_000_000, 0)+usageLine("1", model, old, 1_000_000, 0))
	writeLog(t, filepath.Join(dir, "projects", "p", "new.jsonl"), usageLine("2", model, now, 0, 1_000_000))
	// A log file last changed long ago is still read.
	if err := os.Chtimes(oldLog, old, old); err != nil {
		t.Fatal(err)
	}

	a := trackedScan(dir)
	want := func(a *Account, responses int, cost float64) {
		t.Helper()
		at := allTimeOf(t, a)
		if at.Messages != responses || math.Abs(at.CostUSD-cost) > 1e-9 {
			t.Fatalf("got %d responses at $%v, want %d at $%v", at.Messages, at.CostUSD, responses, cost)
		}
		if wantSince := old.Local().Format("2006-01-02"); at.Since != wantSince {
			t.Fatalf("since %s, want %s", at.Since, wantSince)
		}
	}
	// The repeated line counts once, and the old response stays out of the
	// entries that the 30-day figures are built from.
	want(a, 2, 18)
	if _, es, _, _, _ := a.snapshot(); len(es) != 1 {
		t.Fatalf("got %d recent entries, want 1", len(es))
	}

	// Rescanning, or restarting and reading it all again, counts nothing twice.
	a.Scan(now.Add(-historyWindow))
	want(a, 2, 18)
	want(trackedScan(dir), 2, 18)

	// Claude Code deletes the old log: it's still counted, even after a restart.
	if err := os.Remove(oldLog); err != nil {
		t.Fatal(err)
	}
	want(trackedScan(dir), 2, 18)

	// New usage on a day already in the ledger adds to it.
	writeLog(t, filepath.Join(dir, "projects", "p", "more.jsonl"), usageLine("3", model, now, 0, 1_000_000))
	want(trackedScan(dir), 3, 33)
}

func TestAllTimeKeepsLongPromptPrices(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	const model = "claude-haiku-5-5" // $0.10 in, or $0.50 in over 100K tokens
	now := time.Now()
	// Each of these is under the threshold, but all of them together are not.
	var short string
	for i := 0; i < 20; i++ {
		short += usageLine(fmt.Sprint("s", i), model, now, 50_000, 0)
	}
	writeLog(t, filepath.Join(dir, "projects", "p", "a.jsonl"), short+usageLine("long", model, now, 1_000_000, 0))

	at := allTimeOf(t, trackedScan(dir))
	if want := 0.10 + 0.50; math.Abs(at.CostUSD-want) > 1e-9 {
		t.Fatalf("got $%v, want $%v", at.CostUSD, want)
	}
}

func TestAllTimeReportsUnpricedModels(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	old := time.Now().AddDate(0, 0, -60)
	writeLog(t, filepath.Join(dir, "projects", "p", "a.jsonl"), usageLine("1", "mystery-model", old, 10, 10))

	d := buildDetail(trackedScan(dir), newPricer(nil), time.Now(), 30)
	if d.AllTime == nil || d.AllTime.Messages != 1 {
		t.Fatalf("all time: %+v", d.AllTime)
	}
	if len(d.Unpriced) != 1 || d.Unpriced[0] != "mystery-model" {
		t.Fatalf("unpriced: %v", d.Unpriced)
	}
}

func TestDamagedLedgerIsKeptAside(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := usageLedgerFile(dir)
	writeLog(t, path, "{not json")
	writeLog(t, filepath.Join(dir, "projects", "p", "a.jsonl"), usageLine("1", "claude-sonnet-4-5", time.Now(), 10, 10))

	if at := allTimeOf(t, trackedScan(dir)); at.Messages != 1 {
		t.Fatalf("got %d responses, want 1", at.Messages)
	}
	if b, err := os.ReadFile(path + ".bad"); err != nil || string(b) != "{not json" {
		t.Fatalf("damaged ledger not kept: %q, %v", b, err)
	}
}

// The status line and table use plain accounts, which must stay quick.
func TestPlainAccountDoesNotTrackAllTime(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	now := time.Now()
	oldLog := filepath.Join(dir, "projects", "p", "old.jsonl")
	writeLog(t, oldLog, usageLine("1", "claude-sonnet-4-5", now.AddDate(0, 0, -60), 10, 10))
	if err := os.Chtimes(oldLog, now.AddDate(0, 0, -60), now.AddDate(0, 0, -60)); err != nil {
		t.Fatal(err)
	}

	a := newAccount(AccountConfig{Name: "x", Dir: dir})
	a.Scan(now.Add(-historyWindow))
	if at, _ := a.allTimeUsage(newPricer(nil)); at != nil {
		t.Fatalf("got all-time usage: %+v", at)
	}
	if _, _, _, _, files := a.snapshot(); files != 0 {
		t.Fatalf("read %d old log files, want 0", files)
	}
	if _, err := os.Stat(usageLedgerFile(dir)); !os.IsNotExist(err) {
		t.Fatalf("ledger written: %v", err)
	}
}

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Entry is one billed assistant response.
type Entry struct {
	T     time.Time
	Model string
	In    int64 // input tokens
	Out   int64 // output tokens
	CW5   int64 // cache write, 5-minute TTL
	CW1h  int64 // cache write, 1-hour TTL
	CR    int64 // cache read
}

func (e Entry) Total() int64 { return e.In + e.Out + e.CW5 + e.CW1h + e.CR }

type fileState struct {
	off  int64
	size int64
	mod  time.Time
}

// Account holds the parsed usage of one config dir and scans it incrementally:
// each .jsonl file is read from where the last scan stopped.
type Account struct {
	Cfg AccountConfig

	mu      sync.Mutex
	files   map[string]*fileState
	seen    map[string]struct{}
	entries []Entry
	found   bool
	lastErr string
}

func newAccount(c AccountConfig) *Account {
	return &Account{Cfg: c, files: map[string]*fileState{}, seen: map[string]struct{}{}}
}

type rawLine struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	RequestID string `json:"requestId"`
	Message   *struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage *struct {
			InputTokens   int64 `json:"input_tokens"`
			OutputTokens  int64 `json:"output_tokens"`
			CacheCreation int64 `json:"cache_creation_input_tokens"`
			CacheRead     int64 `json:"cache_read_input_tokens"`
			CC            *struct {
				E5m int64 `json:"ephemeral_5m_input_tokens"`
				E1h int64 `json:"ephemeral_1h_input_tokens"`
			} `json:"cache_creation"`
		} `json:"usage"`
	} `json:"message"`
}

var usageMarker = []byte(`"usage"`)

// Scan reads new lines from the account's projects/ folder. Files last
// modified before cutoff are skipped, and so are entries older than cutoff.
func (a *Account) Scan(cutoff time.Time) {
	root := filepath.Join(expandHome(a.Cfg.Dir), "projects")
	st, err := os.Stat(root)

	a.mu.Lock()
	defer a.mu.Unlock()
	if err != nil || !st.IsDir() {
		a.found = false
		if err != nil {
			a.lastErr = err.Error()
		}
		return
	}
	a.found, a.lastErr = true, ""

	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable subfolder: skip it, keep going
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.ModTime().Before(cutoff) {
			return nil
		}
		fsState := a.files[path]
		if fsState == nil {
			fsState = &fileState{}
			a.files[path] = fsState
		}
		if fsState.size == info.Size() && fsState.mod.Equal(info.ModTime()) {
			return nil
		}
		if info.Size() < fsState.off { // truncated or replaced
			fsState.off = 0
		}
		a.readFrom(path, fsState, cutoff)
		fsState.size, fsState.mod = info.Size(), info.ModTime()
		return nil
	})

	// Drop entries that have aged out so a long-running server stays small.
	if len(a.entries) > 0 && a.entries[0].T.Before(cutoff) {
		kept := a.entries[:0]
		for _, e := range a.entries {
			if !e.T.Before(cutoff) {
				kept = append(kept, e)
			}
		}
		a.entries = kept
	}
}

func (a *Account) readFrom(path string, st *fileState, cutoff time.Time) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	if _, err := f.Seek(st.off, io.SeekStart); err != nil {
		return
	}
	r := bufio.NewReaderSize(f, 256*1024)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) == 0 || line[len(line)-1] != '\n' {
			break // EOF or a partial line still being written: resume here next time
		}
		st.off += int64(len(line))
		if bytes.Contains(line, usageMarker) {
			a.parseLine(line, cutoff)
		}
		if err != nil {
			break
		}
	}
}

func (a *Account) parseLine(line []byte, cutoff time.Time) {
	var rl rawLine
	if json.Unmarshal(line, &rl) != nil {
		return
	}
	if rl.Type != "assistant" || rl.Message == nil || rl.Message.Usage == nil {
		return
	}
	if rl.Message.Model == "" || rl.Message.Model == "<synthetic>" {
		return
	}
	t, err := time.Parse(time.RFC3339Nano, rl.Timestamp)
	if err != nil || t.Before(cutoff) {
		return
	}
	// One response can be logged on several lines (one per content block)
	// with the same usage; count it once.
	if rl.Message.ID != "" && rl.RequestID != "" {
		key := rl.Message.ID + ":" + rl.RequestID
		if _, dup := a.seen[key]; dup {
			return
		}
		a.seen[key] = struct{}{}
	}
	u := rl.Message.Usage
	e := Entry{T: t, Model: rl.Message.Model, In: u.InputTokens, Out: u.OutputTokens, CR: u.CacheRead}
	if u.CC != nil && (u.CC.E5m+u.CC.E1h) > 0 {
		e.CW5, e.CW1h = u.CC.E5m, u.CC.E1h
	} else {
		e.CW5 = u.CacheCreation
	}
	a.entries = append(a.entries, e)
}

// snapshot returns a copy of the entries for computing stats without holding the lock.
func (a *Account) snapshot() ([]Entry, bool, string, int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]Entry, len(a.entries))
	copy(out, a.entries)
	return out, a.found, a.lastErr, len(a.files)
}

package ccmeter

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Claude Code deletes old logs (after 30 days by default), so the logs alone
// can't give an all-time figure. The dashboard keeps its own ledger of usage per
// day and model, which outlives them. It holds token counts rather than costs,
// so a change of price applies to the past too, as it does everywhere else.
//
// The status line and the table don't track this: they read only recent logs,
// because they have to start quickly.

// usageKey identifies one bucket. Days are UTC, so a bucket means the same thing
// wherever and whenever it's compared. Long keeps prompts over longPromptTokens
// apart, because some models charge those at a higher price.
type usageKey struct {
	day, model string
	long       bool
}

type usageBucket struct {
	Day       string `json:"day"`
	Model     string `json:"model"`
	Long      bool   `json:"long,omitempty"`
	Responses int    `json:"responses"`
	In        int64  `json:"input"`
	Out       int64  `json:"output"`
	CW5       int64  `json:"cache_write_5m"`
	CW1h      int64  `json:"cache_write_1h"`
	CR        int64  `json:"cache_read"`
}

func (b usageBucket) key() usageKey { return usageKey{b.Day, b.Model, b.Long} }

type usageLedger struct {
	Dir     string        `json:"dir"`
	First   time.Time     `json:"first"` // the earliest response counted
	Buckets []usageBucket `json:"buckets"`
}

// AllTime is an account's usage over everything ccmeter has seen.
type AllTime struct {
	Totals
	Since string `json:"since"` // local date of the earliest response, 2006-01-02
}

// usageLedgerFile sits in the config dir, not the cache dir with the other state:
// once a log is gone this is the only record of it, so it mustn't be cleared
// along with a cache. It's keyed by the account's dir, like the plan limits.
func usageLedgerFile(dir string) string {
	abs, err := absDir(dir)
	if err != nil {
		abs = dir
	}
	sum := sha256.Sum256([]byte(abs))
	return filepath.Join(userConfigDir(), "ccmeter", "usage", hex.EncodeToString(sum[:])[:16]+".json")
}

// newTrackedAccount is an account that also keeps the all-time ledger. Its scans
// read every log on disk, not only recent ones.
func newTrackedAccount(c AccountConfig) *Account {
	a := newAccount(c)
	a.allTime = true
	a.logged = map[usageKey]usageBucket{}
	return a
}

// countAllTime adds a response to the totals from the logs read so far.
func (a *Account) countAllTime(e Entry) {
	k := usageKey{e.T.UTC().Format("2006-01-02"), e.Model, e.longPrompt()}
	b := a.logged[k]
	b.Day, b.Model, b.Long = k.day, k.model, k.long
	b.Responses++
	b.In += e.In
	b.Out += e.Out
	b.CW5 += e.CW5
	b.CW1h += e.CW1h
	b.CR += e.CR
	a.logged[k] = b
	if a.first.IsZero() || e.T.Before(a.first) {
		a.first = e.T
	}
}

// loadLedger reads the saved ledger once. A file that can't be parsed is moved
// aside rather than overwritten, and one that can't be read turns saving off,
// because either may hold history that exists nowhere else.
func (a *Account) loadLedger() {
	if a.ledger != nil {
		return
	}
	a.ledger = map[usageKey]usageBucket{}
	path := usageLedgerFile(a.Cfg.Dir)
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		log.Printf("usage ledger for %s not read, so not saving it either: %v", a.Cfg.Dir, err)
		a.ledgerOff = true
		return
	}
	var saved usageLedger
	if err := json.Unmarshal(b, &saved); err != nil {
		log.Printf("usage ledger for %s is damaged, keeping it as %s.bad: %v", a.Cfg.Dir, path, err)
		if err := os.Rename(path, path+".bad"); err != nil {
			a.ledgerOff = true
		}
		return
	}
	for _, x := range saved.Buckets {
		a.ledger[x.key()] = x
	}
	a.ledgerFirst = saved.First
}

// syncLedger brings the ledger up to date with the logs and saves it if that
// changed anything. For each bucket the side with more responses wins: logs only
// grow until Claude Code deletes some, so the larger count is the fuller record.
// That makes this safe to repeat after a restart, when every log is read again,
// without counting anything twice.
func (a *Account) syncLedger() {
	a.loadLedger()
	for k, b := range a.logged {
		if b.Responses > a.ledger[k].Responses {
			a.ledger[k] = b
			a.ledgerDirty = true
		}
	}
	if !a.first.IsZero() && (a.ledgerFirst.IsZero() || a.first.Before(a.ledgerFirst)) {
		a.ledgerFirst = a.first
		a.ledgerDirty = true
	}
	if !a.ledgerDirty || a.ledgerOff {
		return
	}
	err := a.saveLedger()
	if err == nil {
		a.ledgerDirty, a.ledgerErr = false, ""
	} else if err.Error() != a.ledgerErr { // it's retried every scan, so say so once
		a.ledgerErr = err.Error()
		log.Printf("usage ledger for %s not saved: %v", a.Cfg.Dir, err)
	}
}

func (a *Account) saveLedger() error {
	l := usageLedger{Dir: a.Cfg.Dir, First: a.ledgerFirst, Buckets: make([]usageBucket, 0, len(a.ledger))}
	for _, b := range a.ledger {
		l.Buckets = append(l.Buckets, b)
	}
	sort.Slice(l.Buckets, func(i, j int) bool {
		x, y := l.Buckets[i], l.Buckets[j]
		if x.Day != y.Day {
			return x.Day < y.Day
		}
		if x.Model != y.Model {
			return x.Model < y.Model
		}
		return !x.Long && y.Long
	})
	b, err := json.MarshalIndent(l, "", " ")
	if err != nil {
		return err
	}
	path := usageLedgerFile(a.Cfg.Dir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// allTimeUsage returns the account's all-time totals at today's prices and any
// models in them with no price, or nil if this account isn't tracked or has no usage.
func (a *Account) allTimeUsage(pr *Pricer) (*AllTime, []string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.allTime {
		return nil, nil
	}
	a.loadLedger()
	if len(a.ledger) == 0 {
		return nil, nil
	}
	at := &AllTime{Since: a.ledgerFirst.Local().Format("2006-01-02")}
	unpriced := map[string]bool{}
	for _, b := range a.ledger {
		e := Entry{Model: b.Model, In: b.In, Out: b.Out, CW5: b.CW5, CW1h: b.CW1h, CR: b.CR}
		c, ok := pr.costAt(e, b.Long)
		if !ok {
			unpriced[b.Model] = true
		}
		at.add(e, c)
		at.Messages += b.Responses - 1 // add counted this bucket as one response
	}
	models := make([]string, 0, len(unpriced))
	for m := range unpriced {
		models = append(models, m)
	}
	return at, models
}

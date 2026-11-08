package ccmeter

import (
	"log"
	"sync"
	"time"
)

// Background plan-limit refreshes, run by `ccmeter serve` (and so by
// `ccmeter start` and autostart). Accounts used in the last 5 hours are
// checked every `interval`; idle accounts every `idleInterval`. Checks run one
// at a time and share the throttle state and lock with the hook, so the two
// never double up.

const (
	defaultLimitsInterval = 5 * time.Minute
	idleLimitsInterval    = 30 * time.Minute
	activeWindow          = 5 * time.Hour
)

// lastActivity is the time of the newest logged response for the account.
func (a *Account) lastActivity() time.Time {
	a.mu.Lock()
	defer a.mu.Unlock()
	var last time.Time
	for _, e := range a.entries {
		if e.T.After(last) {
			last = e.T
		}
	}
	return last
}

func (a *Account) foundDir() (string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.Cfg.Dir, a.found
}

// archived reports whether the account is archived, which keeps it out of
// scheduled plan-limit checks: its config and history are kept, but it is
// treated as dormant.
func (a *Account) archived() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.Cfg.Archived
}

// limitsDue decides whether a scheduled check should run for an account now.
func limitsDue(st fetchState, lastActive time.Time, interval time.Duration, now time.Time) bool {
	if now.Before(st.NextAllowed) {
		return false // backing off after "too many requests"
	}
	iv := idleLimitsInterval
	if now.Sub(lastActive) < activeWindow {
		iv = interval
	}
	if iv < interval {
		iv = interval
	}
	return now.Sub(st.LastAttempt) >= iv
}

type limitsScheduler struct {
	srv      *server
	interval time.Duration
	trigger  chan struct{}

	mu      sync.Mutex
	running bool
}

func newLimitsScheduler(s *server, interval time.Duration) *limitsScheduler {
	if interval > 0 && interval < time.Minute {
		interval = time.Minute
	}
	return &limitsScheduler{srv: s, interval: interval, trigger: make(chan struct{}, 1)}
}

// refreshNow asks for an immediate check of every account (the dashboard's
// refresh button). It returns false if a pass is already running.
func (ls *limitsScheduler) refreshNow() bool {
	ls.mu.Lock()
	busy := ls.running
	ls.mu.Unlock()
	if busy {
		return false
	}
	select {
	case ls.trigger <- struct{}{}:
	default:
	}
	return true
}

func (ls *limitsScheduler) busy() bool {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	return ls.running
}

func (ls *limitsScheduler) run() {
	if ls.interval <= 0 {
		// Scheduling is off; still serve manual refreshes.
		for range ls.trigger {
			ls.pass(true)
		}
		return
	}
	time.Sleep(3 * time.Second) // let the first log scan finish
	ls.pass(false)
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			ls.pass(false)
		case <-ls.trigger:
			ls.pass(true)
		}
	}
}

func (ls *limitsScheduler) pass(force bool) {
	ls.mu.Lock()
	ls.running = true
	ls.mu.Unlock()
	defer func() {
		ls.mu.Lock()
		ls.running = false
		ls.mu.Unlock()
	}()

	accts, _ := ls.srv.current()
	for _, a := range accts {
		if a.archived() {
			continue
		}
		dir, found := a.foundDir()
		if !found {
			continue
		}
		now := time.Now()
		st := readFetchState(dir)
		if force {
			if now.Before(st.NextAllowed) {
				continue
			}
		} else if !limitsDue(st, a.lastActivity(), ls.interval, now) {
			continue
		}
		if _, err := refreshLimits(dir, true); err != nil {
			log.Printf("plan limits for %s: %v", dir, err)
		}
	}
}

// refreshDueLimits brings stale limits up to date before a one-off command
// (`ccmeter`, `ccmeter json`) prints them. Accounts are checked in parallel.
func refreshDueLimits(accts []*Account) {
	var wg sync.WaitGroup
	now := time.Now()
	for _, a := range accts {
		dir, found := a.foundDir()
		if !found || a.archived() || !fetchDue(dir, now) {
			continue
		}
		wg.Add(1)
		go func(dir string) {
			defer wg.Done()
			_, _ = refreshLimits(dir, false)
		}(dir)
	}
	wg.Wait()
}

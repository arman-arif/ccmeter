package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Plan limits from the usage endpoint behind Claude Code's /usage screen.
// Opt-in only (CCMETER_LIMITS_SOURCE=auto or =endpoint): it reads the
// account's saved login token. This endpoint is undocumented and may change. ccmeter only ever reads the
// account's saved login: it never refreshes or rewrites credentials, so it
// can't interfere with Claude Code's own session. An expired token is
// reported and skipped until Claude Code refreshes it.

const (
	usageURL        = "https://api.anthropic.com/api/oauth/usage"
	usageBeta       = "oauth-2025-04-20"
	fetchMinGap     = 5 * time.Minute  // per account, between fetches
	fetchBackoff429 = 15 * time.Minute // after "too many requests"
)

type oauthCreds struct {
	AccessToken string
	ExpiresAt   time.Time // zero if unknown
	Source      string
}

func isDefaultConfigDir(dir string) bool {
	return sameDir(dir, filepath.Join(homeDir(), ".claude"))
}

// readCreds finds the account's OAuth access token without modifying anything.
func readCreds(dir string) (oauthCreds, error) {
	if t := strings.TrimSpace(os.Getenv("CLAUDE_CODE_OAUTH_TOKEN")); t != "" {
		if env := os.Getenv("CLAUDE_CONFIG_DIR"); env == "" || sameDir(env, dir) {
			return oauthCreds{AccessToken: t, Source: "CLAUDE_CODE_OAUTH_TOKEN"}, nil
		}
	}
	var blobs []struct {
		data []byte
		src  string
	}
	if runtime.GOOS == "darwin" {
		for _, svc := range keychainServices(dir) {
			out, err := exec.Command("security", "find-generic-password", "-s", svc, "-w").Output()
			if err == nil && len(out) > 0 {
				blobs = append(blobs, struct {
					data []byte
					src  string
				}{out, "keychain:" + svc})
				break
			}
		}
	}
	path := filepath.Join(expandHome(dir), ".credentials.json")
	if b, err := os.ReadFile(path); err == nil {
		blobs = append(blobs, struct {
			data []byte
			src  string
		}{b, path})
	}
	if len(blobs) == 0 {
		return oauthCreds{}, errors.New("no Claude login found for this config dir (API-key accounts have no plan limits)")
	}
	for _, b := range blobs {
		var f struct {
			ClaudeAiOauth struct {
				AccessToken string  `json:"accessToken"`
				ExpiresAt   float64 `json:"expiresAt"` // epoch milliseconds
			} `json:"claudeAiOauth"`
		}
		if json.Unmarshal(b.data, &f) != nil || f.ClaudeAiOauth.AccessToken == "" {
			continue
		}
		c := oauthCreds{AccessToken: f.ClaudeAiOauth.AccessToken, Source: b.src}
		if f.ClaudeAiOauth.ExpiresAt > 0 {
			c.ExpiresAt = time.UnixMilli(int64(f.ClaudeAiOauth.ExpiresAt))
		}
		return c, nil
	}
	return oauthCreds{}, errors.New("saved Claude login has no access token")
}

// keychainServices lists the macOS Keychain item names Claude Code may use.
// Non-default config dirs get a suffix derived from the dir path.
func keychainServices(dir string) []string {
	base := "Claude Code-credentials"
	if isDefaultConfigDir(dir) {
		return []string{base}
	}
	abs, _ := absDir(dir)
	sum := sha256.Sum256([]byte(abs))
	return []string{base + "-" + hex.EncodeToString(sum[:])[:8], base}
}

type usageWindow struct {
	Utilization *float64 `json:"utilization"` // percent, 0-100
	ResetsAt    *string  `json:"resets_at"`
}

type usageResponse struct {
	FiveHour *usageWindow `json:"five_hour"`
	SevenDay *usageWindow `json:"seven_day"`
}

func (w *usageWindow) toWindow() *LimitWindow {
	if w == nil || w.Utilization == nil {
		return nil
	}
	lw := &LimitWindow{UsedPct: *w.Utilization}
	if w.ResetsAt != nil {
		if t, err := time.Parse(time.RFC3339Nano, *w.ResetsAt); err == nil {
			lw.ResetsAt = t
		}
	}
	return lw
}

type errRateLimited struct{}

func (errRateLimited) Error() string { return "usage endpoint rate-limited this login; backing off" }

func fetchUsage(c oauthCreds) (*LimitWindow, *LimitWindow, error) {
	url := usageURL
	if u := os.Getenv("CCMETER_USAGE_URL"); u != "" { // for testing
		url = u
	}
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+c.AccessToken)
	req.Header.Set("anthropic-beta", usageBeta)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "ccmeter/"+version)
	client := http.Client{Timeout: 10 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	switch {
	case res.StatusCode == http.StatusTooManyRequests:
		return nil, nil, errRateLimited{}
	case res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden:
		return nil, nil, fmt.Errorf("login rejected (%s); open Claude Code in this account to refresh it", res.Status)
	case res.StatusCode != http.StatusOK:
		return nil, nil, fmt.Errorf("usage endpoint returned %s", res.Status)
	}
	var u usageResponse
	if err := json.Unmarshal(body, &u); err != nil {
		return nil, nil, fmt.Errorf("unexpected usage response: %v", err)
	}
	five, seven := u.FiveHour.toWindow(), u.SevenDay.toWindow()
	if five == nil && seven == nil {
		return nil, nil, errors.New("no plan limits in the response (not a Pro/Max/Team login?)")
	}
	return five, seven, nil
}

// ---- per-account fetch state --------------------------------------------

type fetchState struct {
	LastAttempt time.Time `json:"last_attempt"`
	NextAllowed time.Time `json:"next_allowed"`
	LastError   string    `json:"last_error,omitempty"`
	LastOK      time.Time `json:"last_ok,omitempty"`
}

func statePathFor(dir string) string {
	return strings.TrimSuffix(limitsFile(dir), ".json") + ".state.json"
}

func readFetchState(dir string) fetchState {
	var s fetchState
	if b, err := os.ReadFile(statePathFor(dir)); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s
}

func writeFetchState(dir string, s fetchState) {
	path := statePathFor(dir)
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	b, _ := json.Marshal(s)
	tmp := fmt.Sprintf("%s.%d.tmp", path, os.Getpid())
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, path)
	}
}

// fetchDue reports whether a fetch for dir is allowed now.
func fetchDue(dir string, now time.Time) bool {
	s := readFetchState(dir)
	return !now.Before(s.NextAllowed) && now.Sub(s.LastAttempt) >= fetchMinGap
}

// lockFetch takes a short-lived per-account lock so parallel sessions don't
// fetch at the same time. A lock older than a minute is considered stale.
func lockFetch(dir string) (func(), bool) {
	path := strings.TrimSuffix(limitsFile(dir), ".json") + ".lock"
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	for i := 0; i < 2; i++ {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			f.Close()
			return func() { os.Remove(path) }, true
		}
		if st, err := os.Stat(path); err == nil && time.Since(st.ModTime()) > time.Minute {
			os.Remove(path)
			continue
		}
		return nil, false
	}
	return nil, false
}

// refreshLimits fetches and stores the plan limits for dir. With force, the
// minimum gap is ignored (but a rate-limit backoff still applies).
func refreshLimits(dir string, force bool) (*PlanLimits, error) {
	now := time.Now()
	st := readFetchState(dir)
	if now.Before(st.NextAllowed) {
		return loadLimits(dir, now), fmt.Errorf("waiting until %s before asking again (%s)", st.NextAllowed.Local().Format("15:04"), st.LastError)
	}
	if !force && now.Sub(st.LastAttempt) < fetchMinGap {
		return loadLimits(dir, now), nil
	}
	unlock, ok := lockFetch(dir)
	if !ok {
		return loadLimits(dir, now), nil // another process is fetching
	}
	defer unlock()

	st.LastAttempt = now
	st.NextAllowed = time.Time{}
	writeFetchState(dir, st)

	five, seven, models, source, err := fetchPlanLimits(dir, now)
	if err != nil {
		st.LastError = err.Error()
		if errors.As(err, new(errRateLimited)) {
			st.NextAllowed = now.Add(fetchBackoff429)
		}
		writeFetchState(dir, st)
		return loadLimits(dir, now), err
	}
	st.LastError, st.LastOK = "", now
	writeFetchState(dir, st)
	if err := saveLimitWindows(dir, source, five, seven, models, now); err != nil {
		return nil, err
	}
	return loadLimits(dir, now), nil
}

// fetchPlanLimits runs `claude -p /usage` (official, refreshes the login
// itself). The usage endpoint needs the account's saved login token, which
// Anthropic's usage policy reserves for Claude Code and its own apps, so it
// is opt-in: CCMETER_LIMITS_SOURCE=auto tries the CLI and then the endpoint,
// and =endpoint uses only the endpoint.
func fetchPlanLimits(dir string, now time.Time) (five, seven *LimitWindow, models []ModelLimit, source string, err error) {
	mode := strings.ToLower(os.Getenv("CCMETER_LIMITS_SOURCE"))
	var cliErr error
	if mode != "endpoint" {
		if five, seven, models, cliErr = claudeUsage(dir, now); cliErr == nil {
			return five, seven, models, "claude-cli", nil
		}
		if mode != "auto" {
			return nil, nil, nil, "", cliErr
		}
	}
	c, err := readCreds(dir)
	if err == nil && !c.ExpiresAt.IsZero() && time.Until(c.ExpiresAt) < 30*time.Second {
		err = errors.New("saved login has expired; open Claude Code in this account and it will refresh it")
	}
	if err == nil {
		five, seven, err = fetchUsage(c)
	}
	if err != nil {
		if cliErr != nil && !errors.As(err, new(errRateLimited)) {
			err = fmt.Errorf("%v; usage endpoint: %w", cliErr, err)
		}
		return nil, nil, nil, "", err
	}
	return five, seven, nil, "usage-endpoint", nil
}

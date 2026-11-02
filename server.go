package main

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"mime"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

//go:embed web
var webFS embed.FS

// server owns the live account list. Accounts change when the config file
// changes on disk (e.g. `ccmeter add`) or through the dashboard's API.
type server struct {
	cfgPath  string
	readOnly bool

	mu       sync.Mutex
	cfg      Config
	fromFile bool
	cfgMod   time.Time
	pr       *Pricer
	accts    []*Account
}

func fileMod(path string) time.Time {
	st, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return st.ModTime()
}

// applyLocked swaps in a new config, keeping scan state for accounts whose
// dir did not change. Caller holds s.mu.
func (s *server) applyLocked(cfg Config) {
	old := map[string]*Account{}
	for _, a := range s.accts {
		c, _, _, _, _ := a.snapshot()
		if abs, err := absDir(c.Dir); err == nil {
			old[abs] = a
		}
	}
	next := make([]*Account, 0, len(cfg.Accounts))
	for _, c := range cfg.Accounts {
		abs, _ := absDir(c.Dir)
		if a, ok := old[abs]; ok {
			a.setConfig(c)
			next = append(next, a)
			delete(old, abs)
		} else {
			next = append(next, newAccount(c))
		}
	}
	s.cfg, s.accts, s.pr = cfg, next, newPricer(cfg.Pricing)
}

// reloadIfChanged picks up edits made to the config file outside the dashboard.
func (s *server) reloadIfChanged() {
	s.mu.Lock()
	defer s.mu.Unlock()
	mod := fileMod(s.cfgPath)
	if mod.Equal(s.cfgMod) {
		return
	}
	cfg, fromFile, err := loadConfig(s.cfgPath)
	if err != nil {
		log.Printf("config reload failed, keeping the current accounts: %v", err)
		s.cfgMod = mod
		return
	}
	s.cfgMod, s.fromFile = mod, fromFile
	s.applyLocked(cfg)
	log.Printf("config reloaded: %d account(s)", len(cfg.Accounts))
}

func (s *server) current() ([]*Account, *Pricer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*Account(nil), s.accts...), s.pr
}

func (s *server) scan() {
	accts, _ := s.current()
	cutoff := time.Now().Add(-historyWindow)
	var wg sync.WaitGroup
	for _, a := range accts {
		wg.Add(1)
		go func(a *Account) { defer wg.Done(); a.Scan(cutoff) }(a)
	}
	wg.Wait()
}

func serve(cfg Config, cfgPath string, pr *Pricer, addr string, every time.Duration, readOnly bool, limitsEvery time.Duration) error {
	_, fromFile, _ := loadConfig(cfgPath)
	s := &server{cfgPath: cfgPath, readOnly: readOnly, fromFile: fromFile, cfgMod: fileMod(cfgPath)}
	s.applyLocked(cfg)
	s.pr = pr

	log.Printf("scanning %d account(s)...", len(cfg.Accounts))
	s.scan()
	go func() {
		for range time.Tick(every) {
			s.reloadIfChanged()
			s.scan()
		}
	}()
	sched := newLimitsScheduler(s, limitsEvery)
	go sched.run()

	mux := http.NewServeMux()
	sub, _ := fs.Sub(webFS, "web")
	mux.Handle("/", http.FileServer(http.FS(sub)))
	mux.HandleFunc("GET /api/summary", func(w http.ResponseWriter, r *http.Request) {
		accts, pr := s.current()
		writeJSON(w, http.StatusOK, buildReport(accts, pr, time.Now()))
	})
	started := time.Now()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, healthInfo{PID: os.Getpid(), Version: version, Addr: addr, Config: cfgPath, Started: started})
	})
	mux.HandleFunc("GET /api/accounts", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, s.accountList(r))
	})
	mux.HandleFunc("POST /api/limits/refresh", func(w http.ResponseWriter, r *http.Request) {
		if err := s.sameMachine(r); err != nil {
			writeErr(w, http.StatusForbidden, err)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]bool{"started": sched.refreshNow()})
	})
	mux.HandleFunc("GET /api/limits/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"refreshing": sched.busy(), "interval": limitsEvery.String()})
	})
	mux.HandleFunc("POST /api/accounts", s.handleSave)
	mux.HandleFunc("DELETE /api/accounts", s.handleDelete)

	limitsNote := "plan limits every " + limitsEvery.String() + " (idle accounts every " + idleLimitsInterval.String() + ")"
	if limitsEvery <= 0 {
		limitsNote = "scheduled plan limit checks off"
	}
	fmt.Printf("ccmeter dashboard on http://%s  (%d accounts, rescans every %s, %s)\n", addr, len(cfg.Accounts), every, limitsNote)
	return http.ListenAndServe(addr, localOnly(addr, mux))
}

// localOnly blocks DNS rebinding: a website whose domain resolves to
// 127.0.0.1 would otherwise reach this server as its own origin and could
// read usage data or edit accounts. Browsers send that domain as Host, so
// only IP addresses, localhost, this machine's name and the -addr host pass.
func localOnly(addr string, next http.Handler) http.Handler {
	allowed := map[string]bool{"localhost": true}
	if h, _, err := net.SplitHostPort(addr); err == nil && h != "" {
		allowed[strings.ToLower(h)] = true
	}
	if h, err := os.Hostname(); err == nil && h != "" {
		h = strings.ToLower(h)
		allowed[h], allowed[h+".local"] = true, true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allowedHost(r.Host, allowed) {
			writeErr(w, http.StatusForbidden, fmt.Errorf("host %q not allowed: open the dashboard by IP address or localhost, or start it with -addr set to that name", r.Host))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func allowedHost(host string, allowed map[string]bool) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.ToLower(strings.Trim(host, "[]")), ".")
	// Empty Host only comes from non-browser HTTP/1.0 clients.
	return host == "" || net.ParseIP(host) != nil || allowed[host]
}

type accountView struct {
	AccountConfig
	HasProjects bool `json:"has_projects"`
}

type accountsResponse struct {
	ConfigPath string        `json:"config_path"`
	FromFile   bool          `json:"from_file"`
	Editable   bool          `json:"editable"`
	Accounts   []accountView `json:"accounts"`
}

func (s *server) accountList(r *http.Request) accountsResponse {
	s.mu.Lock()
	defer s.mu.Unlock()
	resp := accountsResponse{ConfigPath: s.cfgPath, FromFile: s.fromFile, Editable: s.canEdit(r) == nil}
	for _, a := range s.cfg.Accounts {
		resp.Accounts = append(resp.Accounts, accountView{AccountConfig: a, HasProjects: hasProjects(a.Dir)})
	}
	return resp
}

// canEdit only lets the local machine change accounts, and only from the
// dashboard's own origin. JSON bodies and DELETE also force a CORS preflight,
// which this server never approves, so other websites can't call these routes.
func (s *server) canEdit(r *http.Request) error {
	if s.readOnly {
		return errors.New("the dashboard was started with -read-only")
	}
	return s.sameMachine(r)
}

// sameMachine accepts only loopback requests from the dashboard's own page.
func (s *server) sameMachine(r *http.Request) error {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if ip := net.ParseIP(host); err != nil || ip == nil || !ip.IsLoopback() {
		return errors.New("accounts can only be changed from this machine")
	}
	if o := r.Header.Get("Origin"); o != "" && o != "http://"+r.Host {
		return errors.New("cross-origin request refused")
	}
	return nil
}

type saveRequest struct {
	Original       string  `json:"original"` // current name when editing; empty to add
	Name           string  `json:"name"`
	Dir            string  `json:"dir"`
	BlockLimitUSD  float64 `json:"block_limit_usd"`
	WeeklyLimitUSD float64 `json:"weekly_limit_usd"`
}

func (s *server) handleSave(w http.ResponseWriter, r *http.Request) {
	if err := s.canEdit(r); err != nil {
		writeErr(w, http.StatusForbidden, err)
		return
	}
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
		writeErr(w, http.StatusUnsupportedMediaType, errors.New("expected application/json"))
		return
	}
	var req saveRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("bad request: %v", err))
		return
	}
	acct := AccountConfig{Name: req.Name, Dir: req.Dir, BlockLimitUSD: req.BlockLimitUSD, WeeklyLimitUSD: req.WeeklyLimitUSD}
	if err := s.mutate(func(c *Config) error {
		_, err := upsertAccount(c, acct, req.Original, false)
		return err
	}); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	go s.scan()
	writeJSON(w, http.StatusOK, s.accountList(r))
}

func (s *server) handleDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.canEdit(r); err != nil {
		writeErr(w, http.StatusForbidden, err)
		return
	}
	name := r.URL.Query().Get("name")
	if name == "" {
		writeErr(w, http.StatusBadRequest, errors.New("missing ?name="))
		return
	}
	if err := s.mutate(func(c *Config) error {
		_, err := removeAccount(c, name)
		return err
	}); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, s.accountList(r))
}

// mutate applies fn to a copy of the config, saves it, then swaps it in.
func (s *server) mutate(fn func(*Config) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.cfg
	next.Accounts = append([]AccountConfig(nil), s.cfg.Accounts...)
	if err := fn(&next); err != nil {
		return err
	}
	if err := saveConfig(s.cfgPath, next); err != nil {
		return fmt.Errorf("saving %s: %v", filepath.Base(s.cfgPath), err)
	}
	s.cfgMod, s.fromFile = fileMod(s.cfgPath), true
	s.applyLocked(next)
	return nil
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

package ccmeter

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Background mode: `ccmeter start` launches `ccmeter serve` as a detached
// process, records it in a pid file and logs to a file in the state dir.

const defaultAddr = "127.0.0.1:7878"

func stateDir() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = filepath.Join(homeDir(), ".cache")
	}
	return filepath.Join(dir, "ccmeter")
}

func pidPath() string { return filepath.Join(stateDir(), "ccmeter.pid") }
func logPath() string { return filepath.Join(stateDir(), "ccmeter.log") }

type pidFile struct {
	PID     int       `json:"pid"`
	Addr    string    `json:"addr"`
	Started time.Time `json:"started"`
}

func readPidFile() (pidFile, bool) {
	var p pidFile
	b, err := os.ReadFile(pidPath())
	if err != nil || json.Unmarshal(b, &p) != nil || p.PID <= 0 {
		return p, false
	}
	return p, true
}

func writePidFile(p pidFile) error {
	if err := os.MkdirAll(stateDir(), 0o755); err != nil {
		return err
	}
	b, _ := json.Marshal(p)
	return os.WriteFile(pidPath(), b, 0o644)
}

// serveOpts are the `serve` flags that start and autostart pass through.
type serveOpts struct {
	addr        string
	interval    time.Duration
	readOnly    bool
	limitsEvery time.Duration
}

func (o serveOpts) args(cfgPath string) []string {
	a := []string{"-config", cfgPath, "serve", "-addr", o.addr}
	if o.interval > 0 && o.interval != 10*time.Second {
		a = append(a, "-interval", o.interval.String())
	}
	if o.readOnly {
		a = append(a, "-read-only")
	}
	if o.limitsEvery != defaultLimitsInterval {
		a = append(a, "-limits-interval", o.limitsEvery.String())
	}
	return a
}

func parseServeOpts(name string, args []string) (serveOpts, *flag.FlagSet) {
	var o serveOpts
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	fs.StringVar(&o.addr, "addr", defaultAddr, "listen address")
	fs.DurationVar(&o.interval, "interval", 10*time.Second, "rescan interval")
	fs.BoolVar(&o.readOnly, "read-only", false, "disable managing accounts from the dashboard")
	fs.DurationVar(&o.limitsEvery, "limits-interval", defaultLimitsInterval, "how often to check plan limits (0 = off)")
	fs.Parse(args)
	return o, fs
}

// healthInfo is served at /api/health so start/stop/status can tell whether
// the process on an address really is ccmeter, and which pid it has.
type healthInfo struct {
	PID     int       `json:"pid"`
	Version string    `json:"version"`
	Addr    string    `json:"addr"`
	Config  string    `json:"config"`
	Started time.Time `json:"started"`
}

// probeAddr turns a listen address into one we can connect to.
func probeAddr(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}

func dashboardURL(addr string) string { return "http://" + probeAddr(addr) }

func probe(addr string) (healthInfo, error) {
	var h healthInfo
	c := http.Client{Timeout: 1500 * time.Millisecond}
	res, err := c.Get(dashboardURL(addr) + "/api/health")
	if err != nil {
		return h, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return h, fmt.Errorf("unexpected status %s", res.Status)
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<16)).Decode(&h); err != nil || h.PID == 0 {
		return h, errors.New("something else is listening there")
	}
	return h, nil
}

func portInUse(addr string) bool {
	c, err := net.DialTimeout("tcp", probeAddr(addr), time.Second)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

// executablePath returns the real path of this binary, for launching it later.
func executablePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	if strings.Contains(exe, "go-build") {
		return "", errors.New("this looks like a `go run` binary; build or install ccmeter first so the path stays valid")
	}
	return exe, nil
}

// ensureConfigFile makes sure a config file exists, since a background or
// startup process won't see the CLAUDE_CONFIG_DIR of your shell.
func ensureConfigFile(cfgPath string) error {
	if _, err := os.Stat(cfgPath); err == nil {
		return nil
	}
	if err := writeSampleConfig(cfgPath, false); err != nil {
		return err
	}
	fmt.Println("created", cfgPath, "with the accounts detected now (background processes don't see your shell's CLAUDE_CONFIG_DIR)")
	return nil
}

func absConfigPath(cfgPath string) string {
	if p, err := filepath.Abs(cfgPath); err == nil {
		return p
	}
	return cfgPath
}

// cmdStart: ccmeter start [-addr ADDR] [-interval D] [-read-only]
func cmdStart(cfgPath string, args []string, quiet bool) error {
	o, _ := parseServeOpts("start", args)
	if h, err := probe(o.addr); err == nil {
		if !quiet {
			fmt.Printf("ccmeter is already running at %s (pid %d)\n", dashboardURL(o.addr), h.PID)
		}
		return nil
	}
	if portInUse(o.addr) {
		return fmt.Errorf("%s is already in use by another program; pick another with -addr", o.addr)
	}
	exe, err := executablePath()
	if err != nil {
		return err
	}
	cfgPath = absConfigPath(cfgPath)
	if err := ensureConfigFile(cfgPath); err != nil {
		return err
	}
	if err := os.MkdirAll(stateDir(), 0o755); err != nil {
		return err
	}
	logf, err := os.OpenFile(logPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer logf.Close()
	fmt.Fprintf(logf, "\n--- %s ccmeter start\n", time.Now().Format(time.RFC3339))

	cmd := exec.Command(exe, o.args(cfgPath)...)
	cmd.Stdout, cmd.Stderr, cmd.Stdin = logf, logf, nil
	cmd.Dir = stateDir()
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	deadline := time.After(8 * time.Second)
	for {
		select {
		case err := <-exited:
			return fmt.Errorf("ccmeter exited during startup (%v); see %s:\n%s", err, logPath(), tail(logPath(), 8))
		case <-deadline:
			return fmt.Errorf("ccmeter didn't come up within 8s; see %s", logPath())
		case <-time.After(200 * time.Millisecond):
			if h, err := probe(o.addr); err == nil && h.PID == cmd.Process.Pid {
				_ = writePidFile(pidFile{PID: h.PID, Addr: o.addr, Started: time.Now()})
				_ = cmd.Process.Release()
				if !quiet {
					fmt.Printf("ccmeter running in the background at %s (pid %d)\nlog: %s\n", dashboardURL(o.addr), h.PID, logPath())
				}
				return nil
			}
		}
	}
}

// cmdStop stops the instance launched by `ccmeter start`. It only signals the
// recorded pid after the dashboard on the recorded address confirms that pid,
// so a stale pid file can never kill an unrelated process.
func cmdStop(quiet bool) error {
	p, ok := readPidFile()
	if !ok {
		if h, err := probe(defaultAddr); err == nil {
			return fmt.Errorf("ccmeter (pid %d) is running but wasn't started with `ccmeter start`; if autostart runs it, use `ccmeter autostart disable`", h.PID)
		}
		if !quiet {
			fmt.Println("ccmeter isn't running in the background")
		}
		return nil
	}
	h, err := probe(p.Addr)
	if err != nil || h.PID != p.PID {
		os.Remove(pidPath())
		if !quiet {
			fmt.Println("ccmeter isn't running (removed a stale pid file)")
		}
		return nil
	}
	proc, err := os.FindProcess(p.PID)
	if err != nil {
		return err
	}
	_ = terminate(proc)
	for i := 0; i < 25; i++ {
		time.Sleep(200 * time.Millisecond)
		if _, err := probe(p.Addr); err != nil {
			break
		}
		if i == 24 {
			_ = proc.Kill()
		}
	}
	os.Remove(pidPath())
	if !quiet {
		fmt.Printf("stopped ccmeter (pid %d)\n", p.PID)
	}
	return nil
}

// cmdStatus reports the background instance and the autostart setup.
func cmdStatus(args []string) {
	o, _ := parseServeOpts("status", args)
	addr := o.addr
	p, havePid := readPidFile()
	if havePid {
		addr = p.Addr
	}
	if h, err := probe(addr); err == nil {
		how := "started outside `ccmeter start` (autostart or a terminal)"
		if havePid && p.PID == h.PID {
			how = "started with `ccmeter start`"
		}
		fmt.Printf("running:   %s, pid %d, %s\n           %s\n", paint(styleGreen, "yes"), h.PID, how, dashboardURL(addr))
		fmt.Printf("config:    %s\n", h.Config)
		fmt.Printf("since:     %s\n", h.Started.Local().Format("2006-01-02 15:04"))
	} else {
		fmt.Printf("running:   %s (nothing answering at %s)\n", paint(styleYellow, "no"), dashboardURL(addr))
	}
	if havePid || fileExists(logPath()) {
		fmt.Printf("log:       %s\n", logPath())
	}
	fmt.Printf("autostart: %s\n", autostartStatus())
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func tail(path string, n int) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return "  " + strings.Join(lines, "\n  ")
}

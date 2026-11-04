package ccmeter

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Autostart launches the dashboard when you log in:
//   Linux:   a systemd user service (falls back to an XDG autostart entry
//            when there is no systemd user session)
//   macOS:   a launchd LaunchAgent
//   Windows: a hidden script in the Startup folder (no admin rights needed)

const (
	systemdUnitName = "ccmeter.service"
	launchdLabel    = "com.ccmeter.dashboard"
)

func xdgConfigHome() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return d
	}
	return filepath.Join(homeDir(), ".config")
}

func systemdUnitPath() string {
	return filepath.Join(xdgConfigHome(), "systemd", "user", systemdUnitName)
}
func desktopEntryPath() string { return filepath.Join(xdgConfigHome(), "autostart", "ccmeter.desktop") }
func launchAgentPath() string {
	return filepath.Join(homeDir(), "Library", "LaunchAgents", launchdLabel+".plist")
}
func startupScriptPath() string {
	appdata := os.Getenv("APPDATA")
	if appdata == "" {
		appdata = filepath.Join(homeDir(), "AppData", "Roaming")
	}
	return filepath.Join(appdata, "Microsoft", "Windows", "Start Menu", "Programs", "Startup", "ccmeter.vbs")
}

// withCommand swaps the "serve" subcommand in serve args for another one.
func withCommand(args []string, cmd string) []string {
	out := append([]string(nil), args...)
	for i, a := range out {
		if a == "serve" {
			out[i] = cmd
			break
		}
	}
	return out
}

func systemdUsable() bool {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return false
	}
	return exec.Command("systemctl", "--user", "show-environment").Run() == nil
}

func run(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("%s %s: %s", name, strings.Join(args, " "), msg)
	}
	return nil
}

func writeFileMkdir(path, content string, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), perm)
}

// waitUp waits for the dashboard to answer after a service manager started it.
func waitUp(addr string, d time.Duration) (healthInfo, error) {
	end := time.Now().Add(d)
	for {
		h, err := probe(addr)
		if err == nil || time.Now().After(end) {
			return h, err
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// cmdAutostart: ccmeter autostart enable|disable|status [serve flags]
func cmdAutostart(cfgPath string, args []string) error {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: ccmeter autostart enable [-addr ADDR] [-interval D] [-read-only] | disable | status")
		os.Exit(2)
	}
	switch args[0] {
	case "enable", "on", "install":
		o, _ := parseServeOpts("autostart enable", args[1:])
		return autostartEnable(cfgPath, o)
	case "disable", "off", "uninstall":
		return autostartDisable()
	case "status":
		fmt.Println("autostart:", autostartStatus())
		return nil
	}
	return fmt.Errorf("unknown autostart action %q (use enable, disable or status)", args[0])
}

func autostartEnable(cfgPath string, o serveOpts) error {
	exe, err := executablePath()
	if err != nil {
		return err
	}
	cfgPath = absConfigPath(cfgPath)
	if err := ensureConfigFile(cfgPath); err != nil {
		return err
	}
	// Hand over from an instance started with `ccmeter start`, so the port is free.
	if p, ok := readPidFile(); ok {
		if h, err := probe(p.Addr); err == nil && h.PID == p.PID {
			_ = cmdStop(true)
			fmt.Println("stopped the `ccmeter start` instance; autostart takes over")
		}
	}
	args := o.args(cfgPath)

	switch runtime.GOOS {
	case "linux":
		if systemdUsable() {
			if err := writeFileMkdir(systemdUnitPath(), renderSystemdUnit(exe, args), 0o644); err != nil {
				return err
			}
			for _, a := range [][]string{{"daemon-reload"}, {"enable", systemdUnitName}, {"restart", systemdUnitName}} {
				if err := run("systemctl", append([]string{"--user"}, a...)...); err != nil {
					return err
				}
			}
			fmt.Println("installed systemd user service:", systemdUnitPath())
			fmt.Println("logs: journalctl --user -u ccmeter -f")
			fmt.Printf("tip: to start at boot even before you log in, run: loginctl enable-linger %s\n", os.Getenv("USER"))
		} else {
			if !portFree(o.addr) {
				return fmt.Errorf("%s is already in use; stop that program or pick another -addr", o.addr)
			}
			if err := writeFileMkdir(desktopEntryPath(), renderDesktopEntry(exe, withCommand(args, "start")), 0o644); err != nil {
				return err
			}
			fmt.Println("no systemd user session found; installed a desktop autostart entry:", desktopEntryPath())
			fmt.Println("it runs when your desktop session starts")
			return cmdStart(cfgPath, startFlags(o), false)
		}
	case "darwin":
		path := launchAgentPath()
		if err := writeFileMkdir(path, renderLaunchdPlist(exe, args, logPath()), 0o644); err != nil {
			return err
		}
		if err := os.MkdirAll(stateDir(), 0o755); err != nil {
			return err
		}
		domain := fmt.Sprintf("gui/%d", os.Getuid())
		_ = run("launchctl", "bootout", domain+"/"+launchdLabel) // reload if already present
		if err := run("launchctl", "bootstrap", domain, path); err != nil {
			if err2 := run("launchctl", "load", "-w", path); err2 != nil {
				return err
			}
		}
		fmt.Println("installed LaunchAgent:", path)
		fmt.Println("log:", logPath())
	case "windows":
		if !portFree(o.addr) {
			return fmt.Errorf("%s is already in use; stop that program or pick another -addr", o.addr)
		}
		if err := writeFileMkdir(startupScriptPath(), renderStartupScript(exe, withCommand(args, "start")), 0o644); err != nil {
			return err
		}
		fmt.Println("installed startup script:", startupScriptPath())
		return cmdStart(cfgPath, startFlags(o), false)
	default:
		return fmt.Errorf("autostart isn't supported on %s; run `ccmeter start` from your own startup mechanism", runtime.GOOS)
	}

	h, err := waitUp(o.addr, 10*time.Second)
	if err != nil {
		return fmt.Errorf("autostart is installed, but the dashboard isn't answering at %s yet: %v", dashboardURL(o.addr), err)
	}
	fmt.Printf("ccmeter is running at %s (pid %d) and will start when you log in\n", dashboardURL(o.addr), h.PID)
	return nil
}

func portFree(addr string) bool {
	if _, err := probe(addr); err == nil {
		return true // our own dashboard; start will just report it
	}
	return !portInUse(addr)
}

func startFlags(o serveOpts) []string {
	a := []string{"-addr", o.addr}
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

func autostartDisable() error {
	removed := false
	switch runtime.GOOS {
	case "linux":
		if fileExists(systemdUnitPath()) {
			_ = run("systemctl", "--user", "disable", "--now", systemdUnitName)
			if err := os.Remove(systemdUnitPath()); err != nil {
				return err
			}
			_ = run("systemctl", "--user", "daemon-reload")
			fmt.Println("removed systemd user service")
			removed = true
		}
		if fileExists(desktopEntryPath()) {
			if err := os.Remove(desktopEntryPath()); err != nil {
				return err
			}
			_ = cmdStop(true)
			fmt.Println("removed desktop autostart entry and stopped ccmeter")
			removed = true
		}
	case "darwin":
		if fileExists(launchAgentPath()) {
			_ = run("launchctl", "bootout", fmt.Sprintf("gui/%d/%s", os.Getuid(), launchdLabel))
			if err := os.Remove(launchAgentPath()); err != nil {
				return err
			}
			fmt.Println("removed LaunchAgent and stopped ccmeter")
			removed = true
		}
	case "windows":
		if fileExists(startupScriptPath()) {
			if err := os.Remove(startupScriptPath()); err != nil {
				return err
			}
			_ = cmdStop(true)
			fmt.Println("removed startup script and stopped ccmeter")
			removed = true
		}
	}
	if !removed {
		fmt.Println("autostart isn't enabled")
	}
	return nil
}

func autostartStatus() string {
	switch runtime.GOOS {
	case "linux":
		if fileExists(systemdUnitPath()) {
			state := "installed"
			if out, err := exec.Command("systemctl", "--user", "is-active", systemdUnitName).Output(); err == nil || len(out) > 0 {
				state = strings.TrimSpace(string(out))
			}
			return fmt.Sprintf("enabled (systemd user service, %s): %s", state, systemdUnitPath())
		}
		if fileExists(desktopEntryPath()) {
			return "enabled (desktop autostart entry): " + desktopEntryPath()
		}
	case "darwin":
		if fileExists(launchAgentPath()) {
			return "enabled (LaunchAgent): " + launchAgentPath()
		}
	case "windows":
		if fileExists(startupScriptPath()) {
			return "enabled (Startup folder): " + startupScriptPath()
		}
	}
	return "disabled (enable with `ccmeter autostart enable`)"
}

// ---- file renderers (pure, so they can be tested on any OS) ---------------

func renderSystemdUnit(exe string, args []string) string {
	parts := []string{systemdQuote(exe)}
	for _, a := range args {
		parts = append(parts, systemdQuote(a))
	}
	return fmt.Sprintf(`[Unit]
Description=ccmeter - Claude Code usage dashboard
After=network.target

[Service]
ExecStart=%s
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
`, strings.Join(parts, " "))
}

func systemdQuote(s string) string {
	s = strings.ReplaceAll(s, "%", "%%")
	s = strings.ReplaceAll(s, "$", "$$")
	if s != "" && !strings.ContainsAny(s, " \t\"'\\;") {
		return s
	}
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

func renderDesktopEntry(exe string, args []string) string {
	parts := []string{desktopQuote(exe)}
	for _, a := range args {
		parts = append(parts, desktopQuote(a))
	}
	// Backslashes are escaped once more because Exec is a "string" value.
	execLine := strings.ReplaceAll(strings.Join(parts, " "), `\`, `\\`)
	return fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=ccmeter
Comment=Claude Code usage dashboard
Exec=%s
Terminal=false
NoDisplay=true
X-GNOME-Autostart-enabled=true
`, execLine)
}

func desktopQuote(s string) string {
	s = strings.ReplaceAll(s, "%", "%%")
	safe := s != ""
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("/._:=,+@-", r)) {
			safe = false
			break
		}
	}
	if safe {
		return s
	}
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		if strings.ContainsRune("\"`$\\", r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteByte('"')
	return b.String()
}

func renderLaunchdPlist(exe string, args []string, log string) string {
	esc := func(s string) string {
		var b bytes.Buffer
		_ = xml.EscapeText(&b, []byte(s))
		return b.String()
	}
	var progArgs strings.Builder
	for _, a := range append([]string{exe}, args...) {
		progArgs.WriteString("    <string>" + esc(a) + "</string>\n")
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>%s</string>
  <key>ProgramArguments</key>
  <array>
%s  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <dict>
    <key>SuccessfulExit</key>
    <false/>
  </dict>
  <key>ProcessType</key>
  <string>Background</string>
  <key>StandardOutPath</key>
  <string>%s</string>
  <key>StandardErrorPath</key>
  <string>%s</string>
</dict>
</plist>
`, launchdLabel, progArgs.String(), esc(log), esc(log))
}

// renderStartupScript builds a VBScript that launches ccmeter with no window.
func renderStartupScript(exe string, args []string) string {
	parts := []string{winQuote(exe)}
	for _, a := range args {
		parts = append(parts, winQuote(a))
	}
	cmdline := strings.ReplaceAll(strings.Join(parts, " "), `"`, `""`)
	return "' ccmeter autostart: starts the Claude Code usage dashboard at logon, hidden.\r\n" +
		"' Remove with: ccmeter autostart disable\r\n" +
		"CreateObject(\"WScript.Shell\").Run \"" + cmdline + "\", 0, False\r\n"
}

// winQuote quotes one argument using the Windows command-line rules.
func winQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\"") {
		return s
	}
	var b strings.Builder
	b.WriteByte('"')
	slashes := 0
	for _, r := range s {
		if r == '\\' {
			slashes++
			continue
		}
		if r == '"' {
			b.WriteString(strings.Repeat(`\`, slashes*2+1))
		} else {
			b.WriteString(strings.Repeat(`\`, slashes))
		}
		slashes = 0
		b.WriteRune(r)
	}
	b.WriteString(strings.Repeat(`\`, slashes*2))
	b.WriteByte('"')
	return b.String()
}

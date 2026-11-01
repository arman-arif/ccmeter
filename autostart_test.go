package main

import (
	"encoding/xml"
	"strings"
	"testing"
)

var testArgs = []string{"-config", "/home/me/My Configs/ccmeter.json", "serve", "-addr", "127.0.0.1:7878"}

func TestWinQuote(t *testing.T) {
	cases := map[string]string{
		`C:\tools\ccmeter.exe`:         `C:\tools\ccmeter.exe`,
		`C:\Program Files\ccmeter.exe`: `"C:\Program Files\ccmeter.exe"`,
		`C:\My Dir\`:                   `"C:\My Dir\\"`,
		`say "hi"`:                     `"say \"hi\""`,
		``:                             `""`,
	}
	for in, want := range cases {
		if got := winQuote(in); got != want {
			t.Errorf("winQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSystemdUnit(t *testing.T) {
	u := renderSystemdUnit("/usr/local/bin/ccmeter", testArgs)
	want := `ExecStart=/usr/local/bin/ccmeter -config "/home/me/My Configs/ccmeter.json" serve -addr 127.0.0.1:7878`
	if !strings.Contains(u, want+"\n") {
		t.Fatalf("unit missing %q:\n%s", want, u)
	}
	if systemdQuote("50%") != "50%%" {
		t.Errorf("percent not escaped: %s", systemdQuote("50%"))
	}
}

func TestDesktopEntry(t *testing.T) {
	d := renderDesktopEntry("/opt/cc meter/ccmeter", withCommand(testArgs, "start"))
	want := `Exec="/opt/cc meter/ccmeter" -config "/home/me/My Configs/ccmeter.json" start -addr 127.0.0.1:7878`
	if !strings.Contains(d, want+"\n") {
		t.Fatalf("desktop entry missing %q:\n%s", want, d)
	}
}

func TestLaunchdPlistIsValidXML(t *testing.T) {
	p := renderLaunchdPlist("/Users/a&b/bin/ccmeter", testArgs, "/Users/a&b/Library/Caches/ccmeter/ccmeter.log")
	dec := xml.NewDecoder(strings.NewReader(p))
	dec.Strict = true
	var strs []string
	for {
		tok, err := dec.Token()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			t.Fatalf("invalid XML: %v\n%s", err, p)
		}
		if cd, ok := tok.(xml.CharData); ok && strings.TrimSpace(string(cd)) != "" {
			strs = append(strs, string(cd))
		}
	}
	joined := strings.Join(strs, "|")
	if !strings.Contains(joined, "/Users/a&b/bin/ccmeter|-config|/home/me/My Configs/ccmeter.json|serve") {
		t.Fatalf("program arguments not preserved: %s", joined)
	}
}

func TestStartupScript(t *testing.T) {
	s := renderStartupScript(`C:\Program Files\ccmeter\ccmeter.exe`,
		withCommand([]string{"-config", `C:\Users\me\AppData\Roaming\ccmeter\config.json`, "serve", "-addr", "127.0.0.1:7878"}, "start"))
	want := `CreateObject("WScript.Shell").Run """C:\Program Files\ccmeter\ccmeter.exe"" -config C:\Users\me\AppData\Roaming\ccmeter\config.json start -addr 127.0.0.1:7878", 0, False`
	if !strings.Contains(s, want) {
		t.Fatalf("script missing %q:\n%s", want, s)
	}
}

func TestProbeAddr(t *testing.T) {
	for in, want := range map[string]string{
		"0.0.0.0:7878":   "127.0.0.1:7878",
		":9000":          "127.0.0.1:9000",
		"127.0.0.1:7878": "127.0.0.1:7878",
	} {
		if got := probeAddr(in); got != want {
			t.Errorf("probeAddr(%q) = %q, want %q", in, got, want)
		}
	}
}

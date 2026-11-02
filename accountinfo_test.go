package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const savedAccountJSON = `{
  "numStartups": 3,
  "primaryApiKey": "sk-should-never-be-read",
  "oauthAccount": {
    "accountUuid": "uuid-should-never-be-read",
    "organizationUuid": "org-uuid-should-never-be-read",
    "emailAddress": "sam@example.com",
    "displayName": "Sam",
    "fullName": "Sam Lee",
    "organizationName": "Example Org",
    "organizationType": "claude_max",
    "organizationRateLimitTier": "default_claude_max_5x",
    "organizationRole": "admin",
    "hasExtraUsageEnabled": true,
    "accountCreatedAt": "2025-07-15T17:13:55.290508Z",
    "subscriptionCreatedAt": "2026-07-23T19:09:47.239901Z",
    "profileFetchedAt": 1793594452443
  }
}`

func TestReadAccountInfo(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	if info := readAccountInfo(dir); info != nil {
		t.Errorf("no file should mean no info: %+v", info)
	}
	os.WriteFile(filepath.Join(dir, ".claude.json"), []byte(savedAccountJSON), 0o644)

	info := readAccountInfo(dir)
	if info == nil {
		t.Fatal("no info")
	}
	if info.Email != "sam@example.com" || info.Name != "Sam Lee" || info.Organization != "Example Org" ||
		info.Plan != "Max 5x" || info.Role != "admin" || !info.ExtraUsage {
		t.Errorf("fields: %+v", info)
	}
	if info.AccountCreated == nil || info.AccountCreated.Year() != 2025 || info.SubscriptionSince == nil ||
		info.SubscriptionSince.Year() != 2026 || info.ProfileUpdated == nil {
		t.Errorf("times: %+v", info)
	}

	// Nothing but the named fields can reach the dashboard.
	b, _ := json.Marshal(info)
	for _, secret := range []string{"never-be-read", "numStartups"} {
		if strings.Contains(string(b), secret) {
			t.Errorf("%q leaked: %s", secret, b)
		}
	}
}

func TestReadAccountInfoWithoutALogin(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, body := range []string{`{"numStartups": 3}`, `{"oauthAccount": null}`, `not json`, ``} {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, ".claude.json"), []byte(body), 0o644)
		if info := readAccountInfo(dir); info != nil {
			t.Errorf("%q: got %+v", body, info)
		}
	}
}

// The default account's file sits beside the folder, and a copy inside it may be
// left over from another login.
func TestReadAccountInfoForDefaultDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".claude")
	os.MkdirAll(dir, 0o755)
	profile := func(email string) []byte {
		return []byte(`{"oauthAccount":{"emailAddress":"` + email + `"}}`)
	}
	os.WriteFile(filepath.Join(dir, ".claude.json"), profile("inside@example.com"), 0o644)
	if info := readAccountInfo(dir); info == nil || info.Email != "inside@example.com" {
		t.Errorf("should fall back to the file inside: %+v", info)
	}
	os.WriteFile(filepath.Join(home, ".claude.json"), profile("beside@example.com"), 0o644)
	if info := readAccountInfo(dir); info == nil || info.Email != "beside@example.com" {
		t.Errorf("the file beside the folder should win: %+v", info)
	}
}

func TestPlanLabel(t *testing.T) {
	tests := []struct{ orgType, tier, want string }{
		{"claude_max", "default_claude_max_5x", "Max 5x"},
		{"claude_max", "default_claude_max_20x", "Max 20x"},
		{"claude_pro", "default_claude_ai", "Pro"},
		{"claude_team", "", "Team"},
		{"claude_enterprise", "", "Enterprise"},
		{"", "default_claude_max_5x", ""},
		{"", "", ""},
	}
	for _, tt := range tests {
		if got := planLabel(tt.orgType, tt.tier); got != tt.want {
			t.Errorf("planLabel(%q, %q) = %q, want %q", tt.orgType, tt.tier, got, tt.want)
		}
	}
}

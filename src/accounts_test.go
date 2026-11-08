package ccmeter

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSetArchived(t *testing.T) {
	cfg := Config{Accounts: []AccountConfig{
		{Name: "work", Dir: t.TempDir()},
		{Name: "personal", Dir: t.TempDir()},
	}}
	if _, err := setArchived(&cfg, "work", true); err != nil {
		t.Fatal(err)
	}
	if !cfg.Accounts[0].Archived || cfg.Accounts[1].Archived {
		t.Fatalf("archived by name: work %v, personal %v", cfg.Accounts[0].Archived, cfg.Accounts[1].Archived)
	}
	// A config dir works as the key too (the CLI and the dashboard both allow it).
	if _, err := setArchived(&cfg, cfg.Accounts[1].Dir, true); err != nil {
		t.Fatal(err)
	}
	if !cfg.Accounts[1].Archived {
		t.Error("archived by dir: personal is still active")
	}
	if _, err := setArchived(&cfg, "work", false); err != nil {
		t.Fatal(err)
	}
	if cfg.Accounts[0].Archived {
		t.Error("restoring left work archived")
	}
	if _, err := setArchived(&cfg, "nobody", true); err == nil {
		t.Error("archiving an unknown account succeeded")
	}
}

func TestUpsertAccountKeepsArchivedOnEdit(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{Accounts: []AccountConfig{{Name: "work", Dir: dir, Archived: true}}}
	replaced, err := upsertAccount(&cfg, AccountConfig{Name: "work-2", Dir: dir, WeeklyLimitUSD: 40}, "work", false)
	if err != nil || !replaced {
		t.Fatalf("upsert: replaced %v, err %v", replaced, err)
	}
	if cfg.Accounts[0].Name != "work-2" || cfg.Accounts[0].WeeklyLimitUSD != 40 {
		t.Fatalf("edit did not apply: %+v", cfg.Accounts[0])
	}
	if !cfg.Accounts[0].Archived {
		t.Error("editing an account restored it from the archive")
	}
}

func TestReportSeparatesArchivedAccounts(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	accts := []*Account{
		newAccount(AccountConfig{Name: "work", Dir: t.TempDir()}),
		newAccount(AccountConfig{Name: "old", Dir: t.TempDir(), Archived: true}),
	}
	r := buildReport(accts, newPricer(nil), time.Now())
	if len(r.Accounts) != 1 || r.Accounts[0].Name != "work" {
		t.Fatalf("accounts: %+v", r.Accounts)
	}
	if len(r.Archived) != 1 || r.Archived[0].Name != "old" || !r.Archived[0].Archived {
		t.Fatalf("archived: %+v", r.Archived)
	}
}

func TestArchivedSurvivesConfigRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := Config{Accounts: []AccountConfig{
		{Name: "work", Dir: t.TempDir()},
		{Name: "old", Dir: t.TempDir(), Archived: true},
	}}
	if err := saveConfig(path, cfg); err != nil {
		t.Fatal(err)
	}
	got, fromFile, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if !fromFile {
		t.Fatal("loadConfig did not read the saved file")
	}
	if got.Accounts[0].Archived || !got.Accounts[1].Archived {
		t.Fatalf("archived flags: %+v", got.Accounts)
	}
}

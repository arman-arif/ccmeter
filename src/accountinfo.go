package ccmeter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// AccountInfo is who an account is logged in as. It comes from the profile Claude
// Code saves in .claude.json, which holds no login tokens. Only the fields named
// here are ever read, so nothing else in that file can reach the dashboard.
type AccountInfo struct {
	Email             string     `json:"email,omitempty"`
	Name              string     `json:"name,omitempty"`
	Organization      string     `json:"organization,omitempty"`
	Plan              string     `json:"plan,omitempty"` // "Max 5x", "Pro"
	Role              string     `json:"role,omitempty"`
	ExtraUsage        bool       `json:"extra_usage"`
	AccountCreated    *time.Time `json:"account_created,omitempty"`
	SubscriptionSince *time.Time `json:"subscription_since,omitempty"`
	ProfileUpdated    *time.Time `json:"profile_updated,omitempty"`
}

type savedProfile struct {
	OAuthAccount *struct {
		Email             string  `json:"emailAddress"`
		FullName          string  `json:"fullName"`
		DisplayName       string  `json:"displayName"`
		Organization      string  `json:"organizationName"`
		OrganizationType  string  `json:"organizationType"`
		RateLimitTier     string  `json:"organizationRateLimitTier"`
		Role              string  `json:"organizationRole"`
		ExtraUsage        bool    `json:"hasExtraUsageEnabled"`
		AccountCreated    string  `json:"accountCreatedAt"`
		SubscriptionSince string  `json:"subscriptionCreatedAt"`
		ProfileFetchedMS  float64 `json:"profileFetchedAt"`
	} `json:"oauthAccount"`
}

// readAccountInfo returns nil when there's no saved login, as with an API key or
// before the first sign-in. The default ~/.claude account keeps its file beside
// the folder, at ~/.claude.json; any other keeps it inside.
func readAccountInfo(dir string) *AccountInfo {
	paths := []string{filepath.Join(expandHome(dir), ".claude.json")}
	if isDefaultConfigDir(dir) {
		paths = append([]string{filepath.Join(homeDir(), ".claude.json")}, paths...)
	}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var s savedProfile
		if json.Unmarshal(b, &s) != nil || s.OAuthAccount == nil {
			continue
		}
		o := s.OAuthAccount
		info := &AccountInfo{
			Email:             o.Email,
			Name:              o.FullName,
			Organization:      o.Organization,
			Plan:              planLabel(o.OrganizationType, o.RateLimitTier),
			Role:              o.Role,
			ExtraUsage:        o.ExtraUsage,
			AccountCreated:    parseProfileTime(o.AccountCreated),
			SubscriptionSince: parseProfileTime(o.SubscriptionSince),
		}
		if info.Name == "" {
			info.Name = o.DisplayName
		}
		if o.ProfileFetchedMS > 0 {
			t := time.UnixMilli(int64(o.ProfileFetchedMS))
			info.ProfileUpdated = &t
		}
		return info
	}
	return nil
}

func parseProfileTime(s string) *time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return nil
	}
	return &t
}

var planMultiplierRe = regexp.MustCompile(`_(\d+x)$`)

// planLabel turns claude_max and default_claude_max_5x into "Max 5x".
func planLabel(orgType, tier string) string {
	name := strings.TrimPrefix(strings.ToLower(orgType), "claude_")
	if name == "" {
		return ""
	}
	label := strings.ToUpper(name[:1]) + strings.ReplaceAll(name[1:], "_", " ")
	if m := planMultiplierRe.FindStringSubmatch(tier); m != nil {
		label += " " + m[1]
	}
	return label
}

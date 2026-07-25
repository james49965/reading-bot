package config

import (
	"strings"
	"testing"
)

// allKeys is every variable Load reads. Tests clear all of them so a
// developer who has sourced a real .env gets the same results as CI.
var allKeys = []string{
	"TELEGRAM_BOT_TOKEN",
	"TELEGRAM_ALLOWED_USER_ID",
	"GITHUB_TOKEN",
	"GITHUB_OWNER",
	"GITHUB_REPO",
	"GITHUB_BRANCH",
	"CONTENT_DIR",
	"DRY_RUN",
}

// setEnv clears every known key, then applies the given values.
func setEnv(t *testing.T, values map[string]string) {
	t.Helper()
	for _, k := range allKeys {
		t.Setenv(k, "")
	}
	for k, v := range values {
		t.Setenv(k, v)
	}
}

// validEnv is the minimum that should load cleanly.
func validEnv() map[string]string {
	return map[string]string{
		// Deliberately not shaped like real credentials, so secret
		// scanners have nothing to flag in this file.
		"TELEGRAM_BOT_TOKEN":       "telegram-token-placeholder",
		"TELEGRAM_ALLOWED_USER_ID": "42",
		"GITHUB_TOKEN":             "github-token-placeholder",
		"GITHUB_OWNER":             "example",
		"GITHUB_REPO":              "example-site",
	}
}

func TestLoadAppliesDefaults(t *testing.T) {
	setEnv(t, validEnv())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned an error for valid config: %v", err)
	}

	if cfg.GitHubBranch != "v4" {
		t.Errorf("GitHubBranch = %q, want %q", cfg.GitHubBranch, "v4")
	}
	if cfg.ContentDir != "content/reading" {
		t.Errorf("ContentDir = %q, want %q", cfg.ContentDir, "content/reading")
	}
	if !cfg.DryRun {
		t.Error("DryRun = false, want true: the default must never write to the live site")
	}
	if cfg.AllowedUserID != 42 {
		t.Errorf("AllowedUserID = %d, want 42", cfg.AllowedUserID)
	}
}

func TestLoadReportsEveryMissingVariableAtOnce(t *testing.T) {
	setEnv(t, nil)

	_, err := Load()
	if err == nil {
		t.Fatal("Load() succeeded with an empty environment, want an error")
	}

	// The point of the aggregated error: one restart, all the problems.
	for _, key := range []string{
		"TELEGRAM_BOT_TOKEN",
		"TELEGRAM_ALLOWED_USER_ID",
		"GITHUB_TOKEN",
		"GITHUB_OWNER",
		"GITHUB_REPO",
	} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("error does not mention missing %s:\n%v", key, err)
		}
	}
}

func TestLoadRejectsNonNumericUserID(t *testing.T) {
	env := validEnv()
	env["TELEGRAM_ALLOWED_USER_ID"] = "@jwalsh"
	setEnv(t, env)

	_, err := Load()
	if err == nil {
		t.Fatal("Load() accepted a @username as a user ID, want an error")
	}
	if !strings.Contains(err.Error(), "TELEGRAM_ALLOWED_USER_ID") {
		t.Errorf("error does not name the offending variable:\n%v", err)
	}
}

func TestLoadRejectsUnparseableDryRun(t *testing.T) {
	env := validEnv()
	env["DRY_RUN"] = "yes"
	setEnv(t, env)

	// "yes" silently becoming false would mean writing to the live site.
	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted DRY_RUN=yes, want an error")
	}
}

func TestLoadParsesDryRunOff(t *testing.T) {
	env := validEnv()
	env["DRY_RUN"] = "0"
	setEnv(t, env)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned an error: %v", err)
	}
	if cfg.DryRun {
		t.Error("DryRun = true, want false for DRY_RUN=0")
	}
}

func TestContentDirIsTrimmedOfSlashes(t *testing.T) {
	env := validEnv()
	env["CONTENT_DIR"] = "/content/reading/"
	setEnv(t, env)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned an error: %v", err)
	}
	// Leading or trailing slashes produce a double slash in GitHub API
	// paths, which 404s in a way that looks like a missing file.
	if cfg.ContentDir != "content/reading" {
		t.Errorf("ContentDir = %q, want %q", cfg.ContentDir, "content/reading")
	}
}

func TestRedactedHidesTokens(t *testing.T) {
	setEnv(t, validEnv())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned an error: %v", err)
	}

	got := cfg.Redacted()
	for _, secret := range []string{cfg.BotToken, cfg.GitHubToken} {
		if strings.Contains(got, secret) {
			t.Errorf("Redacted() leaked a token in full:\n%s", got)
		}
	}
	if !strings.Contains(got, "example/example-site") {
		t.Errorf("Redacted() should still identify the target repo:\n%s", got)
	}
}

func TestRedactShortValuesEntirely(t *testing.T) {
	if got := redact("short"); got != "<set>" {
		t.Errorf("redact(short value) = %q, want %q", got, "<set>")
	}
	if got := redact(""); got != "<empty>" {
		t.Errorf("redact(empty) = %q, want %q", got, "<empty>")
	}
}

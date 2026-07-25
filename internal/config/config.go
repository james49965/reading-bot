// Package config loads the bot's configuration from the environment.
//
// Everything is an environment variable, including the values that aren't
// secret. Nothing is read from a file in the repo, so there is no config file
// to accidentally commit, and the repo stays usable by anyone who clones it.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config is the fully-validated configuration. If Load returns no error,
// every field here is safe to use.
type Config struct {
	// Telegram
	BotToken      string
	AllowedUserID int64

	// GitHub: the repo holding the site's markdown.
	GitHubToken  string
	GitHubOwner  string
	GitHubRepo   string
	GitHubBranch string
	ContentDir   string

	// DryRun stops the bot from ever calling a GitHub write endpoint. It
	// replies with the file content it would have written instead.
	DryRun bool
}

// Load reads and validates the environment.
//
// It reports every problem at once rather than failing on the first one. A
// misconfigured deploy should tell you everything that's wrong in one go,
// at startup, rather than one variable per restart.
func Load() (*Config, error) {
	var l loader

	cfg := &Config{
		BotToken:      l.required("TELEGRAM_BOT_TOKEN"),
		AllowedUserID: l.requiredInt64("TELEGRAM_ALLOWED_USER_ID"),
		GitHubToken:   l.required("GITHUB_TOKEN"),
		GitHubOwner:   l.required("GITHUB_OWNER"),
		GitHubRepo:    l.required("GITHUB_REPO"),
		GitHubBranch:  l.withDefault("GITHUB_BRANCH", "v4"),
		ContentDir:    strings.Trim(l.withDefault("CONTENT_DIR", "content/reading"), "/"),
		DryRun:        l.boolWithDefault("DRY_RUN", true),
	}

	if err := l.err(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Redacted returns a one-line summary safe to write to logs. The tokens are
// the whole reason this method exists: log lines end up pasted into issues.
func (c *Config) Redacted() string {
	return fmt.Sprintf(
		"bot_token=%s allowed_user_id=%d github_token=%s repo=%s/%s branch=%s content_dir=%s dry_run=%t",
		redact(c.BotToken), c.AllowedUserID, redact(c.GitHubToken),
		c.GitHubOwner, c.GitHubRepo, c.GitHubBranch, c.ContentDir, c.DryRun,
	)
}

// redact keeps just enough to tell two tokens apart in a log without
// revealing anything useful. Short values are hidden entirely, since
// showing a prefix of a short secret gives away too much of it.
func redact(s string) string {
	if s == "" {
		return "<empty>"
	}
	if len(s) < 12 {
		return "<set>"
	}
	return s[:4] + "…" + fmt.Sprintf("(%d chars)", len(s))
}

// loader accumulates validation problems so Load can report them together.
type loader struct {
	problems []string
}

func (l *loader) required(key string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		l.problems = append(l.problems, key+" is required but not set")
	}
	return v
}

func (l *loader) requiredInt64(key string) int64 {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		l.problems = append(l.problems, key+" is required but not set")
		return 0
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		l.problems = append(l.problems, key+" must be a whole number (a numeric Telegram user ID, not a @username)")
		return 0
	}
	if n <= 0 {
		l.problems = append(l.problems, key+" must be a positive Telegram user ID")
		return 0
	}
	return n
}

func (l *loader) withDefault(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// boolWithDefault treats an unparseable value as a problem rather than as
// false. DRY_RUN=yes silently meaning "write to the live site" is exactly
// the sort of thing this whole package exists to prevent.
func (l *loader) boolWithDefault(key string, fallback bool) bool {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		l.problems = append(l.problems, key+" must be a boolean (1/0, true/false), got "+strconv.Quote(raw))
		return fallback
	}
	return v
}

func (l *loader) err() error {
	if len(l.problems) == 0 {
		return nil
	}
	return errors.New("invalid configuration:\n  - " + strings.Join(l.problems, "\n  - "))
}

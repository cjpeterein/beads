package main

import (
	"os"
	"testing"

	"github.com/spf13/cobra"
)

// newSessionTestCmd builds a cobra command carrying a --session flag set to the
// given value (empty string means "not provided"). A nil return models a command
// with no --session flag at all.
func newSessionTestCmd(flagValue string, hasFlag bool) *cobra.Command {
	cmd := &cobra.Command{Use: "test"}
	if hasFlag {
		cmd.Flags().String("session", "", "")
		if flagValue != "" {
			_ = cmd.Flags().Set("session", flagValue)
		}
	}
	return cmd
}

// TestGetSession tests the session resolution fallback chain.
// Priority: --session flag > BEADS_SESSION_ID env > CLAUDE_SESSION_ID env (deprecated).
func TestGetSession(t *testing.T) {
	origBeads, beadsSet := os.LookupEnv("BEADS_SESSION_ID")
	origClaude, claudeSet := os.LookupEnv("CLAUDE_SESSION_ID")
	defer func() {
		if beadsSet {
			os.Setenv("BEADS_SESSION_ID", origBeads)
		} else {
			os.Unsetenv("BEADS_SESSION_ID")
		}
		if claudeSet {
			os.Setenv("CLAUDE_SESSION_ID", origClaude)
		} else {
			os.Unsetenv("CLAUDE_SESSION_ID")
		}
	}()

	tests := []struct {
		name      string
		hasFlag   bool
		flagValue string
		beadsEnv  string
		claudeEnv string
		expected  string
	}{
		{
			name:      "flag takes priority over both env vars",
			hasFlag:   true,
			flagValue: "flag-sess",
			beadsEnv:  "beads-sess",
			claudeEnv: "claude-sess",
			expected:  "flag-sess",
		},
		{
			name:      "BEADS_SESSION_ID takes priority over CLAUDE_SESSION_ID when no flag",
			hasFlag:   true,
			flagValue: "",
			beadsEnv:  "beads-sess",
			claudeEnv: "claude-sess",
			expected:  "beads-sess",
		},
		{
			name:      "CLAUDE_SESSION_ID used as deprecated fallback",
			hasFlag:   true,
			flagValue: "",
			beadsEnv:  "",
			claudeEnv: "claude-sess",
			expected:  "claude-sess",
		},
		{
			name:      "empty when nothing set",
			hasFlag:   true,
			flagValue: "",
			beadsEnv:  "",
			claudeEnv: "",
			expected:  "",
		},
		{
			name:      "env vars resolve when command has no session flag",
			hasFlag:   false,
			flagValue: "",
			beadsEnv:  "beads-sess",
			claudeEnv: "claude-sess",
			expected:  "beads-sess",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.beadsEnv != "" {
				os.Setenv("BEADS_SESSION_ID", tt.beadsEnv)
			} else {
				os.Unsetenv("BEADS_SESSION_ID")
			}
			if tt.claudeEnv != "" {
				os.Setenv("CLAUDE_SESSION_ID", tt.claudeEnv)
			} else {
				os.Unsetenv("CLAUDE_SESSION_ID")
			}

			cmd := newSessionTestCmd(tt.flagValue, tt.hasFlag)
			result := getSession(cmd)
			if result != tt.expected {
				t.Errorf("getSession() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestGetSessionNilCmd verifies getSession resolves from the environment when
// passed a nil command (no flag context).
func TestGetSessionNilCmd(t *testing.T) {
	origBeads, beadsSet := os.LookupEnv("BEADS_SESSION_ID")
	origClaude, claudeSet := os.LookupEnv("CLAUDE_SESSION_ID")
	defer func() {
		if beadsSet {
			os.Setenv("BEADS_SESSION_ID", origBeads)
		} else {
			os.Unsetenv("BEADS_SESSION_ID")
		}
		if claudeSet {
			os.Setenv("CLAUDE_SESSION_ID", origClaude)
		} else {
			os.Unsetenv("CLAUDE_SESSION_ID")
		}
	}()

	os.Setenv("BEADS_SESSION_ID", "beads-nil-sess")
	os.Unsetenv("CLAUDE_SESSION_ID")
	if got := getSession(nil); got != "beads-nil-sess" {
		t.Errorf("getSession(nil) = %q, want %q", got, "beads-nil-sess")
	}

	os.Unsetenv("BEADS_SESSION_ID")
	os.Setenv("CLAUDE_SESSION_ID", "claude-nil-sess")
	if got := getSession(nil); got != "claude-nil-sess" {
		t.Errorf("getSession(nil) = %q, want %q", got, "claude-nil-sess")
	}
}

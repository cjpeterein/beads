package main

// Regression coverage for bd-3en: `bd init` must not silently overwrite
// user-curated agent-config files (AGENTS.md, CLAUDE.md,
// .claude/settings.json, project-root .gitignore) or auto-commit them when
// the workspace was previously initialized (`.beads/config.yaml` already
// exists with a valid backend).
//
// The setup mimics a fresh clone of an upstream project that gitignores
// `.beads/embeddeddolt/`: the curated `metadata.json` is tracked, the
// embedded Dolt directory is absent. Pre-fix, `bd init` here saw a clean
// slate and rewrote the agent files. Post-fix, it preserves them.

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	curatedAgentsMD     = "# My Project\n\nCurated agent instructions specific to this project.\n"
	curatedClaudeMD     = "# My Project: CLAUDE.md\n\nCustom Claude guidance unique to this repo.\n"
	curatedClaudeJSON   = "{\n  \"prompt\": \"Custom project prompt\"\n}\n"
	curatedGitignoreBd  = "# Project gitignore\n.beads/*\n!.beads/formulas/\n!.beads/formulas/*\n"
	curatedMetadataJSON = `{
  "version": "1",
  "database": "dolt",
  "backend": "dolt",
  "issue_prefix": "myapp",
  "dolt_database": "myapp",
  "dolt_mode": "embedded",
  "project_id": "00000000-0000-0000-0000-000000000abc"
}
`
)

// stageWorkspace lays out a "previously initialized fresh clone" workspace
// under tmpDir. Tracks the curated agent files in git so the test can
// detect post-init drift via git diff.
func stageWorkspace(t *testing.T, tmpDir string) {
	t.Helper()

	runGitForBootstrapTest(t, tmpDir, "init", "-b", "main")
	runGitForBootstrapTest(t, tmpDir, "config", "user.email", "test@test.com")
	runGitForBootstrapTest(t, tmpDir, "config", "user.name", "Test User")
	runGitForBootstrapTest(t, tmpDir, "config", "core.hooksPath", ".git/hooks")
	runGitForBootstrapTest(t, tmpDir, "config", "commit.gpgsign", "false")

	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatalf("mkdir .beads: %v", err)
	}
	if err := os.WriteFile(filepath.Join(beadsDir, "metadata.json"), []byte(curatedMetadataJSON), 0o644); err != nil {
		t.Fatalf("write metadata.json: %v", err)
	}

	claudeDir := filepath.Join(tmpDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}

	files := map[string]string{
		"AGENTS.md":             curatedAgentsMD,
		"CLAUDE.md":             curatedClaudeMD,
		".claude/settings.json": curatedClaudeJSON,
		".gitignore":            curatedGitignoreBd,
	}
	for rel, body := range files {
		path := filepath.Join(tmpDir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s parent: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	runGitForBootstrapTest(t, tmpDir, "add",
		"AGENTS.md",
		"CLAUDE.md",
		".claude/settings.json",
		".gitignore",
	)
	// metadata.json is gitignored by the curated .beads/* rule; force-add it
	// to mirror upstream projects that whitelist specific .beads/ files.
	runGitForBootstrapTest(t, tmpDir, "add", "-f", ".beads/metadata.json")
	runGitForBootstrapTest(t, tmpDir, "commit", "-m", "seed curated workspace")
}

// readFile is a helper that fails the test on read error.
func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// gitHEADCommit returns the SHA of HEAD.
func gitHEADCommit(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse HEAD: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// TestInit_PreviouslyInitialized_PreservesCuratedFiles is the bd-3en
// regression. With a curated `.beads/metadata.json` already in place, init
// must not modify AGENTS.md, CLAUDE.md, .claude/settings.json, or the
// project-root .gitignore.
func TestInit_PreviouslyInitialized_PreservesCuratedFiles(t *testing.T) {
	bd := buildBDForInitTests(t)
	tmpDir := t.TempDir()
	stageWorkspace(t, tmpDir)

	headBefore := gitHEADCommit(t, tmpDir)

	cmd := exec.Command(bd, "init", "--prefix", "myapp", "--quiet", "--non-interactive", "--skip-hooks")
	cmd.Dir = tmpDir
	cmd.Env = append(os.Environ(), "BD_NON_INTERACTIVE=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// init may fail if Dolt is unavailable; only the file-preservation
		// invariants are interesting here. Surface stderr for triage but
		// continue: the bug-3en concern is what was *written before* init
		// reached the database step, and our guard runs early.
		t.Logf("bd init exited non-zero (expected when Dolt unavailable): %v\n%s", err, stderr.String())
	}

	checks := map[string]string{
		"AGENTS.md":             curatedAgentsMD,
		"CLAUDE.md":             curatedClaudeMD,
		".claude/settings.json": curatedClaudeJSON,
		".gitignore":            curatedGitignoreBd,
	}
	for rel, want := range checks {
		got := readFile(t, filepath.Join(tmpDir, rel))
		if got != want {
			t.Errorf("%s was modified by `bd init` in a previously-initialized workspace.\n--- want ---\n%s\n--- got ---\n%s", rel, want, got)
		}
	}

	headAfter := gitHEADCommit(t, tmpDir)
	if headAfter != headBefore {
		t.Errorf("`bd init` auto-committed in a previously-initialized workspace (HEAD %s -> %s)", headBefore, headAfter)
	}
}

// TestInit_PreviouslyInitialized_ForceBypassesGuard verifies that `--force`
// restores full re-init behavior, including agent-file modifications. We
// only check that AT LEAST ONE of the curated files was touched — the
// exact merge logic is covered by other tests; here we just confirm the
// previously-initialized guard does not block --force.
func TestInit_PreviouslyInitialized_ForceBypassesGuard(t *testing.T) {
	bd := buildBDForInitTests(t)
	tmpDir := t.TempDir()
	stageWorkspace(t, tmpDir)

	cmd := exec.Command(bd, "init",
		"--prefix", "myapp",
		"--quiet",
		"--non-interactive",
		"--skip-hooks",
		"--reinit-local",
		"--destroy-token", "DESTROY-myapp",
	)
	cmd.Dir = tmpDir
	cmd.Env = append(os.Environ(), "BD_NON_INTERACTIVE=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// As above: tolerate Dolt unavailability. The guard bypass is what
		// we're checking.
		t.Logf("bd init --reinit-local exited non-zero (Dolt may be absent): %v\n%s", err, stderr.String())
	}

	// With --reinit-local, addAgentsInstructions runs and appends a BEADS
	// section. The exact rendering is covered elsewhere; here we just
	// confirm the curated file changed.
	if got := readFile(t, filepath.Join(tmpDir, "AGENTS.md")); got == curatedAgentsMD {
		t.Errorf("--reinit-local should bypass the previously-initialized guard and modify AGENTS.md, but it was unchanged")
	}
}

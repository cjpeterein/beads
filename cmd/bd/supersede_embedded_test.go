//go:build cgo

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
)

// bdSupersede runs "bd supersede" with the given args and returns raw stdout.
func bdSupersede(t *testing.T, bd, dir string, args ...string) string {
	t.Helper()
	fullArgs := append([]string{"supersede"}, args...)
	cmd := exec.Command(bd, fullArgs...)
	cmd.Dir = dir
	cmd.Env = bdEnv(dir)
	stdout, stderr, err := runCommandBuffers(t, cmd)
	if err != nil {
		t.Fatalf("bd supersede %s failed: %v\nstdout:\n%s\nstderr:\n%s", strings.Join(args, " "), err, stdout.String(), stderr.String())
	}
	return stdout.String()
}

// bdSupersedeFail runs "bd supersede" expecting failure.
func bdSupersedeFail(t *testing.T, bd, dir string, args ...string) string {
	t.Helper()
	fullArgs := append([]string{"supersede"}, args...)
	cmd := exec.Command(bd, fullArgs...)
	cmd.Dir = dir
	cmd.Env = bdEnv(dir)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected bd supersede %s to fail, but succeeded:\n%s", strings.Join(args, " "), out)
	}
	return string(out)
}

func TestEmbeddedSupersede(t *testing.T) {
	if os.Getenv("BEADS_TEST_EMBEDDED_DOLT") != "1" {
		t.Skip("set BEADS_TEST_EMBEDDED_DOLT=1 to run embedded dolt integration tests")
	}
	t.Parallel()

	bd := buildEmbeddedBD(t)
	dir, beadsDir, _ := bdInit(t, bd, "--prefix", "ss")

	// ===== Mark as superseded =====

	t.Run("mark_superseded", func(t *testing.T) {
		oldIssue := bdCreate(t, bd, dir, "Old spec v1", "--type", "task")
		newIssue := bdCreate(t, bd, dir, "New spec v2", "--type", "task")
		out := bdSupersede(t, bd, dir, oldIssue.ID, "--with", newIssue.ID)
		if !strings.Contains(out, "superseded") {
			t.Errorf("expected 'superseded' in output: %s", out)
		}
	})

	// ===== Verify closure =====

	t.Run("superseded_is_closed", func(t *testing.T) {
		oldIssue := bdCreate(t, bd, dir, "Closed old", "--type", "task")
		newIssue := bdCreate(t, bd, dir, "Closed new", "--type", "task")
		bdSupersede(t, bd, dir, oldIssue.ID, "--with", newIssue.ID)

		s := openStore(t, beadsDir, "ss")
		issue, err := s.GetIssue(t.Context(), oldIssue.ID)
		if err != nil {
			t.Fatalf("GetIssue: %v", err)
		}
		if issue.Status != "closed" {
			t.Errorf("expected status=closed, got %s", issue.Status)
		}
	})

	// ===== Creates supersedes link =====

	t.Run("creates_supersedes_link", func(t *testing.T) {
		oldIssue := bdCreate(t, bd, dir, "Link old", "--type", "task")
		newIssue := bdCreate(t, bd, dir, "Link new", "--type", "task")
		bdSupersede(t, bd, dir, oldIssue.ID, "--with", newIssue.ID)

		out := bdDep(t, bd, dir, "list", oldIssue.ID)
		if !strings.Contains(out, newIssue.ID) {
			t.Errorf("expected new issue in dep list: %s", out)
		}
	})

	// ===== JSON output =====

	t.Run("json_output", func(t *testing.T) {
		oldIssue := bdCreate(t, bd, dir, "JSON old", "--type", "task")
		newIssue := bdCreate(t, bd, dir, "JSON new", "--type", "task")
		fullArgs := []string{"supersede", oldIssue.ID, "--with", newIssue.ID, "--json"}
		cmd := exec.Command(bd, fullArgs...)
		cmd.Dir = dir
		cmd.Env = bdEnv(dir)
		stdout, stderr, err := runCommandBuffers(t, cmd)
		if err != nil {
			t.Fatalf("supersede --json failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
		}
		s := strings.TrimSpace(stdout.String())
		start := strings.Index(s, "{")
		if start < 0 {
			t.Fatalf("no JSON: %s", s)
		}
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(s[start:]), &m); err != nil {
			t.Fatalf("parse: %v", err)
		}
		if m["superseded"] != oldIssue.ID {
			t.Errorf("expected superseded=%s, got %v", oldIssue.ID, m["superseded"])
		}
		if m["replacement"] != newIssue.ID {
			t.Errorf("expected replacement=%s, got %v", newIssue.ID, m["replacement"])
		}
	})

	// ===== Session attribution =====

	t.Run("session_flag", func(t *testing.T) {
		oldIssue := bdCreate(t, bd, dir, "Session flag old", "--type", "task")
		newIssue := bdCreate(t, bd, dir, "Session flag new", "--type", "task")
		bdSupersede(t, bd, dir, oldIssue.ID, "--with", newIssue.ID, "--session", "ss-flag-sess")
		session := querySessionSQL(t, beadsDir, oldIssue.ID)
		if session != "ss-flag-sess" {
			t.Errorf("expected closed_by_session 'ss-flag-sess', got %q", session)
		}
	})

	t.Run("session_beads_env", func(t *testing.T) {
		oldIssue := bdCreate(t, bd, dir, "Session env old", "--type", "task")
		newIssue := bdCreate(t, bd, dir, "Session env new", "--type", "task")
		cmd := exec.Command(bd, "supersede", oldIssue.ID, "--with", newIssue.ID)
		cmd.Dir = dir
		env := bdEnv(dir)
		env = append(env, "BEADS_SESSION_ID=ss-env-sess")
		cmd.Env = env
		stdout, stderr, err := runCommandBuffers(t, cmd)
		if err != nil {
			t.Fatalf("bd supersede with BEADS_SESSION_ID failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
		}
		session := querySessionSQL(t, beadsDir, oldIssue.ID)
		if session != "ss-env-sess" {
			t.Errorf("expected closed_by_session 'ss-env-sess', got %q", session)
		}
	})

	// ===== Error: same ID =====

	t.Run("error_same_id", func(t *testing.T) {
		issue := bdCreate(t, bd, dir, "Same ID", "--type", "task")
		bdSupersedeFail(t, bd, dir, issue.ID, "--with", issue.ID)
	})

	// ===== Error: nonexistent replacement =====

	t.Run("error_nonexistent_replacement", func(t *testing.T) {
		issue := bdCreate(t, bd, dir, "No replacement", "--type", "task")
		bdSupersedeFail(t, bd, dir, issue.ID, "--with", "ss-nonexistent999")
	})
}

// TestEmbeddedSupersedeCyclicDoesNotCascadeClose reproduces bd-p50l: a cyclic
// supersede edge between A <-> B must not cause unrelated beads to close when
// a later, unrelated supersede runs. It also verifies that supersede records
// audit-visible close metadata (close_reason set) so the close is not silent.
func TestEmbeddedSupersedeCyclicDoesNotCascadeClose(t *testing.T) {
	if os.Getenv("BEADS_TEST_EMBEDDED_DOLT") != "1" {
		t.Skip("set BEADS_TEST_EMBEDDED_DOLT=1 to run embedded dolt integration tests")
	}
	t.Parallel()

	bd := buildEmbeddedBD(t)
	dir, beadsDir, _ := bdInit(t, bd, "--prefix", "ssc2")

	a := bdCreate(t, bd, dir, "Bead A (rpgjp analog)", "--type", "task")
	b := bdCreate(t, bd, dir, "Bead B (t1k66 analog)", "--type", "task")
	c := bdCreate(t, bd, dir, "Bead C (bgloz analog)", "--type", "task")
	d := bdCreate(t, bd, dir, "Bead D (v2m8x analog)", "--type", "task")

	// Step 1: supersede B -> A (B is replaced by A). Closes B.
	bdSupersede(t, bd, dir, b.ID, "--with", a.ID)

	// Step 2: supersede A -> B (A is replaced by B). Creates cyclic edge A<->B.
	// Closes A.
	bdSupersede(t, bd, dir, a.ID, "--with", b.ID)

	s := openStore(t, beadsDir, "ssc2")

	aBefore, err := s.GetIssue(t.Context(), a.ID)
	if err != nil {
		t.Fatalf("GetIssue %s: %v", a.ID, err)
	}
	if aBefore.Status != "closed" {
		t.Fatalf("precondition: expected A closed after self-supersede, got %s", aBefore.Status)
	}
	aClosedAt := aBefore.ClosedAt

	// Step 3: supersede an UNRELATED pair C -> D.
	bdSupersede(t, bd, dir, c.ID, "--with", d.ID)

	aAfter, err := s.GetIssue(t.Context(), a.ID)
	if err != nil {
		t.Fatalf("GetIssue %s after unrelated supersede: %v", a.ID, err)
	}
	if aAfter.ClosedAt == nil || aClosedAt == nil {
		t.Fatalf("expected A.closed_at populated before and after; got before=%v after=%v", aClosedAt, aAfter.ClosedAt)
	}
	if !aAfter.ClosedAt.Equal(*aClosedAt) {
		t.Errorf("A's closed_at changed after unrelated supersede: was %v, now %v (cascade close bug)", aClosedAt, aAfter.ClosedAt)
	}

	// Audit-trail check: A was closed by supersede; close_reason must
	// reference the replacement so the close is not silent (bd-p50l).
	if aAfter.CloseReason == "" {
		t.Errorf("A.close_reason is empty after supersede close (silent close bug)")
	}
	if !strings.Contains(aAfter.CloseReason, b.ID) {
		t.Errorf("A.close_reason should reference replacement %s, got %q", b.ID, aAfter.CloseReason)
	}

	// Also verify the unrelated supersede recorded its own close_reason
	// referencing D.
	cIssue, err := s.GetIssue(t.Context(), c.ID)
	if err != nil {
		t.Fatalf("GetIssue %s: %v", c.ID, err)
	}
	if cIssue.Status != "closed" {
		t.Errorf("expected C closed, got %s", cIssue.Status)
	}
	if !strings.Contains(cIssue.CloseReason, d.ID) {
		t.Errorf("C.close_reason should reference replacement %s, got %q", d.ID, cIssue.CloseReason)
	}
}

// TestEmbeddedSupersedeConcurrent exercises supersede operations concurrently.
func TestEmbeddedSupersedeConcurrent(t *testing.T) {
	if os.Getenv("BEADS_TEST_EMBEDDED_DOLT") != "1" {
		t.Skip("set BEADS_TEST_EMBEDDED_DOLT=1 to run embedded dolt integration tests")
	}
	t.Parallel()

	bd := buildEmbeddedBD(t)
	dir, _, _ := bdInit(t, bd, "--prefix", "ssc")

	newIssue := bdCreate(t, bd, dir, "Concurrent replacement", "--type", "task")
	var oldIDs []string
	for i := 0; i < 8; i++ {
		old := bdCreate(t, bd, dir, fmt.Sprintf("concurrent-old-%d", i), "--type", "task")
		oldIDs = append(oldIDs, old.ID)
	}

	const numWorkers = 8
	type workerResult struct {
		worker int
		err    error
	}
	results := make([]workerResult, numWorkers)
	var wg sync.WaitGroup
	wg.Add(numWorkers)

	for w := 0; w < numWorkers; w++ {
		go func(worker int) {
			defer wg.Done()
			r := workerResult{worker: worker}

			args := []string{"supersede", oldIDs[worker], "--with", newIssue.ID}
			cmd := exec.Command(bd, args...)
			cmd.Dir = dir
			cmd.Env = bdEnv(dir)
			out, err := cmd.CombinedOutput()
			if err != nil {
				r.err = fmt.Errorf("worker %d: %v\n%s", worker, err, out)
			}
			results[worker] = r
		}(w)
	}
	wg.Wait()

	for _, r := range results {
		if r.err != nil && !strings.Contains(r.err.Error(), "one writer at a time") {
			t.Errorf("worker %d failed: %v", r.worker, r.err)
		}
	}
}

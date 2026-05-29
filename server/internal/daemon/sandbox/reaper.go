package sandbox

import (
	"context"
	"strings"
	"time"
)

// Touch records sandbox use now; the launch seam calls this per task.
func (m *Manager) Touch(issueID string) { m.touch(issueID, time.Now()) }

// TaskStarted marks a task as in-flight on the issue. While the count is > 0 the
// reaper will not remove the issue's container, even if its last-used timestamp
// is older than the TTL — a long-running agent (longer than the idle TTL) must
// not have its container reaped out from under it.
func (m *Manager) TaskStarted(issueID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.active[issueID]++
}

// TaskFinished marks an in-flight task as done. It also refreshes the last-used
// timestamp so the idle TTL is measured from task completion, not dispatch.
func (m *Manager) TaskFinished(issueID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active[issueID] > 0 {
		m.active[issueID]--
	}
	if m.active[issueID] == 0 {
		delete(m.active, issueID)
	}
	m.lastUsed[issueID] = time.Now()
}

// buildKillByEnvScript returns a POSIX sh script that SIGTERMs every process in
// the container whose environment contains MULTICA_TASK_ID=<taskID>. We match on
// the environment (not the command line) because the task id is only ever in the
// agent's env, never its argv — so `pkill -f <taskID>` would match nothing.
func buildKillByEnvScript(taskID string) string {
	// taskID is single-quoted to keep it inert in the shell; embedded single
	// quotes are escaped the standard POSIX way ('\'').
	needle := "MULTICA_TASK_ID=" + taskID
	q := "'" + strings.ReplaceAll(needle, "'", `'\''`) + "'"
	return "for p in /proc/[0-9]*; do " +
		"if tr '\\0' '\\n' < \"$p/environ\" 2>/dev/null | grep -qxF " + q + "; then " +
		"kill -TERM \"$(basename \"$p\")\" 2>/dev/null || true; fi; done"
}

// KillTaskProcess terminates the in-container agent process for a task. It runs
// from the daemon (not the shim, which on cancellation receives an uncatchable
// SIGKILL), so it is the reliable cleanup path for the shared per-issue container.
// A no-op when the process already exited (normal completion).
func (m *Manager) KillTaskProcess(ctx context.Context, containerID, taskID string) error {
	if containerID == "" || taskID == "" {
		return nil
	}
	_, err := m.docker.RunInside(ctx, containerID, "sh", "-c", buildKillByEnvScript(taskID))
	return err
}

// Adopt seeds last-used timestamps for sandbox containers that already exist
// (e.g. survivors of a daemon restart, whose in-memory state was lost). Without
// this they are reused on demand but never reaped. Called once at startup.
func (m *Manager) Adopt(ctx context.Context) {
	issues, err := m.docker.ListIssues(ctx)
	if err != nil {
		return
	}
	now := time.Now()
	for _, issueID := range issues {
		m.touch(issueID, now)
	}
}

// ReapIdle removes containers whose issue has been idle longer than ttl.
// Issues with an in-flight task are never reaped. Returns the number removed.
func (m *Manager) ReapIdle(ctx context.Context, ttl time.Duration) (int, error) {
	m.mu.Lock()
	cutoff := time.Now().Add(-ttl)
	stale := make([]string, 0)
	for issueID, last := range m.lastUsed {
		if m.active[issueID] > 0 {
			continue // a task is running on this issue
		}
		if last.Before(cutoff) {
			stale = append(stale, issueID)
		}
	}
	m.mu.Unlock()

	removed := 0
	for _, issueID := range stale {
		ids, err := m.docker.Inspect(ctx, issueLabel+"="+issueID)
		if err != nil {
			return removed, err
		}
		for _, id := range ids {
			if err := m.docker.Remove(ctx, id); err != nil {
				return removed, err
			}
			removed++
		}
		m.mu.Lock()
		delete(m.lastUsed, issueID)
		delete(m.locks, issueID)
		m.mu.Unlock()
	}
	return removed, nil
}

// ReapLoop runs ReapIdle every interval until ctx is cancelled.
func (m *Manager) ReapLoop(ctx context.Context, interval, ttl time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_, _ = m.ReapIdle(ctx, ttl)
		}
	}
}

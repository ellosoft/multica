package sandbox

import (
	"context"
	"time"
)

// Touch records sandbox use now; the launch seam calls this per task.
func (m *Manager) Touch(issueID string) { m.touch(issueID, time.Now()) }

// ReapIdle removes containers whose issue has been idle longer than ttl.
// Returns the number of containers removed.
func (m *Manager) ReapIdle(ctx context.Context, ttl time.Duration) (int, error) {
	m.mu.Lock()
	cutoff := time.Now().Add(-ttl)
	stale := make([]string, 0)
	for issueID, last := range m.lastUsed {
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

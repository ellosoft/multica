package sandbox

import (
	"context"
	"fmt"
	"sync"
	"time"
)

const issueLabel = "multica.issue"

// Config is the resolved (project→global) sandbox configuration for an issue.
type Config struct {
	Enabled      bool
	Image        string
	SetupCommand string
	CPUs         string
	Memory       string
}

// Manager creates and reuses one container per issue.
type Manager struct {
	docker DockerClient

	mu       sync.Mutex
	locks    map[string]*sync.Mutex
	lastUsed map[string]time.Time
	active   map[string]int // count of in-flight tasks per issue; reaper skips issues with active > 0
}

// NewManager returns a Manager backed by the given DockerClient.
func NewManager(d DockerClient) *Manager {
	return &Manager{
		docker:   d,
		locks:    map[string]*sync.Mutex{},
		lastUsed: map[string]time.Time{},
		active:   map[string]int{},
	}
}

// issueLock returns the per-issue mutex, creating it if absent.
func (m *Manager) issueLock(issueID string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	lk := m.locks[issueID]
	if lk == nil {
		lk = &sync.Mutex{}
		m.locks[issueID] = lk
	}
	return lk
}

func containerName(issueID string) string { return "multica-sbx-" + issueID }

// EnsureSandbox returns the container ID for the issue, creating it (and running
// the setup command once) if absent. Returns "" when disabled so callers fall
// back to host execution.
func (m *Manager) EnsureSandbox(ctx context.Context, issueID string, cfg Config, mounts []Mount) (string, error) {
	if !cfg.Enabled {
		return "", nil
	}
	if cfg.Image == "" {
		return "", fmt.Errorf("sandbox enabled for issue %s but no image configured", issueID)
	}

	lk := m.issueLock(issueID)
	lk.Lock()
	defer lk.Unlock()

	label := issueLabel + "=" + issueID
	if ids, err := m.docker.Inspect(ctx, label); err != nil {
		return "", err
	} else if len(ids) > 0 {
		m.touch(issueID, time.Now())
		return ids[0], nil
	}

	// No running container. A stopped/exited leftover with the same name (e.g.
	// from a crash, or a daemon restart that lost its in-memory state) is invisible
	// to Inspect's running-only filter but still makes `docker run --name` fail with
	// a name conflict. Force-remove it by name first (no-op if absent).
	_ = m.docker.Remove(ctx, containerName(issueID))

	id, err := m.docker.Run(ctx, RunSpec{
		Name:       containerName(issueID),
		Image:      cfg.Image,
		Labels:     map[string]string{issueLabel: issueID},
		AddHosts:   []string{"host.docker.internal:host-gateway"},
		Mounts:     mounts,
		CPUs:       cfg.CPUs,
		Memory:     cfg.Memory,
		Entrypoint: []string{"sleep", "infinity"},
	})
	if err != nil {
		return "", err
	}

	if cfg.SetupCommand != "" {
		if _, err := m.docker.RunInside(ctx, id, "sh", "-lc", cfg.SetupCommand); err != nil {
			_ = m.docker.Remove(ctx, id)
			return "", fmt.Errorf("sandbox setup command failed for issue %s: %w", issueID, err)
		}
	}

	m.touch(issueID, time.Now())
	return id, nil
}

// touch records the last-used time for an issue (used by Task 3's reaper).
func (m *Manager) touch(issueID string, t time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastUsed[issueID] = t
}

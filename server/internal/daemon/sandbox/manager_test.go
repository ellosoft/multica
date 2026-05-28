package sandbox

import (
	"context"
	"os/exec"
	"sync"
	"testing"
)

type fakeDocker struct {
	mu       sync.Mutex
	byLabel  map[string][]string
	runCalls int
	setup    []string
}

func newFakeDocker() *fakeDocker { return &fakeDocker{byLabel: map[string][]string{}} }

func (f *fakeDocker) Run(_ context.Context, spec RunSpec) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runCalls++
	id := "cid-" + spec.Name
	for k, v := range spec.Labels {
		f.byLabel[k+"="+v] = append(f.byLabel[k+"="+v], id)
	}
	return id, nil
}
func (f *fakeDocker) ExecCmd(ctx context.Context, _ ExecSpec) *exec.Cmd { return exec.CommandContext(ctx, "true") }
func (f *fakeDocker) RunInside(_ context.Context, _ string, argv ...string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setup = argv
	return "", nil
}
func (f *fakeDocker) Inspect(_ context.Context, label string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.byLabel[label], nil
}
func (f *fakeDocker) Remove(_ context.Context, _ string) error { return nil }

func cfg() Config { return Config{Enabled: true, Image: "img:latest", SetupCommand: "echo hi"} }

func TestEnsureSandbox_CreatesOnceAndReuses(t *testing.T) {
	fd := newFakeDocker()
	m := NewManager(fd)
	id1, err := m.EnsureSandbox(context.Background(), "ISSUE1", cfg(), nil)
	if err != nil {
		t.Fatal(err)
	}
	id2, err := m.EnsureSandbox(context.Background(), "ISSUE1", cfg(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if id1 != id2 {
		t.Fatalf("expected reuse, got %q then %q", id1, id2)
	}
	if fd.runCalls != 1 {
		t.Fatalf("expected 1 create, got %d", fd.runCalls)
	}
	if len(fd.setup) == 0 {
		t.Fatal("setup command not run")
	}
}

func TestEnsureSandbox_ConcurrentFirstTasksCreateOnce(t *testing.T) {
	fd := newFakeDocker()
	m := NewManager(fd)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = m.EnsureSandbox(context.Background(), "ISSUE1", cfg(), nil) }()
	}
	wg.Wait()
	if fd.runCalls != 1 {
		t.Fatalf("expected exactly 1 create under concurrency, got %d", fd.runCalls)
	}
}

func TestEnsureSandbox_DisabledReturnsEmpty(t *testing.T) {
	m := NewManager(newFakeDocker())
	id, err := m.EnsureSandbox(context.Background(), "ISSUE1", Config{Enabled: false}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if id != "" {
		t.Fatalf("disabled should yield empty id, got %q", id)
	}
}

func TestEnsureSandbox_SetupFailureRemovesContainer(t *testing.T) {
	fd := &removeTrackingDocker{fakeDocker: newFakeDocker(), failSetup: true}
	m := NewManager(fd)
	_, err := m.EnsureSandbox(context.Background(), "ISSUE1", cfg(), nil)
	if err == nil {
		t.Fatal("expected setup failure error")
	}
	if !fd.removed {
		t.Fatal("expected the half-provisioned container to be removed on setup failure")
	}
}

type removeTrackingDocker struct {
	*fakeDocker
	failSetup bool
	removed   bool
}

func (d *removeTrackingDocker) RunInside(ctx context.Context, id string, argv ...string) (string, error) {
	if d.failSetup {
		return "", context.DeadlineExceeded
	}
	return d.fakeDocker.RunInside(ctx, id, argv...)
}
func (d *removeTrackingDocker) Remove(ctx context.Context, id string) error {
	d.removed = true
	return d.fakeDocker.Remove(ctx, id)
}

package sandbox

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

type riCall struct {
	cid  string
	argv []string
}

type fakeDocker struct {
	mu        sync.Mutex
	byLabel   map[string][]string
	runCalls  int
	setup     []string
	namesUsed map[string]bool // container names that already exist (block `docker run --name`)
	removed   []string        // names/ids passed to Remove, in order
	riCalls   []riCall        // every RunInside invocation
	issues    []string        // issue ids returned by ListIssues
}

func newFakeDocker() *fakeDocker {
	return &fakeDocker{byLabel: map[string][]string{}, namesUsed: map[string]bool{}}
}

// nameInUse marks a container name as already present (e.g. an exited leftover).
func (f *fakeDocker) nameInUse(name string) { f.namesUsed[name] = true }

// seedIssue pretends a container for issueID already exists (survived a restart).
func (f *fakeDocker) seedIssue(issueID, cid string) {
	f.byLabel[issueLabel+"="+issueID] = append(f.byLabel[issueLabel+"="+issueID], cid)
	f.issues = append(f.issues, issueID)
}

func (f *fakeDocker) removedName(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.removed {
		if r == name {
			return true
		}
	}
	return false
}

func (f *fakeDocker) lastRunInsideOn(cid string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.riCalls) - 1; i >= 0; i-- {
		if f.riCalls[i].cid == cid {
			return f.riCalls[i].argv
		}
	}
	return nil
}

func (f *fakeDocker) Run(_ context.Context, spec RunSpec) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.namesUsed[spec.Name] {
		return "", fmt.Errorf("docker run: name %q already in use", spec.Name)
	}
	f.runCalls++
	id := "cid-" + spec.Name
	for k, v := range spec.Labels {
		f.byLabel[k+"="+v] = append(f.byLabel[k+"="+v], id)
	}
	return id, nil
}
func (f *fakeDocker) ExecCmd(ctx context.Context, _ ExecSpec) *exec.Cmd {
	return exec.CommandContext(ctx, "true")
}
func (f *fakeDocker) RunInside(_ context.Context, cid string, argv ...string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setup = argv
	f.riCalls = append(f.riCalls, riCall{cid: cid, argv: argv})
	return "", nil
}
func (f *fakeDocker) Inspect(_ context.Context, label string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.byLabel[label], nil
}
func (f *fakeDocker) ListIssues(_ context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.issues...), nil
}
func (f *fakeDocker) Remove(_ context.Context, idOrName string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removed = append(f.removed, idOrName)
	delete(f.namesUsed, idOrName)
	return nil
}

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

func TestEnsureSandbox_RemovesStaleNameBeforeCreate(t *testing.T) {
	fd := newFakeDocker()
	// An exited leftover container with the same name blocks `docker run --name`;
	// Inspect (running-only) doesn't see it, so EnsureSandbox must clear it.
	fd.nameInUse(containerName("ISSUE1"))
	m := NewManager(fd)
	id, err := m.EnsureSandbox(context.Background(), "ISSUE1", cfg(), nil)
	if err != nil {
		t.Fatalf("should recover from a stale same-name container: %v", err)
	}
	if id == "" {
		t.Fatal("expected a container id after recovery")
	}
	if !fd.removedName(containerName("ISSUE1")) {
		t.Fatal("expected the stale container to be force-removed by name before create")
	}
}

func TestEnsureSandbox_TaskTrackingBlocksReaper(t *testing.T) {
	fd := newFakeDocker()
	m := NewManager(fd)
	if _, err := m.EnsureSandbox(context.Background(), "ISSUE1", cfg(), nil); err != nil {
		t.Fatal(err)
	}
	m.touch("ISSUE1", time.Now().Add(-2*time.Hour)) // long idle by timestamp
	m.TaskStarted("ISSUE1")                         // ...but a task is actively running

	removed, err := m.ReapIdle(context.Background(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 0 {
		t.Fatalf("must not reap a container with an in-flight task, removed %d", removed)
	}

	// Finishing refreshes the idle clock (idle is measured from completion, not
	// dispatch), so the container is not immediately stale...
	m.TaskFinished("ISSUE1")
	if removed, err := m.ReapIdle(context.Background(), time.Hour); err != nil || removed != 0 {
		t.Fatalf("just-finished container must not be reaped, removed %d (err %v)", removed, err)
	}
	// ...but once it has been idle past the TTL with no task, it is reaped.
	m.touch("ISSUE1", time.Now().Add(-2*time.Hour))
	removed, err = m.ReapIdle(context.Background(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("idle container should be reaped after the TTL, removed %d", removed)
	}
}

func TestKillTaskProcess_KillsByTaskEnvInContainer(t *testing.T) {
	fd := newFakeDocker()
	m := NewManager(fd)
	if err := m.KillTaskProcess(context.Background(), "cid1", "task-123"); err != nil {
		t.Fatal(err)
	}
	call := fd.lastRunInsideOn("cid1")
	if len(call) < 2 || call[0] != "sh" || call[1] != "-c" {
		t.Fatalf("expected `sh -c <script>` invocation, got %v", call)
	}
	script := strings.Join(call, " ")
	// Must identify the process by its environment (MULTICA_TASK_ID), not by
	// command line — `pkill -f` would match argv, which never contains the id.
	if !strings.Contains(script, "environ") || !strings.Contains(script, "task-123") {
		t.Fatalf("kill must scan /proc environ for the task id, got: %s", script)
	}
}

func TestAdopt_SeedsLastUsedSoReaperGovernsSurvivors(t *testing.T) {
	fd := newFakeDocker()
	fd.seedIssue("ISSUE1", "cid-existing") // container survived a daemon restart
	m := NewManager(fd)

	// Before adoption the reaper has no record of the survivor.
	if n, _ := m.ReapIdle(context.Background(), -time.Second); n != 0 {
		t.Fatalf("survivor must be unknown before Adopt, reaped %d", n)
	}

	m.Adopt(context.Background())

	// After adoption it is governed by the reaper (ttl < 0 => everything stale).
	if n, _ := m.ReapIdle(context.Background(), -time.Second); n != 1 {
		t.Fatalf("adopted survivor should be reapable, reaped %d", n)
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

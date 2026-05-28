# Ellosoft Patches

Tracks insertion points for Ellosoft-specific changes in the Multica fork.

## server/cmd/server/router.go

- Inside the `/api/daemon` route group (`r.Route("/api/daemon", ...)`), after the last existing route line (`r.Post("/tasks/{taskId}/session", ...)`), added:
  ```go
  r.Get("/projects/{projectId}/sandbox-config", h.GetProjectSandboxConfig)
  ```
  This route is protected by `middleware.DaemonAuth` (the daemon's existing Bearer token).

## server/internal/daemon/sandbox/manager.go

- In `EnsureSandbox`, in the `RunSpec{...}` passed to `m.docker.Run`, added `AddHosts: []string{"host.docker.internal:host-gateway"}` so the in-container agent can reach the host daemon over the docker default bridge. Network left empty.

## server/internal/daemon/daemon.go (per-issue sandbox launch, flag-guarded)

- Imports: added `"os/exec"` and `"github.com/multica-ai/multica/server/internal/daemon/sandbox"`.
- `type Daemon struct`: added two fields after `runUpdateFn`:
  ```go
  sandboxMgr     *sandbox.Manager // nil when docker is unavailable; gates sandbox mode
  sandboxIdleTTL time.Duration    // reap idle issue containers after this
  ```
- `New()`: after `d.runUpdateFn = d.runUpdate`, added a docker-detect block that constructs `d.sandboxMgr = sandbox.NewManager(sandbox.NewDockerClient())` + `d.sandboxIdleTTL = 4h` only when `exec.LookPath("docker")` succeeds. When docker is absent, `sandboxMgr` stays nil (host execution).
- Added top-level helper `func shellQuote(s string) string` (POSIX single-quote escaping) immediately before `runTask` (no pre-existing helper).
- `runTask()`: immediately BEFORE the `backend, err := agent.New(...)` call, inserted the sandbox-redirect block. It computes `execPathForAgent` (default `entry.Path`), and when `d.sandboxMgr != nil && task.ProjectID != ""` and the server's `GetProjectSandboxConfig` returns an enabled config, it calls `EnsureSandbox(ctx, task.IssueID, ...)` (mounting WorkspacesRoot, the agent bin dir, the multica self-bin dir, and TempDir), writes an `agent-shim.sh` wrapper into `filepath.Dir(env.WorkDir)` (the env root, outside the git worktree) that execs `multica __sandbox-exec`, and sets `execPathForAgent = shimPath`. Config-fetch errors fall back to host execution (warn-only); ensure/write errors return `TaskResult{}, fmt.Errorf(...)`. The only change to the `agent.New` call is `ExecutablePath: execPathForAgent`.
- `Run()`: next to the other `go d.…Loop(ctx)` launches (after `go d.serveHealth(...)`), added a guarded `go d.sandboxMgr.ReapLoop(ctx, 10*time.Minute, d.sandboxIdleTTL)` started only when `d.sandboxMgr != nil`.

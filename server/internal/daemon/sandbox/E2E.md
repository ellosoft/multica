# Per-Issue Sandbox — Validation Guide

How to validate the per-issue Docker sandbox feature (Ellosoft fork). Two levels:
**A** needs only `go` + `docker`; **B** needs a live server + daemon + Docker.

Design: `projects/docs/specs/2026-05-28-multica-per-issue-sandbox-design.md`.

---

## A. Mechanism smoke (no server/daemon)

```bash
bash scripts/sandbox-smoke.sh
```

This builds `multica`, runs the `sandbox` package + shim unit tests, then spins up a
throwaway `alpine` container and proves `multica __sandbox-exec` routes a command
**into** the container at the **mirrored working directory**, reading a host-mounted
file. Expected last line: `PASS: shim executed the command inside the container ...`.

> **macOS Docker Desktop caveat:** the script uses a `$HOME`-based temp dir, not
> `$TMPDIR` (`/var/folders/...` is not a Docker-shared path on macOS, so a bind mount
> of it is empty in the container). The real feature has the same requirement: on a Mac
> dev box, `WorkspacesRoot` (default `~/multica_workspaces`) and the host bin dirs must
> be inside Docker Desktop's shared file paths. On the Linux self-host, any path works.

---

## B. Full end-to-end (live stack)

### Prerequisites
- Multica **server** with migration **111** applied (runs on startup) and a **daemon**
  on a host with Docker.
- A base **`sandbox_image`** that provides the agent's **runtime dependencies** (e.g.
  `node` for `claude`). The agent binary *and* the `multica` CLI are **not** baked into
  the image — they are mirror-mounted read-only from the host at identical absolute
  paths, along with `WorkspacesRoot` and the OS temp dir. So the image needs the
  interpreter/libraries the agent needs, plus whatever `setup_command` installs.
- macOS dev: ensure `WorkspacesRoot` + host bin dirs are within Docker Desktop's shared
  paths (see caveat above). Linux self-host: N/A.

### 1. Enable the sandbox for a project
With a daemon/admin token:
```bash
curl -fsS -X PUT "$SERVER/api/projects/$PROJECT_ID/sandbox-config" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"enabled":true,"image":"YOUR_BASE_IMAGE","setup_command":"echo provisioning && <install deps>","cpus":"2","memory":"4g"}'
```
Or set a global default for all projects:
```bash
curl -fsS -X PUT "$SERVER/api/sandbox-config/global" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"enabled":true,"image":"YOUR_BASE_IMAGE","setup_command":"...","cpus":"2","memory":"4g"}'
```
Confirm the daemon resolves it:
```bash
curl -fsS "$SERVER/api/daemon/projects/$PROJECT_ID/sandbox-config" -H "Authorization: Bearer $TOKEN"
# => {"enabled":true,"image":"...","setup_command":"...","cpus":"2","memory":"4g"}
```

### 2. Dispatch a task
Trigger a task on an **issue** in that project through the normal Multica flow.

### 3. Observe (this is the assertion)
- **Daemon log** shows: `sandbox: routing agent into container` with the issue id +
  container id (and, at startup, `sandbox: docker detected ...`).
- **Container exists**, keyed by issue:
  ```bash
  docker ps --filter "label=multica.issue=$ISSUE_ID"
  # one container named multica-sbx-<issueID>
  ```
- **Setup ran once** — verify whatever `setup_command` did (e.g. a marker, an installed tool):
  ```bash
  docker exec multica-sbx-$ISSUE_ID sh -lc 'which <tool-you-installed>'
  ```
- **Agent ran inside** — during the task: `docker exec multica-sbx-$ISSUE_ID ps` shows the
  agent process; the task progresses and produces output as normal.
- **Shared across the issue** — dispatch a *second* task on the **same** issue and confirm
  `docker ps` still shows **one** container (reused, setup not re-run).

### 4. Reaping
The reaper removes a container after it has been idle past the TTL (default **4h**,
currently a constant in `daemon.New`). To observe quickly, drop the TTL (temporarily lower
the constant or add an env override), idle the issue, and confirm the container disappears:
```bash
docker ps --filter "label=multica.issue=$ISSUE_ID"   # empty after the TTL
```

### 5. Flag-off regression
```bash
curl -fsS -X PUT "$SERVER/api/projects/$PROJECT_ID/sandbox-config" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{"enabled":false}'
```
Dispatch a task → it runs **on the host** exactly as before, and **no** container is created.

---

## Troubleshooting
- **Agent can't reach the daemon:** the sandbox runs with `--add-host
  host.docker.internal:host-gateway` and the shim rewrites loopback daemon URLs to
  `host.docker.internal`. Ensure the daemon's local port is reachable from containers.
  `MULTICA_SERVER_URL` is left as-is (it must be a network-reachable address).
- **`exec: <agent>: not found` in the container:** the agent binary is mirror-mounted, but
  its runtime deps aren't — make sure the base image (or `setup_command`) provides them.
- **Empty mounts / files missing inside the container (macOS):** the shared-path caveat above.
- **No container created though enabled:** check the daemon host actually has `docker` on
  PATH (the manager is nil otherwise → silent host fallback) and that the task's issue has a
  `project_id`.

## Known follow-ups (not blockers)
- Idle TTL is a constant (`4h`) in `daemon.New` — expose as an env/config knob if you want
  to tune or test it without editing code.
- No UI for the config endpoints yet (API/`curl` only, by design — UI is the highest-churn
  upstream area).

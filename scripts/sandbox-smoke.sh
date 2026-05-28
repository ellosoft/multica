#!/usr/bin/env bash
# Mechanism smoke for the per-issue sandbox shim — NO server/daemon required.
#
# Proves the load-bearing pieces work end-to-end without a full stack:
#   1. the sandbox package + shim helpers pass their unit tests, and
#   2. `multica __sandbox-exec` actually routes a command INTO a container,
#      at the mirrored working directory, reading a host-mounted file.
#
# Requires: go, docker. Run from anywhere:  bash scripts/sandbox-smoke.sh
set -euo pipefail
cd "$(cd "$(dirname "$0")/../server" && pwd)"

echo "== build multica =="
go build -o /tmp/multica-smoke ./cmd/multica
echo "ok"

echo "== unit tests: sandbox package + shim helpers =="
go test ./internal/daemon/sandbox/ ./cmd/multica/ \
  -run 'Ensure|Reap|CLIDocker|Resolve|BuildDockerExecArgs|RewriteLoopbackEnv'

echo "== live shim smoke against a throwaway alpine container =="
# Use a $HOME-based dir, not $TMPDIR: on macOS Docker Desktop /var/folders is not a
# shared path, so a bind mount of it would be empty inside the container. On the
# Linux self-host any path works. (This mirrors the real feature's requirement that
# WorkspacesRoot be within Docker's shared paths on macOS.)
WORK="$(mktemp -d "${HOME}/.multica-smoke.XXXXXX")"
echo "hello-from-host" > "$WORK/marker.txt"
CID="$(docker run -d --rm -v "$WORK:$WORK" -w "$WORK" alpine:3.20 sleep 300)"
cleanup() { docker rm -f "$CID" >/dev/null 2>&1 || true; rm -rf "$WORK"; }
trap cleanup EXIT

# Run `cat marker.txt` INSIDE the container via the shim, from the mirrored cwd.
# This exercises: arg building, -w <cwd> mirroring, and the docker exec path.
cd "$WORK"
OUT="$(MULTICA_TASK_ID=smoke /tmp/multica-smoke __sandbox-exec \
  --container "$CID" --exec /bin/cat -- marker.txt)"

echo "shim output: $OUT"
if [ "$OUT" = "hello-from-host" ]; then
  echo "PASS: shim executed the command inside the container at the mirrored cwd"
else
  echo "FAIL: expected 'hello-from-host', got '$OUT'"
  exit 1
fi

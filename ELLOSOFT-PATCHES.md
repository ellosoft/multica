# Ellosoft Patches

Tracks insertion points for Ellosoft-specific changes in the Multica fork.

## server/cmd/server/router.go

- Inside the `/api/daemon` route group (`r.Route("/api/daemon", ...)`), after the last existing route line (`r.Post("/tasks/{taskId}/session", ...)`), added:
  ```go
  r.Get("/projects/{projectId}/sandbox-config", h.GetProjectSandboxConfig)
  ```
  This route is protected by `middleware.DaemonAuth` (the daemon's existing Bearer token).

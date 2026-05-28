package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

// SandboxConfigResponse is the resolved per-issue sandbox config returned by
// the endpoint. Fields match the daemon's SandboxConfig type wire-for-wire.
type SandboxConfigResponse struct {
	Enabled      bool   `json:"enabled"`
	Image        string `json:"image"`
	SetupCommand string `json:"setup_command"`
	CPUs         string `json:"cpus"`
	Memory       string `json:"memory"`
}

// sandboxRow holds the raw columns from ellosoft_sandbox_config.
type sandboxRow struct {
	enabled      bool
	image        string
	setupCommand string
	cpus         string
	memory       string
}

// resolveSandbox applies precedence: project row wins over global row; if
// neither is present the sandbox is disabled with all fields empty.
func resolveSandbox(project, global *sandboxRow) SandboxConfigResponse {
	if project != nil {
		return SandboxConfigResponse{
			Enabled:      project.enabled,
			Image:        project.image,
			SetupCommand: project.setupCommand,
			CPUs:         project.cpus,
			Memory:       project.memory,
		}
	}
	if global != nil {
		return SandboxConfigResponse{
			Enabled:      global.enabled,
			Image:        global.image,
			SetupCommand: global.setupCommand,
			CPUs:         global.cpus,
			Memory:       global.memory,
		}
	}
	return SandboxConfigResponse{Enabled: false}
}

// readSandboxRow executes q with the given args and scans a single sandboxRow.
// Returns (nil, nil) when the row does not exist (pgx.ErrNoRows).
func (h *Handler) readSandboxRow(ctx context.Context, q string, args ...any) (*sandboxRow, error) {
	var s sandboxRow
	err := h.DB.QueryRow(ctx, q, args...).Scan(
		&s.enabled,
		&s.image,
		&s.setupCommand,
		&s.cpus,
		&s.memory,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &s, nil
}

// GetProjectSandboxConfig returns the resolved sandbox configuration for a
// project. It is registered under the /api/daemon group so the daemon's
// existing Bearer token authenticates the call.
func (h *Handler) GetProjectSandboxConfig(w http.ResponseWriter, r *http.Request) {
	projectIDStr := chi.URLParam(r, "projectId")
	projectUUID, ok := parseUUIDOrBadRequest(w, projectIDStr, "project id")
	if !ok {
		return
	}

	ctx := r.Context()

	const projectQ = `SELECT enabled, image, setup_command, cpus, memory
		FROM ellosoft_sandbox_config
		WHERE project_id = $1`
	project, err := h.readSandboxRow(ctx, projectQ, projectUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to fetch project sandbox config")
		return
	}

	const globalQ = `SELECT enabled, image, setup_command, cpus, memory
		FROM ellosoft_sandbox_config
		WHERE is_global = true`
	global, err := h.readSandboxRow(ctx, globalQ)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to fetch global sandbox config")
		return
	}

	writeJSON(w, http.StatusOK, resolveSandbox(project, global))
}

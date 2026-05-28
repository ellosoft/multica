package handler

import (
	"context"
	"encoding/json"
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

// sandboxConfigInput is the write payload for both project and global sandbox
// config endpoints.
type sandboxConfigInput struct {
	Enabled      bool   `json:"enabled"`
	Image        string `json:"image"`
	SetupCommand string `json:"setup_command"`
	CPUs         string `json:"cpus"`
	Memory       string `json:"memory"`
}

// UpsertProjectSandboxConfig sets the sandbox config for one project. It is
// registered under the user-auth'd /api/projects/{id} group.
func (h *Handler) UpsertProjectSandboxConfig(w http.ResponseWriter, r *http.Request) {
	projectID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "project id")
	if !ok {
		return
	}
	var in sandboxConfigInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	_, err := h.DB.Exec(r.Context(),
		`INSERT INTO ellosoft_sandbox_config (project_id, enabled, image, setup_command, cpus, memory)
		 VALUES ($1,$2,$3,$4,$5,$6)
		 ON CONFLICT (project_id) DO UPDATE SET
		   enabled=EXCLUDED.enabled, image=EXCLUDED.image, setup_command=EXCLUDED.setup_command,
		   cpus=EXCLUDED.cpus, memory=EXCLUDED.memory`,
		projectID, in.Enabled, in.Image, in.SetupCommand, in.CPUs, in.Memory)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save sandbox config")
		return
	}
	writeJSON(w, http.StatusOK, SandboxConfigResponse{
		Enabled:      in.Enabled,
		Image:        in.Image,
		SetupCommand: in.SetupCommand,
		CPUs:         in.CPUs,
		Memory:       in.Memory,
	})
}

// UpsertGlobalSandboxConfig sets the single global-default sandbox config row.
// It is registered under the user-auth'd group as PUT /api/sandbox-config/global.
// The partial unique index on is_global=true makes ON CONFLICT awkward; we use
// UPDATE-then-INSERT to handle the upsert cleanly.
func (h *Handler) UpsertGlobalSandboxConfig(w http.ResponseWriter, r *http.Request) {
	var in sandboxConfigInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	ctx := r.Context()
	tag, err := h.DB.Exec(ctx,
		`UPDATE ellosoft_sandbox_config
		 SET enabled=$1, image=$2, setup_command=$3, cpus=$4, memory=$5
		 WHERE is_global = true`,
		in.Enabled, in.Image, in.SetupCommand, in.CPUs, in.Memory)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save global sandbox config")
		return
	}
	if tag.RowsAffected() == 0 {
		if _, err := h.DB.Exec(ctx,
			`INSERT INTO ellosoft_sandbox_config (project_id, enabled, image, setup_command, cpus, memory, is_global)
			 VALUES (NULL,$1,$2,$3,$4,$5,true)`,
			in.Enabled, in.Image, in.SetupCommand, in.CPUs, in.Memory); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create global sandbox config")
			return
		}
	}
	writeJSON(w, http.StatusOK, SandboxConfigResponse{
		Enabled:      in.Enabled,
		Image:        in.Image,
		SetupCommand: in.SetupCommand,
		CPUs:         in.CPUs,
		Memory:       in.Memory,
	})
}

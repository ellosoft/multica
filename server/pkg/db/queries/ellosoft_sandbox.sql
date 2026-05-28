-- name: GetProjectSandboxConfig :one
SELECT enabled, image, setup_command, cpus, memory
FROM ellosoft_sandbox_config
WHERE project_id = $1;

-- name: GetGlobalSandboxConfig :one
SELECT enabled, image, setup_command, cpus, memory
FROM ellosoft_sandbox_config
WHERE is_global = true;

-- name: UpsertProjectSandboxConfig :exec
INSERT INTO ellosoft_sandbox_config (project_id, enabled, image, setup_command, cpus, memory)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (project_id) DO UPDATE
SET enabled = EXCLUDED.enabled, image = EXCLUDED.image,
    setup_command = EXCLUDED.setup_command, cpus = EXCLUDED.cpus, memory = EXCLUDED.memory;

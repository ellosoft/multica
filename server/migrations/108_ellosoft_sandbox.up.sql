-- Ellosoft fork: per-project sandbox config (side table; does not touch upstream `projects`).
-- project_id NULL + is_global=true is the single global-default row.
CREATE TABLE ellosoft_sandbox_config (
    project_id     uuid UNIQUE REFERENCES projects (id) ON DELETE CASCADE,
    enabled        boolean NOT NULL DEFAULT false,
    image          text    NOT NULL DEFAULT '',
    setup_command  text    NOT NULL DEFAULT '',
    cpus           text    NOT NULL DEFAULT '',
    memory         text    NOT NULL DEFAULT '',
    is_global      boolean NOT NULL DEFAULT false
);
CREATE UNIQUE INDEX ellosoft_sandbox_one_global ON ellosoft_sandbox_config ((true)) WHERE is_global;

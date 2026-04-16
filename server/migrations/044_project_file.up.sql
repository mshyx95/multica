-- project_file stores file metadata and content pushed by daemons.
-- Replaces the pull-based proxy to daemon health server.
CREATE TABLE IF NOT EXISTS project_file (
    project_id UUID NOT NULL REFERENCES project_v2(id) ON DELETE CASCADE,
    path       TEXT NOT NULL,
    name       TEXT NOT NULL,
    size       BIGINT NOT NULL DEFAULT 0,
    is_dir     BOOLEAN NOT NULL DEFAULT FALSE,
    mod_time   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    category   TEXT NOT NULL DEFAULT 'other',
    content    TEXT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (project_id, path)
);

CREATE INDEX idx_project_file_project ON project_file(project_id);

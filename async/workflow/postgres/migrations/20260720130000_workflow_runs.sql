-- +goose Up
CREATE TABLE workflow_runs (
    id         text PRIMARY KEY,
    workflow   text NOT NULL,
    scope      text NOT NULL DEFAULT '',
    status     text NOT NULL,
    error      text NOT NULL DEFAULT '',
    state      jsonb NOT NULL,
    step       int NOT NULL,
    attempt    int NOT NULL,
    version    int NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);

CREATE INDEX workflow_runs_terminal_sweep_idx ON workflow_runs (updated_at) WHERE status IN ('completed', 'failed');

-- +goose Down
DROP TABLE workflow_runs;

-- +goose Up
-- state is json, not jsonb: checkpoints are written and read back verbatim,
-- never queried server-side, and json accepts the U+0000 unicode escape
-- jsonb rejects (a NUL captured into workflow state must not wedge every
-- checkpoint). Same choice as the queue_jobs payload column.
CREATE TABLE workflow_runs (
    id         text PRIMARY KEY,
    workflow   text NOT NULL,
    scope      text NOT NULL DEFAULT '',
    status     text NOT NULL,
    error      text NOT NULL DEFAULT '',
    state      json NOT NULL,
    step       int NOT NULL,
    attempt    int NOT NULL,
    version    int NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);

CREATE INDEX workflow_runs_terminal_sweep_idx ON workflow_runs (updated_at) WHERE status IN ('completed', 'failed');

-- +goose Down
DROP TABLE workflow_runs;

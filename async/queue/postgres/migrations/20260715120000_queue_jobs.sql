-- +goose Up
CREATE TABLE queue_jobs (
    id text PRIMARY KEY,
    queue text NOT NULL,
    type text NOT NULL,
    payload jsonb NOT NULL,
    scope text NOT NULL DEFAULT '',
    attempt integer NOT NULL DEFAULT 0,
    max_attempts integer NOT NULL DEFAULT 0,
    run_at timestamptz NOT NULL,
    claimed_until timestamptz,
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'dead')),
    last_error text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL
);

CREATE INDEX queue_jobs_claim_idx ON queue_jobs (queue, status, run_at);

-- +goose Down
DROP TABLE queue_jobs;

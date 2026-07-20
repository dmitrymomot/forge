-- +goose Up
CREATE TABLE outbox_jobs (
    id           uuid PRIMARY KEY,
    available_at timestamptz NOT NULL,
    run_at       timestamptz NOT NULL,
    created_at   timestamptz NOT NULL,
    attempts     integer NOT NULL DEFAULT 0,
    max_attempts integer NOT NULL DEFAULT 0,
    queue        text NOT NULL,
    type         text NOT NULL,
    scope        text NOT NULL DEFAULT '',
    last_error   text NOT NULL DEFAULT '',
    payload      json NOT NULL
) WITH (fillfactor = 90, autovacuum_vacuum_scale_factor = 0.02, autovacuum_analyze_scale_factor = 0.02);

CREATE INDEX outbox_jobs_claim_idx ON outbox_jobs (created_at, id);

-- +goose Down
DROP TABLE outbox_jobs;

-- +goose Up
CREATE TABLE queue_jobs (
    id            uuid PRIMARY KEY,
    claim_token   uuid,
    run_at        timestamptz NOT NULL,
    claimed_until timestamptz,
    created_at    timestamptz NOT NULL,
    attempt       integer NOT NULL DEFAULT 0,
    max_attempts  integer NOT NULL DEFAULT 0,
    queue         text NOT NULL,
    type          text NOT NULL,
    scope         text NOT NULL DEFAULT '',
    last_error    text NOT NULL DEFAULT '',
    payload       json NOT NULL
) WITH (fillfactor = 90, autovacuum_vacuum_scale_factor = 0.02, autovacuum_analyze_scale_factor = 0.02);

CREATE INDEX queue_jobs_claim_idx ON queue_jobs (queue, run_at);

CREATE TABLE queue_jobs_dead (
    id           uuid PRIMARY KEY,
    run_at       timestamptz NOT NULL,
    created_at   timestamptz NOT NULL,
    died_at      timestamptz NOT NULL,
    attempt      integer NOT NULL,
    max_attempts integer NOT NULL,
    queue        text NOT NULL,
    type         text NOT NULL,
    scope        text NOT NULL DEFAULT '',
    last_error   text NOT NULL DEFAULT '',
    payload      json NOT NULL
);

CREATE INDEX queue_jobs_dead_list_idx ON queue_jobs_dead (queue, died_at);
CREATE INDEX queue_jobs_dead_sweep_idx ON queue_jobs_dead (died_at);

-- +goose Down
DROP TABLE queue_jobs_dead;
DROP TABLE queue_jobs;

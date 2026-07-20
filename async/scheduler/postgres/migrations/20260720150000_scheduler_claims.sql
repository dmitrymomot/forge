-- +goose Up
CREATE TABLE scheduler_claims (
    name          text NOT NULL,
    scheduled_for timestamptz NOT NULL,
    claimed_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (name, scheduled_for)
);

CREATE INDEX scheduler_claims_sweep_idx ON scheduler_claims (scheduled_for);

-- +goose Down
DROP TABLE scheduler_claims;

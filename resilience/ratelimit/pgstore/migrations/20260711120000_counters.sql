-- +goose Up
CREATE TABLE IF NOT EXISTS forge_ratelimit_counters (
    key        text PRIMARY KEY,
    val        bigint NOT NULL,
    expires_at timestamptz
);

-- +goose Down
DROP TABLE IF EXISTS forge_ratelimit_counters;

-- +goose Up
CREATE TABLE IF NOT EXISTS forge_locks (
    key        text PRIMARY KEY,
    owner      text NOT NULL,
    expires_at timestamptz NOT NULL,
    fence      bigint NOT NULL
);
CREATE SEQUENCE IF NOT EXISTS forge_locks_fence_seq;

-- +goose Down
DROP TABLE IF EXISTS forge_locks;
DROP SEQUENCE IF EXISTS forge_locks_fence_seq;

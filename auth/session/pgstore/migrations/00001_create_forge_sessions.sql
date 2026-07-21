-- +goose Up
CREATE TABLE forge_sessions (
    token_hash   text PRIMARY KEY,
    id           uuid NOT NULL,
    user_id      text NOT NULL DEFAULT '',
    scope        text NOT NULL DEFAULT '',
    ip           text NOT NULL DEFAULT '',
    user_agent   text NOT NULL DEFAULT '',
    data         bytea NOT NULL DEFAULT ''::bytea,
    fingerprint  bytea,
    created_at   timestamptz NOT NULL,
    expires_at   timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL
);

-- Multi-device listings and per-user deletion; anonymous sessions carry no
-- user and stay out of the index.
CREATE INDEX forge_sessions_user_idx ON forge_sessions (scope, user_id, id DESC)
    WHERE user_id <> '';

-- DeleteExpired sweep.
CREATE INDEX forge_sessions_expires_idx ON forge_sessions (expires_at);

-- +goose Down
DROP TABLE forge_sessions;

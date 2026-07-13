-- +goose Up
CREATE TABLE forge_api_keys (
    id           uuid PRIMARY KEY,
    hash         text NOT NULL UNIQUE,
    preview      text NOT NULL,
    name         text NOT NULL DEFAULT '',
    subject      text NOT NULL,
    tenant       text NOT NULL DEFAULT '',
    scopes       text[] NOT NULL DEFAULT '{}',
    meta         jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at   timestamptz NOT NULL,
    expires_at   timestamptz,
    last_used_at timestamptz,
    revoked_at   timestamptz
);

CREATE INDEX forge_api_keys_list_idx ON forge_api_keys (tenant, subject, id DESC);

-- +goose Down
DROP TABLE forge_api_keys;

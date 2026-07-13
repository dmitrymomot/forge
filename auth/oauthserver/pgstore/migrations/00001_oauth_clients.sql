-- +goose Up
CREATE TABLE IF NOT EXISTS oauth_clients (
    id            text PRIMARY KEY,
    name          text NOT NULL,
    secret_hash   bytea NOT NULL,
    scopes        text[] NOT NULL DEFAULT '{}',
    grants        text[] NOT NULL DEFAULT '{}',
    redirect_uris text[] NOT NULL DEFAULT '{}',
    tenant_id     text NOT NULL DEFAULT '',
    token_ttl_ms  bigint NOT NULL DEFAULT 0,
    revoked_at    timestamptz,
    created_at    timestamptz NOT NULL
);

CREATE INDEX IF NOT EXISTS oauth_clients_tenant_idx ON oauth_clients (tenant_id);

-- +goose Down
DROP TABLE IF EXISTS oauth_clients;

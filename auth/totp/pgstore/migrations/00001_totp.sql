-- +goose Up
CREATE TABLE forge_totp (
    tenant        text NOT NULL DEFAULT '',
    subject       text NOT NULL,
    secret        bytea NOT NULL,
    confirmed     boolean NOT NULL DEFAULT false,
    last_used_at  timestamptz,
    backup_hashes bytea[] NOT NULL DEFAULT '{}',
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant, subject)
);

-- +goose Down
DROP TABLE forge_totp;

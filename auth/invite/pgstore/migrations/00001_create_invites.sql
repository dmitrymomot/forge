-- +goose Up
CREATE TABLE forge_invites (
    id          uuid PRIMARY KEY,
    hash        text NOT NULL UNIQUE,
    email       text NOT NULL,
    tenant      text NOT NULL DEFAULT '',
    role        text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL,
    expires_at  timestamptz NOT NULL,
    accepted_at timestamptz,
    revoked_at  timestamptz
);

CREATE INDEX forge_invites_list_idx ON forge_invites (tenant, email, id DESC);

-- +goose Down
DROP TABLE forge_invites;

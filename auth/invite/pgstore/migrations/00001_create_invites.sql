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

-- Serves a pending-only List (no tenant/email filter): the predicate matches
-- the query's accepted/revoked conditions exactly, and the expires_at range
-- scan returns just the acceptable rows. Expired-but-pending rows are never
-- deleted, so without this a cross-tenant pending listing degrades into a
-- sequential scan as dead invites accumulate.
CREATE INDEX forge_invites_pending_idx ON forge_invites (expires_at)
    WHERE accepted_at IS NULL AND revoked_at IS NULL;

-- +goose Down
DROP TABLE forge_invites;

-- +goose Up
CREATE TABLE forge_approval_requests (
    id          uuid PRIMARY KEY,
    kind        text NOT NULL,
    tenant      text NOT NULL DEFAULT '',
    requester   text NOT NULL,
    reason      text NOT NULL DEFAULT '',
    status      smallint NOT NULL,
    version     bigint NOT NULL DEFAULT 1,
    payload     json NOT NULL,
    decisions   json NOT NULL DEFAULT '[]'::json,
    meta        jsonb NOT NULL DEFAULT '{}'::jsonb,
    claimed_by  text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL,
    expires_at  timestamptz,
    claimed_at  timestamptz,
    decided_at  timestamptz
);

-- Covers the Filter WHERE columns in filter order, then id for the
-- newest-first ordering. An index that does not cover the filter is an
-- index Postgres will not use.
CREATE INDEX forge_approval_requests_list_idx
    ON forge_approval_requests (tenant, kind, status, id DESC);

-- Partial: rows with no expiry are never selected by an expiry bound, so
-- they do not belong in this index.
CREATE INDEX forge_approval_requests_expiry_idx
    ON forge_approval_requests (status, expires_at)
    WHERE expires_at IS NOT NULL;

-- Covers a List filtered by Requester within a Tenant, which
-- forge_approval_requests_list_idx cannot serve since it leads with kind and
-- status. A single-tenant deployment (tenant uniformly '') gets no benefit:
-- List omits the tenant predicate entirely, leaving this index's leading
-- column unqualified, so Postgres cannot use requester as a search key.
CREATE INDEX forge_approval_requests_requester_idx
    ON forge_approval_requests (tenant, requester, id DESC);

-- +goose Down
DROP TABLE forge_approval_requests;

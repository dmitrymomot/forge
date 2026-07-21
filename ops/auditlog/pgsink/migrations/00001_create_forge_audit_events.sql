-- +goose Up
CREATE TABLE forge_audit_events (
    id          uuid PRIMARY KEY,
    tenant      text NOT NULL DEFAULT '',
    actor       text NOT NULL DEFAULT '',
    action      text NOT NULL,
    resource    text NOT NULL DEFAULT '',
    outcome     text NOT NULL,
    meta        jsonb NOT NULL DEFAULT '{}'::jsonb,
    prev_hash   text NOT NULL DEFAULT '',
    hash        text NOT NULL DEFAULT '',
    occurred_at timestamptz NOT NULL
);

-- Serves tenant-isolated keyset pagination (id is a time-ordered UUIDv7)
-- and the chain-head/verify scans, which walk the same (tenant, id) order.
CREATE INDEX forge_audit_events_list_idx ON forge_audit_events (tenant, id DESC);

-- +goose Down
DROP TABLE forge_audit_events;

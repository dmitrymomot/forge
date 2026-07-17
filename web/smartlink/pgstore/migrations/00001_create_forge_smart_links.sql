-- +goose Up
CREATE TABLE forge_smart_links (
    code           text PRIMARY KEY,
    target         text NOT NULL DEFAULT '',
    ref            text NOT NULL DEFAULT '',
    metadata       jsonb NOT NULL DEFAULT '{}',
    tenant         text NOT NULL DEFAULT '',
    created_at     timestamptz NOT NULL,
    expires_at     timestamptz,
    deactivated_at timestamptz
);
CREATE INDEX forge_smart_links_tenant_created_idx ON forge_smart_links (tenant, created_at DESC, code);
-- +goose Down
DROP TABLE forge_smart_links;

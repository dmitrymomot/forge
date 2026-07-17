-- +goose Up
CREATE TABLE forge_short_links (
    code           text PRIMARY KEY,
    url            text NOT NULL,
    tenant         text NOT NULL DEFAULT '',
    created_at     timestamptz NOT NULL,
    expires_at     timestamptz,
    deactivated_at timestamptz
);

CREATE INDEX forge_short_links_list_idx ON forge_short_links (tenant, created_at DESC, code);

-- +goose Down
DROP TABLE forge_short_links;

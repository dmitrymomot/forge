-- +goose Up
CREATE TABLE forge_acl_entries (
    tenant        text NOT NULL DEFAULT '',
    subject       text NOT NULL,
    resource_type text NOT NULL,
    resource_id   text NOT NULL DEFAULT '',
    action        text NOT NULL,
    effect        text NOT NULL CHECK (effect IN ('allow', 'deny')),
    PRIMARY KEY (tenant, subject, resource_type, resource_id, action)
);

-- +goose Down
DROP TABLE forge_acl_entries;

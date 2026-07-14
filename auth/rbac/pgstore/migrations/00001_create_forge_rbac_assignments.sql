-- +goose Up
CREATE TABLE forge_rbac_assignments (
    tenant  text NOT NULL DEFAULT '',
    subject text NOT NULL,
    role    text NOT NULL,
    PRIMARY KEY (tenant, subject, role)
);

-- +goose Down
DROP TABLE forge_rbac_assignments;

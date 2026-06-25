-- +goose Up
CREATE TABLE IF NOT EXISTS db_test_widgets (
    id   TEXT PRIMARY KEY,
    name TEXT NOT NULL
);

-- +goose Down
DROP TABLE IF EXISTS db_test_widgets;

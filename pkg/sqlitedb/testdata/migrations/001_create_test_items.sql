-- +goose Up
CREATE TABLE test_items (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL
);

-- +goose Down
DROP TABLE test_items;

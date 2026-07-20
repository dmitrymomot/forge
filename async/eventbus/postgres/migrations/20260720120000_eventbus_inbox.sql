-- +goose Up
CREATE TABLE eventbus_inbox (
    consumer text NOT NULL,
    event_id text NOT NULL,
    seen_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (consumer, event_id)
);

CREATE INDEX eventbus_inbox_sweep_idx ON eventbus_inbox (seen_at);

-- +goose Down
DROP TABLE eventbus_inbox;

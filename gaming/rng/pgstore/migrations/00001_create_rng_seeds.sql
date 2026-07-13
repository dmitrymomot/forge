-- +goose Up
CREATE TABLE forge_rng_seeds (
    id          text PRIMARY KEY,
    scope       text NOT NULL DEFAULT '',
    player_id   text NOT NULL,
    server_seed bytea NOT NULL,
    client_seed text NOT NULL,
    nonce       bigint NOT NULL DEFAULT 0,
    status      text NOT NULL,
    algorithm   text NOT NULL,
    created_at  timestamptz NOT NULL,
    revealed_at timestamptz
);

CREATE UNIQUE INDEX forge_rng_seeds_active_idx ON forge_rng_seeds (scope, player_id) WHERE status = 'active';

-- +goose Down
DROP TABLE forge_rng_seeds;

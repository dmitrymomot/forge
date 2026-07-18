-- +goose Up
CREATE TABLE redirector_offers (
    ref         text PRIMARY KEY,
    name        text NOT NULL,
    default_url text NOT NULL,
    rules       jsonb NOT NULL DEFAULT '[]',
    created_at  timestamptz NOT NULL
);

CREATE TABLE redirector_clicks (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    code         text NOT NULL,
    rule         text NOT NULL DEFAULT '',
    url          text NOT NULL,
    country      text NOT NULL DEFAULT '',
    device       text NOT NULL DEFAULT '',
    fingerprint  text NOT NULL DEFAULT '',
    is_duplicate boolean NOT NULL,
    clicked_at   timestamptz NOT NULL
);
CREATE INDEX redirector_clicks_code_idx ON redirector_clicks (code, id DESC);
CREATE INDEX redirector_clicks_code_fp_idx ON redirector_clicks (code, fingerprint);

-- +goose Down
DROP TABLE redirector_clicks;
DROP TABLE redirector_offers;

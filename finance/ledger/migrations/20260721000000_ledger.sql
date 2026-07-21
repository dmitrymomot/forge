-- +goose Up
CREATE TABLE ledger_accounts (
    id         uuid PRIMARY KEY,
    tenant     text NOT NULL DEFAULT '',
    owner      text NOT NULL,
    purpose    text NOT NULL,
    currency   text NOT NULL,
    -- floor NULL = floor-free: no materialized balance/held, balance derived
    -- from snapshots + postings. floor NOT NULL = materialized balance/held
    -- maintained under row locks, available (balance - held) never drops
    -- below floor.
    floor      numeric,
    balance    numeric,
    held       numeric,
    created_at timestamptz NOT NULL,
    UNIQUE (tenant, owner, purpose, currency),
    CHECK ((floor IS NULL AND balance IS NULL AND held IS NULL)
        OR (floor IS NOT NULL AND balance IS NOT NULL AND held IS NOT NULL))
);

CREATE TABLE ledger_postings (
    seq         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    -- Transaction id stamped at insert; snapshot horizons compare against it
    -- so derived balances never miss an in-flight posting.
    txid        xid8 NOT NULL DEFAULT pg_current_xact_id(),
    ref         text NOT NULL UNIQUE,
    group_ref   text NOT NULL DEFAULT '',
    src_account uuid NOT NULL REFERENCES ledger_accounts (id),
    dst_account uuid NOT NULL REFERENCES ledger_accounts (id),
    amount      numeric NOT NULL CHECK (amount > 0),
    currency    text NOT NULL,
    adjusts     text NOT NULL DEFAULT '',
    -- Reserved for FIFO-expiring balance lots (loyalty); unused in v1.
    lot         text,
    created_at  timestamptz NOT NULL,
    CHECK (src_account <> dst_account)
);

CREATE INDEX ledger_postings_src_txid_idx ON ledger_postings (src_account, txid);
CREATE INDEX ledger_postings_dst_txid_idx ON ledger_postings (dst_account, txid);
CREATE INDEX ledger_postings_src_seq_idx ON ledger_postings (src_account, seq DESC);
CREATE INDEX ledger_postings_dst_seq_idx ON ledger_postings (dst_account, seq DESC);
CREATE INDEX ledger_postings_group_idx ON ledger_postings (group_ref) WHERE group_ref <> '';

CREATE TABLE ledger_holds (
    ref         text PRIMARY KEY,
    account     uuid NOT NULL REFERENCES ledger_accounts (id),
    amount      numeric NOT NULL CHECK (amount > 0),
    currency    text NOT NULL,
    status      text NOT NULL CHECK (status IN ('open', 'settled', 'voided')),
    expires_at  timestamptz,
    created_at  timestamptz NOT NULL,
    resolved_at timestamptz
);

CREATE INDEX ledger_holds_expiry_idx ON ledger_holds (expires_at)
    WHERE status = 'open' AND expires_at IS NOT NULL;
CREATE INDEX ledger_holds_account_open_idx ON ledger_holds (account)
    WHERE status = 'open';

-- One row per account: the latest verified balance cache for floor-free
-- accounts. balance covers postings with txid < snapshots.txid; readers add
-- SUM(postings WHERE txid >= snapshots.txid).
CREATE TABLE ledger_snapshots (
    account    uuid PRIMARY KEY REFERENCES ledger_accounts (id),
    balance    numeric NOT NULL,
    txid       xid8 NOT NULL,
    created_at timestamptz NOT NULL
);

-- +goose Down
DROP TABLE ledger_snapshots;
DROP TABLE ledger_holds;
DROP TABLE ledger_postings;
DROP TABLE ledger_accounts;

-- The catalog. Schema changes after this one are expand-contract only: two
-- versions of catalog run at once during a canary (Phase 6 scenario 5), so a
-- migration may add, backfill and relax, and may drop only what no running
-- version still reads.
CREATE TABLE items (
    sku              text   PRIMARY KEY,
    name             text   NOT NULL,
    unit_price_cents bigint NOT NULL CHECK (unit_price_cents >= 0)
);

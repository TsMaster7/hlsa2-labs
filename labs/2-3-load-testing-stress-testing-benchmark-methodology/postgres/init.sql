-- Seed schema and data for the /medium endpoint.
--
-- ~50,000 rows is enough that the working set is larger than the
-- per-row B-tree leaf, so the medium endpoint shows the 10-30 ms
-- bracket described in the homework. The only index is on the
-- primary key, deliberately - this leaves room for an "add an index
-- on sku" optimization candidate if the student doesn't pick the
-- pool-size knob in Step 5.
--
-- The Zipfian-ish access pattern is generated client-side by the SUT,
-- not here.

CREATE TABLE IF NOT EXISTS items (
    id           BIGSERIAL PRIMARY KEY,
    sku          TEXT        NOT NULL,
    payload      JSONB       NOT NULL,
    amount_cents BIGINT      NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO items (sku, payload, amount_cents)
SELECT
    'SKU-' || lpad(g::text, 8, '0'),
    jsonb_build_object(
        'name', 'item-' || g,
        'tags', ARRAY['hlsa2', 'lab2-3'],
        'attrs', jsonb_build_object(
            'color', (ARRAY['red','blue','green','yellow','black','white'])[1 + (g % 6)],
            'size',  (ARRAY['xs','s','m','l','xl'])[1 + (g % 5)]
        )
    ),
    100 + (g * 7) % 99900
FROM generate_series(1, 50000) AS g
ON CONFLICT DO NOTHING;

ANALYZE items;

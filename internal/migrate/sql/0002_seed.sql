-- Seed: 50 items, SKU-0001 .. SKU-0050, with prices spread from $1.99 to $99.
-- ON CONFLICT keeps it harmless if the rows already exist.
INSERT INTO items (sku, name, unit_price_cents)
SELECT format('SKU-%s', lpad(i::text, 4, '0')),
       (ARRAY['Widget', 'Gadget', 'Sprocket', 'Gizmo', 'Doohickey'])[1 + i % 5] || ' ' || i,
       199 + (i * 137) % 9800
FROM generate_series(1, 50) AS i
ON CONFLICT (sku) DO NOTHING;

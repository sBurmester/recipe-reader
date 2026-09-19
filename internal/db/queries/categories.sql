-- name: FindOrCreateCategory :one
-- Returns the category row with this name, inserting it first only if there is none.
--
-- It reads before it writes. The plain INSERT ... ON CONFLICT DO UPDATE this
-- replaces went through the insert path on every call: it spent a sequence
-- value, wrote a new row version and took a row lock even when the row existed,
-- which is nearly always. Since recipe writes resolve names inside their own
-- transaction, that lock was held until the recipe committed, so two writes
-- naming the same category queued behind each other, and two naming a pair of them
-- in opposite order could deadlock.
--
-- The INSERT runs only when the read found nothing, and keeps DO UPDATE for the
-- one case the read cannot see: a row another transaction committed after this
-- statement's snapshot. DO UPDATE, unlike DO NOTHING, still returns that row,
-- so the statement always yields exactly one.
WITH existing AS (
    SELECT id, name FROM categories WHERE name = sqlc.arg(name)::text
), inserted AS (
    INSERT INTO categories (name)
    SELECT sqlc.arg(name)::text WHERE NOT EXISTS (SELECT 1 FROM existing)
    ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
    RETURNING id, name
)
SELECT id, name FROM existing
UNION ALL
SELECT id, name FROM inserted;

-- name: ListCategories :many
SELECT * FROM categories ORDER BY name;

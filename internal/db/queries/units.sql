-- name: FindOrCreateUnit :one
INSERT INTO units (name) VALUES ($1)
ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
RETURNING *;

-- name: ListUnits :many
SELECT * FROM units ORDER BY name;

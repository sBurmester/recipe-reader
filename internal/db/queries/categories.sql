-- name: FindOrCreateCategory :one
INSERT INTO categories (name) VALUES ($1)
ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
RETURNING *;

-- name: ListCategories :many
SELECT * FROM categories ORDER BY name;

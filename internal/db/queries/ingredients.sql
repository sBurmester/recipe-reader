-- name: FindOrCreateIngredient :one
INSERT INTO ingredients (name) VALUES ($1)
ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
RETURNING *;

-- name: ListIngredients :many
SELECT * FROM ingredients ORDER BY name;

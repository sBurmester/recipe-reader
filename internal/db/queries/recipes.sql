-- name: CreateRecipe :one
INSERT INTO recipes (name, instructions, image_url, source, status)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetRecipe :one
SELECT * FROM recipes WHERE id = $1;

-- name: GetRecipeBySource :one
SELECT * FROM recipes WHERE source = $1;

-- name: UpdateRecipe :one
UPDATE recipes
SET name = $2, instructions = $3, image_url = $4, status = $5, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteRecipe :execrows
DELETE FROM recipes WHERE id = $1;

-- name: SearchRecipes :many
SELECT DISTINCT r.* FROM recipes r
LEFT JOIN recipe_categories rc ON rc.recipe_id = r.id
WHERE (sqlc.narg(text)::text IS NULL OR lower(r.name) LIKE '%' || lower(sqlc.narg(text)::text) || '%')
  AND (sqlc.narg(category_id)::bigint IS NULL OR rc.category_id = sqlc.narg(category_id)::bigint)
  AND (sqlc.narg(status)::text IS NULL OR r.status = sqlc.narg(status)::text)
ORDER BY r.name, r.id
LIMIT $1 OFFSET $2;

-- name: CountRecipes :one
SELECT COUNT(DISTINCT r.id) FROM recipes r
LEFT JOIN recipe_categories rc ON rc.recipe_id = r.id
WHERE (sqlc.narg(text)::text IS NULL OR lower(r.name) LIKE '%' || lower(sqlc.narg(text)::text) || '%')
  AND (sqlc.narg(category_id)::bigint IS NULL OR rc.category_id = sqlc.narg(category_id)::bigint)
  AND (sqlc.narg(status)::text IS NULL OR r.status = sqlc.narg(status)::text);

-- name: ListRecipeIngredients :many
SELECT ri.ingredient_id, i.name AS ingredient_name, ri.amount, ri.unit_id, COALESCE(u.name, '') AS unit_name
FROM recipe_ingredients ri
JOIN ingredients i ON i.id = ri.ingredient_id
LEFT JOIN units u ON u.id = ri.unit_id
WHERE ri.recipe_id = $1
ORDER BY ri.position, ri.id;

-- name: ListRecipeCategories :many
SELECT c.id, c.name FROM categories c
JOIN recipe_categories rc ON rc.category_id = c.id
WHERE rc.recipe_id = $1
ORDER BY c.name;

-- name: AddRecipeIngredient :one
INSERT INTO recipe_ingredients (recipe_id, ingredient_id, amount, unit_id, position)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: DeleteRecipeIngredients :exec
DELETE FROM recipe_ingredients WHERE recipe_id = $1;

-- name: AddRecipeCategory :exec
INSERT INTO recipe_categories (recipe_id, category_id) VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: DeleteRecipeCategories :exec
DELETE FROM recipe_categories WHERE recipe_id = $1;

-- +goose Up
-- 0003 made (recipe_id, position) unique, and the index behind that constraint
-- leads with recipe_id. It therefore serves every lookup by recipe_id that
-- idx_recipe_ingredients_recipe_id served — the two reads in recipes.sql, the
-- delete that replaces a recipe's ingredients, and the ON DELETE CASCADE from
-- recipes. The single-column index only stayed to be maintained on every write
-- for nothing.
--
-- A plain DROP INDEX rather than DROP INDEX CONCURRENTLY: the table is one
-- person's recipe lines, the lock is over in milliseconds, and CONCURRENTLY
-- would need goose's no-transaction mode to buy nothing here. (Comments must
-- not spell that annotation out: goose takes any comment line holding its
-- prefix for one. Alone on a line, as an example, it would take this file out
-- of its transaction; mid-sentence, it makes goose refuse the file.)
DROP INDEX IF EXISTS idx_recipe_ingredients_recipe_id;

-- +goose Down
CREATE INDEX idx_recipe_ingredients_recipe_id ON recipe_ingredients (recipe_id);

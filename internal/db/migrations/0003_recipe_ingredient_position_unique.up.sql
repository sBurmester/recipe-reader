-- persistence P9 asked whether a recipe may list the same ingredient twice, and
-- the answer is yes: "Für den Teig: 200 g Zucker" and "Für den Belag: 50 g
-- Zucker" are two lines of one recipe. UNIQUE (recipe_id, ingredient_id) would
-- refuse that recipe, or force the two lines into one and lose the difference.
--
-- What tells two such lines apart is their position, so that is what is made
-- unique: the order of a recipe's ingredients is total, and a repeated
-- ingredient is always a separate, ordered line rather than an accidental copy.
--
-- writeAssociations has always written positions 0..n-1 after deleting the old
-- rows, so nothing it wrote collides. A row written by hand may; each recipe's
-- lines are renumbered first, in the order they are already read in
-- (position, then id), which changes no position that was already unique. One
-- transaction, as in 0002, so a failure leaves neither half behind.
BEGIN;

UPDATE recipe_ingredients AS ri
SET position = renumbered.position
FROM (
    SELECT id, (row_number() OVER (PARTITION BY recipe_id ORDER BY position, id) - 1)::int AS position
    FROM recipe_ingredients
) AS renumbered
WHERE ri.id = renumbered.id AND ri.position <> renumbered.position;

ALTER TABLE recipe_ingredients
    ADD CONSTRAINT recipe_ingredients_recipe_id_position_key UNIQUE (recipe_id, position);

COMMIT;

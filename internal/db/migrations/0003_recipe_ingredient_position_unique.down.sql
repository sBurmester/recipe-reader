-- The renumbered positions are not restored: which rows collided was never
-- recorded, and the renumbering kept their order.
ALTER TABLE recipe_ingredients DROP CONSTRAINT IF EXISTS recipe_ingredients_recipe_id_position_key;

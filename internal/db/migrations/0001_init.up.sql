CREATE TABLE units (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE
);

CREATE TABLE categories (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE
);

CREATE TABLE ingredients (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE
);

CREATE TABLE recipes (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    instructions TEXT NOT NULL DEFAULT '',
    image_url TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- idx_recipes_name_lower serves a future exact lower(name) = $1 lookup only.
-- It does NOT accelerate SearchRecipes: that filter is a leading-wildcard
-- LIKE ('%term%'), which no btree index can serve — substring search is a
-- seq scan until this is replaced with pg_trgm + a GIN index.
CREATE INDEX idx_recipes_name_lower ON recipes (lower(name));
CREATE INDEX idx_recipes_status ON recipes (status);

CREATE TABLE recipe_ingredients (
    id BIGSERIAL PRIMARY KEY,
    recipe_id BIGINT NOT NULL REFERENCES recipes(id) ON DELETE CASCADE,
    ingredient_id BIGINT NOT NULL REFERENCES ingredients(id),
    amount DOUBLE PRECISION NOT NULL DEFAULT 0,
    unit_id BIGINT REFERENCES units(id),
    position INT NOT NULL DEFAULT 0
);
CREATE INDEX idx_recipe_ingredients_recipe_id ON recipe_ingredients (recipe_id);

CREATE TABLE recipe_categories (
    recipe_id BIGINT NOT NULL REFERENCES recipes(id) ON DELETE CASCADE,
    category_id BIGINT NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
    PRIMARY KEY (recipe_id, category_id)
);

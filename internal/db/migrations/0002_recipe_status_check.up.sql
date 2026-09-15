-- recipes.status has exactly two legal values (domain.RecipeStatus), and until
-- this migration nothing enforced either: both write handlers stored whatever
-- status a client sent, so a database that has been written to may already hold
-- '' or anything else.
--
-- Those rows are moved to needs_review first — the state that puts a person in
-- front of them — because the constraint would otherwise refuse to apply, and a
-- migration that fails stops an existing deployment from starting at all. One
-- transaction, so a failure leaves neither half behind.
BEGIN;

UPDATE recipes SET status = 'needs_review'
WHERE status NOT IN ('needs_review', 'published');

ALTER TABLE recipes ADD CONSTRAINT recipes_status_check
    CHECK (status IN ('needs_review', 'published'));

COMMIT;

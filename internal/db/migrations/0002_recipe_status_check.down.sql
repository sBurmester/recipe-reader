-- The backfilled rows are not restored: which ones held an out-of-domain status
-- was never recorded, and needs_review is a legal value under either schema.
ALTER TABLE recipes DROP CONSTRAINT IF EXISTS recipes_status_check;

-- 000008 rollback (operators only; application startup never runs downs).
DROP INDEX IF EXISTS payments_business_kind_idx;
ALTER TABLE payments DROP COLUMN IF EXISTS kind;

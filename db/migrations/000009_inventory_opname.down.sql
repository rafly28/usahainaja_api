DROP INDEX IF EXISTS stock_adjustments_business_status_idx;
DROP INDEX IF EXISTS stock_movements_filter_idx;

ALTER TABLE stock_adjustment_items
    DROP COLUMN IF EXISTS system_quantity,
    DROP COLUMN IF EXISTS counted_quantity;

ALTER TABLE stock_adjustments DROP COLUMN IF EXISTS adjustment_kind;
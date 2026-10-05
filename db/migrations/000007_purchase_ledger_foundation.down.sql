-- 000007 rollback (operators only; application startup never runs downs).
ALTER TABLE product_inventory DROP CONSTRAINT IF EXISTS product_inventory_quantity_nonnegative_check;

DELETE FROM number_sequences WHERE sequence_type = 'PURCHASE_RECEIPT';

DROP TABLE IF EXISTS payment_methods;
DROP TABLE IF EXISTS idempotency_keys;

ALTER TABLE stock_movements DROP CONSTRAINT IF EXISTS stock_movements_source_type_check;
ALTER TABLE stock_movements DROP CONSTRAINT IF EXISTS stock_movements_single_source_check;
ALTER TABLE stock_movements DROP CONSTRAINT IF EXISTS stock_movements_business_stock_adjustment_id_fkey;
ALTER TABLE stock_movements DROP CONSTRAINT IF EXISTS stock_movements_business_purchase_receipt_id_fkey;
ALTER TABLE stock_movements DROP CONSTRAINT IF EXISTS stock_movements_business_sale_id_fkey;
ALTER TABLE stock_movements DROP COLUMN IF EXISTS stock_adjustment_id;
ALTER TABLE stock_movements DROP COLUMN IF EXISTS purchase_receipt_id;
ALTER TABLE stock_movements DROP COLUMN IF EXISTS sale_id;
-- Rollback restores the legacy NOT NULL only when no NULLs exist
-- (i.e. no new-style writes happened yet; otherwise archive them first).
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM stock_movements WHERE reference_id IS NULL) THEN
        ALTER TABLE stock_movements ALTER COLUMN reference_id SET NOT NULL;
    ELSE
        RAISE NOTICE 'stock_movements.reference_id keeps NULLs; NOT NULL not restored';
    END IF;
END $$;

DROP TABLE IF EXISTS purchase_receipt_items;
DROP TABLE IF EXISTS purchase_receipts;

ALTER TABLE purchase_items DROP CONSTRAINT IF EXISTS purchase_items_business_line_unique;
ALTER TABLE sale_items DROP COLUMN IF EXISTS line_number;
ALTER TABLE purchase_items DROP COLUMN IF EXISTS line_number;
ALTER TABLE purchase_items DROP COLUMN IF EXISTS received_quantity;
ALTER TABLE purchase_items DROP COLUMN IF EXISTS ordered_quantity;

ALTER TABLE stock_movements DROP CONSTRAINT IF EXISTS stock_movements_movement_type_check;
ALTER TABLE stock_movements ADD CONSTRAINT stock_movements_movement_type_check
    CHECK (movement_type IN ('PURCHASE', 'SALE', 'ADJUSTMENT', 'SPOILAGE', 'DAMAGE', 'TRANSFER', 'RETURN', 'OPENING_BALANCE'));

ALTER TABLE purchases DROP CONSTRAINT IF EXISTS purchases_status_check;
ALTER TABLE purchases ADD CONSTRAINT purchases_status_check
    CHECK (status IN ('DRAFT', 'COMPLETED', 'CANCELLED'));

ALTER TABLE sales DROP CONSTRAINT IF EXISTS sales_status_check;
ALTER TABLE sales ADD CONSTRAINT sales_status_check
    CHECK (status IN ('DRAFT', 'COMPLETED', 'CANCELLED', 'REFUNDED'));

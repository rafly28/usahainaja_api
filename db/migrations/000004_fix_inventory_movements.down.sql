ALTER TABLE stock_movements RENAME COLUMN reference_id TO stock_adjustment_id;
ALTER TABLE stock_movements ADD CONSTRAINT stock_movements_business_id_stock_adjustment_id_fkey FOREIGN KEY (business_id, stock_adjustment_id) REFERENCES stock_adjustments(business_id, id);

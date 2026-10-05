-- 000008: Append-only payment refunds (T05 void keeps ledger immutable).
--
-- Refunds are new positive-amount rows with kind='REFUND' referencing the
-- original sale/purchase. paid_total = CHARGE sum - REFUND sum.
ALTER TABLE payments
    ADD COLUMN kind varchar(20) NOT NULL DEFAULT 'CHARGE'
    CHECK (kind IN ('CHARGE', 'REFUND'));

CREATE INDEX payments_business_kind_idx ON payments (business_id, kind);

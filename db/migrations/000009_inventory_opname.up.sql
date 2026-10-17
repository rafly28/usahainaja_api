-- T12 — Inventory operations & documents.
--
-- Stock opname memakai stock_adjustments yang sudah ada (tanpa tabel baru).
-- Kolom baru hanya membedakan jenis penyesuaian dan menyimpan-jejak hasil hitung fisik.

ALTER TABLE stock_adjustments
    ADD COLUMN adjustment_kind varchar(20) NOT NULL DEFAULT 'MANUAL'
        CHECK (adjustment_kind IN ('MANUAL', 'OPNAME', 'OPENING_BALANCE'));

-- Stok awal yang sudah tercatat sebelum kolom ini ada ditandai sebagai OPENING_BALANCE.
UPDATE stock_adjustments
SET adjustment_kind = 'OPENING_BALANCE'
WHERE reason = 'OPENING_BALANCE';

-- Jejak hasil opname: qty sistem saat hitung fisik vs qty hasil hitung.
ALTER TABLE stock_adjustment_items
    ADD COLUMN system_quantity numeric(18,4),
    ADD COLUMN counted_quantity numeric(18,4);

-- Mendukung filter riwayat pergerakan sesuai kontrak: lokasi, jenis, dan tanggal.
CREATE INDEX stock_movements_filter_idx
    ON stock_movements (business_id, location_id, movement_type, occurred_at DESC);

-- Mendukung daftar penyesuaian stok (draft + completed) per bisnis.
CREATE INDEX stock_adjustments_business_status_idx
    ON stock_adjustments (business_id, status, adjustment_date DESC);

-- Nomor opname dipisahkan dari adjustment biasa agar mudah dikenali pengguna.
INSERT INTO number_sequences (business_id, sequence_type, prefix)
SELECT b.id, 'STOCK_OPNAME', 'OPN' FROM businesses b
ON CONFLICT (business_id, sequence_type) DO NOTHING;
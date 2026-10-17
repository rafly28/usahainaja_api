-- T12 — logo toko pada dokumen cetak (struk/konfirmasi).
--
-- Nilai disimpan sebagai teks agar dapat menerima data URI (selalu tersedia
-- saat cetak) maupun URL HTTPS. Kosong berarti toko belum memasang logo dan
-- dokumen memakai nama toko sebagai identitas.

ALTER TABLE businesses
    ADD COLUMN logo_url text;

-- Batas ukuran yang wajar untuk data URI maupun URL biasa.
ALTER TABLE businesses
    ADD CONSTRAINT businesses_logo_url_length_check
    CHECK (logo_url IS NULL OR char_length(logo_url) <= 2000000);
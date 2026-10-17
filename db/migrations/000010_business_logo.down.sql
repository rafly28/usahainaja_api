ALTER TABLE businesses DROP CONSTRAINT IF EXISTS businesses_logo_url_length_check;
ALTER TABLE businesses DROP COLUMN IF EXISTS logo_url;
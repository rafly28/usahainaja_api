ALTER TABLE products DROP CONSTRAINT IF EXISTS products_business_category_id_fkey;
ALTER TABLE products DROP COLUMN IF EXISTS category_id;
DROP TABLE IF EXISTS party_addresses;
DROP TABLE IF EXISTS party_contacts;
DROP TABLE IF EXISTS party_relationships;
DROP TABLE IF EXISTS parties;
DROP TABLE IF EXISTS unit_conversions;
ALTER TABLE units DROP COLUMN IF EXISTS status;
DROP TABLE IF EXISTS categories;

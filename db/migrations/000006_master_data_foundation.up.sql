CREATE TABLE categories (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    parent_id uuid,
    public_code varchar(32) NOT NULL,
    category_type varchar(30) NOT NULL CHECK (category_type IN ('PRODUCT', 'SERVICE', 'ASSET', 'EXPENSE')),
    name varchar(150) NOT NULL,
    status varchar(30) NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'INACTIVE')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (business_id, id),
    UNIQUE (business_id, public_code),
    UNIQUE (business_id, category_type, name),
    FOREIGN KEY (business_id, parent_id) REFERENCES categories(business_id, id) ON DELETE RESTRICT
);

CREATE INDEX categories_business_type_idx ON categories (business_id, category_type, status);

ALTER TABLE products ADD COLUMN category_id uuid;
ALTER TABLE products
    ADD CONSTRAINT products_business_category_id_fkey
    FOREIGN KEY (business_id, category_id) REFERENCES categories(business_id, id) ON DELETE RESTRICT;

ALTER TABLE units ADD COLUMN status varchar(30) NOT NULL DEFAULT 'ACTIVE'
    CHECK (status IN ('ACTIVE', 'INACTIVE'));

CREATE TABLE unit_conversions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    product_id uuid,
    from_unit_id uuid NOT NULL,
    to_unit_id uuid NOT NULL,
    multiplier numeric(18,6) NOT NULL CHECK (multiplier > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (business_id, product_id, from_unit_id, to_unit_id),
    CHECK (from_unit_id <> to_unit_id),
    FOREIGN KEY (business_id, product_id) REFERENCES products(business_id, id) ON DELETE CASCADE,
    FOREIGN KEY (business_id, from_unit_id) REFERENCES units(business_id, id),
    FOREIGN KEY (business_id, to_unit_id) REFERENCES units(business_id, id)
);

CREATE TABLE parties (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    public_code varchar(32) NOT NULL,
    party_type varchar(30) NOT NULL CHECK (party_type IN ('PERSON', 'ORGANIZATION')),
    display_name varchar(150) NOT NULL,
    legal_name varchar(150),
    status varchar(30) NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'INACTIVE')),
    notes text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (business_id, id),
    UNIQUE (business_id, public_code)
);

CREATE INDEX parties_business_name_idx ON parties (business_id, display_name, status);

CREATE TABLE party_relationships (
    party_id uuid NOT NULL REFERENCES parties(id) ON DELETE CASCADE,
    relationship_type varchar(30) NOT NULL CHECK (relationship_type IN ('CUSTOMER', 'SUPPLIER', 'PARTNER', 'CLIENT', 'TALENT', 'EMPLOYEE', 'OTHER')),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (party_id, relationship_type)
);

CREATE TABLE party_contacts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    party_id uuid NOT NULL REFERENCES parties(id) ON DELETE CASCADE,
    contact_type varchar(30) NOT NULL CHECK (contact_type IN ('PHONE', 'WHATSAPP', 'EMAIL', 'OTHER')),
    label varchar(100),
    value varchar(150) NOT NULL,
    is_primary boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX party_contacts_one_primary_idx ON party_contacts (party_id) WHERE is_primary;

CREATE TABLE party_addresses (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    party_id uuid NOT NULL REFERENCES parties(id) ON DELETE CASCADE,
    address_type varchar(30) NOT NULL DEFAULT 'OTHER' CHECK (address_type IN ('BILLING', 'SHIPPING', 'OFFICE', 'OTHER')),
    label varchar(100),
    address text NOT NULL,
    city varchar(100),
    province varchar(100),
    postal_code varchar(20),
    is_primary boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX party_addresses_one_primary_idx ON party_addresses (party_id) WHERE is_primary;

INSERT INTO number_sequences (business_id, sequence_type, prefix)
SELECT b.id, sequence_defaults.sequence_type, sequence_defaults.prefix
FROM businesses b
CROSS JOIN (VALUES ('CATEGORY', 'CAT'), ('PARTY', 'PTY'), ('LOCATION', 'LOC'), ('UNIT', 'UNT')) AS sequence_defaults(sequence_type, prefix)
ON CONFLICT (business_id, sequence_type) DO NOTHING;

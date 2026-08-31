CREATE TABLE business_modules (
    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    module_code varchar(30) NOT NULL
        CHECK (module_code IN ('CATALOG', 'INVENTORY', 'SALES', 'PURCHASE', 'FINANCE', 'BOOKING', 'REPORTING')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (business_id, module_code)
);

CREATE INDEX business_modules_module_idx ON business_modules (module_code, business_id);

-- Seed the default profile for businesses that predate module configuration.
INSERT INTO business_modules (business_id, module_code)
SELECT b.id, profile.module_code
FROM businesses b
CROSS JOIN LATERAL (
    SELECT unnest(
        CASE b.business_type
            WHEN 'RETAIL' THEN ARRAY['CATALOG', 'INVENTORY', 'SALES', 'PURCHASE', 'FINANCE', 'REPORTING']
            WHEN 'SERVICE' THEN ARRAY['BOOKING', 'FINANCE', 'REPORTING']
            WHEN 'ENTERTAINMENT' THEN ARRAY['BOOKING', 'FINANCE', 'REPORTING']
            ELSE ARRAY[]::text[]
        END
    )::varchar(30) AS module_code
) profile;

-- Earlier onboarding did not create all operational defaults. Make the
-- foundation usable for existing tenants without replacing an existing default.
INSERT INTO cash_accounts (business_id, public_code, name, account_type, balance, is_default)
SELECT b.id, 'CSH-DEFAULT', 'Kas Utama', 'CASH', 0, true
FROM businesses b
WHERE NOT EXISTS (
    SELECT 1 FROM cash_accounts ca WHERE ca.business_id = b.id AND ca.is_default
);

INSERT INTO number_sequences (business_id, sequence_type, prefix)
SELECT b.id, sequence_defaults.sequence_type, sequence_defaults.prefix
FROM businesses b
CROSS JOIN (
    VALUES
        ('PRODUCT', 'PRD'), ('OPENING_STOCK', 'OPEN'), ('STOCK_ADJUSTMENT', 'ADJ'),
        ('CONTACT', 'CNT'), ('CASH', 'CSH'), ('SALE', 'SL'), ('PURC', 'PUR'), ('PAY', 'PAY')
) AS sequence_defaults(sequence_type, prefix)
ON CONFLICT (business_id, sequence_type) DO NOTHING;

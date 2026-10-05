-- 000007: Transaction ledger foundation (T04/T05).
--
-- Forward-only contract alignment. Older migrations are untouched.
-- Legacy experimental rows keep working through the `reference_id` path;
-- new code must use sale_id / purchase_receipt_id / stock_adjustment_id.

-- ---------------------------------------------------------------------------
-- 1. Lifecycle status aligned with transaction_contract_v1.md.
-- Legacy values (sales REFUNDED, purchases COMPLETED) are kept so existing
-- experimental rows stay valid; they are removed by a later data migration.
-- Auto-generated CHECK names for inline column constraints are
-- {table}_{column}_check; the drops below rely on that Postgres convention
-- and run inside the migration transaction, so a mismatch fails loudly
-- without partial application.
-- ---------------------------------------------------------------------------
ALTER TABLE sales DROP CONSTRAINT IF EXISTS sales_status_check;
ALTER TABLE sales ADD CONSTRAINT sales_status_check
    CHECK (status IN ('DRAFT', 'COMPLETED', 'CANCELLED', 'VOIDED', 'REFUNDED'));

ALTER TABLE purchases DROP CONSTRAINT IF EXISTS purchases_status_check;
ALTER TABLE purchases ADD CONSTRAINT purchases_status_check
    CHECK (status IN ('DRAFT', 'ORDERED', 'PARTIALLY_RECEIVED', 'RECEIVED', 'COMPLETED', 'CANCELLED'));

-- SALE_VOID is the append-only reversal movement; legacy code paths that
-- still write movement_type='SALE' keep working until T05 adopts it.
ALTER TABLE stock_movements DROP CONSTRAINT IF EXISTS stock_movements_movement_type_check;
ALTER TABLE stock_movements ADD CONSTRAINT stock_movements_movement_type_check
    CHECK (movement_type IN ('PURCHASE', 'SALE', 'SALE_VOID', 'ADJUSTMENT', 'SPOILAGE', 'DAMAGE', 'TRANSFER', 'RETURN', 'OPENING_BALANCE'));

-- ---------------------------------------------------------------------------
-- 2. Purchase lines carry ordered vs received quantities.
-- ---------------------------------------------------------------------------
ALTER TABLE purchase_items
    ADD COLUMN ordered_quantity numeric(18,4) NOT NULL DEFAULT 0 CHECK (ordered_quantity > 0),
    ADD COLUMN received_quantity numeric(18,4) NOT NULL DEFAULT 0 CHECK (received_quantity >= 0);

-- Backfill: ordered = legacy quantity; items of already-COMPLETED purchases
-- count as fully received, everything else as not yet received.
UPDATE purchase_items pi
SET ordered_quantity = pi.quantity,
    received_quantity = CASE WHEN p.status = 'COMPLETED' THEN pi.quantity ELSE 0 END
FROM purchases p
WHERE p.business_id = pi.business_id AND p.id = pi.purchase_id;

-- Line numbers for receipt references (contract uses line_number, not item id).
ALTER TABLE purchase_items ADD COLUMN line_number integer;
ALTER TABLE sale_items ADD COLUMN line_number integer;

WITH numbered_purchase AS (
    SELECT id, ROW_NUMBER() OVER (PARTITION BY business_id, purchase_id ORDER BY created_at, id) AS rn
    FROM purchase_items
)
UPDATE purchase_items pi SET line_number = np.rn FROM numbered_purchase np WHERE np.id = pi.id;

WITH numbered_sale AS (
    SELECT id, ROW_NUMBER() OVER (PARTITION BY business_id, sale_id ORDER BY id) AS rn
    FROM sale_items
)
UPDATE sale_items si SET line_number = ns.rn FROM numbered_sale ns WHERE ns.id = si.id;

ALTER TABLE purchase_items ALTER COLUMN line_number SET NOT NULL;
ALTER TABLE sale_items ALTER COLUMN line_number SET NOT NULL;
ALTER TABLE purchase_items ADD CONSTRAINT purchase_items_business_line_unique UNIQUE (business_id, purchase_id, line_number);

-- ---------------------------------------------------------------------------
-- 3. Purchase receipts: every receive creates its own receipt.
-- ---------------------------------------------------------------------------
CREATE TABLE purchase_receipts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    purchase_id uuid NOT NULL,
    location_id uuid NOT NULL,
    receipt_number varchar(40) NOT NULL,
    received_at timestamptz NOT NULL DEFAULT now(),
    reference_number varchar(100),
    status varchar(30) NOT NULL DEFAULT 'COMPLETED'
        CHECK (status IN ('COMPLETED', 'CANCELLED')),
    notes text,
    created_by uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (business_id, receipt_number),
    UNIQUE (business_id, id),
    FOREIGN KEY (business_id, purchase_id) REFERENCES purchases(business_id, id) ON DELETE CASCADE,
    FOREIGN KEY (business_id, location_id) REFERENCES locations(business_id, id)
);

CREATE TABLE purchase_receipt_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    purchase_receipt_id uuid NOT NULL,
    purchase_item_id uuid NOT NULL,
    product_id uuid NOT NULL,
    line_number integer NOT NULL CHECK (line_number > 0),
    received_quantity numeric(18,4) NOT NULL CHECK (received_quantity > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (business_id, id),
    FOREIGN KEY (business_id, purchase_receipt_id) REFERENCES purchase_receipts(business_id, id) ON DELETE CASCADE,
    FOREIGN KEY (business_id, purchase_item_id) REFERENCES purchase_items(business_id, id),
    FOREIGN KEY (business_id, product_id) REFERENCES products(business_id, id)
);

CREATE INDEX purchase_receipts_business_purchase_idx
    ON purchase_receipts (business_id, purchase_id);

-- ---------------------------------------------------------------------------
-- 4. stock_movements: explicit nullable sources with composite tenant-aware
-- FKs. Legacy `reference_id` rows stay valid; new rows must reference
-- exactly one source, with SALE* -> sale_id and PURCHASE -> receipt.
-- ---------------------------------------------------------------------------
ALTER TABLE stock_movements
    ADD COLUMN sale_id uuid,
    ADD COLUMN purchase_receipt_id uuid,
    ADD COLUMN stock_adjustment_id uuid;

-- Legacy `reference_id` (ex stock_adjustment_id) becomes nullable so new
-- code can write typed sources; it stays populated for old rows and is
-- deprecated for new writes.
ALTER TABLE stock_movements ALTER COLUMN reference_id DROP NOT NULL;

ALTER TABLE stock_movements
    ADD CONSTRAINT stock_movements_business_sale_id_fkey
        FOREIGN KEY (business_id, sale_id) REFERENCES sales(business_id, id),
    ADD CONSTRAINT stock_movements_business_purchase_receipt_id_fkey
        FOREIGN KEY (business_id, purchase_receipt_id) REFERENCES purchase_receipts(business_id, id),
    ADD CONSTRAINT stock_movements_business_stock_adjustment_id_fkey
        FOREIGN KEY (business_id, stock_adjustment_id) REFERENCES stock_adjustments(business_id, id);

ALTER TABLE stock_movements ADD CONSTRAINT stock_movements_single_source_check CHECK (
    (reference_id IS NOT NULL AND sale_id IS NULL AND purchase_receipt_id IS NULL AND stock_adjustment_id IS NULL)
    OR
    (reference_id IS NULL AND num_nonnulls(sale_id, purchase_receipt_id, stock_adjustment_id) = 1)
);

ALTER TABLE stock_movements ADD CONSTRAINT stock_movements_source_type_check CHECK (
    reference_id IS NOT NULL
    OR (movement_type IN ('SALE', 'SALE_VOID') AND sale_id IS NOT NULL)
    OR (movement_type = 'PURCHASE' AND purchase_receipt_id IS NOT NULL)
    OR (movement_type IN ('ADJUSTMENT', 'SPOILAGE', 'DAMAGE', 'OPENING_BALANCE', 'TRANSFER', 'RETURN') AND stock_adjustment_id IS NOT NULL)
);

CREATE INDEX stock_movements_business_sale_idx
    ON stock_movements (business_id, sale_id) WHERE sale_id IS NOT NULL;
CREATE INDEX stock_movements_business_receipt_idx
    ON stock_movements (business_id, purchase_receipt_id) WHERE purchase_receipt_id IS NOT NULL;

-- ---------------------------------------------------------------------------
-- 5. Idempotency records, tenant-scoped per contract.
-- ---------------------------------------------------------------------------
CREATE TABLE idempotency_keys (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    actor_user_id uuid REFERENCES users(id) ON DELETE SET NULL,
    operation varchar(100) NOT NULL,
    route varchar(200) NOT NULL,
    idempotency_key uuid NOT NULL,
    request_hash char(64) NOT NULL,
    response_status integer NOT NULL,
    response_body jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (business_id, idempotency_key)
);

CREATE INDEX idempotency_keys_business_actor_idx
    ON idempotency_keys (business_id, actor_user_id, operation);

-- ---------------------------------------------------------------------------
-- 6. Payment method master with default CASH per business.
-- ---------------------------------------------------------------------------
CREATE TABLE payment_methods (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    public_code varchar(32) NOT NULL,
    name varchar(100) NOT NULL,
    method_type varchar(30) NOT NULL
        CHECK (method_type IN ('CASH', 'TRANSFER', 'DEBIT_CARD', 'CREDIT_CARD', 'EWALLET')),
    status varchar(30) NOT NULL DEFAULT 'ACTIVE'
        CHECK (status IN ('ACTIVE', 'INACTIVE')),
    is_default boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (business_id, public_code),
    UNIQUE (business_id, id)
);

CREATE UNIQUE INDEX payment_methods_one_default_per_business_idx
    ON payment_methods (business_id) WHERE is_default;

INSERT INTO payment_methods (business_id, public_code, name, method_type, is_default)
SELECT b.id, 'CASH', 'Tunai', 'CASH', true
FROM businesses b
WHERE NOT EXISTS (
    SELECT 1 FROM payment_methods pm WHERE pm.business_id = b.id AND pm.is_default
);

-- ---------------------------------------------------------------------------
-- 7. Missing sequences for existing businesses; new-business onboarding
-- creates the same set (see CreateBusiness repository).
-- ---------------------------------------------------------------------------
INSERT INTO number_sequences (business_id, sequence_type, prefix)
SELECT b.id, sequence_defaults.sequence_type, sequence_defaults.prefix
FROM businesses b
CROSS JOIN (VALUES ('PURCHASE_RECEIPT', 'RCV')) AS sequence_defaults(sequence_type, prefix)
ON CONFLICT (business_id, sequence_type) DO NOTHING;

-- ---------------------------------------------------------------------------
-- 8. Inventory snapshot must never go negative for tracked products.
-- NOT VALID keeps legacy experimental rows deployable; application code
-- already guards decrements and a VALIDATE step follows in T12.
-- ---------------------------------------------------------------------------
ALTER TABLE product_inventory
    ADD CONSTRAINT product_inventory_quantity_nonnegative_check
    CHECK (quantity >= 0) NOT VALID;

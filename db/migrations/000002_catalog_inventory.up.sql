CREATE TABLE products (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    public_code varchar(32) NOT NULL,
    base_unit_id uuid NOT NULL,
    name varchar(150) NOT NULL,
    sku varchar(100),
    barcode varchar(100),
    default_purchase_price numeric(18,2) NOT NULL DEFAULT 0 CHECK (default_purchase_price >= 0),
    default_selling_price numeric(18,2) NOT NULL DEFAULT 0 CHECK (default_selling_price >= 0),
    min_stock numeric(18,4) NOT NULL DEFAULT 0 CHECK (min_stock >= 0),
    is_stock_tracked boolean NOT NULL DEFAULT true,
    status varchar(30) NOT NULL DEFAULT 'ACTIVE'
        CHECK (status IN ('ACTIVE', 'INACTIVE')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (business_id, public_code),
    UNIQUE (business_id, id),
    FOREIGN KEY (business_id, base_unit_id) REFERENCES units(business_id, id)
);

CREATE UNIQUE INDEX products_business_sku_key
    ON products (business_id, sku) WHERE sku IS NOT NULL;
CREATE UNIQUE INDEX products_business_barcode_key
    ON products (business_id, barcode) WHERE barcode IS NOT NULL;
CREATE INDEX products_business_name_idx ON products (business_id, name);

CREATE TABLE stock_adjustments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    location_id uuid NOT NULL,
    adjustment_number varchar(40) NOT NULL,
    adjustment_date timestamptz NOT NULL DEFAULT now(),
    reason varchar(50) NOT NULL DEFAULT 'OPENING_BALANCE'
        CHECK (reason IN ('CORRECTION', 'SPOILAGE', 'DAMAGE', 'LOST', 'OPENING_BALANCE', 'OTHER')),
    status varchar(30) NOT NULL DEFAULT 'COMPLETED'
        CHECK (status IN ('DRAFT', 'COMPLETED', 'CANCELLED')),
    notes text,
    created_by uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (business_id, adjustment_number),
    UNIQUE (business_id, id),
    FOREIGN KEY (business_id, location_id) REFERENCES locations(business_id, id)
);

CREATE TABLE stock_adjustment_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    stock_adjustment_id uuid NOT NULL,
    product_id uuid NOT NULL,
    quantity numeric(18,4) NOT NULL CHECK (quantity > 0),
    unit_id uuid NOT NULL,
    base_quantity numeric(18,4) NOT NULL CHECK (base_quantity > 0),
    base_unit_id uuid NOT NULL,
    direction varchar(10) NOT NULL CHECK (direction IN ('IN', 'OUT')),
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (business_id, stock_adjustment_id) REFERENCES stock_adjustments(business_id, id) ON DELETE CASCADE,
    FOREIGN KEY (business_id, product_id) REFERENCES products(business_id, id),
    FOREIGN KEY (business_id, unit_id) REFERENCES units(business_id, id),
    FOREIGN KEY (business_id, base_unit_id) REFERENCES units(business_id, id)
);

CREATE TABLE stock_movements (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    product_id uuid NOT NULL,
    location_id uuid NOT NULL,
    movement_type varchar(50) NOT NULL
        CHECK (movement_type IN ('PURCHASE', 'SALE', 'ADJUSTMENT', 'SPOILAGE', 'DAMAGE', 'TRANSFER', 'RETURN', 'OPENING_BALANCE')),
    direction varchar(10) NOT NULL CHECK (direction IN ('IN', 'OUT')),
    quantity numeric(18,4) NOT NULL CHECK (quantity > 0),
    unit_id uuid NOT NULL REFERENCES units(id),
    base_quantity numeric(18,4) NOT NULL CHECK (base_quantity > 0),
    base_unit_id uuid NOT NULL REFERENCES units(id),
    stock_adjustment_id uuid NOT NULL,
    reason text,
    occurred_at timestamptz NOT NULL DEFAULT now(),
    created_by uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (business_id, product_id) REFERENCES products(business_id, id),
    FOREIGN KEY (business_id, location_id) REFERENCES locations(business_id, id),
    FOREIGN KEY (business_id, unit_id) REFERENCES units(business_id, id),
    FOREIGN KEY (business_id, base_unit_id) REFERENCES units(business_id, id),
    FOREIGN KEY (business_id, stock_adjustment_id) REFERENCES stock_adjustments(business_id, id)
);

CREATE INDEX stock_movements_business_product_idx
    ON stock_movements (business_id, product_id, occurred_at DESC);
CREATE INDEX stock_movements_product_location_idx
    ON stock_movements (product_id, location_id);
CREATE UNIQUE INDEX stock_movements_one_opening_balance_idx
    ON stock_movements (business_id, product_id, location_id)
    WHERE movement_type = 'OPENING_BALANCE';

-- This table is a rebuildable read snapshot. stock_movements remains the ledger/source of truth.
CREATE TABLE product_inventory (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    product_id uuid NOT NULL,
    location_id uuid NOT NULL,
    quantity numeric(18,4) NOT NULL DEFAULT 0,
    base_unit_id uuid NOT NULL REFERENCES units(id),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (business_id, product_id, location_id),
    FOREIGN KEY (business_id, product_id) REFERENCES products(business_id, id) ON DELETE CASCADE,
    FOREIGN KEY (business_id, location_id) REFERENCES locations(business_id, id) ON DELETE CASCADE,
    FOREIGN KEY (business_id, base_unit_id) REFERENCES units(business_id, id)
);

CREATE INDEX product_inventory_business_location_idx
    ON product_inventory (business_id, location_id);

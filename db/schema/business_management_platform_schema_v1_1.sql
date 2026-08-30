-- ============================================================
-- Business Management Platform for UMKM
-- PostgreSQL Schema v1.1
-- ============================================================
-- Backend direction : Go modular monolith
-- Frontend direction: React + TypeScript
-- Deployment        : Docker Compose for MVP / pilot
-- API strategy      : REST + JSON external, gRPC reserved for future internal services
-- Security strategy : httpOnly cookie, same-origin /api proxy, UUID internal only
--
-- Design principles:
--   - UUID primary keys are internal identifiers.
--   - Frontend URLs should use public_code or transaction_number, not UUID.
--   - All business data is scoped by business_id.
--   - Financial and inventory references use explicit nullable foreign keys.
--   - Audit log uses polymorphic entity_type/entity_id.
--   - Source of truth for inventory is stock_movements.
--   - Source of truth for cash movement is payments.
--   - product_inventory.quantity and cash_accounts.current_balance are snapshots.
-- ============================================================


-- ============================================================
-- 0. Extensions
-- ============================================================

CREATE EXTENSION IF NOT EXISTS pgcrypto;


-- ============================================================
-- 1. Foundation / Tenant
-- ============================================================

CREATE TABLE users (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),

    name varchar(150) NOT NULL,
    email varchar(150) UNIQUE,
    phone varchar(50),
    password varchar(255) NOT NULL,

    status varchar(30) NOT NULL DEFAULT 'ACTIVE'
        CHECK (status IN ('ACTIVE', 'INACTIVE', 'SUSPENDED')),

    created_at timestamp NOT NULL DEFAULT now(),
    updated_at timestamp NOT NULL DEFAULT now()
);


CREATE TABLE businesses (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),

    name varchar(150) NOT NULL,

    business_type varchar(50) NOT NULL DEFAULT 'OTHER'
        CHECK (business_type IN ('RETAIL', 'SERVICE', 'ENTERTAINMENT', 'OTHER')),

    status varchar(30) NOT NULL DEFAULT 'ACTIVE'
        CHECK (status IN ('ACTIVE', 'INACTIVE')),

    timezone varchar(100) NOT NULL DEFAULT 'Asia/Jakarta',
    currency char(3) NOT NULL DEFAULT 'IDR',

    created_at timestamp NOT NULL DEFAULT now(),
    updated_at timestamp NOT NULL DEFAULT now()
);


CREATE TABLE roles (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),

    business_id uuid NULL REFERENCES businesses(id) ON DELETE CASCADE,

    name varchar(100) NOT NULL,
    code varchar(50) NOT NULL,
    is_system_role boolean NOT NULL DEFAULT false,

    created_at timestamp NOT NULL DEFAULT now(),
    updated_at timestamp NOT NULL DEFAULT now(),

    UNIQUE (business_id, code)
);


CREATE TABLE business_members (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),

    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id uuid NOT NULL REFERENCES roles(id),

    status varchar(30) NOT NULL DEFAULT 'ACTIVE'
        CHECK (status IN ('ACTIVE', 'INACTIVE', 'INVITED')),

    joined_at timestamp NULL,

    created_at timestamp NOT NULL DEFAULT now(),
    updated_at timestamp NOT NULL DEFAULT now(),

    UNIQUE (business_id, user_id)
);


CREATE TABLE business_modules (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),

    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,

    module_code varchar(50) NOT NULL,
    is_enabled boolean NOT NULL DEFAULT true,

    created_at timestamp NOT NULL DEFAULT now(),
    updated_at timestamp NOT NULL DEFAULT now(),

    UNIQUE (business_id, module_code)
);


CREATE TABLE locations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),

    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,

    name varchar(150) NOT NULL,

    type varchar(50) NOT NULL DEFAULT 'OTHER'
        CHECK (type IN ('STORE', 'WAREHOUSE', 'BOOTH', 'EVENT_VENUE', 'OTHER')),

    address text,
    is_default boolean NOT NULL DEFAULT false,

    status varchar(30) NOT NULL DEFAULT 'ACTIVE'
        CHECK (status IN ('ACTIVE', 'INACTIVE')),

    created_at timestamp NOT NULL DEFAULT now(),
    updated_at timestamp NOT NULL DEFAULT now()
);

CREATE INDEX idx_locations_business_id
    ON locations(business_id);

CREATE UNIQUE INDEX uniq_default_location_per_business
    ON locations(business_id)
    WHERE is_default = true;


-- ============================================================
-- 2. Party
-- ============================================================

CREATE TABLE parties (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),

    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,

    -- User-facing identifier. Use this in URL/API path instead of UUID.
    public_code varchar(100),

    party_type varchar(30) NOT NULL
        CHECK (party_type IN ('PERSON', 'ORGANIZATION')),

    display_name varchar(150) NOT NULL,
    legal_name varchar(150),

    phone varchar(50),
    email varchar(150),

    status varchar(30) NOT NULL DEFAULT 'ACTIVE'
        CHECK (status IN ('ACTIVE', 'INACTIVE')),

    notes text,

    created_at timestamp NOT NULL DEFAULT now(),
    updated_at timestamp NOT NULL DEFAULT now()
);

CREATE INDEX idx_parties_business_id
    ON parties(business_id);

CREATE INDEX idx_parties_display_name
    ON parties(display_name);

CREATE UNIQUE INDEX uniq_parties_business_public_code
    ON parties(business_id, public_code)
    WHERE public_code IS NOT NULL;


CREATE TABLE party_roles (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),

    party_id uuid NOT NULL REFERENCES parties(id) ON DELETE CASCADE,

    role_type varchar(50) NOT NULL
        CHECK (role_type IN ('CUSTOMER', 'SUPPLIER', 'PARTNER', 'EMPLOYEE', 'OTHER')),

    created_at timestamp NOT NULL DEFAULT now(),

    UNIQUE (party_id, role_type)
);


CREATE TABLE party_contacts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),

    party_id uuid NOT NULL REFERENCES parties(id) ON DELETE CASCADE,

    contact_type varchar(30) NOT NULL
        CHECK (contact_type IN ('PHONE', 'WHATSAPP', 'EMAIL', 'OTHER')),

    label varchar(100),
    value varchar(150) NOT NULL,
    is_primary boolean NOT NULL DEFAULT false,

    created_at timestamp NOT NULL DEFAULT now(),
    updated_at timestamp NOT NULL DEFAULT now()
);

CREATE INDEX idx_party_contacts_party_id
    ON party_contacts(party_id);


CREATE TABLE party_addresses (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),

    party_id uuid NOT NULL REFERENCES parties(id) ON DELETE CASCADE,

    address_type varchar(50) NOT NULL DEFAULT 'OTHER'
        CHECK (address_type IN ('BILLING', 'SHIPPING', 'OFFICE', 'WAREHOUSE', 'OTHER')),

    label varchar(100),
    address text NOT NULL,
    city varchar(100),
    province varchar(100),
    postal_code varchar(20),
    is_primary boolean NOT NULL DEFAULT false,

    created_at timestamp NOT NULL DEFAULT now(),
    updated_at timestamp NOT NULL DEFAULT now()
);

CREATE INDEX idx_party_addresses_party_id
    ON party_addresses(party_id);


-- ============================================================
-- 3. Resource: Category, Unit, Product, Service, Asset
-- ============================================================

CREATE TABLE categories (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),

    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    parent_id uuid NULL REFERENCES categories(id) ON DELETE SET NULL,

    category_type varchar(30) NOT NULL
        CHECK (category_type IN ('PRODUCT', 'SERVICE', 'ASSET', 'EXPENSE')),

    name varchar(150) NOT NULL,

    status varchar(30) NOT NULL DEFAULT 'ACTIVE'
        CHECK (status IN ('ACTIVE', 'INACTIVE')),

    created_at timestamp NOT NULL DEFAULT now(),
    updated_at timestamp NOT NULL DEFAULT now(),

    UNIQUE (business_id, category_type, name)
);

CREATE INDEX idx_categories_business_type
    ON categories(business_id, category_type);


CREATE TABLE units (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),

    business_id uuid NULL REFERENCES businesses(id) ON DELETE CASCADE,

    name varchar(100) NOT NULL,
    symbol varchar(30) NOT NULL,

    unit_type varchar(30) NOT NULL DEFAULT 'COUNT'
        CHECK (unit_type IN ('COUNT', 'WEIGHT', 'VOLUME', 'TIME', 'OTHER')),

    created_at timestamp NOT NULL DEFAULT now(),
    updated_at timestamp NOT NULL DEFAULT now(),

    UNIQUE (business_id, symbol)
);


CREATE TABLE products (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),

    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    category_id uuid NULL REFERENCES categories(id) ON DELETE SET NULL,
    base_unit_id uuid NOT NULL REFERENCES units(id),

    -- User-facing identifier. Use this in URL/API path instead of UUID.
    public_code varchar(100),

    name varchar(150) NOT NULL,
    sku varchar(100),
    barcode varchar(100),

    default_purchase_price numeric(18,2) NOT NULL DEFAULT 0,
    default_selling_price numeric(18,2) NOT NULL DEFAULT 0,

    min_stock numeric(18,4) NOT NULL DEFAULT 0,
    is_stock_tracked boolean NOT NULL DEFAULT true,

    status varchar(30) NOT NULL DEFAULT 'ACTIVE'
        CHECK (status IN ('ACTIVE', 'INACTIVE')),

    created_at timestamp NOT NULL DEFAULT now(),
    updated_at timestamp NOT NULL DEFAULT now()
);

CREATE INDEX idx_products_business_id
    ON products(business_id);

CREATE INDEX idx_products_name
    ON products(name);

CREATE UNIQUE INDEX uniq_products_business_public_code
    ON products(business_id, public_code)
    WHERE public_code IS NOT NULL;

CREATE UNIQUE INDEX uniq_products_business_sku
    ON products(business_id, sku)
    WHERE sku IS NOT NULL;

CREATE UNIQUE INDEX uniq_products_business_barcode
    ON products(business_id, barcode)
    WHERE barcode IS NOT NULL;


CREATE TABLE unit_conversions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),

    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    product_id uuid NULL REFERENCES products(id) ON DELETE CASCADE,

    from_unit_id uuid NOT NULL REFERENCES units(id),
    to_unit_id uuid NOT NULL REFERENCES units(id),

    multiplier numeric(18,6) NOT NULL CHECK (multiplier > 0),

    created_at timestamp NOT NULL DEFAULT now(),
    updated_at timestamp NOT NULL DEFAULT now(),

    UNIQUE (business_id, product_id, from_unit_id, to_unit_id),
    CHECK (from_unit_id <> to_unit_id)
);


CREATE TABLE services (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),

    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    category_id uuid NULL REFERENCES categories(id) ON DELETE SET NULL,

    -- User-facing identifier. Use this in URL/API path instead of UUID.
    public_code varchar(100),

    name varchar(150) NOT NULL,
    description text,

    default_price numeric(18,2) NOT NULL DEFAULT 0,
    default_duration_minutes integer NULL CHECK (
        default_duration_minutes IS NULL OR default_duration_minutes > 0
    ),

    status varchar(30) NOT NULL DEFAULT 'ACTIVE'
        CHECK (status IN ('ACTIVE', 'INACTIVE')),

    created_at timestamp NOT NULL DEFAULT now(),
    updated_at timestamp NOT NULL DEFAULT now()
);

CREATE INDEX idx_services_business_id
    ON services(business_id);

CREATE UNIQUE INDEX uniq_services_business_public_code
    ON services(business_id, public_code)
    WHERE public_code IS NOT NULL;


CREATE TABLE assets (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),

    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    category_id uuid NULL REFERENCES categories(id) ON DELETE SET NULL,
    location_id uuid NULL REFERENCES locations(id) ON DELETE SET NULL,

    -- User-facing identifier. Use this in URL/API path instead of UUID.
    public_code varchar(100),

    name varchar(150) NOT NULL,
    asset_code varchar(100),

    purchase_date date NULL,
    purchase_value numeric(18,2) NULL,

    condition varchar(30) NOT NULL DEFAULT 'UNKNOWN'
        CHECK (condition IN ('GOOD', 'FAIR', 'DAMAGED', 'UNKNOWN')),

    status varchar(30) NOT NULL DEFAULT 'ACTIVE'
        CHECK (status IN ('ACTIVE', 'MAINTENANCE', 'RETIRED', 'LOST', 'SOLD')),

    notes text,

    created_at timestamp NOT NULL DEFAULT now(),
    updated_at timestamp NOT NULL DEFAULT now()
);

CREATE INDEX idx_assets_business_id
    ON assets(business_id);

CREATE UNIQUE INDEX uniq_assets_business_public_code
    ON assets(business_id, public_code)
    WHERE public_code IS NOT NULL;

CREATE UNIQUE INDEX uniq_assets_business_asset_code
    ON assets(business_id, asset_code)
    WHERE asset_code IS NOT NULL;


-- ============================================================
-- 4. Booking / Service
-- ============================================================

CREATE TABLE bookings (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),

    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,

    -- User-facing transaction identifier.
    booking_number varchar(100) NOT NULL,

    client_party_id uuid NULL REFERENCES parties(id) ON DELETE SET NULL,
    service_id uuid NULL REFERENCES services(id) ON DELETE SET NULL,

    performer_member_id uuid NULL REFERENCES business_members(id) ON DELETE SET NULL,
    managed_by_member_id uuid NULL REFERENCES business_members(id) ON DELETE SET NULL,

    location_id uuid NULL REFERENCES locations(id) ON DELETE SET NULL,

    title varchar(200) NOT NULL,
    description text,

    event_location_text text,

    start_at timestamp NULL,
    end_at timestamp NULL,

    status varchar(30) NOT NULL DEFAULT 'INQUIRY'
        CHECK (status IN ('INQUIRY', 'NEGOTIATING', 'CONFIRMED', 'COMPLETED', 'CANCELLED')),

    agreed_price numeric(18,2) NOT NULL DEFAULT 0,

    notes text,

    created_by uuid NULL REFERENCES users(id) ON DELETE SET NULL,

    created_at timestamp NOT NULL DEFAULT now(),
    updated_at timestamp NOT NULL DEFAULT now(),

    UNIQUE (business_id, booking_number),

    CHECK (
        start_at IS NULL
        OR end_at IS NULL
        OR end_at > start_at
    )
);

CREATE INDEX idx_bookings_business_start
    ON bookings(business_id, start_at);

CREATE INDEX idx_bookings_client
    ON bookings(client_party_id);

CREATE INDEX idx_bookings_status
    ON bookings(status);


-- ============================================================
-- 5. Sales / POS
-- ============================================================

CREATE TABLE sales (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),

    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    location_id uuid NOT NULL REFERENCES locations(id),
    customer_party_id uuid NULL REFERENCES parties(id) ON DELETE SET NULL,

    -- User-facing transaction identifier.
    sale_number varchar(100) NOT NULL,
    sale_date timestamp NOT NULL DEFAULT now(),

    status varchar(30) NOT NULL DEFAULT 'DRAFT'
        CHECK (status IN ('DRAFT', 'COMPLETED', 'VOID', 'PARTIALLY_REFUNDED', 'REFUNDED')),

    subtotal numeric(18,2) NOT NULL DEFAULT 0,
    discount_total numeric(18,2) NOT NULL DEFAULT 0,
    tax_total numeric(18,2) NOT NULL DEFAULT 0,
    grand_total numeric(18,2) NOT NULL DEFAULT 0,

    notes text,

    created_by uuid NULL REFERENCES users(id) ON DELETE SET NULL,

    created_at timestamp NOT NULL DEFAULT now(),
    updated_at timestamp NOT NULL DEFAULT now(),

    UNIQUE (business_id, sale_number)
);

CREATE INDEX idx_sales_business_date
    ON sales(business_id, sale_date);

CREATE INDEX idx_sales_customer
    ON sales(customer_party_id);


CREATE TABLE sale_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),

    sale_id uuid NOT NULL REFERENCES sales(id) ON DELETE CASCADE,
    product_id uuid NOT NULL REFERENCES products(id),

    quantity numeric(18,4) NOT NULL CHECK (quantity > 0),
    unit_id uuid NOT NULL REFERENCES units(id),

    base_quantity numeric(18,4) NOT NULL CHECK (base_quantity > 0),
    base_unit_id uuid NOT NULL REFERENCES units(id),

    unit_price numeric(18,2) NOT NULL DEFAULT 0,
    cost_price numeric(18,2) NOT NULL DEFAULT 0,

    discount_amount numeric(18,2) NOT NULL DEFAULT 0,
    line_total numeric(18,2) NOT NULL DEFAULT 0,

    created_at timestamp NOT NULL DEFAULT now(),
    updated_at timestamp NOT NULL DEFAULT now()
);

CREATE INDEX idx_sale_items_sale_id
    ON sale_items(sale_id);

CREATE INDEX idx_sale_items_product_id
    ON sale_items(product_id);


-- ============================================================
-- 6. Purchase
-- ============================================================

CREATE TABLE purchases (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),

    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    supplier_party_id uuid NULL REFERENCES parties(id) ON DELETE SET NULL,
    location_id uuid NOT NULL REFERENCES locations(id),

    -- User-facing transaction identifier.
    purchase_number varchar(100) NOT NULL,
    purchase_date timestamp NOT NULL DEFAULT now(),

    status varchar(30) NOT NULL DEFAULT 'DRAFT'
        CHECK (status IN ('DRAFT', 'ORDERED', 'PARTIALLY_RECEIVED', 'RECEIVED', 'COMPLETED', 'CANCELLED')),

    subtotal numeric(18,2) NOT NULL DEFAULT 0,
    discount_total numeric(18,2) NOT NULL DEFAULT 0,
    additional_cost numeric(18,2) NOT NULL DEFAULT 0,
    grand_total numeric(18,2) NOT NULL DEFAULT 0,

    notes text,

    created_by uuid NULL REFERENCES users(id) ON DELETE SET NULL,

    created_at timestamp NOT NULL DEFAULT now(),
    updated_at timestamp NOT NULL DEFAULT now(),

    UNIQUE (business_id, purchase_number)
);

CREATE INDEX idx_purchases_business_date
    ON purchases(business_id, purchase_date);

CREATE INDEX idx_purchases_supplier
    ON purchases(supplier_party_id);


CREATE TABLE purchase_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),

    purchase_id uuid NOT NULL REFERENCES purchases(id) ON DELETE CASCADE,
    product_id uuid NOT NULL REFERENCES products(id),

    quantity numeric(18,4) NOT NULL CHECK (quantity > 0),
    unit_id uuid NOT NULL REFERENCES units(id),

    base_quantity numeric(18,4) NOT NULL CHECK (base_quantity > 0),
    base_unit_id uuid NOT NULL REFERENCES units(id),

    unit_cost numeric(18,2) NOT NULL DEFAULT 0,
    line_total numeric(18,2) NOT NULL DEFAULT 0,

    received_quantity numeric(18,4) NOT NULL DEFAULT 0 CHECK (received_quantity >= 0),

    created_at timestamp NOT NULL DEFAULT now(),
    updated_at timestamp NOT NULL DEFAULT now()
);

CREATE INDEX idx_purchase_items_purchase_id
    ON purchase_items(purchase_id);

CREATE INDEX idx_purchase_items_product_id
    ON purchase_items(product_id);


-- ============================================================
-- 7. Inventory
-- ============================================================

CREATE TABLE product_inventory (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),

    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    product_id uuid NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    location_id uuid NOT NULL REFERENCES locations(id) ON DELETE CASCADE,

    quantity numeric(18,4) NOT NULL DEFAULT 0,
    base_unit_id uuid NOT NULL REFERENCES units(id),

    updated_at timestamp NOT NULL DEFAULT now(),

    UNIQUE (business_id, product_id, location_id)
);

CREATE INDEX idx_product_inventory_product
    ON product_inventory(product_id);

CREATE INDEX idx_product_inventory_location
    ON product_inventory(location_id);


CREATE TABLE stock_adjustments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),

    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    location_id uuid NOT NULL REFERENCES locations(id),

    -- User-facing transaction identifier.
    adjustment_number varchar(100) NOT NULL,
    adjustment_date timestamp NOT NULL DEFAULT now(),

    reason varchar(50) NOT NULL DEFAULT 'CORRECTION'
        CHECK (reason IN ('CORRECTION', 'SPOILAGE', 'DAMAGE', 'LOST', 'OPENING_BALANCE', 'OTHER')),

    status varchar(30) NOT NULL DEFAULT 'DRAFT'
        CHECK (status IN ('DRAFT', 'COMPLETED', 'CANCELLED')),

    notes text,

    created_by uuid NULL REFERENCES users(id) ON DELETE SET NULL,

    created_at timestamp NOT NULL DEFAULT now(),
    updated_at timestamp NOT NULL DEFAULT now(),

    UNIQUE (business_id, adjustment_number)
);


CREATE TABLE stock_adjustment_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),

    stock_adjustment_id uuid NOT NULL REFERENCES stock_adjustments(id) ON DELETE CASCADE,
    product_id uuid NOT NULL REFERENCES products(id),

    quantity numeric(18,4) NOT NULL CHECK (quantity > 0),
    unit_id uuid NOT NULL REFERENCES units(id),

    base_quantity numeric(18,4) NOT NULL CHECK (base_quantity > 0),
    base_unit_id uuid NOT NULL REFERENCES units(id),

    direction varchar(10) NOT NULL CHECK (direction IN ('IN', 'OUT')),

    notes text,

    created_at timestamp NOT NULL DEFAULT now(),
    updated_at timestamp NOT NULL DEFAULT now()
);

CREATE INDEX idx_stock_adjustment_items_adjustment
    ON stock_adjustment_items(stock_adjustment_id);


CREATE TABLE stock_opnames (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),

    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    location_id uuid NOT NULL REFERENCES locations(id),

    -- User-facing transaction identifier.
    opname_number varchar(100) NOT NULL,
    opname_date timestamp NOT NULL DEFAULT now(),

    status varchar(30) NOT NULL DEFAULT 'DRAFT'
        CHECK (status IN ('DRAFT', 'COMPLETED', 'CANCELLED')),

    notes text,

    created_by uuid NULL REFERENCES users(id) ON DELETE SET NULL,

    created_at timestamp NOT NULL DEFAULT now(),
    updated_at timestamp NOT NULL DEFAULT now(),

    UNIQUE (business_id, opname_number)
);


CREATE TABLE stock_opname_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),

    stock_opname_id uuid NOT NULL REFERENCES stock_opnames(id) ON DELETE CASCADE,
    product_id uuid NOT NULL REFERENCES products(id),

    system_quantity numeric(18,4) NOT NULL DEFAULT 0,
    actual_quantity numeric(18,4) NOT NULL DEFAULT 0,
    difference_quantity numeric(18,4) NOT NULL DEFAULT 0,

    base_unit_id uuid NOT NULL REFERENCES units(id),

    reason text,

    created_at timestamp NOT NULL DEFAULT now(),
    updated_at timestamp NOT NULL DEFAULT now()
);

CREATE INDEX idx_stock_opname_items_opname
    ON stock_opname_items(stock_opname_id);


CREATE TABLE stock_transfers (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),

    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,

    -- User-facing transaction identifier.
    transfer_number varchar(100) NOT NULL,

    from_location_id uuid NOT NULL REFERENCES locations(id),
    to_location_id uuid NOT NULL REFERENCES locations(id),

    transfer_date timestamp NOT NULL DEFAULT now(),

    status varchar(30) NOT NULL DEFAULT 'DRAFT'
        CHECK (status IN ('DRAFT', 'IN_TRANSIT', 'COMPLETED', 'CANCELLED')),

    notes text,

    created_by uuid NULL REFERENCES users(id) ON DELETE SET NULL,

    created_at timestamp NOT NULL DEFAULT now(),
    updated_at timestamp NOT NULL DEFAULT now(),

    UNIQUE (business_id, transfer_number),

    CHECK (from_location_id <> to_location_id)
);


CREATE TABLE stock_transfer_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),

    stock_transfer_id uuid NOT NULL REFERENCES stock_transfers(id) ON DELETE CASCADE,
    product_id uuid NOT NULL REFERENCES products(id),

    quantity numeric(18,4) NOT NULL CHECK (quantity > 0),
    unit_id uuid NOT NULL REFERENCES units(id),

    base_quantity numeric(18,4) NOT NULL CHECK (base_quantity > 0),
    base_unit_id uuid NOT NULL REFERENCES units(id),

    created_at timestamp NOT NULL DEFAULT now(),
    updated_at timestamp NOT NULL DEFAULT now()
);

CREATE INDEX idx_stock_transfer_items_transfer
    ON stock_transfer_items(stock_transfer_id);


CREATE TABLE stock_movements (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),

    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    product_id uuid NOT NULL REFERENCES products(id),
    location_id uuid NOT NULL REFERENCES locations(id),

    movement_type varchar(50) NOT NULL
        CHECK (movement_type IN (
            'PURCHASE',
            'SALE',
            'ADJUSTMENT',
            'SPOILAGE',
            'DAMAGE',
            'TRANSFER',
            'RETURN',
            'OPENING_BALANCE'
        )),

    direction varchar(10) NOT NULL CHECK (direction IN ('IN', 'OUT')),

    quantity numeric(18,4) NOT NULL CHECK (quantity > 0),
    unit_id uuid NOT NULL REFERENCES units(id),

    base_quantity numeric(18,4) NOT NULL CHECK (base_quantity > 0),
    base_unit_id uuid NOT NULL REFERENCES units(id),

    sale_id uuid NULL REFERENCES sales(id) ON DELETE SET NULL,
    purchase_id uuid NULL REFERENCES purchases(id) ON DELETE SET NULL,
    stock_adjustment_id uuid NULL REFERENCES stock_adjustments(id) ON DELETE SET NULL,
    stock_transfer_id uuid NULL REFERENCES stock_transfers(id) ON DELETE SET NULL,
    stock_opname_id uuid NULL REFERENCES stock_opnames(id) ON DELETE SET NULL,

    reason text,

    occurred_at timestamp NOT NULL DEFAULT now(),

    created_by uuid NULL REFERENCES users(id) ON DELETE SET NULL,
    created_at timestamp NOT NULL DEFAULT now(),

    CHECK (
        sale_id IS NOT NULL
        OR purchase_id IS NOT NULL
        OR stock_adjustment_id IS NOT NULL
        OR stock_transfer_id IS NOT NULL
        OR stock_opname_id IS NOT NULL
    )
);

CREATE INDEX idx_stock_movements_business_product
    ON stock_movements(business_id, product_id);

CREATE INDEX idx_stock_movements_product_location
    ON stock_movements(product_id, location_id);

CREATE INDEX idx_stock_movements_occurred_at
    ON stock_movements(occurred_at);

CREATE INDEX idx_stock_movements_sale
    ON stock_movements(sale_id);

CREATE INDEX idx_stock_movements_purchase
    ON stock_movements(purchase_id);

CREATE INDEX idx_stock_movements_adjustment
    ON stock_movements(stock_adjustment_id);

CREATE INDEX idx_stock_movements_transfer
    ON stock_movements(stock_transfer_id);

CREATE INDEX idx_stock_movements_opname
    ON stock_movements(stock_opname_id);


-- ============================================================
-- 8. Finance & Document
-- ============================================================

CREATE TABLE payment_methods (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),

    business_id uuid NULL REFERENCES businesses(id) ON DELETE CASCADE,

    name varchar(100) NOT NULL,
    code varchar(50) NOT NULL,

    is_active boolean NOT NULL DEFAULT true,

    created_at timestamp NOT NULL DEFAULT now(),
    updated_at timestamp NOT NULL DEFAULT now(),

    UNIQUE (business_id, code)
);


CREATE TABLE cash_accounts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),

    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,

    name varchar(150) NOT NULL,

    account_type varchar(30) NOT NULL
        CHECK (account_type IN ('CASH', 'BANK', 'EWALLET', 'QRIS', 'OTHER')),

    initial_balance numeric(18,2) NOT NULL DEFAULT 0,
    current_balance numeric(18,2) NOT NULL DEFAULT 0,

    status varchar(30) NOT NULL DEFAULT 'ACTIVE'
        CHECK (status IN ('ACTIVE', 'INACTIVE')),

    created_at timestamp NOT NULL DEFAULT now(),
    updated_at timestamp NOT NULL DEFAULT now(),

    UNIQUE (business_id, name)
);


CREATE TABLE invoices (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),

    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,

    -- User-facing transaction identifier.
    invoice_number varchar(100) NOT NULL,

    party_id uuid NOT NULL REFERENCES parties(id),

    sale_id uuid NULL REFERENCES sales(id) ON DELETE SET NULL,
    booking_id uuid NULL REFERENCES bookings(id) ON DELETE SET NULL,
    purchase_id uuid NULL REFERENCES purchases(id) ON DELETE SET NULL,

    invoice_date date NOT NULL DEFAULT CURRENT_DATE,
    due_date date NULL,

    status varchar(30) NOT NULL DEFAULT 'DRAFT'
        CHECK (status IN ('DRAFT', 'ISSUED', 'VOID')),

    subtotal numeric(18,2) NOT NULL DEFAULT 0,
    discount_total numeric(18,2) NOT NULL DEFAULT 0,
    tax_total numeric(18,2) NOT NULL DEFAULT 0,
    grand_total numeric(18,2) NOT NULL DEFAULT 0,

    notes text,

    created_by uuid NULL REFERENCES users(id) ON DELETE SET NULL,

    created_at timestamp NOT NULL DEFAULT now(),
    updated_at timestamp NOT NULL DEFAULT now(),

    UNIQUE (business_id, invoice_number),

    CHECK (
        sale_id IS NOT NULL
        OR booking_id IS NOT NULL
        OR purchase_id IS NOT NULL
    )
);

CREATE INDEX idx_invoices_business_date
    ON invoices(business_id, invoice_date);

CREATE INDEX idx_invoices_party
    ON invoices(party_id);

CREATE INDEX idx_invoices_sale
    ON invoices(sale_id);

CREATE INDEX idx_invoices_booking
    ON invoices(booking_id);

CREATE INDEX idx_invoices_purchase
    ON invoices(purchase_id);


CREATE TABLE invoice_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),

    invoice_id uuid NOT NULL REFERENCES invoices(id) ON DELETE CASCADE,

    description text NOT NULL,

    quantity numeric(18,4) NOT NULL DEFAULT 1 CHECK (quantity > 0),
    unit_price numeric(18,2) NOT NULL DEFAULT 0,
    line_total numeric(18,2) NOT NULL DEFAULT 0,

    created_at timestamp NOT NULL DEFAULT now(),
    updated_at timestamp NOT NULL DEFAULT now()
);

CREATE INDEX idx_invoice_items_invoice
    ON invoice_items(invoice_id);


CREATE TABLE expenses (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),

    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,

    -- User-facing transaction identifier.
    expense_number varchar(100) NOT NULL,
    expense_date date NOT NULL DEFAULT CURRENT_DATE,

    category_id uuid NULL REFERENCES categories(id) ON DELETE SET NULL,

    amount numeric(18,2) NOT NULL CHECK (amount >= 0),

    related_booking_id uuid NULL REFERENCES bookings(id) ON DELETE SET NULL,
    related_purchase_id uuid NULL REFERENCES purchases(id) ON DELETE SET NULL,
    related_asset_id uuid NULL REFERENCES assets(id) ON DELETE SET NULL,

    payment_method_id uuid NULL REFERENCES payment_methods(id) ON DELETE SET NULL,
    cash_account_id uuid NULL REFERENCES cash_accounts(id) ON DELETE SET NULL,

    description text NOT NULL,
    notes text,

    created_by uuid NULL REFERENCES users(id) ON DELETE SET NULL,

    created_at timestamp NOT NULL DEFAULT now(),
    updated_at timestamp NOT NULL DEFAULT now(),

    UNIQUE (business_id, expense_number)
);

CREATE INDEX idx_expenses_business_date
    ON expenses(business_id, expense_date);

CREATE INDEX idx_expenses_booking
    ON expenses(related_booking_id);

CREATE INDEX idx_expenses_purchase
    ON expenses(related_purchase_id);

CREATE INDEX idx_expenses_asset
    ON expenses(related_asset_id);


CREATE TABLE payments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),

    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,

    -- User-facing transaction identifier.
    payment_number varchar(100) NOT NULL,
    payment_date timestamp NOT NULL DEFAULT now(),

    direction varchar(10) NOT NULL CHECK (direction IN ('IN', 'OUT')),

    payment_method_id uuid NOT NULL REFERENCES payment_methods(id),
    cash_account_id uuid NOT NULL REFERENCES cash_accounts(id),

    party_id uuid NULL REFERENCES parties(id) ON DELETE SET NULL,

    sale_id uuid NULL REFERENCES sales(id) ON DELETE SET NULL,
    purchase_id uuid NULL REFERENCES purchases(id) ON DELETE SET NULL,
    booking_id uuid NULL REFERENCES bookings(id) ON DELETE SET NULL,
    invoice_id uuid NULL REFERENCES invoices(id) ON DELETE SET NULL,
    expense_id uuid NULL REFERENCES expenses(id) ON DELETE SET NULL,

    amount numeric(18,2) NOT NULL CHECK (amount > 0),

    notes text,

    created_by uuid NULL REFERENCES users(id) ON DELETE SET NULL,

    created_at timestamp NOT NULL DEFAULT now(),
    updated_at timestamp NOT NULL DEFAULT now(),

    UNIQUE (business_id, payment_number),

    CHECK (
        sale_id IS NOT NULL
        OR purchase_id IS NOT NULL
        OR booking_id IS NOT NULL
        OR invoice_id IS NOT NULL
        OR expense_id IS NOT NULL
    )
);

CREATE INDEX idx_payments_business_date
    ON payments(business_id, payment_date);

CREATE INDEX idx_payments_sale
    ON payments(sale_id);

CREATE INDEX idx_payments_purchase
    ON payments(purchase_id);

CREATE INDEX idx_payments_booking
    ON payments(booking_id);

CREATE INDEX idx_payments_invoice
    ON payments(invoice_id);

CREATE INDEX idx_payments_expense
    ON payments(expense_id);

CREATE INDEX idx_payments_cash_account
    ON payments(cash_account_id);


-- ============================================================
-- 9. Number Sequence
-- ============================================================

CREATE TABLE number_sequences (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),

    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,

    sequence_type varchar(50) NOT NULL,
    prefix varchar(50) NOT NULL,

    current_number bigint NOT NULL DEFAULT 0,
    padding integer NOT NULL DEFAULT 6 CHECK (padding > 0),

    created_at timestamp NOT NULL DEFAULT now(),
    updated_at timestamp NOT NULL DEFAULT now(),

    UNIQUE (business_id, sequence_type)
);


-- ============================================================
-- 10. Audit
-- ============================================================

CREATE TABLE audit_logs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),

    business_id uuid NULL REFERENCES businesses(id) ON DELETE CASCADE,

    actor_user_id uuid NULL REFERENCES users(id) ON DELETE SET NULL,

    entity_type varchar(100) NOT NULL,
    entity_id uuid NOT NULL,

    action varchar(100) NOT NULL,

    before_json jsonb,
    after_json jsonb,

    ip_address inet,
    user_agent text,

    created_at timestamp NOT NULL DEFAULT now()
);

CREATE INDEX idx_audit_logs_business
    ON audit_logs(business_id);

CREATE INDEX idx_audit_logs_entity
    ON audit_logs(entity_type, entity_id);

CREATE INDEX idx_audit_logs_created_at
    ON audit_logs(created_at);


-- ============================================================
-- 11. Optional Seed Notes
-- ============================================================
-- Recommended default sequence_type values:
--   PRODUCT, PARTY, SERVICE, ASSET,
--   SALE, PURCHASE, BOOKING, INVOICE, PAYMENT, EXPENSE,
--   STOCK_ADJUSTMENT, STOCK_OPNAME, STOCK_TRANSFER
--
-- Recommended default roles:
--   OWNER, ADMIN, CASHIER, STAFF, VIEWER
--
-- Recommended default payment methods:
--   CASH, BANK_TRANSFER, QRIS, EWALLET, OTHER
--
-- Recommended default modules:
--   POS, INVENTORY, PURCHASE, BOOKING, FINANCE, REPORTING, CATALOG
--
-- Example apply command:
--   psql -U postgres -d your_database -f business_management_platform_schema_v1_1.sql
-- ============================================================

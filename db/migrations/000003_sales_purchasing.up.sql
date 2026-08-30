CREATE TABLE contacts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    public_code varchar(32) NOT NULL,
    contact_type varchar(30) NOT NULL DEFAULT 'CUSTOMER'
        CHECK (contact_type IN ('CUSTOMER', 'SUPPLIER', 'BOTH')),
    name varchar(150) NOT NULL,
    email varchar(254),
    phone varchar(50),
    address text,
    status varchar(30) NOT NULL DEFAULT 'ACTIVE'
        CHECK (status IN ('ACTIVE', 'INACTIVE')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (business_id, public_code),
    UNIQUE (business_id, id)
);

CREATE TABLE cash_accounts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    public_code varchar(32) NOT NULL,
    name varchar(100) NOT NULL,
    account_type varchar(50) NOT NULL DEFAULT 'CASH'
        CHECK (account_type IN ('CASH', 'BANK', 'EWALLET')),
    balance numeric(18,2) NOT NULL DEFAULT 0,
    is_default boolean NOT NULL DEFAULT false,
    status varchar(30) NOT NULL DEFAULT 'ACTIVE'
        CHECK (status IN ('ACTIVE', 'INACTIVE')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (business_id, public_code),
    UNIQUE (business_id, id)
);

CREATE UNIQUE INDEX cash_accounts_one_default_per_business_idx
    ON cash_accounts (business_id) WHERE is_default;

CREATE TABLE sales (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    location_id uuid NOT NULL REFERENCES locations(id),
    customer_id uuid REFERENCES contacts(id),
    receipt_number varchar(40) NOT NULL,
    sale_date timestamptz NOT NULL DEFAULT now(),
    status varchar(30) NOT NULL DEFAULT 'COMPLETED'
        CHECK (status IN ('DRAFT', 'COMPLETED', 'CANCELLED', 'REFUNDED')),
    payment_status varchar(30) NOT NULL DEFAULT 'PAID'
        CHECK (payment_status IN ('UNPAID', 'PARTIAL', 'PAID')),
    subtotal numeric(18,2) NOT NULL DEFAULT 0 CHECK (subtotal >= 0),
    discount_total numeric(18,2) NOT NULL DEFAULT 0 CHECK (discount_total >= 0),
    tax_total numeric(18,2) NOT NULL DEFAULT 0 CHECK (tax_total >= 0),
    grand_total numeric(18,2) NOT NULL DEFAULT 0 CHECK (grand_total >= 0),
    notes text,
    created_by uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (business_id, receipt_number),
    UNIQUE (business_id, id)
);

CREATE TABLE sale_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    sale_id uuid NOT NULL REFERENCES sales(id) ON DELETE CASCADE,
    product_id uuid NOT NULL REFERENCES products(id),
    quantity numeric(18,4) NOT NULL CHECK (quantity > 0),
    unit_price numeric(18,2) NOT NULL CHECK (unit_price >= 0),
    discount numeric(18,2) NOT NULL DEFAULT 0 CHECK (discount >= 0),
    subtotal numeric(18,2) NOT NULL CHECK (subtotal >= 0),
    notes text,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (business_id, id)
);

CREATE TABLE purchases (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    location_id uuid NOT NULL REFERENCES locations(id),
    supplier_id uuid REFERENCES contacts(id),
    purchase_number varchar(40) NOT NULL,
    reference_number varchar(100),
    purchase_date timestamptz NOT NULL DEFAULT now(),
    status varchar(30) NOT NULL DEFAULT 'COMPLETED'
        CHECK (status IN ('DRAFT', 'COMPLETED', 'CANCELLED')),
    payment_status varchar(30) NOT NULL DEFAULT 'PAID'
        CHECK (payment_status IN ('UNPAID', 'PARTIAL', 'PAID')),
    subtotal numeric(18,2) NOT NULL DEFAULT 0 CHECK (subtotal >= 0),
    discount_total numeric(18,2) NOT NULL DEFAULT 0 CHECK (discount_total >= 0),
    tax_total numeric(18,2) NOT NULL DEFAULT 0 CHECK (tax_total >= 0),
    grand_total numeric(18,2) NOT NULL DEFAULT 0 CHECK (grand_total >= 0),
    notes text,
    created_by uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (business_id, purchase_number),
    UNIQUE (business_id, id)
);

CREATE TABLE purchase_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    purchase_id uuid NOT NULL REFERENCES purchases(id) ON DELETE CASCADE,
    product_id uuid NOT NULL REFERENCES products(id),
    quantity numeric(18,4) NOT NULL CHECK (quantity > 0),
    unit_price numeric(18,2) NOT NULL CHECK (unit_price >= 0),
    discount numeric(18,2) NOT NULL DEFAULT 0 CHECK (discount >= 0),
    subtotal numeric(18,2) NOT NULL CHECK (subtotal >= 0),
    notes text,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (business_id, id)
);

CREATE TABLE payments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    cash_account_id uuid NOT NULL REFERENCES cash_accounts(id),
    sale_id uuid REFERENCES sales(id) ON DELETE CASCADE,
    purchase_id uuid REFERENCES purchases(id) ON DELETE CASCADE,
    payment_number varchar(40) NOT NULL,
    payment_date timestamptz NOT NULL DEFAULT now(),
    payment_method varchar(50) NOT NULL DEFAULT 'CASH'
        CHECK (payment_method IN ('CASH', 'TRANSFER', 'DEBIT_CARD', 'CREDIT_CARD', 'EWALLET')),
    amount numeric(18,2) NOT NULL CHECK (amount > 0),
    reference_number varchar(100),
    notes text,
    created_by uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (business_id, payment_number),
    UNIQUE (business_id, id),
    CONSTRAINT check_single_reference CHECK (
        (sale_id IS NOT NULL AND purchase_id IS NULL) OR
        (sale_id IS NULL AND purchase_id IS NOT NULL)
    )
);

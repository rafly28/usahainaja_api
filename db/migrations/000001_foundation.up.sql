CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE users (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    public_code varchar(32) NOT NULL UNIQUE,
    name varchar(150) NOT NULL,
    email varchar(254) NOT NULL,
    password_hash varchar(100) NOT NULL,
    status varchar(30) NOT NULL DEFAULT 'ACTIVE'
        CHECK (status IN ('ACTIVE', 'INACTIVE', 'SUSPENDED')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX users_email_normalized_key ON users (lower(email));

CREATE TABLE businesses (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    public_code varchar(32) NOT NULL UNIQUE,
    name varchar(150) NOT NULL,
    business_type varchar(50) NOT NULL DEFAULT 'OTHER'
        CHECK (business_type IN ('RETAIL', 'SERVICE', 'ENTERTAINMENT', 'OTHER')),
    status varchar(30) NOT NULL DEFAULT 'ACTIVE'
        CHECK (status IN ('ACTIVE', 'INACTIVE')),
    timezone varchar(100) NOT NULL DEFAULT 'Asia/Jakarta',
    currency char(3) NOT NULL DEFAULT 'IDR',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE roles (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    name varchar(100) NOT NULL,
    code varchar(50) NOT NULL,
    is_system_role boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (business_id, code),
    UNIQUE (business_id, id)
);

CREATE TABLE business_members (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id uuid NOT NULL,
    status varchar(30) NOT NULL DEFAULT 'ACTIVE'
        CHECK (status IN ('ACTIVE', 'INACTIVE', 'INVITED')),
    joined_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (business_id, user_id),
    FOREIGN KEY (business_id, role_id) REFERENCES roles(business_id, id)
);

CREATE INDEX business_members_user_status_idx ON business_members (user_id, status);

CREATE TABLE locations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    public_code varchar(32) NOT NULL,
    name varchar(150) NOT NULL,
    type varchar(50) NOT NULL DEFAULT 'STORE'
        CHECK (type IN ('STORE', 'WAREHOUSE', 'BOOTH', 'EVENT_VENUE', 'OTHER')),
    address text,
    is_default boolean NOT NULL DEFAULT false,
    status varchar(30) NOT NULL DEFAULT 'ACTIVE'
        CHECK (status IN ('ACTIVE', 'INACTIVE')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (business_id, public_code),
    UNIQUE (business_id, id)
);

CREATE UNIQUE INDEX locations_one_default_per_business_idx
    ON locations (business_id) WHERE is_default;

CREATE TABLE units (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    public_code varchar(32) NOT NULL,
    name varchar(100) NOT NULL,
    symbol varchar(30) NOT NULL,
    unit_type varchar(30) NOT NULL DEFAULT 'COUNT'
        CHECK (unit_type IN ('COUNT', 'WEIGHT', 'VOLUME', 'TIME', 'OTHER')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (business_id, public_code),
    UNIQUE (business_id, symbol),
    UNIQUE (business_id, id)
);

CREATE TABLE number_sequences (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    sequence_type varchar(50) NOT NULL,
    prefix varchar(16) NOT NULL,
    last_number bigint NOT NULL DEFAULT 0 CHECK (last_number >= 0),
    padding smallint NOT NULL DEFAULT 6 CHECK (padding BETWEEN 1 AND 12),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (business_id, sequence_type)
);

CREATE TABLE audit_logs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id uuid NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    actor_user_id uuid REFERENCES users(id) ON DELETE SET NULL,
    entity_type varchar(80) NOT NULL,
    entity_id uuid,
    entity_code varchar(64),
    action varchar(80) NOT NULL,
    before_data jsonb,
    after_data jsonb,
    reason text,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX audit_logs_business_created_idx
    ON audit_logs (business_id, created_at DESC);

CREATE TABLE sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash bytea NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    csrf_token varchar(64) NOT NULL,
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    active_business_id uuid REFERENCES businesses(id) ON DELETE SET NULL,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    user_agent varchar(512),
    ip_address inet
);

CREATE INDEX sessions_user_id_idx ON sessions (user_id);
CREATE INDEX sessions_expires_at_idx ON sessions (expires_at);

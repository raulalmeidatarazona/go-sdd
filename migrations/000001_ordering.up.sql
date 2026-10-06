-- Ordering bounded context: write model, read model and messaging tables.
-- Runs as the schema owner. The application connects as oms_app, which is not
-- the owner, so row-level security always applies to it.

CREATE SCHEMA IF NOT EXISTS ordering;
CREATE SCHEMA IF NOT EXISTS ordering_read;
CREATE SCHEMA IF NOT EXISTS messaging;

-- Tenant of the current transaction, set by postgres.DB.InTenantTx.
-- NULLIF turns the "unset" empty string into NULL so policies deny by default.
CREATE OR REPLACE FUNCTION current_tenant_id() RETURNS uuid
    LANGUAGE sql STABLE
AS $$ SELECT NULLIF(current_setting('app.tenant_id', true), '')::uuid $$;

-- Write model -----------------------------------------------------------------

CREATE TABLE ordering.orders (
    tenant_id           uuid        NOT NULL,
    id                  uuid        NOT NULL,
    customer_id         text        NOT NULL,
    status              text        NOT NULL CHECK (status IN ('PLACED', 'CANCELLED')),
    currency            char(3)     NOT NULL,
    total_minor         bigint      NOT NULL CHECK (total_minor >= 0),
    cancellation_reason text,
    idempotency_key     text        NOT NULL,
    request_fingerprint text        NOT NULL,
    version             bigint      NOT NULL CHECK (version > 0),
    placed_at           timestamptz NOT NULL,
    updated_at          timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, id),
    CONSTRAINT orders_tenant_id_idempotency_key_key UNIQUE (tenant_id, idempotency_key)
);

CREATE TABLE ordering.order_lines (
    tenant_id        uuid   NOT NULL,
    order_id         uuid   NOT NULL,
    line_no          int    NOT NULL,
    sku              text   NOT NULL,
    quantity         int    NOT NULL CHECK (quantity > 0),
    unit_price_minor bigint NOT NULL CHECK (unit_price_minor >= 0),
    PRIMARY KEY (tenant_id, order_id, line_no),
    FOREIGN KEY (tenant_id, order_id) REFERENCES ordering.orders (tenant_id, id) ON DELETE CASCADE
);

-- Read model (projection) -----------------------------------------------------

CREATE TABLE ordering_read.order_views (
    tenant_id           uuid        NOT NULL,
    id                  uuid        NOT NULL,
    customer_id         text        NOT NULL,
    status              text        NOT NULL,
    currency            char(3)     NOT NULL,
    lines               jsonb       NOT NULL,
    item_count          int         NOT NULL,
    total_minor         bigint      NOT NULL,
    cancellation_reason text,
    placed_at           timestamptz NOT NULL,
    updated_at          timestamptz NOT NULL,
    version             bigint      NOT NULL,
    PRIMARY KEY (tenant_id, id)
);

CREATE INDEX order_views_recent_idx ON ordering_read.order_views (tenant_id, placed_at DESC, id DESC);
CREATE INDEX order_views_customer_idx ON ordering_read.order_views (tenant_id, customer_id, placed_at DESC, id DESC);
CREATE INDEX order_views_status_idx ON ordering_read.order_views (tenant_id, status, placed_at DESC, id DESC);

-- Tenant isolation ------------------------------------------------------------

ALTER TABLE ordering.orders ENABLE ROW LEVEL SECURITY;
ALTER TABLE ordering.orders FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON ordering.orders
    USING (tenant_id = current_tenant_id()) WITH CHECK (tenant_id = current_tenant_id());

ALTER TABLE ordering.order_lines ENABLE ROW LEVEL SECURITY;
ALTER TABLE ordering.order_lines FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON ordering.order_lines
    USING (tenant_id = current_tenant_id()) WITH CHECK (tenant_id = current_tenant_id());

ALTER TABLE ordering_read.order_views ENABLE ROW LEVEL SECURITY;
ALTER TABLE ordering_read.order_views FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON ordering_read.order_views
    USING (tenant_id = current_tenant_id()) WITH CHECK (tenant_id = current_tenant_id());

-- Messaging (cross-tenant infrastructure, no RLS) -----------------------------

CREATE TABLE messaging.outbox (
    seq               bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    message_id        uuid        NOT NULL UNIQUE,
    tenant_id         uuid        NOT NULL,
    event_type        text        NOT NULL,
    aggregate_type    text        NOT NULL,
    aggregate_id      uuid        NOT NULL,
    aggregate_version bigint      NOT NULL,
    payload           bytea       NOT NULL,
    headers           jsonb       NOT NULL DEFAULT '{}',
    occurred_at       timestamptz NOT NULL,
    created_at        timestamptz NOT NULL DEFAULT now(),
    published_at      timestamptz
);

CREATE INDEX outbox_unpublished_idx ON messaging.outbox (seq) WHERE published_at IS NULL;

CREATE TABLE messaging.inbox (
    consumer     text        NOT NULL,
    message_id   uuid        NOT NULL,
    tenant_id    uuid        NOT NULL,
    processed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (consumer, message_id)
);

-- Privileges: least privilege for the application role ------------------------

GRANT USAGE ON SCHEMA ordering, ordering_read, messaging TO oms_app;
GRANT EXECUTE ON FUNCTION current_tenant_id() TO oms_app;
GRANT SELECT, INSERT, UPDATE ON ordering.orders, ordering.order_lines TO oms_app;
GRANT SELECT, INSERT, UPDATE ON ordering_read.order_views TO oms_app;
GRANT SELECT, INSERT, UPDATE ON messaging.outbox, messaging.inbox TO oms_app;

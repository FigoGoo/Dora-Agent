-- Dora business service migration 0023
-- Owner: 业务微服务后端工程师 (B0 Recharge)
-- Scope: 测试环境 Billing Package / SKU / Order / Mock Payment / Entitlement 交易基线。
-- 约束: 无数据库级外键；真实支付网关、退款和对账不在 B0 冒烟范围。
-- 锁风险: create-time only for new local schema.

CREATE TABLE IF NOT EXISTS recharge_packages (
  id varchar(64) PRIMARY KEY,
  package_id varchar(64) NOT NULL UNIQUE,
  package_type varchar(64) NOT NULL DEFAULT 'personal_credit_pack',
  target_scope varchar(32) NOT NULL DEFAULT 'personal',
  billing_mode varchar(32) NOT NULL DEFAULT 'one_time',
  display_name varchar(128) NOT NULL,
  name varchar(128) NOT NULL DEFAULT '',
  points bigint NOT NULL,
  granted_points bigint NOT NULL DEFAULT 0,
  bonus_points bigint NOT NULL DEFAULT 0,
  price_cents bigint NOT NULL,
  price_amount bigint NOT NULL DEFAULT 0,
  currency varchar(16) NOT NULL DEFAULT 'CNY',
  credit_valid_duration varchar(32) NOT NULL,
  credit_expiry_policy varchar(64) NOT NULL DEFAULT 'P1M',
  spend_scope_json jsonb NOT NULL DEFAULT '["tool_generation","skill_usage"]'::jsonb,
  settlement_eligible boolean NOT NULL DEFAULT true,
  entitlement_policy_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  renewal_policy_json jsonb NOT NULL DEFAULT '{"mode":"none"}'::jsonb,
  refund_policy_json jsonb NOT NULL DEFAULT '{"mode":"unused_refund"}'::jsonb,
  visible_scope varchar(64) NOT NULL DEFAULT 'all_users',
  status varchar(32) NOT NULL DEFAULT 'active',
  created_by varchar(64),
  updated_by varchar(64),
  deleted_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_recharge_packages_status_price
  ON recharge_packages (status, price_cents ASC);
CREATE INDEX IF NOT EXISTS idx_recharge_packages_target_status
  ON recharge_packages (target_scope, package_type, status, updated_at DESC);

CREATE TABLE IF NOT EXISTS billing_package_skus (
  id varchar(64) PRIMARY KEY,
  sku_id varchar(64) NOT NULL UNIQUE,
  package_id varchar(64) NOT NULL,
  channel_code varchar(64) NOT NULL DEFAULT 'default',
  price_amount bigint NOT NULL,
  currency varchar(16) NOT NULL DEFAULT 'CNY',
  activity_price_amount bigint,
  effective_at timestamptz NOT NULL DEFAULT now(),
  expired_at timestamptz,
  status varchar(32) NOT NULL DEFAULT 'active',
  created_by varchar(64),
  updated_by varchar(64),
  deleted_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_billing_package_skus_package_status
  ON billing_package_skus (package_id, status, channel_code, effective_at DESC);

CREATE TABLE IF NOT EXISTS recharge_orders (
  id varchar(64) PRIMARY KEY,
  order_id varchar(64) NOT NULL UNIQUE,
  user_id varchar(64) NOT NULL,
  enterprise_id varchar(64),
  account_id varchar(64) NOT NULL,
  package_id varchar(64) NOT NULL,
  sku_id varchar(64),
  package_type varchar(64) NOT NULL DEFAULT 'personal_credit_pack',
  target_scope varchar(32) NOT NULL DEFAULT 'personal',
  billing_mode varchar(32) NOT NULL DEFAULT 'one_time',
  points bigint NOT NULL,
  granted_points bigint NOT NULL DEFAULT 0,
  bonus_points bigint NOT NULL DEFAULT 0,
  price_cents bigint NOT NULL,
  price_amount bigint NOT NULL DEFAULT 0,
  currency varchar(16) NOT NULL,
  payment_provider varchar(64) NOT NULL DEFAULT 'mock_payment',
  payment_status varchar(32) NOT NULL DEFAULT 'pending',
  credit_lot_id varchar(64),
  entitlement_snapshot_id varchar(64),
  order_source varchar(64) NOT NULL DEFAULT 'user_purchase',
  idempotency_key varchar(128) NOT NULL,
  trace_id varchar(128),
  paid_at timestamptz,
  failed_reason varchar(256),
  created_by varchar(64),
  updated_by varchar(64),
  deleted_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (account_id, idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_recharge_orders_user_status
  ON recharge_orders (user_id, payment_status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_recharge_orders_account_status
  ON recharge_orders (account_id, payment_status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_recharge_orders_enterprise_status
  ON recharge_orders (enterprise_id, payment_status, created_at DESC);

CREATE TABLE IF NOT EXISTS mock_payment_transactions (
  id varchar(64) PRIMARY KEY,
  transaction_id varchar(128) NOT NULL UNIQUE,
  order_id varchar(64) NOT NULL,
  payment_result varchar(32) NOT NULL,
  payment_status varchar(32) NOT NULL,
  idempotency_key varchar(128) NOT NULL,
  trace_id varchar(128),
  request_payload_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_by varchar(64),
  updated_by varchar(64),
  deleted_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (order_id, idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_mock_payment_transactions_order_status
  ON mock_payment_transactions (order_id, payment_status, created_at DESC);

CREATE TABLE IF NOT EXISTS package_entitlement_snapshots (
  id varchar(64) PRIMARY KEY,
  entitlement_snapshot_id varchar(64) NOT NULL UNIQUE,
  account_id varchar(64) NOT NULL,
  user_id varchar(64),
  enterprise_id varchar(64),
  package_id varchar(64) NOT NULL,
  order_id varchar(64) NOT NULL,
  target_scope varchar(32) NOT NULL,
  entitlement_policy_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  status varchar(32) NOT NULL DEFAULT 'active',
  effective_at timestamptz NOT NULL,
  expires_at timestamptz,
  created_by varchar(64),
  updated_by varchar(64),
  deleted_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_package_entitlement_snapshots_target
  ON package_entitlement_snapshots (target_scope, account_id, status, effective_at DESC);
CREATE INDEX IF NOT EXISTS idx_package_entitlement_snapshots_order
  ON package_entitlement_snapshots (order_id, status);

CREATE TABLE IF NOT EXISTS enterprise_contracts (
  id varchar(64) PRIMARY KEY,
  contract_id varchar(64) NOT NULL UNIQUE,
  enterprise_id varchar(64) NOT NULL,
  package_id varchar(64) NOT NULL,
  order_id varchar(64),
  contract_status varchar(32) NOT NULL DEFAULT 'active',
  billing_mode varchar(32) NOT NULL DEFAULT 'subscription',
  period_start timestamptz NOT NULL,
  period_end timestamptz,
  seat_quota int NOT NULL DEFAULT 0,
  budget_points bigint NOT NULL DEFAULT 0,
  approval_policy_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  invoice_policy_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_by varchar(64),
  updated_by varchar(64),
  deleted_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_enterprise_contracts_enterprise_status
  ON enterprise_contracts (enterprise_id, contract_status, period_start DESC);

CREATE TABLE IF NOT EXISTS billing_invoices (
  id varchar(64) PRIMARY KEY,
  invoice_id varchar(64) NOT NULL UNIQUE,
  enterprise_id varchar(64),
  order_id varchar(64),
  amount bigint NOT NULL,
  currency varchar(16) NOT NULL DEFAULT 'CNY',
  invoice_status varchar(32) NOT NULL DEFAULT 'pending',
  issued_at timestamptz,
  due_at timestamptz,
  metadata_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_by varchar(64),
  updated_by varchar(64),
  deleted_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_billing_invoices_enterprise_status
  ON billing_invoices (enterprise_id, invoice_status, created_at DESC);

CREATE TABLE IF NOT EXISTS billing_promotions (
  id varchar(64) PRIMARY KEY,
  promotion_id varchar(64) NOT NULL UNIQUE,
  promotion_name varchar(128) NOT NULL,
  package_id varchar(64),
  discount_policy_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  visible_scope varchar(64) NOT NULL DEFAULT 'all_users',
  status varchar(32) NOT NULL DEFAULT 'active',
  starts_at timestamptz NOT NULL,
  ends_at timestamptz,
  created_by varchar(64),
  updated_by varchar(64),
  deleted_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_billing_promotions_status_time
  ON billing_promotions (status, starts_at DESC, ends_at);

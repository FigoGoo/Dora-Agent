-- Dora business service migration 0023 (down)
-- 回滚 B0 Recharge / Mock Payment 表。

DROP INDEX IF EXISTS idx_mock_payment_transactions_order_status;
DROP TABLE IF EXISTS mock_payment_transactions;

DROP INDEX IF EXISTS idx_billing_promotions_status_time;
DROP TABLE IF EXISTS billing_promotions;

DROP INDEX IF EXISTS idx_billing_invoices_enterprise_status;
DROP TABLE IF EXISTS billing_invoices;

DROP INDEX IF EXISTS idx_enterprise_contracts_enterprise_status;
DROP TABLE IF EXISTS enterprise_contracts;

DROP INDEX IF EXISTS idx_package_entitlement_snapshots_order;
DROP INDEX IF EXISTS idx_package_entitlement_snapshots_target;
DROP TABLE IF EXISTS package_entitlement_snapshots;

DROP INDEX IF EXISTS idx_recharge_orders_account_status;
DROP INDEX IF EXISTS idx_recharge_orders_user_status;
DROP INDEX IF EXISTS idx_recharge_orders_enterprise_status;
DROP TABLE IF EXISTS recharge_orders;

DROP INDEX IF EXISTS idx_billing_package_skus_package_status;
DROP TABLE IF EXISTS billing_package_skus;

DROP INDEX IF EXISTS idx_recharge_packages_target_status;
DROP INDEX IF EXISTS idx_recharge_packages_status_price;
DROP TABLE IF EXISTS recharge_packages;

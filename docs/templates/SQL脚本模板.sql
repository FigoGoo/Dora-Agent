-- 功能点：
-- owner：
-- 数据库：Agent DB / Business DB
-- 方向：up / down
-- 影响表：
-- 风险：
-- 回滚方式：
-- 表关联：禁止创建数据库级 FOREIGN KEY / REFERENCES，也不通过关联键表达表关联；确需保存上游标识时仅作为普通字段、查询过滤条件或审计信息。

BEGIN;

-- 在这里编写 SQL。

COMMIT;

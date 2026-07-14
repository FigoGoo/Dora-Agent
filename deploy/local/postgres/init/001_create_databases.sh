#!/bin/sh
set -eu

create_database() {
  role_name="$1"
  role_password="$2"
  database_name="$3"

  psql --set=ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" \
    --set=role_name="$role_name" --set=role_password="$role_password" --set=database_name="$database_name" <<-'EOSQL'
SELECT format('CREATE ROLE %I LOGIN PASSWORD %L', :'role_name', :'role_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'role_name') \gexec

SELECT format('CREATE DATABASE %I OWNER %I', :'database_name', :'role_name')
WHERE NOT EXISTS (SELECT 1 FROM pg_database WHERE datname = :'database_name') \gexec
EOSQL
}

create_database "dora_business_app" "$DORA_BUSINESS_DB_PASSWORD" "dora_business"
create_database "dora_agent_app" "$DORA_AGENT_DB_PASSWORD" "dora_agent"
create_database "dora_worker_app" "$DORA_WORKER_DB_PASSWORD" "dora_worker"

# 契约测试数据库与日常开发库物理隔离；测试允许重建 Schema，但不得清空开发数据库。
create_database "dora_business_app" "$DORA_BUSINESS_DB_PASSWORD" "dora_business_test"
create_database "dora_agent_app" "$DORA_AGENT_DB_PASSWORD" "dora_agent_test"
create_database "dora_worker_app" "$DORA_WORKER_DB_PASSWORD" "dora_worker_test"

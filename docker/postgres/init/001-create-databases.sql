SELECT 'CREATE DATABASE dora_business'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'dora_business')\gexec

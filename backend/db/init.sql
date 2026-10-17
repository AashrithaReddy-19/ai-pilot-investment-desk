-- db/init.sql
-- Initial database setup. Creates ONLY the role and the empty databases. No tables.
-- Tables are created later by the goose migrations (go run ./cmd/api migrate).
--
-- 1. Change CHANGE_ME below (twice is NOT needed; one password for the role).
-- 2. Run once as the postgres superuser:   sudo -u postgres psql -f db/init.sql
-- Safe to re-run: every step checks whether it already exists.

DO $$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'c23') THEN
    CREATE ROLE c23 LOGIN PASSWORD 'CHANGE_ME';
  END IF;
END
$$;

-- App database
SELECT 'CREATE DATABASE c23 OWNER c23'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'c23')\gexec
REVOKE ALL ON DATABASE c23 FROM PUBLIC;
GRANT CONNECT ON DATABASE c23 TO c23;
ALTER DATABASE c23 SET timezone TO 'UTC';

-- Separate database for the store tests (never point tests at your real data)
SELECT 'CREATE DATABASE c23_test OWNER c23'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'c23_test')\gexec
REVOKE ALL ON DATABASE c23_test FROM PUBLIC;
GRANT CONNECT ON DATABASE c23_test TO c23;
ALTER DATABASE c23_test SET timezone TO 'UTC';

-- Make sure the app role can create tables in the public schema (needed on older Postgres)
\connect c23
GRANT ALL ON SCHEMA public TO c23;
\connect c23_test
GRANT ALL ON SCHEMA public TO c23;

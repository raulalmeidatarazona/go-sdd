#!/bin/sh
# Creates the least-privilege application role on first database start.
# Migrations run as the owner (POSTGRES_USER); the services connect as oms_app.
set -eu

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<SQL
CREATE ROLE oms_app LOGIN PASSWORD '${OMS_APP_PASSWORD}' NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS;
GRANT CONNECT ON DATABASE "$POSTGRES_DB" TO oms_app;
SQL

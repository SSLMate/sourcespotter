#!/bin/bash
set -e

REPO_ROOT=$(dirname "$0")/..
cd "$REPO_ROOT"

# Install dependencies
apt-get update
DEBIAN_FRONTEND=noninteractive apt-get install -y postgresql mkcert >/dev/null

# Start PostgreSQL
service postgresql start

# Create database and user if not exist
sudo -u postgres psql <<'PSQL'
DO $$
BEGIN
   IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'sourcespotter') THEN
      CREATE ROLE sourcespotter LOGIN PASSWORD 'sourcespotter';
   END IF;
END$$;
CREATE DATABASE sourcespotter OWNER sourcespotter;
PSQL

# Load schema
sudo -u postgres psql -d sourcespotter -f schema/20-tables.sql
sudo -u postgres psql -d sourcespotter -f schema/30-triggers.sql

# Populate dummy data
sudo -u postgres psql -d sourcespotter <<'PSQL'
\copy db from 'testenv/testdata/db'
\copy sth from 'testenv/testdata/sth'
\copy record from 'testenv/testdata/record'
\copy toolchain_source from 'testenv/testdata/toolchain_source'
\copy toolchain_build from 'testenv/testdata/toolchain_build'
PSQL

# Generate TLS certificate
mkcert -install >/dev/null
mkcert -cert-file testenv/cert.pem -key-file testenv/key.pem \
       sourcespotter.localhost *.api.sourcespotter.localhost

# Combine cert and key because go-listener expects them in one file
cat testenv/key.pem testenv/cert.pem > testenv/server.pem

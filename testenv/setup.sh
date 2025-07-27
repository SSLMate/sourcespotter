#!/bin/bash
set -e

REPO_ROOT=$(dirname "$0")/..
cd "$REPO_ROOT"

# Install dependencies
apt-get update || true
DEBIAN_FRONTEND=noninteractive apt-get install -y postgresql mkcert >/dev/null

# Start PostgreSQL
service postgresql start

# Initialize database
sudo -u postgres psql <<'PSQL'
CREATE ROLE sourcespotter LOGIN PASSWORD 'sourcespotter';
CREATE DATABASE sourcespotter OWNER sourcespotter;
SET ROLE sourcespotter;
\i schema/20-tables.sql
\i schema/30-triggers.sql
\copy db from 'testenv/testdata/db'
\copy sth from 'testenv/testdata/sth'
\copy record from 'testenv/testdata/record'
\copy toolchain_source from 'testenv/testdata/toolchain_source'
\copy toolchain_build from 'testenv/testdata/toolchain_build'
PSQL

# Generate TLS certificate
mkcert -install >/dev/null
mkcert -cert-file testenv/cert.pem -key-file testenv/key.pem \
       sourcespotter.localhost '*.api.sourcespotter.localhost'

# Combine cert and key because go-listener expects them in one file
cat testenv/key.pem testenv/cert.pem > testenv/server.pem
chmod 644 testenv/server.pem

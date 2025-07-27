#!/bin/bash
set -e

REPO_ROOT=$(dirname "$0")/..
cd "$REPO_ROOT"

# Install dependencies
apt-get update || true
DEBIAN_FRONTEND=noninteractive apt-get install -y postgresql mkcert

# Start PostgreSQL
service postgresql start

# Initialize database
sudo -u postgres psql <<'PSQL'
DROP DATABASE IF EXISTS sourcespotter WITH (force);
DROP ROLE IF EXISTS sourcespotter;
CREATE ROLE sourcespotter LOGIN PASSWORD 'sourcespotter';
CREATE DATABASE sourcespotter OWNER sourcespotter;
\c sourcespotter
SET ROLE sourcespotter;
\i schema/20-tables.sql
\i schema/30-triggers.sql
\copy db from 'testenv/testdata/db'
\copy sth from 'testenv/testdata/sth'
\copy record from 'testenv/testdata/record'
\copy toolchain_source from 'testenv/testdata/toolchain_source'
\copy toolchain_build from 'testenv/testdata/toolchain_build'
SELECT setval('db_db_id_seq', (SELECT MAX(db_id) FROM db), true);
SELECT setval('sth_sth_id_seq', (SELECT MAX(sth_id) FROM sth), true);
PSQL

# Generate TLS certificate
mkcert -install
mkcert -cert-file /etc/sourcespotter-certonly.pem -key-file /etc/sourcespotter-key.pem \
       sourcespotter.localhost '*.api.sourcespotter.localhost'

# Combine cert and key because go-listener expects them in one file
cat /etc/sourcespotter-key.pem /etc/sourcespotter-certonly.pem > /etc/sourcespotter-cert.pem
chmod 644 /etc/sourcespotter-cert.pem

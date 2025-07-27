#!/bin/bash
set -e

REPO_ROOT=$(dirname "$0")/..
cd "$REPO_ROOT"

CERT_PATH=$(realpath testenv/server.pem)
exec go run ./cmd/sourcespotter \
  -config testenv/config.json \
  -listen "tls:${CERT_PATH}:tcp:8443" \
  "$@"

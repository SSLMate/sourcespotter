#!/bin/bash
set -e

REPO_ROOT=$(dirname "$0")/..
cd "$REPO_ROOT"

exec go run ./cmd/sourcespotter \
  -config testenv/config.json \
  -listen "tls:testenv/server.pem:tcp:8443" \
  "$@"

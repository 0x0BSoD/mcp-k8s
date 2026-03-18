#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OUT="$SCRIPT_DIR/gen/clusteragentpb"

mkdir -p "$OUT"

protoc \
  --proto_path="$SCRIPT_DIR" \
  --go_out="$OUT" \
  --go_opt=paths=source_relative \
  --go-grpc_out="$OUT" \
  --go-grpc_opt=paths=source_relative \
  clusteragent.proto

echo "generated → $OUT"

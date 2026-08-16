#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

if rg -n 'projects/(insurance|crossborder)' projects/crossborder projects/insurance \
  --glob '*.go' --glob 'go.mod'; then
  echo "vertical projects must not import each other" >&2
  exit 1
fi

echo "vertical project isolation check passed"

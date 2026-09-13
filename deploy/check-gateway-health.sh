#!/usr/bin/env bash
set -euo pipefail

STATUS=$(curl -s -o /dev/null -w "%{http_code}" --max-time 5 http://127.0.0.1:8086/health || echo "000")
if [ "$STATUS" = "200" ]; then
    exit 0
else
    echo "Sub2API health check failed with status: $STATUS" >&2
    exit 1
fi

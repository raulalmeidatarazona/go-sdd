#!/usr/bin/env bash
# Exercises the REST facade end to end: command -> outbox -> RabbitMQ -> projection -> query.
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"
TENANT="${TENANT:-8b5d3c1e-7d6f-4c2a-9a59-0d7f3c0b2a11}"
KEY="smoke-$(date +%s)-$RANDOM"

call() {
  curl -sS --fail-with-body -H "X-Tenant-Id: $TENANT" -H "Content-Type: application/json" "$@"
}

echo "== place order (idempotency key $KEY)"
ORDER_ID=$(call -X POST "$BASE_URL/v1/orders" -d @- <<JSON | sed -E 's/.*"orderId":"([^"]+)".*/\1/'
{
  "idempotencyKey": "$KEY",
  "customerId": "customer-42",
  "currencyCode": "EUR",
  "lines": [
    {"sku": "SKU-RED-M", "quantity": 2, "unitPriceMinor": 1999},
    {"sku": "SKU-BLUE-L", "quantity": 1, "unitPriceMinor": 4500}
  ]
}
JSON
)
echo "order id: $ORDER_ID"

echo "== wait for projection (eventual consistency)"
for _ in $(seq 1 50); do
  if call "$BASE_URL/v1/orders/$ORDER_ID" >/dev/null 2>&1; then break; fi
  sleep 0.1
done
call "$BASE_URL/v1/orders/$ORDER_ID"; echo

echo "== list orders"
call "$BASE_URL/v1/orders?pageSize=5" | head -c 400; echo " ..."

echo "== cancel order"
call -X POST "$BASE_URL/v1/orders/$ORDER_ID:cancel" -d '{"reason":"customer changed mind"}'; echo

echo "== another tenant cannot see it (expect 404)"
curl -sS -o /dev/null -w "%{http_code}\n" -H "X-Tenant-Id: 00000000-0000-4000-8000-000000000001" "$BASE_URL/v1/orders/$ORDER_ID"

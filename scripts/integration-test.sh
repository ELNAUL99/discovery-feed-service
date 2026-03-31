#!/bin/bash
set -e

BASE_URL="http://localhost:8080"
USER_ID="11111111-1111-1111-1111-111111111111"

echo "=== Discovery Feed Integration Tests ==="

# Health check
echo -n "Health check... "
HEALTH=$(curl -s "$BASE_URL/health")
if echo "$HEALTH" | grep -q "healthy"; then
    echo "PASS"
else
    echo "FAIL: $HEALTH"
    exit 1
fi

# Get feed
echo -n "Get feed... "
FEED=$(curl -s "$BASE_URL/api/v1/feed?user_id=$USER_ID&latitude=40.7128&longitude=-74.0060&limit=5")
if echo "$FEED" | grep -q "items"; then
    echo "PASS"
else
    echo "FAIL: $FEED"
    exit 1
fi

# Record click
echo -n "Record click... "
CLICK=$(curl -s -X POST "$BASE_URL/api/v1/interactions/click" \
    -H "Content-Type: application/json" \
    -d "{\"user_id\":\"$USER_ID\",\"venue_id\":\"22222222-2222-2222-2222-222222222222\"}")
if echo "$CLICK" | grep -q "recorded"; then
    echo "PASS"
else
    echo "FAIL: $CLICK"
    exit 1
fi

# List experiments
echo -n "List experiments... "
EXPS=$(curl -s "$BASE_URL/api/v1/experiments")
if echo "$EXPS" | grep -q "experiments"; then
    echo "PASS"
else
    echo "FAIL: $EXPS"
    exit 1
fi

# AI optimize
echo -n "AI optimize... "
OPT=$(curl -s -X POST "$BASE_URL/api/v1/ai/optimize" \
    -H "Content-Type: application/json" \
    -d '{"goal":"increase_ctr"}')
if echo "$OPT" | grep -q "recommendation"; then
    echo "PASS"
else
    echo "FAIL: $OPT"
    exit 1
fi

# Get metrics
echo -n "Metrics endpoint... "
METRICS=$(curl -s "$BASE_URL/metrics")
if echo "$METRICS" | grep -q "feed_requests_total"; then
    echo "PASS"
else
    echo "FAIL"
    exit 1
fi

echo "=== All tests passed! ==="

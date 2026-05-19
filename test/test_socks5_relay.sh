#!/bin/bash
# SOCKS5/HTTP CONNECT Relay Test Runner

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo "=== SOCKS5/HTTP CONNECT Relay Test Runner ==="
echo ""

# Check if binaries exist
if [ ! -f "$PROJECT_ROOT/bin/frps" ]; then
    echo -e "${RED}✗ frps binary not found. Run 'make build' first.${NC}"
    exit 1
fi

if [ ! -f "$PROJECT_ROOT/bin/frpc" ]; then
    echo -e "${RED}✗ frpc binary not found. Run 'make build' first.${NC}"
    exit 1
fi

# Parse arguments
MODE="${1:-quick}"

case "$MODE" in
    quick)
        echo -e "${YELLOW}Mode: Quick tests${NC}"
        DELAY=1
        ;;
    full)
        echo -e "${YELLOW}Mode: Full tests${NC}"
        DELAY=3
        ;;
    *)
        echo "Usage: $0 [quick|full]"
        exit 1
esac

echo ""
echo "Starting services..."

# Kill existing processes
pkill -f "bin/frps" 2>/dev/null || true
pkill -f "bin/frpc" 2>/dev/null || true
pkill -f "slow_server.py" 2>/dev/null || true
sleep 1

# Start frps
"$PROJECT_ROOT/bin/frps" --config "$SCRIPT_DIR/configs/frps-dashboard-test.toml" \
    > /tmp/frps_test.log 2>&1 &
FRPS_PID=$!
sleep 2

if ! pgrep -f "bin/frps" > /dev/null; then
    echo -e "${RED}✗ frps failed to start${NC}"
    cat /tmp/frps_test.log
    exit 1
fi
echo -e "${GREEN}✓ frps running (PID: $FRPS_PID)${NC}"

# Start frpc
"$PROJECT_ROOT/bin/frpc" --config "$SCRIPT_DIR/configs/frpc-dashboard-test.toml" \
    > /tmp/frpc_test.log 2>&1 &
FRPC_PID=$!
sleep 2

if ! pgrep -f "bin/frpc" > /dev/null; then
    echo -e "${RED}✗ frpc failed to start${NC}"
    cat /tmp/frpc_test.log
    exit 1
fi
echo -e "${GREEN}✓ frpc running (PID: $FRPC_PID)${NC}"


echo ""
echo "=== Running Tests ==="
echo ""

# Test 1: API Endpoints
echo "Test 1: API Endpoints"
SERVER_INFO=$(curl -s http://127.0.0.1:17500/api/serverinfo \
    -H "Authorization: Basic YWRtaW46YWRtaW4=")

if echo "$SERVER_INFO" | jq -e '.socks5ProxyPort == 10800' > /dev/null; then
    echo -e "${GREEN}✓ Server info API working${NC}"
else
    echo -e "${RED}✗ Server info API failed${NC}"
fi

# Test 2: SOCKS5 Proxy
echo ""
echo "Test 2: SOCKS5 Proxy"
if curl -s -x socks5://testgroup:testpass@127.0.0.1:10800 \
    --max-time 5 http://example.com > /dev/null; then
    echo -e "${GREEN}✓ SOCKS5 proxy working${NC}"
else
    echo -e "${RED}✗ SOCKS5 proxy failed${NC}"
fi

# Test 3: HTTP CONNECT Proxy  
echo ""
echo "Test 3: HTTP CONNECT Proxy (HTTPS)"
if curl -s -x http://testgroup:testpass@127.0.0.1:10801 \
    --max-time 5 https://example.com > /dev/null; then
    echo -e "${GREEN}✓ HTTP CONNECT proxy working${NC}"
else
    echo -e "${RED}✗ HTTP CONNECT proxy failed${NC}"
fi

# Test 4: SSE Events
echo ""
echo "Test 4: SSE Endpoint"
timeout 3 curl -s -N http://127.0.0.1:17500/api/socks5relay/events \
    -H "Authorization: Basic YWRtaW46YWRtaW4=" > /tmp/sse_test.txt 2>&1 &
SSE_PID=$!

sleep 1
curl -s -x socks5://testgroup:testpass@127.0.0.1:10800 \
    http://example.com > /dev/null

sleep 2
kill $SSE_PID 2>/dev/null 2>/dev/null || true

if grep -q "event: created" /tmp/sse_test.txt; then
    echo -e "${GREEN}✓ SSE endpoint working${NC}"
else
    echo -e "${RED}✗ SSE endpoint failed${NC}"
fi

# Test 5: Retention API
echo ""
echo "Test 5: Retention API"
RETENTION=$(curl -s http://127.0.0.1:17500/api/socks5relay/retention \
    -H "Authorization: Basic YWRtaW46YWRtaW4=")

if echo "$RETENTION" | jq -e '.retentionSeconds >= 0' > /dev/null; then
    echo -e "${GREEN}✓ Retention GET working${NC}"
else
    echo -e "${RED}✗ Retention GET failed${NC}"
fi

# Test setting retention
SET_RESULT=$(curl -s -X PUT http://127.0.0.1:17500/api/socks5relay/retention \
    -H "Authorization: Basic YWRtaW46YWRtaW4=" \
    -H "Content-Type: application/json" \
    -d '{"retentionSeconds": 300}')

if echo "$SET_RESULT" | jq -e '.retentionSeconds == 300' > /dev/null; then
    echo -e "${GREEN}✓ Retention PUT working${NC}"
else
    echo -e "${RED}✗ Retention PUT failed${NC}"
fi

# Reset to default
curl -s -X PUT http://127.0.0.1:17500/api/socks5relay/retention \
    -H "Authorization: Basic YWRtaW46YWRtaW4=" \
    -H "Content-Type: application/json" \
    -d '{"retentionSeconds": 600}' > /dev/null

# Test 6: Recent Connections
echo ""
echo "Test 6: Recent Connections"

# Make a connection that closes quickly
curl -s -x socks5://testgroup:testpass@127.0.0.1:10800 \
    http://example.com > /dev/null

sleep 1

CONNECTIONS=$(curl -s http://127.0.0.1:17500/api/socks5relay/connections \
    -H "Authorization: Basic YWRtaW46YWRtaW4=")

if echo "$CONNECTIONS" | jq -e '. | length > 0' > /dev/null; then
    echo -e "${GREEN}✓ Connections API working${NC}"

    # Check for closed connections
    CLOSED_COUNT=$(echo "$CONNECTIONS" | jq '[.[] | select(.isActive == false)] | length')
    if [ "$CLOSED_COUNT" -gt 0 ]; then
        echo -e "${GREEN}✓ Recent (closed) connections tracked${NC}"
    else
        echo -e "${YELLOW}⚠ No closed connections (may have cleared already)${NC}"
    fi
else
    echo -e "${RED}✗ Connections API failed${NC}"
fi

# Summary
echo ""
echo "=== Test Complete ==="
echo ""
echo "Services:"
echo "  frps: http://127.0.0.1:17500/static/"
echo "  Dashboard: http://127.0.0.1:17500/static/#/connections"
echo ""
echo "Test commands:"
echo "  curl -x socks5://testgroup:testpass@127.0.0.1:10800 http://example.com"
echo "  curl -x http://testgroup:testpass@127.0.0.1:10801 https://example.com"
echo ""
echo "Press Ctrl+C to stop services"

# Wait for user interrupt
trap "echo ''; echo 'Stopping services...'; kill $FRPS_PID $FRPC_PID 2>/dev/null; exit 0" INT

wait

#!/bin/bash
# Cold-start measurement: fresh dir + fresh DB + binary start + healthz + first toggle.
# Prints wall-clock seconds for each phase. Run: bash scripts/coldstart.sh [path-to-binary]
set -u
BIN="${1:-./switchyard}"
PORT=19081
D=$(mktemp -d)
cd "$D" || exit 1

echo "== cold start: fresh dir, fresh DB, $BIN =="

T0=$(date +%s.%N)
"$BIN" serve --addr "127.0.0.1:$PORT" > server.log 2>&1 &
SRV=$!
for i in $(seq 1 200); do
    curl -sf "http://127.0.0.1:$PORT/healthz" >/dev/null 2>&1 && break
    sleep 0.05
done
if ! curl -sf "http://127.0.0.1:$PORT/healthz" >/dev/null 2>&1; then
    echo "FATAL: server never became healthy; log:"
    cat server.log
    kill "$SRV" 2>/dev/null
    exit 1
fi
T1=$(date +%s.%N)

curl -sf -X POST "http://127.0.0.1:$PORT/flags" -H 'Content-Type: application/json' \
     -d '{"key":"cold-start-probe","kind":"boolean"}' >/dev/null
T2=$(date +%s.%N)

curl -sf -X POST "http://127.0.0.1:$PORT/flags/cold-start-probe/toggle" \
     > /dev/null
T3=$(date +%s.%N)

kill "$SRV" 2>/dev/null
wait "$SRV" 2>/dev/null

START=$(echo "$T1 $T0" | awk '{printf "%.2f", $1-$2}')
CREATE=$(echo "$T2 $T1" | awk '{printf "%.0f", ($1-$2)*1000}')
TOGGLE=$(echo "$T3 $T2" | awk '{printf "%.0f", ($1-$2)*1000}')

echo "start-to-healthz:        ${START}s"
echo "healthz-to-flag-created: ${CREATE}ms"
echo "flag-created-to-toggled: ${TOGGLE}ms"
echo "db file created:         $(ls -la switchyard.db 2>/dev/null | awk '{print $5" bytes"}')"
echo "server log:              $(cat server.log | head -1)"
cd /
rm -rf "$D"

#!/bin/bash
set -e

echo "[entrypoint] starting ai-web-gateway..."

# Create required directories
mkdir -p /sites/.runtime /sites/.runtime/ssl /etc/nginx/conf.d/projects

# Ensure nginx can access sites
chown -R nginx:nginx /sites 2>/dev/null || true

# Start Go gateway in background
echo "[entrypoint] starting go-gateway on ${LISTEN_ADDR:-:9000}..."
/usr/local/bin/go-gateway &
GATEWAY_PID=$!

# Wait for Go gateway to be ready
for i in $(seq 1 30); do
    if curl -s -o /dev/null http://127.0.0.1:${LISTEN_ADDR##*:}/ 2>/dev/null; then
        echo "[entrypoint] go-gateway ready (pid=$GATEWAY_PID)"
        break
    fi
    sleep 0.5
done

# Start nginx in foreground
echo "[entrypoint] starting nginx..."
exec nginx -g "daemon off;"

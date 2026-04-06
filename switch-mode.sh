#!/bin/bash
set -e

COMPOSE="docker compose -f deployments/docker-compose.yml"
HAPROXY_CONTAINER="deployments-haproxy-1"
NETWORK="deployments_default"

usage() {
  echo "Usage: $0 [active-active|active-passive|status]"
  exit 1
}

current_mode() {
  if docker ps --format '{{.Names}}' | grep -q "^${HAPROXY_CONTAINER}$"; then
    if docker exec "$HAPROXY_CONTAINER" grep -q "backup" /usr/local/etc/haproxy/haproxy.cfg 2>/dev/null; then
      echo "active-passive"
    else
      echo "active-active"
    fi
  else
    echo "stopped"
  fi
}

switch_haproxy() {
  local cfg="$1"

  echo ">>> Останавливаем haproxy..."
  docker stop "$HAPROXY_CONTAINER" 2>/dev/null || true
  docker rm "$HAPROXY_CONTAINER" 2>/dev/null || true

  echo ">>> Запускаем haproxy с конфигом: $cfg"
  docker run -d \
    --name "$HAPROXY_CONTAINER" \
    --network "$NETWORK" \
    -p 8080:8080 \
    -p 8404:8404 \
    -v "$(pwd)/deployments/haproxy/${cfg}:/usr/local/etc/haproxy/haproxy.cfg:ro" \
    haproxy:2.9-alpine

  echo ">>> Ждём haproxy..."
  sleep 2

  if curl -sf http://localhost:8080/health > /dev/null; then
    echo "[OK] HAProxy поднят и проксирует трафик"
  else
    echo "[WARN] HAProxy запущен, но /health не отвечает — проверь логи: docker logs $HAPROXY_CONTAINER"
  fi
}

MODE="${1:-}"

case "$MODE" in
  status)
    echo "Текущий режим: $(current_mode)"
    ;;
  active-active)
    echo "=== Переключаем в режим: active-active ==="
    switch_haproxy "active-active.cfg"
    echo ""
    echo "Режим: active-active (round-robin между llm-proxy-1 и llm-proxy-2)"
    echo "Stats: http://localhost:8404/stats"
    ;;
  active-passive)
    echo "=== Переключаем в режим: active-passive ==="
    switch_haproxy "active-passive.cfg"
    echo ""
    echo "Режим: active-passive (llm-proxy-1 — primary, llm-proxy-2 — backup)"
    echo "Stats: http://localhost:8404/stats"
    ;;
  *)
    usage
    ;;
esac

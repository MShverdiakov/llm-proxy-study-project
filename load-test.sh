#!/bin/bash
set -e

AUTH_URL="http://localhost:8001"
BILLING_URL="http://localhost:8002"
PROXY_URL="http://localhost:8080"
REQUESTS=${1:-50}

echo ">>> Регистрируем пользователя..."
REGISTER=$(curl -sf -X POST "$AUTH_URL/auth/register" \
  -H "Content-Type: application/json" \
  -d '{"email":"loadtest@example.com","password":"secret123"}' 2>/dev/null || \
  curl -sf -X POST "$AUTH_URL/auth/login" \
  -H "Content-Type: application/json" \
  -d '{"email":"loadtest@example.com","password":"secret123"}')

API_KEY=$(echo "$REGISTER" | grep -o '"api_key":"[^"]*"' | cut -d'"' -f4)
USER_ID=$(echo "$REGISTER" | grep -o '"ID":"[^"]*"' | cut -d'"' -f4)
[ -z "$USER_ID" ] && USER_ID=$(echo "$REGISTER" | grep -o '"user_id":"[^"]*"' | cut -d'"' -f4)

echo "API_KEY: ${API_KEY:0:16}..."

echo ">>> Пополняем баланс..."
curl -sf -X POST "$BILLING_URL/billing/deposit" \
  -H "Content-Type: application/json" \
  -d "{\"user_id\":\"$USER_ID\",\"amount\":100000}" > /dev/null

MESSAGES=(
  "What is 2+2?"
  "Explain machine learning in one sentence"
  "What is the capital of France?"
  "Write a haiku about programming"
  "What is REST API?"
  "Explain Docker in simple terms"
  "What is a microservice?"
  "How does Redis work?"
  "What is load balancing?"
  "Explain HAProxy"
)

echo ">>> Отправляем $REQUESTS запросов..."
for i in $(seq 1 $REQUESTS); do
  MSG="${MESSAGES[$((RANDOM % ${#MESSAGES[@]}))]}"
  curl -sf -X POST "$PROXY_URL/completions" \
    -H "Authorization: Bearer $API_KEY" \
    -H "Content-Type: application/json" \
    -d "{\"model\":\"mock-gpt-4\",\"messages\":[{\"role\":\"user\",\"content\":\"$MSG\"}]}" > /dev/null
  echo -ne "\r  $i/$REQUESTS"
done
echo ""
echo "[OK] Готово. Обнови дашборды в Grafana."

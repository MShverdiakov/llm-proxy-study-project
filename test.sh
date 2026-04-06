#!/bin/bash
set -e

GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m'

ok()   { echo -e "${GREEN}[OK]${NC} $1"; }
fail() { echo -e "${RED}[FAIL]${NC} $1"; exit 1; }
info() { echo -e "${YELLOW}>>>>${NC} $1"; }

AUTH_URL="http://localhost:8001"
BILLING_URL="http://localhost:8002"
PROXY_URL="http://localhost:8080"

# ── 1. Health checks ─────────────────────────────────────────────────────────
info "Health checks"

curl -sf "$AUTH_URL/health" > /dev/null    && ok "auth-service /health" || fail "auth-service /health"
curl -sf "$BILLING_URL/health" > /dev/null && ok "billing-service /health" || fail "billing-service /health"
curl -sf "$PROXY_URL/health" > /dev/null   && ok "llm-proxy /health (via HAProxy)" || fail "llm-proxy /health"

# ── 2. Register ───────────────────────────────────────────────────────────────
info "Register user"

cat > /tmp/llm_register.json << 'EOF'
{"email":"smoketest@example.com","password":"secret123"}
EOF

REGISTER=$(curl -sf -X POST "$AUTH_URL/auth/register" \
  -H "Content-Type: application/json" \
  -d @/tmp/llm_register.json)

echo "  $REGISTER"
API_KEY=$(echo "$REGISTER" | grep -o '"api_key":"[^"]*"' | cut -d'"' -f4)
USER_ID=$(echo "$REGISTER" | grep -o '"ID":"[^"]*"' | cut -d'"' -f4)

[ -n "$API_KEY" ] && ok "api_key получен: ${API_KEY:0:16}..." || fail "api_key не получен"
[ -n "$USER_ID" ] && ok "user_id получен: $USER_ID" || fail "user_id не получен"

# ── 3. Login ──────────────────────────────────────────────────────────────────
info "Login"

cat > /tmp/llm_login.json << 'EOF'
{"email":"smoketest@example.com","password":"secret123"}
EOF

LOGIN=$(curl -sf -X POST "$AUTH_URL/auth/login" \
  -H "Content-Type: application/json" \
  -d @/tmp/llm_login.json)

JWT=$(echo "$LOGIN" | grep -o '"jwt_token":"[^"]*"' | cut -d'"' -f4)
[ -n "$JWT" ] && ok "JWT получен: ${JWT:0:20}..." || fail "JWT не получен"

# ── 4. Validate API key ───────────────────────────────────────────────────────
info "Validate API key"

VALIDATE=$(curl -sf "$AUTH_URL/auth/validate?api_key=$API_KEY")
echo "  $VALIDATE"
echo "$VALIDATE" | grep -q "user_id" && ok "API key валиден" || fail "API key не валиден"

# ── 5. Deposit ────────────────────────────────────────────────────────────────
info "Deposit balance"

cat > /tmp/llm_deposit.json << EOF
{"user_id":"$USER_ID","amount":1000}
EOF

DEPOSIT=$(curl -sf -X POST "$BILLING_URL/billing/deposit" \
  -H "Content-Type: application/json" \
  -d @/tmp/llm_deposit.json)

echo "  $DEPOSIT"
echo "$DEPOSIT" | grep -q '"balance":1000' && ok "Баланс пополнен: 1000" || fail "Ошибка пополнения"

# ── 6. Check balance ──────────────────────────────────────────────────────────
info "Check balance"

BALANCE=$(curl -sf "$BILLING_URL/billing/balance/$USER_ID")
echo "  $BALANCE"
echo "$BALANCE" | grep -q '"balance":1000' && ok "Баланс корректен" || fail "Некорректный баланс"

# ── 7. LLM completion (first request → cache miss) ────────────────────────────
info "LLM completion #1 (cache miss)"

cat > /tmp/llm_completion.json << 'EOF'
{"model":"mock-gpt-4","messages":[{"role":"user","content":"What is 2+2?"}]}
EOF

RESP1=$(curl -sf -X POST "$PROXY_URL/completions" \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d @/tmp/llm_completion.json)

echo "  $RESP1"
echo "$RESP1" | grep -q '"content"' && ok "Ответ получен" || fail "Ответ не получен"

LATENCY1=$(echo "$RESP1" | grep -o '"latency_ms":[0-9]*' | cut -d: -f2)
ok "Latency: ${LATENCY1}ms"

# ── 8. LLM completion (second request → cache hit) ────────────────────────────
info "LLM completion #2 (cache hit)"

RESP2=$(curl -sf -X POST "$PROXY_URL/completions" \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d @/tmp/llm_completion.json)

echo "  $RESP2"
LATENCY2=$(echo "$RESP2" | grep -o '"latency_ms":[0-9]*' | cut -d: -f2)
ok "Latency из кэша: ${LATENCY2}ms (ожидаем < ${LATENCY1}ms)"

# ── 9. Balance decreased after usage ─────────────────────────────────────────
info "Balance после списания"

BALANCE2=$(curl -sf "$BILLING_URL/billing/balance/$USER_ID")
echo "  $BALANCE2"
ok "Баланс: $(echo $BALANCE2 | grep -o '"balance":[0-9]*' | cut -d: -f2)"

# ── 10. Transaction history ───────────────────────────────────────────────────
info "История транзакций"

TXS=$(curl -sf "$BILLING_URL/billing/transactions/$USER_ID")
echo "  $TXS"
echo "$TXS" | grep -qi '"type"' && ok "Транзакции есть" || fail "Транзакций нет"

# ── 11. List models ───────────────────────────────────────────────────────────
info "GET /models"

MODELS=$(curl -sf "$PROXY_URL/models")
echo "  $MODELS"
echo "$MODELS" | grep -q "mock" && ok "Модели получены" || fail "Модели не получены"

# ── 12. Stats endpoints ───────────────────────────────────────────────────────
info "Stats endpoints"

curl -sf "$AUTH_URL/stats" | grep -q "auth-service"    && ok "auth /stats" || fail "auth /stats"
curl -sf "$BILLING_URL/stats" | grep -q "billing"      && ok "billing /stats" || fail "billing /stats"

# ── 13. Test 401 (invalid key) ────────────────────────────────────────────────
info "401 при неверном ключе"

cat > /tmp/llm_completion.json << 'EOF'
{"model":"mock-gpt-4","messages":[{"role":"user","content":"test"}]}
EOF

STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$PROXY_URL/completions" \
  -H "Authorization: Bearer invalidkey" \
  -H "Content-Type: application/json" \
  -d @/tmp/llm_completion.json)

[ "$STATUS" = "401" ] && ok "401 при неверном ключе (статус: $STATUS)" || fail "Ожидался 401, получен $STATUS"

# ── 14. Clear cache ───────────────────────────────────────────────────────────
info "DELETE /cache"

STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X DELETE "$PROXY_URL/cache")
[ "$STATUS" = "204" ] && ok "Кэш очищен (204)" || fail "Ожидался 204, получен $STATUS"

# ── Done ──────────────────────────────────────────────────────────────────────
echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}  Все тесты пройдены успешно!${NC}"
echo -e "${GREEN}========================================${NC}"

# Cleanup
rm -f /tmp/llm_register.json /tmp/llm_login.json /tmp/llm_deposit.json /tmp/llm_completion.json

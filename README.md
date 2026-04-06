# LLM Proxy System

**АМРС — Лабораторная работа №1**
**Швердяков М.А.**

Платформа проксирования запросов к LLM-провайдерам на основе трёх Go-микросервисов.

---

## Архитектура

```
Клиент
  │
  ▼
HAProxy :8080  (active/active или active/passive)
  │               │
  ▼               ▼
llm-proxy-1    llm-proxy-2   (:8000)
  │
  ├── Auth Service     (:8001)  — валидация API-ключей, JWT
  ├── Billing Service  (:8002)  — баланс, тарификация
  ├── Redis            (:6379)  — кэш ответов LLM
  ├── RabbitMQ         (:5672)  — async событие usage.recorded
  └── LLM Provider             — mock / OpenAI / Anthropic
```

### Технологический стек

| Слой | Технологии |
|---|---|
| Язык | Go 1.22 |
| HTTP-роутер | chi v5 |
| База данных | PostgreSQL 16 + pgx v5 |
| Кэш | Redis 7 |
| Очередь сообщений | RabbitMQ 3.13 |
| Балансировка | HAProxy 2.9 |
| Трассировка | OpenTelemetry → Jaeger |
| Метрики | OTel → Prometheus → Grafana |
| Логирование | slog (JSON) |
| Контейнеризация | Docker + Docker Compose |

---

## Структура проекта

```
llm-proxy-system/
├── cmd/
│   ├── llm-proxy/main.go          # бинарник LLM Proxy
│   ├── auth-service/main.go       # бинарник Auth Service
│   └── billing-service/main.go    # бинарник Billing Service
├── internal/
│   ├── llmproxy/
│   │   ├── handler/               # HTTP-обработчики
│   │   ├── service/               # бизнес-логика, кэш, RabbitMQ publish
│   │   └── provider/              # LLMProvider: mock + OpenAI
│   ├── auth/
│   │   ├── handler/               # регистрация, JWT middleware
│   │   ├── service/               # bcrypt, JWT, API-ключи
│   │   └── store/                 # pgx-запросы: users, api_keys
│   └── billing/
│       ├── handler/               # баланс, пополнение, история
│       ├── service/               # тарификация, списание
│       ├── store/                 # pgx-запросы: balances, transactions
│       └── consumer/              # RabbitMQ listener
├── pkg/
│   ├── stats/                     # /stats endpoint (общий для всех сервисов)
│   ├── telemetry/                 # OTel SDK init (traces + metrics)
│   ├── middleware/                # logging, tracing, metrics middleware
│   └── client/                   # HTTP-клиенты: AuthClient, BillingClient, LLMProxyClient
├── migrations/
│   ├── auth/001_init.sql
│   └── billing/001_init.sql
├── deployments/
│   ├── docker-compose.yml
│   ├── llm-proxy/Dockerfile
│   ├── auth-service/Dockerfile
│   ├── billing-service/Dockerfile
│   ├── haproxy/
│   │   ├── active-active.cfg
│   │   └── active-passive.cfg
│   ├── otel-collector/config.yaml
│   ├── prometheus/prometheus.yml
│   └── grafana/
│       ├── provisioning/          # автопровизионинг datasource + dashboards
│       └── dashboards/            # system-overview, llm-proxy, billing-auth
├── go.mod
└── Makefile
```

---

## Микросервисы и API

### LLM Proxy — порт 8000

Основной шлюз. Принимает запросы от клиентов, валидирует через Auth, проверяет баланс через Billing, проксирует к LLM-провайдеру.

| Метод | Путь | Описание |
|---|---|---|
| POST | `/completions` | Запрос к LLM |
| GET | `/models` | Список доступных моделей |
| DELETE | `/cache` | Очистить Redis-кэш |
| GET | `/stats` | Статистика сервиса |
| GET | `/health` | Health check для HAProxy |
| GET | `/metrics` | Prometheus метрики |

**Поток `POST /completions`:**
1. Извлечь `api_key` из `Authorization: Bearer`
2. Валидировать ключ → Auth Service (sync HTTP)
3. Проверить баланс → Billing Service (sync HTTP)
4. Если баланс < 10 → `402 Payment Required`
5. Проверить Redis-кэш (ключ = SHA256(model + messages))
6. При cache miss — запрос к LLM Provider
7. Сохранить ответ в Redis (TTL конфигурируемый)
8. Опубликовать `usage.recorded` в RabbitMQ
9. Вернуть ответ клиенту

**Пример ответа:**
```json
{
  "content": "Hello! How can I help you?",
  "model": "gpt-4",
  "usage": {
    "prompt_tokens": 150,
    "completion_tokens": 80,
    "total_tokens": 230
  },
  "latency_ms": 1240
}
```

---

### Auth Service — порт 8001

| Метод | Путь | Описание |
|---|---|---|
| POST | `/auth/register` | Регистрация → `{user, api_key}` |
| POST | `/auth/login` | Логин → `{jwt_token}` |
| GET | `/auth/validate` | Валидация API-ключа (internal) |
| POST | `/auth/keys` | Создать доп. ключ (JWT required) |
| DELETE | `/auth/keys/{key_id}` | Отозвать ключ |
| GET | `/auth/users/me` | Профиль пользователя |

**Схема БД:**
```sql
users    (id UUID, email, password_hash, role, created_at)
api_keys (id UUID, user_id, key_hash, name, revoked, created_at)
```

**Безопасность:** пароли — bcrypt, API-ключи хранятся как SHA256-хэш, JWT — HS256 с TTL 24 ч.

---

### Billing Service — порт 8002

| Метод | Путь | Описание |
|---|---|---|
| GET | `/billing/balance/{user_id}` | Баланс пользователя |
| POST | `/billing/deposit` | Пополнение баланса |
| GET | `/billing/usage` | Статистика (`?period=day\|week\|month`) |
| GET | `/billing/transactions/{user_id}` | История транзакций |

**Схема БД:**
```sql
balances     (user_id UUID, amount BIGINT, updated_at)
transactions (id UUID, user_id, type, amount, model, tokens, created_at)
```

**RabbitMQ Consumer** — слушает очередь `usage.recorded`, при получении события вычисляет стоимость (`tokens / 1000 * price`) и списывает с баланса. Переподключение с backoff при разрыве.

---

## Балансировка нагрузки (HAProxy)

Два режима, переключаются через конфиг:

### Active/Active (round-robin)
Оба экземпляра llm-proxy принимают трафик. При падении одного HAProxy автоматически исключает его из ротации.

### Active/Passive
`llm-proxy-2` получает трафик только если `llm-proxy-1` не проходит 3 health-check подряд.

**HAProxy Stats UI:** `http://localhost:8404/stats`

---

## Мониторинг и телеметрия

### OpenTelemetry
Каждый сервис инициализирует OTel SDK при старте:
- **Traces** — span на каждый HTTP-запрос + исходящие вызовы к auth/billing/LLM
- **Metrics** — `http_requests_total`, `http_request_duration_seconds`
- **Logs** — slog JSON с trace_id

Экспорт: OTLP gRPC → OTel Collector → Jaeger (traces) + Prometheus (metrics)

### Grafana Dashboards (provisioning — импорт не требуется)

| Дашборд | Содержание |
|---|---|
| System Overview | RPS, error rate, p50/p95/p99 latency, HAProxy статус |
| LLM Proxy | Cache hit/miss, токены по моделям, latency провайдера |
| Billing & Auth | Успешность валидации ключей, операции пополнения/списания |

---

## Конфигурация (env-переменные)

| Переменная | Сервис | По умолчанию |
|---|---|---|
| `PORT` | все | 8000/8001/8002 |
| `DB_URL` | auth, billing | `postgres://llm:llm@localhost:5432/llm` |
| `REDIS_URL` | llm-proxy | `redis://localhost:6379` |
| `RABBITMQ_URL` | llm-proxy, billing | `amqp://llm:llm@localhost:5672/` |
| `JWT_SECRET` | auth | — |
| `AUTH_SERVICE_URL` | llm-proxy | `http://localhost:8001` |
| `BILLING_SERVICE_URL` | llm-proxy | `http://localhost:8002` |
| `LLM_PROVIDER` | llm-proxy | `mock` |
| `OPENAI_API_KEY` | llm-proxy | — |
| `CACHE_TTL` | llm-proxy | `300` (сек) |
| `PRICE_GPT4` | billing | `0.03` (кредитов / 1000 токенов) |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | все | `localhost:4317` |
| `SERVICE_VERSION` | все | `1.0.0` |

---

## Запуск

### Все сервисы через Docker Compose

```bash
cd deployments
docker compose up -d
```

После запуска доступно:
- LLM Proxy (через HAProxy): http://localhost:8080
- Grafana: http://localhost:3000 (admin / admin)
- Prometheus: http://localhost:9090
- Jaeger UI: http://localhost:16686
- HAProxy Stats: http://localhost:8404/stats
- RabbitMQ Management: http://localhost:15672 (llm / llm)

### Active/Passive режим

```bash
# Изменить volume в docker-compose.yml:
# ./haproxy/active-passive.cfg вместо active-active.cfg
docker compose up -d haproxy
```

### Миграции БД

```bash
export DB_URL=postgres://llm:llm@localhost:5432/llm
make migrate
```

### Локальная сборка

```bash
make build        # собрать бинарники
make test         # запустить тесты
make lint         # golangci-lint
make docker-build # собрать Docker-образы
```

---

## Быстрый тест

```bash
# 1. Зарегистрировать пользователя
curl -X POST http://localhost:8080/../auth-service:8001/auth/register \
  -H "Content-Type: application/json" \
  -d '{"email":"user@example.com","password":"secret123"}'
# Ответ: {"user":{...},"api_key":"<KEY>"}

# 2. Пополнить баланс
curl -X POST http://localhost:8002/billing/deposit \
  -H "Content-Type: application/json" \
  -d '{"user_id":"<USER_ID>","amount":1000}'

# 3. Запрос к LLM (через HAProxy)
curl -X POST http://localhost:8080/completions \
  -H "Authorization: Bearer <KEY>" \
  -H "Content-Type: application/json" \
  -d '{"model":"mock-gpt-4","messages":[{"role":"user","content":"Hello!"}]}'
```

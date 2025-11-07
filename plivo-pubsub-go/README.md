# In-Memory Pub/Sub over WebSocket + REST (Go)

A compact, production-style implementation of the **Plivo Assignment 2**:

- **WebSocket** endpoint: `/ws` supports `publish`, `subscribe`, `unsubscribe`, `ping`
- **REST** APIs: topic management & observability
- **In-memory** only (no external DB/broker)
- **Backpressure** policy & graceful shutdown
- **Replay** via per-topic ring buffer (last 100 messages)

## Why Go & Library Choice

- Language: **Go** fits a concurrency-heavy pub/sub the best.
- WebSocket lib: **nhooyr.io/websocket** — modern, context-first, simple to test.

## API Contracts (As per spec)

### WebSocket `/ws`

Client → Server:

```json
{
  "type": "subscribe" | "unsubscribe" | "publish" | "ping",
  "topic": "orders",
  "message": { "id": "uuid", "payload": {...} },
  "client_id": "s1",
  "last_n": 5,
  "request_id": "uuid-optional"
}
```

Server → Client:

```json
{
  "type": "ack" | "event" | "error" | "pong" | "info",
  "request_id": "uuid-optional",
  "topic": "orders",
  "message": { "id": "uuid", "payload": {...} },
  "error": { "code": "BAD_REQUEST", "message": "..." },
  "ts": "RFC3339 timestamp",
  "status": "ok",
  "msg": "ping"
}
```

- Heartbeat: periodic `{"type":"info","msg":"ping"}`
- `event` is sent to subscribers on publish
- `ack` confirms subscribe/unsubscribe/publish

### REST

- `POST /topics` → `{ "status":"created", "topic":"<name>" }`  
- `DELETE /topics/{name}` → `{ "status":"deleted", "topic":"<name>" }`  
- `GET /topics` → `{ "topics": [{"name":"orders","subscribers":3}] }`  
- `GET /health` → `{ "uptime_sec": 12, "topics": 1, "subscribers": 2 }`  
- `GET /stats` → `{ "topics": { "orders": {"messages": 42, "subscribers": 3} } }`  

## Backpressure Policy

- **Per-subscriber bounded queue**: size = 100.
- On overflow, we **disconnect** the slow subscriber with error code `SLOW_CONSUMER`.
- Rationale: simple, robust, prevents head-of-line blocking.

## Replay

- Each topic stores a **ring buffer of last 100 messages**.
- On subscribe, if `last_n > 0`, those messages are replayed (subject to backpressure).

## Graceful Shutdown

- SIGINT/SIGTERM cause HTTP server to stop accepting new requests; in-flight requests have 10s to finish.
- All active sockets are closed best-effort.

## Run Locally

### Prereqs
- Go 1.22+
- (optional) `make` / `docker`

### Start

```bash
go mod download
go run .
# server on :8080
```

Set address:
```bash
ADDR=":9090" go run .
```

### Docker

```bash
docker build -t plivo-pubsub:latest .
docker run --rm -p 8080:8080 plivo-pubsub:latest
```

## Quick Test (WebSocket)

Open a terminal and run `wscat` (or any WS client):

```bash
# terminal A: subscriber
wscat -c ws://localhost:8080/ws
> {"type":"subscribe","topic":"orders","client_id":"s1","last_n":5,"request_id":"r1"}
# expect {"type":"ack","status":"ok"...}

# terminal B: publisher
wscat -c ws://localhost:8080/ws
> {"type":"publish","topic":"orders","message":{"id":"550e8400-e29b-41d4-a716-446655440000","payload":{"order_id":"ORD-123","amount":99.5}},"request_id":"p1"}
# terminal A should receive {"type":"event", ...}
```

Create topic via REST before subscribing/publishing:

```bash
curl -s -X POST localhost:8080/topics -H 'Content-Type: application/json' -d '{"name":"orders"}' | jq
curl -s localhost:8080/topics | jq
curl -s localhost:8080/health | jq
curl -s localhost:8080/stats | jq
curl -s -X DELETE localhost:8080/topics/orders | jq
```

## Project Layout

```
.
├── go.mod
├── main.go
├── router.go
├── util.go
├── types.go
├── ringbuffer.go
├── topics.go
├── ws.go
├── Dockerfile
└── README.md
```

## Notes & Assumptions

- **Auth**: Not implemented; can be added via `X-API-Key` header.
- **Validation**: Message `id` format left flexible (UUID recommended).
- **Isolation**: Strict per-topic; subscribers belong to one topic per subscription call.
- **Replay+Backpressure**: Replay respects per-subscriber queue capacity.

## Talking Points (Interview)

- Why Go & bounded queues; why disconnect slow consumers.
- Ring buffer vs. persistence; tradeoffs for replay.
- Fan-out complexity and avoiding head-of-line blocking.
- Graceful shutdown semantics and server timeouts.
- How to extend to multiple nodes (e.g., with an external broker) while keeping API stable.
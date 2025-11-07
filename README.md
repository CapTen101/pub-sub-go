# In-Memory Pub/Sub System (WebSocket + REST)

This project implements a lightweight **Publish/Subscribe messaging system** with WebSocket fan-out and REST topic management.  
It is designed to demonstrate **concurrency, streaming delivery, backpressure handling, replay buffers, graceful shutdown, and operational observability** — typical concerns in distributed system design.

## Core Features

| Feature | Description |
|---|---|
| Publish/Subscribe | WebSocket subscribers receive messages for their topic in real-time. |
| Topic Management (REST) | Create, delete, and list topics. |
| Message Broadcasting | Publishing sends messages to all active subscribers of that topic. |
| Replay Support (`last_n`) | Subscribers may request the latest N messages from a topic, stored in a ring buffer. |
| Bounded Backpressure Queues | Slow consumers are disconnected to prevent backpressure buildup. |
| Graceful Shutdown | Server drains subscriber queues and closes WebSockets cleanly. |
| Metrics & Health Endpoints | `/stats` and `/health` expose runtime information. |
| Heartbeats | Server periodically sends `info: ping` frames to keep connections alive. |
| Optional Authentication | `X-API-Key` required when enabled via environment variable. |

## Architecture Overview

REST (topic management) + WebSocket streaming delivery.

## Backpressure Policy

Each subscriber has a bounded channel. If it fills, the subscriber is disconnected with a `SLOW_CONSUMER` error.

## Replay (`last_n`)

Subscribers may request recent messages, served from a ring buffer per topic.

## Graceful Shutdown

On shutdown:
- Server stops accepting new connections
- Drains subscriber queues (bounded time)
- Closes WebSockets cleanly

## Optional Authentication

Enable:
```
export API_KEY=mysecret
```

Then each request must include:
```
X-API-Key: mysecret
```

## Configuration

| Variable | Default | Meaning |
|---|---|---|
| ADDR | `:8080` | Server listen address |
| API_KEY | *(empty)* | Enables authentication if set |
| SUBSCRIBER_QUEUE_SIZE | `100` | Backpressure queue depth |
| RING_BUFFER_SIZE | `100` | Replay ring buffer size per topic |

## Run

```
go mod download
go run .
```

## Docker

```
docker build -t pubsub .
docker run --rm -p 8080:8080 -e API_KEY=supersecret pubsub
```

## WebSocket Example

Subscribe:
```
{ "type": "subscribe", "topic": "orders", "client_id": "ui" }
```

Publish:
```
{
  "type": "publish",
  "topic": "orders",
  "message": { "id": "m1", "payload": {"order_id": "123"} }
}
```

Receive:
```
{
  "type": "event",
  "topic": "orders",
  "message": { "id": "m1", "payload": {"order_id": "123"} }
}
```

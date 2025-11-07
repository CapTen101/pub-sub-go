package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

// WSConn abstracts write & close for testability
type WSConn interface {
	Read(ctx context.Context) (ClientToServer, error)
	Write(ctx context.Context, msg ServerToClient) error
	Close(status websocket.StatusCode, reason string) error
	SetReadLimit(n int64)
}

type nhooyrConn struct {
	c *websocket.Conn
}

func (n *nhooyrConn) Read(ctx context.Context) (ClientToServer, error) {
	var m ClientToServer
	if err := wsjson.Read(ctx, n.c, &m); err != nil {
		return m, err
	}
	return m, nil
}

func (n *nhooyrConn) Write(ctx context.Context, msg ServerToClient) error {
	return wsjson.Write(ctx, n.c, msg)
}

func (n *nhooyrConn) Close(status websocket.StatusCode, reason string) error {
	return n.c.Close(status, reason)
}

func (n *nhooyrConn) SetReadLimit(nbytes int64) {
	n.c.SetReadLimit(nbytes)
}

const (
	// Backpressure policy: bounded queue per subscriber.
	// If full, we DISCONNECT the subscriber with SLOW_CONSUMER.
	// This keeps the system healthy and is simple to reason about.
	heartbeatInterval = 20 * time.Second
	readLimitBytes    = 1 << 20 // 1MB
)

func wsHandler(w http.ResponseWriter, r *http.Request) {
	// --- API Key Auth Check for WebSocket ---
	expected := getEnv("API_KEY", "")
	if expected != "" { // auth is enabled only if env var is set
		if r.Header.Get("X-API-Key") != expected {
			// Reject before websocket upgrade
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("Unauthorized WebSocket: Missing or invalid X-API-Key"))
			return
		}
	}

	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// In real-world, set Origin checks / subprotocols
	})
	if err != nil {
		log.Printf("accept err: %v", err)
		return
	}
	conn := &nhooyrConn{c: c}
	conn.SetReadLimit(readLimitBytes)

	ctx := r.Context()
	// writer cancel context
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// heartbeat goroutine
	go func() {
		t := time.NewTicker(heartbeatInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				_ = conn.Write(context.Background(), ServerToClient{
					Type: "info",
					Msg:  "ping",
					TS:   time.Now().UTC(),
				})
			}
		}
	}()

	// per-connection state: subscriptions by topic -> *Subscriber
	subs := map[string]*Subscriber{}

	for {
		var in ClientToServer
		in, err = conn.Read(ctx)
		if err != nil {
			// connection closed or error
			break
		}
		switch in.Type {
		case "ping":
			_ = conn.Write(ctx, ServerToClient{Type: "pong", RequestID: in.RequestID, TS: time.Now().UTC()})
		case "subscribe":
			if in.Topic == "" || in.ClientID == "" {
				_ = conn.Write(ctx, ServerToClient{Type: "error", RequestID: in.RequestID, Error: &ErrObj{Code: "BAD_REQUEST", Message: "topic and client_id required"}, TS: time.Now().UTC()})
				continue
			}
			topic, err := globalTopics.GetTopic(in.Topic)
			if err != nil {
				_ = conn.Write(ctx, ServerToClient{Type: "error", RequestID: in.RequestID, Error: &ErrObj{Code: "TOPIC_NOT_FOUND", Message: "topic not found"}, TS: time.Now().UTC()})
				continue
			}
			// create subscriber
			sub := &Subscriber{
				id:     in.ClientID,
				send:   make(chan Message, subscriberQueueSize),
				conn:   conn,
				closed: make(chan struct{}),
			}
			// writer goroutine for this subscriber
			go subscriberWriteLoop(ctx, topic.name, sub)

			// register
			topic.mu.Lock()
			// if already subscribed, close old one
			if old, ok := topic.subscribers[sub.id]; ok {
				old.Close()
			}
			topic.subscribers[sub.id] = sub
			topic.mu.Unlock()

			// optional replay
			if in.LastN > 0 {
				for _, m := range topic.history.LastN(in.LastN) {
					select {
					case sub.send <- m:
					default:
						// overflow during replay: disconnect
						sub.CloseWithError("SLOW_CONSUMER", "replay overflow")
					}
				}
			}

			_ = conn.Write(ctx, ServerToClient{Type: "ack", RequestID: in.RequestID, Topic: topic.name, Status: "ok", TS: time.Now().UTC()})
		case "unsubscribe":
			if in.Topic == "" || in.ClientID == "" {
				_ = conn.Write(ctx, ServerToClient{Type: "error", RequestID: in.RequestID, Error: &ErrObj{Code: "BAD_REQUEST", Message: "topic and client_id required"}, TS: time.Now().UTC()})
				continue
			}
			if topic, err := globalTopics.GetTopic(in.Topic); err == nil {
				topic.mu.Lock()
				if sub, ok := topic.subscribers[in.ClientID]; ok {
					sub.Close()
					delete(topic.subscribers, in.ClientID)
				}
				topic.mu.Unlock()
			}
			_ = conn.Write(ctx, ServerToClient{Type: "ack", RequestID: in.RequestID, Topic: in.Topic, Status: "ok", TS: time.Now().UTC()})
		case "publish":
			if in.Topic == "" || in.Message == nil {
				_ = conn.Write(ctx, ServerToClient{Type: "error", RequestID: in.RequestID, Error: &ErrObj{Code: "BAD_REQUEST", Message: "topic and message required"}, TS: time.Now().UTC()})
				continue
			}
			if err := publishToTopic(in.Topic, *in.Message); err != nil {
				code := "INTERNAL"
				if errors.Is(err, ErrTopicNotFound) {
					code = "TOPIC_NOT_FOUND"
				}
				_ = conn.Write(ctx, ServerToClient{Type: "error", RequestID: in.RequestID, Error: &ErrObj{Code: code, Message: err.Error()}, TS: time.Now().UTC()})
				continue
			}
			_ = conn.Write(ctx, ServerToClient{Type: "ack", RequestID: in.RequestID, Topic: in.Topic, Status: "ok", TS: time.Now().UTC()})
		default:
			_ = conn.Write(ctx, ServerToClient{Type: "error", RequestID: in.RequestID, Error: &ErrObj{Code: "BAD_REQUEST", Message: "unknown type"}, TS: time.Now().UTC()})
		}
	}

	// cleanup: unsubscribe all
	for topicName, sub := range subs {
		_ = topicName
		sub.Close()
	}
	_ = conn.Close(websocket.StatusNormalClosure, "bye")
}

func subscriberWriteLoop(ctx context.Context, topicName string, sub *Subscriber) {
	// Writes events from sub.send to the socket as ServerToClient{type:"event"}.
	// Exit when channel closes or context done.
	for {
		select {
		case <-ctx.Done():
			return
		case m, ok := <-sub.send:
			if !ok {
				return
			}
			err := sub.conn.Write(context.Background(), ServerToClient{
				Type:    "event",
				Topic:   topicName,
				Message: &m,
				TS:      time.Now().UTC(),
			})
			if err != nil {
				sub.Close()
				return
			}
		}
	}
}

func publishToTopic(topicName string, m Message) error {
	t, err := globalTopics.GetTopic(topicName)
	if err != nil {
		return err
	}
	// record in history
	t.history.Add(m)
	// fan-out
	t.mu.RLock()
	defer t.mu.RUnlock()
	for id, s := range t.subscribers {
		select {
		case s.send <- m:
		default:
			// backpressure overflow: disconnect slow consumer
			log.Printf("disconnecting slow consumer %s on topic %s", id, topicName)
			_ = s.conn.Write(context.Background(), ServerToClient{
				Type:  "error",
				Error: &ErrObj{Code: "SLOW_CONSUMER", Message: "subscriber queue overflow"},
				TS:    time.Now().UTC(),
			})
			s.Close()
			delete(t.subscribers, id)
		}
	}
	t.msgCount++
	return nil
}

func (s *Subscriber) Close() {
	select {
	case <-s.closed:
		return
	default:
		close(s.closed)
	}
	close(s.send) // end writer loop
	_ = s.conn.Close(websocket.StatusNormalClosure, "subscriber closed")
}

func (s *Subscriber) CloseWithError(code, reason string) {
	_ = s.conn.Write(context.Background(), ServerToClient{
		Type:  "error",
		Error: &ErrObj{Code: code, Message: reason},
		TS:    time.Now().UTC(),
	})
	s.Close()
}

// Helpers for debugging JSON
func debugJSON(v any) string {
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}

func bad(msg string) error { return fmt.Errorf(msg) }

var subscriberQueueSize = func() int {
	if v := os.Getenv("SUBSCRIBER_QUEUE_SIZE"); v != "" {
		if n, _ := strconv.Atoi(v); n > 0 {
			return n
		}
	}
	return 100
}()

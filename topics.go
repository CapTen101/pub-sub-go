package main

import (
	"errors"
	"sync"
	"time"
)

var (
	ErrTopicExists   = errors.New("topic already exists")
	ErrTopicNotFound = errors.New("topic not found")
)

type TopicsManager struct {
	mu     sync.RWMutex
	topics map[string]*Topic
	start  time.Time
}

type Topic struct {
	name        string
	mu          sync.RWMutex
	subscribers map[string]*Subscriber
	history     *RingBuffer
	msgCount    int64
}

type Subscriber struct {
	id     string
	send   chan Message // bounded channel for backpressure
	conn   WSConn       // abstracted websocket conn
	closed chan struct{}
}

func NewTopicsManager() *TopicsManager {
	return &TopicsManager{
		topics: make(map[string]*Topic),
		start:  time.Now(),
	}
}

var globalTopics = NewTopicsManager()

func (tm *TopicsManager) CreateTopic(name string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if _, ok := tm.topics[name]; ok {
		return ErrTopicExists
	}
	tm.topics[name] = &Topic{
		name:        name,
		subscribers: make(map[string]*Subscriber),
		history:     NewRingBuffer(100),
	}
	return nil
}

func (tm *TopicsManager) GetTopic(name string) (*Topic, error) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	t, ok := tm.topics[name]
	if !ok {
		return nil, ErrTopicNotFound
	}
	return t, nil
}

func (tm *TopicsManager) DeleteTopic(name string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	t, ok := tm.topics[name]
	if !ok {
		return ErrTopicNotFound
	}
	// disconnect all subscribers
	t.mu.Lock()
	for _, sub := range t.subscribers {
		sub.Close()
	}
	t.mu.Unlock()
	delete(tm.topics, name)
	// broadcast info to others is handled on ws loop when reading this error response
	return nil
}

func (tm *TopicsManager) ListTopics() []map[string]any {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	out := make([]map[string]any, 0, len(tm.topics))
	for _, t := range tm.topics {
		t.mu.RLock()
		out = append(out, map[string]any{
			"name":        t.name,
			"subscribers": len(t.subscribers),
		})
		t.mu.RUnlock()
	}
	return out
}

func (tm *TopicsManager) Health() map[string]any {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	subs := 0
	for _, t := range tm.topics {
		t.mu.RLock()
		subs += len(t.subscribers)
		t.mu.RUnlock()
	}
	return map[string]any{
		"uptime_sec":  int(time.Since(tm.start).Seconds()),
		"topics":      len(tm.topics),
		"subscribers": subs,
	}
}

func (tm *TopicsManager) Stats() map[string]any {
	stats := map[string]any{"topics": map[string]any{}}
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	topics := stats["topics"].(map[string]any)
	for _, t := range tm.topics {
		t.mu.RLock()
		topics[t.name] = map[string]any{
			"messages":    t.msgCount,
			"subscribers": len(t.subscribers),
		}
		t.mu.RUnlock()
	}
	return stats
}

func (tm *TopicsManager) CloseAll() {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	for _, t := range tm.topics {
		t.mu.RLock()
		for _, s := range t.subscribers {
			s.Close()
		}
		t.mu.RUnlock()
	}
}

func (tm *TopicsManager) CloseAllGracefully(timeout time.Duration) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	for _, t := range tm.topics {
		t.mu.RLock()
		for _, sub := range t.subscribers {
			sub.CloseGracefully(timeout)
		}
		t.mu.RUnlock()
	}
}

func (s *Subscriber) CloseGracefully(timeout time.Duration) {
	close(s.closed) // signal stop
	// drain queue without blocking
	drainUntil := time.Now().Add(timeout)
	for {
		select {
		case <-s.send:
			if time.Now().After(drainUntil) {
				return
			}
		default:
			return
		}
	}
}

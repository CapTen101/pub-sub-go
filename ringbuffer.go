package main

import "sync"

// RingBuffer is a simple fixed-size ring buffer for Message.
type RingBuffer struct {
	mu   sync.RWMutex
	data []Message
	size int
	next int
	full bool
}

func NewRingBuffer(size int) *RingBuffer {
	return &RingBuffer{
		data: make([]Message, size),
		size: size,
	}
}

func (r *RingBuffer) Add(m Message) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data[r.next] = m
	r.next = (r.next + 1) % r.size
	if r.next == 0 {
		r.full = true
	}
}

func (r *RingBuffer) LastN(n int) []Message {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if n <= 0 || r.size == 0 {
		return nil
	}
	if !r.full {
		if n > r.next {
			n = r.next
		}
		out := make([]Message, 0, n)
		for i := r.next - n; i < r.next; i++ {
			out = append(out, r.data[i])
		}
		return out
	}
	// full
	if n > r.size {
		n = r.size
	}
	out := make([]Message, 0, n)
	start := (r.next - n + r.size) % r.size
	for i := 0; i < n; i++ {
		idx := (start + i) % r.size
		out = append(out, r.data[idx])
	}
	return out
}
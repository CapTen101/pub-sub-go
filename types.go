package main

import "time"

type ClientToServer struct {
	Type       string      `json:"type"`
	Topic      string      `json:"topic,omitempty"`
	Message    *Message    `json:"message,omitempty"`
	ClientID   string      `json:"client_id,omitempty"`
	LastN      int         `json:"last_n,omitempty"`
	RequestID  string      `json:"request_id,omitempty"`
}

type ServerToClient struct {
	Type      string      `json:"type"`
	RequestID string      `json:"request_id,omitempty"`
	Topic     string      `json:"topic,omitempty"`
	Message   *Message    `json:"message,omitempty"`
	Error     *ErrObj     `json:"error,omitempty"`
	TS        time.Time   `json:"ts,omitempty"`
	Status    string      `json:"status,omitempty"` // for ack
	Msg       string      `json:"msg,omitempty"`    // for info
}

type ErrObj struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Message struct {
	ID      string      `json:"id"`
	Payload interface{} `json:"payload"`
}
// Package stream provides an in-memory pub/sub Hub for broadcasting log events
// to sse subscribers per deployment ID.
package stream

import (
	"sync"
	"time"
)

type Event struct {
	ID        string    `json:"id"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

type Hub struct {
	mu      sync.RWMutex
	streams map[string]map[chan Event]struct{}
}

func NewHub() *Hub {
	return &Hub{
		streams: make(map[string]map[chan Event]struct{}),
	}
}

func (h *Hub) Subscribe(deploymentID string) chan Event {
	ch := make(chan Event, 32)

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.streams[deploymentID] == nil {
		h.streams[deploymentID] = make(map[chan Event]struct{})
	}

	h.streams[deploymentID][ch] = struct{}{}
	return ch
}

func (h *Hub) Unsubscribe(deploymentID string, ch chan Event) {
	h.mu.Lock()
	defer h.mu.Unlock()

	subs, ok := h.streams[deploymentID]
	if !ok {
		return
	}

	_, ok = subs[ch]
	if !ok {
		return
	}

	delete(subs, ch)

	if len(subs) == 0 {
		delete(h.streams, deploymentID)
	}
}

// Broadcast sends an event to all subscribers of a deployment.
// If a subscriber's channel buffer is full, the event is dropped
// to ensure that broadcasting to other subscribers remains unblocked.
func (h *Hub) Broadcast(deploymentID string, event Event) {
	h.mu.RLock()
	subs := h.streams[deploymentID]
	channs := make([]chan Event, 0, len(subs))
	for c := range subs {
		channs = append(channs, c)
	}
	h.mu.RUnlock()

	for _, c := range channs {
		select {
		case c <- event:
		default:
		}
	}
}

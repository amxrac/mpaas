package stream

import (
	"sync"
	"testing"
	"time"
)

func TestHubSubscribeAndBroadcast(t *testing.T) {
	hub := NewHub()
	ch := hub.Subscribe("deployment-1")

	event := Event{
		ID:        "event-1",
		Message:   "building",
		CreatedAt: time.Now(),
	}

	hub.Broadcast("deployment-1", event)

	select {
	case got := <-ch:
		if got != event {
			t.Fatalf("got %+v, want %+v", got, event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}

	hub.Unsubscribe("deployment-1", ch)
}

func TestHubBroadcastsToAllSubscribers(t *testing.T) {
	hub := NewHub()

	ch1 := hub.Subscribe("deployment-1")
	ch2 := hub.Subscribe("deployment-1")

	event := Event{
		ID:        "event-1",
		Message:   "building",
		CreatedAt: time.Now(),
	}

	hub.Broadcast("deployment-1", event)

	for i, ch := range []chan Event{ch1, ch2} {
		select {
		case got := <-ch:
			if got != event {
				t.Fatalf("subscriber %d: got %+v, want %+v", i, got, event)
			}
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d: timed out waiting for event", i)
		}
	}

	hub.Unsubscribe("deployment-1", ch1)
	hub.Unsubscribe("deployment-1", ch2)
}

func TestHubBroadcastIsolatedByDeployment(t *testing.T) {
	hub := NewHub()

	ch1 := hub.Subscribe("deployment-1")
	ch2 := hub.Subscribe("deployment-2")

	event := Event{
		ID:        "event-1",
		Message:   "building",
		CreatedAt: time.Now(),
	}

	hub.Broadcast("deployment-1", event)

	select {
	case got := <-ch1:
		if got != event {
			t.Fatalf("got %+v, want %+v", got, event)
		}
	case <-time.After(time.Second):
		t.Fatal("deployment-1 subscriber did not receive event")
	}

	select {
	case got := <-ch2:
		t.Fatalf("deployment-2 subscriber unexpectedly received %+v", got)
	case <-time.After(20 * time.Millisecond):
		// no event expected
	}
}

func TestHubUnsubscribe(t *testing.T) {
	hub := NewHub()
	ch := hub.Subscribe("deployment-1")

	hub.Unsubscribe("deployment-1", ch)

	hub.Broadcast("deployment-1", Event{
		ID:      "event-1",
		Message: "should not be received",
	})

	select {
	case got := <-ch:
		t.Fatalf("received event after unsubscribe: %+v", got)
	case <-time.After(20 * time.Millisecond):
		// no event expected
	}
}

func TestHubUnsubscribeUnknownSubscriber(t *testing.T) {
	hub := NewHub()

	ch := make(chan Event)

	// does not panic
	hub.Unsubscribe("deployment-1", ch)

	subs := hub.Subscribe("deployment-1")
	hub.Unsubscribe("unknown-deployment", subs)
	hub.Unsubscribe("deployment-1", ch)
	hub.Unsubscribe("deployment-1", subs)
}

func TestHubBroadcastDoesNotBlockOnFullSubscriber(t *testing.T) {
	hub := NewHub()
	ch := hub.Subscribe("deployment-1")

	event := Event{
		ID:      "event-1",
		Message: "log",
	}

	// subscribe buffers can hold 32 events
	for range 32 {
		hub.Broadcast("deployment-1", event)
	}

	done := make(chan struct{})

	go func() {
		hub.Broadcast("deployment-1", event)
		close(done)
	}()

	select {
	case <-done:
		// broadcast correclt drops the event instead of blocking
	case <-time.After(time.Second):
		t.Fatal("Broadcast blocked on full subscriber")
	}

	hub.Unsubscribe("deployment-1", ch)
}

func TestHubConcurrentAccess(t *testing.T) {
	hub := NewHub()

	const (
		goroutines = 20
		iterations = 100
	)

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for range goroutines {
		go func() {
			defer wg.Done()

			for range iterations {
				ch := hub.Subscribe("deployment-1")

				hub.Broadcast("deployment-1", Event{
					ID:      "event-1",
					Message: "log",
				})

				hub.Unsubscribe("deployment-1", ch)
			}
		}()
	}

	wg.Wait()
}

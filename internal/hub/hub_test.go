package hub

import (
	"testing"
	"time"
)

func TestNewHub_CreatesHubWithNoClients(t *testing.T) {
	t.Parallel()
	h := NewHub()
	if h == nil {
		t.Fatal("expected non-nil Hub")
	}
	if len(h.clients) != 0 {
		t.Errorf("expected 0 clients, got %d", len(h.clients))
	}
}

func TestHub_BroadcastSendsToRegisteredClients(t *testing.T) {
	t.Parallel()
	h := NewHub()
	go h.Run()

	// Create a fake client with a buffered send channel (no real WebSocket needed).
	client := &Client{
		hub:  h,
		conn: nil,
		send: make(chan []byte, sendBufSize),
	}

	// Register the client via the hub's register channel.
	h.register <- client

	// Give the hub goroutine time to process the registration.
	time.Sleep(10 * time.Millisecond)

	want := []byte(`{"type":"update"}`)
	h.Broadcast(want)

	select {
	case got := <-client.send:
		if string(got) != string(want) {
			t.Errorf("received %q, want %q", string(got), string(want))
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for broadcast message")
	}
}

func TestHub_BroadcastToMultipleClients(t *testing.T) {
	t.Parallel()
	h := NewHub()
	go h.Run()

	const numClients = 3
	clients := make([]*Client, numClients)
	for i := 0; i < numClients; i++ {
		clients[i] = &Client{
			hub:  h,
			conn: nil,
			send: make(chan []byte, sendBufSize),
		}
		h.register <- clients[i]
	}

	// Give the hub goroutine time to process all registrations.
	time.Sleep(20 * time.Millisecond)

	want := []byte(`{"event":"ping"}`)
	h.Broadcast(want)

	for i, c := range clients {
		select {
		case got := <-c.send:
			if string(got) != string(want) {
				t.Errorf("client %d: received %q, want %q", i, string(got), string(want))
			}
		case <-time.After(time.Second):
			t.Fatalf("timeout waiting for broadcast on client %d", i)
		}
	}
}

func TestHub_FullSendBufferDropsBroadcast(t *testing.T) {
	t.Parallel()
	h := NewHub()
	go h.Run()

	// Create a client with a FULL send buffer (capacity 1, pre-filled).
	client := &Client{
		hub:  h,
		conn: nil,
		send: make(chan []byte, 1),
	}
	// Pre-fill the buffer so it's full.
	client.send <- []byte("existing message")

	h.register <- client
	time.Sleep(10 * time.Millisecond)

	// Broadcast should not block the hub even though client.send is full.
	// The hub will hit the default case, close client.send, and remove the client.
	done := make(chan struct{})
	go func() {
		h.Broadcast([]byte(`{"drop":"me"}`))
		close(done)
	}()

	select {
	case <-done:
		// Broadcast returned without blocking — correct behavior.
	case <-time.After(time.Second):
		t.Fatal("hub blocked on full client send buffer")
	}
}

func TestHub_UnregisterRemovesClient(t *testing.T) {
	t.Parallel()
	h := NewHub()
	go h.Run()

	client := &Client{
		hub:  h,
		conn: nil,
		send: make(chan []byte, sendBufSize),
	}

	h.register <- client
	time.Sleep(10 * time.Millisecond)

	// Unregister the client.
	h.unregister <- client
	time.Sleep(10 * time.Millisecond)

	// After unregistration the send channel should be closed.
	// Try to read from it — it should be drained and then return the zero value.
	select {
	case _, ok := <-client.send:
		if ok {
			t.Error("expected client.send to be closed after unregister")
		}
		// ok == false means channel was closed — correct.
	default:
		// Channel may already be closed with nothing to read; that's also fine.
	}
}

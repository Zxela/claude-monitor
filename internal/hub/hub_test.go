package hub

import (
	"fmt"
	"sync"
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
	defer h.Stop()

	// Create a fake client with a buffered send channel (no real WebSocket needed).
	client := &Client{
		hub:  h,
		conn: nil,
		send: make(chan []byte, sendBufSize),
	}

	// Register the client via the hub's register channel.
	// register is unbuffered, so this blocks until the hub processes it.
	h.register <- client

	want := []byte(`{"type":"update"}`)
	h.Broadcast(want)

	select {
	case got := <-client.send:
		if string(got) != string(want) {
			t.Errorf("received %q, want %q", string(got), string(want))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for broadcast message")
	}
}

func TestHub_BroadcastToMultipleClients(t *testing.T) {
	t.Parallel()
	h := NewHub()
	go h.Run()
	defer h.Stop()

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

	want := []byte(`{"event":"ping"}`)
	h.Broadcast(want)

	for i, c := range clients {
		select {
		case got := <-c.send:
			if string(got) != string(want) {
				t.Errorf("client %d: received %q, want %q", i, string(got), string(want))
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timeout waiting for broadcast on client %d", i)
		}
	}
}

func TestHub_FullSendBufferDropsBroadcast(t *testing.T) {
	t.Parallel()
	h := NewHub()
	go h.Run()
	defer h.Stop()

	// Create a client with a FULL send buffer (capacity 1, pre-filled).
	client := &Client{
		hub:  h,
		conn: nil,
		send: make(chan []byte, 1),
	}
	// Pre-fill the buffer so it's full.
	client.send <- []byte("existing message")

	h.register <- client

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
	case <-time.After(2 * time.Second):
		t.Fatal("hub blocked on full client send buffer")
	}
}

func TestHub_UnregisterRemovesClient(t *testing.T) {
	t.Parallel()
	h := NewHub()
	go h.Run()
	defer h.Stop()

	client := &Client{
		hub:  h,
		conn: nil,
		send: make(chan []byte, sendBufSize),
	}

	h.register <- client

	// Unregister the client. Unbuffered channel, so this blocks until processed.
	h.unregister <- client

	// After unregistration the send channel should be closed.
	select {
	case _, ok := <-client.send:
		if ok {
			t.Error("expected client.send to be closed after unregister")
		}
		// ok == false means channel was closed — correct.
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for client.send to close")
	}
}

func TestHub_BroadcastAfterClientUnregistered(t *testing.T) {
	t.Parallel()
	h := NewHub()
	go h.Run()
	defer h.Stop()

	client := &Client{
		hub:  h,
		conn: nil,
		send: make(chan []byte, sendBufSize),
	}

	h.register <- client
	h.unregister <- client

	// Broadcasting after all clients have been unregistered must not panic.
	h.Broadcast([]byte(`{"after":"unregister"}`))

	// Send another message through the hub to confirm it is still functioning.
	// We do this by registering a new client and verifying it receives a broadcast.
	client2 := &Client{
		hub:  h,
		conn: nil,
		send: make(chan []byte, sendBufSize),
	}
	h.register <- client2

	want := `{"still":"alive"}`
	h.Broadcast([]byte(want))

	// Drain messages until we find the expected one. The earlier broadcast
	// (to no clients) may still be buffered and delivered to client2.
	timeout := time.After(2 * time.Second)
	for {
		select {
		case got := <-client2.send:
			if string(got) == want {
				return // success
			}
		case <-timeout:
			t.Fatal("timeout: hub stopped working after broadcast to unregistered client")
		}
	}
}

func TestHub_ConcurrentRegisterUnregister(t *testing.T) {
	t.Parallel()
	h := NewHub()
	go h.Run()
	defer h.Stop()

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			c := &Client{
				hub:  h,
				conn: nil,
				send: make(chan []byte, sendBufSize),
			}
			h.register <- c
			h.unregister <- c
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All goroutines completed without race or panic.
	case <-time.After(5 * time.Second):
		t.Fatal("timeout: concurrent register/unregister did not complete")
	}
}

func TestHub_MultipleSequentialBroadcastsReceivedInOrder(t *testing.T) {
	t.Parallel()
	h := NewHub()
	go h.Run()
	defer h.Stop()

	client := &Client{
		hub:  h,
		conn: nil,
		send: make(chan []byte, sendBufSize),
	}
	h.register <- client

	const msgCount = 10
	for i := 0; i < msgCount; i++ {
		h.Broadcast([]byte(fmt.Sprintf("msg-%d", i)))
	}

	for i := 0; i < msgCount; i++ {
		want := fmt.Sprintf("msg-%d", i)
		select {
		case got := <-client.send:
			if string(got) != want {
				t.Errorf("message %d: got %q, want %q", i, string(got), want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timeout waiting for message %d", i)
		}
	}
}

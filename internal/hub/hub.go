// Package hub implements a WebSocket broadcast hub using gorilla/websocket.
package hub

import (
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 512 * 1024 // 512 KB
	sendBufSize    = 256
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// Allow same-origin requests and localhost. Rejects cross-site WebSocket
	// hijacking while permitting access from any machine that loaded the UI.
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true // non-browser clients (curl, etc.)
		}
		// Same-origin: origin's host matches the Host header (covers LAN IPs).
		host := r.Host
		if strings.Contains(origin, "://"+host) {
			return true
		}
		// Also allow localhost variants for dev.
		for _, prefix := range []string{
			"http://localhost", "https://localhost",
			"http://127.0.0.1", "https://127.0.0.1",
			"http://[::1]", "https://[::1]",
		} {
			if origin == prefix || strings.HasPrefix(origin, prefix+":") {
				return true
			}
		}
		return false
	},
}

// Client is a single WebSocket connection registered with the Hub.
type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan []byte
}

// Hub maintains the set of active clients and broadcasts messages to them.
type Hub struct {
	clients    map[*Client]bool
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
	done       chan struct{}
}

// NewHub creates a Hub ready to be started with Run.
func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan []byte, 256),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		done:       make(chan struct{}),
	}
}

// Run processes register/unregister/broadcast events. Call in a goroutine.
// It returns when Stop is called.
func (h *Hub) Run() {
	for {
		select {
		case <-h.done:
			return

		case client := <-h.register:
			h.clients[client] = true

		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}

		case msg := <-h.broadcast:
			for client := range h.clients {
				select {
				case client.send <- msg:
				default:
					// Slow client — drop and disconnect.
					close(client.send)
					delete(h.clients, client)
				}
			}
		}
	}
}

// Stop signals the hub to stop processing events and return from Run.
func (h *Hub) Stop() {
	close(h.done)
}

// Broadcast enqueues msg for delivery to all connected clients.
// If the broadcast channel is full the message is dropped to avoid blocking
// the caller (typically the event pipeline goroutine).
func (h *Hub) Broadcast(msg []byte) {
	select {
	case h.broadcast <- msg:
	default:
		log.Printf("hub: broadcast channel full, dropping message (%d bytes)", len(msg))
	}
}

// GracefulStop sends a WebSocket close frame to every connected client, then
// stops the hub. This gives browsers the opportunity to handle the closure
// cleanly instead of seeing an abnormal TCP disconnect.
func (h *Hub) GracefulStop() {
	for client := range h.clients {
		client.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		client.conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseGoingAway, "server shutting down"))
		client.conn.Close()
	}
	h.Stop()
}

// ServeWs upgrades an HTTP connection to WebSocket and registers it with hub.
func ServeWs(hub *Hub, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade error: %v", err)
		return
	}

	client := &Client{
		hub:  hub,
		conn: conn,
		send: make(chan []byte, sendBufSize),
	}
	hub.register <- client

	go client.writePump()
	go client.readPump()
}

// readPump drains inbound messages (we don't act on them) and handles pong
// frames to keep the connection alive.
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("ws read error: %v", err)
			}
			break
		}
	}
}

// writePump serialises outgoing messages and sends periodic pings.
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// Hub closed the channel.
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				log.Printf("ws write error: %v", err)
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

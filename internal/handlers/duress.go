package handlers

import (
	"encoding/json"
	"log"
	"sync"

	"github.com/gofiber/websocket/v2"
)

// Minimal message both sides understand.
type wsMessage struct {
	Event  string `json:"event"`
	Data   string `json:"data"`
	RoomID string `json:"roomId,omitempty"`
}

// room socket pair + last offer
type roomSockets struct {
	mu          sync.RWMutex
	broadcaster *websocket.Conn
	viewer      *websocket.Conn
	lastOffer   *wsMessage // cache the most recent offer
}

var (
	roomsMu    sync.RWMutex
	socketByID = map[string]*roomSockets{}
)

func getRoomSockets(roomID string) *roomSockets {
	roomsMu.Lock()
	defer roomsMu.Unlock()
	rs, ok := socketByID[roomID]
	if !ok {
		rs = &roomSockets{}
		socketByID[roomID] = rs
	}
	return rs
}

// Victim WS
func DuressWebSocket(c *websocket.Conn) {
	roomID := c.Params("roomId")
	if roomID == "" {
		log.Println("DuressWebSocket: missing roomId")
		return
	}
	rs := getRoomSockets(roomID)

	rs.mu.Lock()
	if rs.broadcaster != nil {
		_ = rs.broadcaster.Close()
	}
	rs.broadcaster = c
	rs.mu.Unlock()

	log.Printf("Broadcaster connected to room: %s\n", roomID)
	defer func() {
		rs.mu.Lock()
		if rs.broadcaster == c {
			rs.broadcaster = nil
		}
		rs.mu.Unlock()
		_ = c.Close()
		log.Printf("Broadcaster disconnected for room: %s\n", roomID)
	}()

	for {
		_, raw, err := c.ReadMessage()
		if err != nil {
			log.Printf("Broadcaster read error: %v\n", err)
			return
		}
		var msg wsMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			msg = wsMessage{Event: "unknown", Data: string(raw)}
		}

		// Cache the latest OFFER so late viewers can receive it immediately
		if msg.Event == "offer" {
			rs.mu.Lock()
			copy := msg
			rs.lastOffer = &copy
			rs.mu.Unlock()
		}

		rs.mu.RLock()
		v := rs.viewer
		rs.mu.RUnlock()
		if v == nil {
			continue
		}
		if err := v.WriteJSON(msg); err != nil {
			log.Printf("Relay to viewer failed: %v\n", err)
		}
	}
}

// Helper WS
func DuressViewerWebSocket(c *websocket.Conn) {
	roomID := c.Params("roomId")
	if roomID == "" {
		log.Println("DuressViewerWebSocket: missing roomId")
		return
	}
	rs := getRoomSockets(roomID)

	rs.mu.Lock()
	if rs.viewer != nil {
		_ = rs.viewer.Close()
	}
	rs.viewer = c
	// snapshot any cached offer
	cachedOffer := rs.lastOffer
	rs.mu.Unlock()

	log.Printf("Viewer connected to room: %s\n", roomID)
	defer func() {
		rs.mu.Lock()
		if rs.viewer == c {
			rs.viewer = nil
		}
		rs.mu.Unlock()
		_ = c.Close()
		log.Printf("Viewer disconnected for room: %s\n", roomID)
	}()

	// Immediately push the cached OFFER if we have one
	if cachedOffer != nil {
		_ = c.WriteJSON(cachedOffer)
	}

	for {
		_, raw, err := c.ReadMessage()
		if err != nil {
			log.Printf("Viewer read error: %v\n", err)
			return
		}
		var msg wsMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			msg = wsMessage{Event: "unknown", Data: string(raw)}
		}
		rs.mu.RLock()
		bc := rs.broadcaster
		rs.mu.RUnlock()
		if bc == nil {
			continue
		}
		if err := bc.WriteJSON(msg); err != nil {
			log.Printf("Relay to broadcaster failed: %v\n", err)
		}
	}
}

package handlers

import (
	"fmt"
	"log"

	"github.com/gofiber/websocket/v2"
	w "webrtc-streaming/pkg/webrtc"
)

// WS /duress/:roomId/websocket
// Broadcaster (victim) WebSocket: publishes media into the room
func DuressWebSocket(c *websocket.Conn) {
	roomID := c.Params("roomId")
	if roomID == "" {
		log.Println("DuressWebSocket: missing roomId")
		return
	}

	w.RoomsLock.RLock()
	room, ok := w.Rooms[roomID]
	w.RoomsLock.RUnlock()

	if !ok || room == nil {
		log.Printf("DuressWebSocket: room not found for ID %s\n", roomID)
		return
	}

	log.Printf("Broadcaster connected to duress room: %s\n", roomID)
	// Optional: notify any connected viewers
	room.Peers.Broadcast("duress-alert", fmt.Sprintf("Duress stream triggered for room %s", roomID))

	// Broadcaster handler: creates PC, receives remote track(s), rebroadcasts to viewers
	w.RoomConn(c, room.Peers, room)
}

// WS /duress/:roomId/viewer/websocket
// Viewer (helper) WebSocket: subscribes to the room media
func DuressViewerWebSocket(c *websocket.Conn) {
	roomID := c.Params("roomId")
	if roomID == "" {
		log.Println("DuressViewerWebSocket: missing roomId")
		return
	}

	w.RoomsLock.RLock()
	room, ok := w.Rooms[roomID]
	w.RoomsLock.RUnlock()

	if !ok || room == nil {
		log.Printf("DuressViewerWebSocket: room not found for ID %s\n", roomID)
		return
	}

	log.Printf("Viewer connected to duress room: %s\n", roomID)
	// Viewer handler: handles offer/answer exchange and ICE with the viewer peer
	w.StreamConn(c, room.Peers, room)
	log.Printf("Viewer WebSocket closed for room: %s\n", roomID)
}

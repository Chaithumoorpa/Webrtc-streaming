package handlers

import (
    "log"

    "github.com/gofiber/websocket/v2"
    w "webrtc-streaming/pkg/webrtc"
)

// WebSocket for broadcaster (victim)
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

    log.Printf("Broadcaster connected to room: %s\n", roomID)
    room.Peers.Broadcast("duress-alert", "Duress stream triggered")

    w.RoomConn(c, room.Peers, room)
}

// WebSocket for viewer (helper)
func DuressViewerWebSocket(c *websocket.Conn) {
    roomID := c.Params("roomId")
    if roomID == "" {
        log.Println("DuressViewerWebSocket: missing roomId")
        return
    }

    log.Printf("Viewer attempting to connect to room: %s\n", roomID)

    w.RoomsLock.RLock()
    room, ok := w.Rooms[roomID]
    w.RoomsLock.RUnlock()

    if !ok || room == nil {
        log.Printf("DuressViewerWebSocket: room not found for ID %s\n", roomID)
        return
    }

    log.Printf("Viewer WebSocket connected to room: %s\n", roomID)
    w.StreamConn(c, room.Peers, room)
    log.Printf("Viewer WebSocket closed for room: %s\n", roomID)
}

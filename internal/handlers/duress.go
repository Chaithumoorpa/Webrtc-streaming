package handlers

import (
    "fmt"
    // "os"

    "github.com/gofiber/websocket/v2"
    w "webrtc-streaming/pkg/webrtc"
)

func DuressWebSocket(c *websocket.Conn) {
    roomID := c.Params("roomId")
    if roomID == "" {
        return
    }

    w.RoomsLock.RLock()
    room, ok := w.Rooms[roomID]
    w.RoomsLock.RUnlock()

    if !ok || room == nil {
        return
    }

    room.Peers.Broadcast("duress-alert", "Duress stream triggered")
    w.RoomConn(c, room.Peers)
}

func DuressViewerWebSocket(c *websocket.Conn) {
    roomID := c.Params("roomId")
    if roomID == "" {
        fmt.Println("Viewer connection rejected: missing roomId")
        return
    }

    fmt.Println("Viewer attempting to connect to room:", roomID)

    w.RoomsLock.RLock()
    room, ok := w.Rooms[roomID]
    w.RoomsLock.RUnlock()

    if !ok || room == nil {
        fmt.Println("Viewer connection failed: room not found:", roomID)
        return
    }

    fmt.Println("Viewer WebSocket connected to room:", roomID)
    w.StreamConn(c, room.Peers)
    fmt.Println("Viewer WebSocket closed for room:", roomID)
}

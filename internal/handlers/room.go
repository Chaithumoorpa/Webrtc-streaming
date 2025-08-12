package handlers

import (
    "crypto/sha256"
    "fmt"
    "log"
    "time"

    "github.com/gofiber/websocket/v2"
    "github.com/pion/webrtc/v3"
    w "webrtc-streaming/pkg/webrtc"
)

// WebSocket for broadcaster
func RoomWebsocket(c *websocket.Conn) {
    uuid := c.Params("uuid")
    if uuid == "" {
        log.Println("RoomWebsocket: missing uuid")
        return
    }

    _, _, room := createOrGetRoom(uuid)
    log.Printf("Broadcaster connected to room: %s\n", uuid)
    w.RoomConn(c, room.Peers, room)
}

// WebSocket for viewer
func RoomViewerWebsocket(c *websocket.Conn) {
    uuid := c.Params("uuid")
    if uuid == "" {
        log.Println("RoomViewerWebsocket: missing uuid")
        return
    }

    w.RoomsLock.Lock()
    room, ok := w.Rooms[uuid]
    w.RoomsLock.Unlock()

    if !ok || room == nil {
        log.Printf("RoomViewerWebsocket: room not found for uuid %s\n", uuid)
        return
    }

    log.Printf("Viewer connected to room: %s\n", uuid)
    roomViewerConn(c, room.Peers)
}

// Create or retrieve room
func createOrGetRoom(uuid string) (string, string, *w.Room) {
    w.RoomsLock.Lock()
    defer w.RoomsLock.Unlock()

    suuid := generateStreamUUID(uuid)

    // Return existing room if available
    if room := w.Rooms[uuid]; room != nil {
        if _, exists := w.Streams[suuid]; !exists {
            w.Streams[suuid] = room
        }
        return uuid, suuid, room
    }

    // Create new room
    peers := &w.Peers{TrackLocals: make(map[string]*webrtc.TrackLocalStaticRTP)}
    room := &w.Room{Peers: peers}
    w.Rooms[uuid] = room
    w.Streams[suuid] = room

    log.Printf("New room created: %s (stream ID: %s)\n", uuid, suuid)
    return uuid, suuid, room
}

// Generate hashed stream UUID
func generateStreamUUID(uuid string) string {
    h := sha256.New()
    h.Write([]byte(uuid))
    return fmt.Sprintf("%x", h.Sum(nil))
}

// Viewer connection ticker
func roomViewerConn(c *websocket.Conn, peers *w.Peers) {
    ticker := time.NewTicker(1 * time.Second)
    defer ticker.Stop()
    defer c.Close()

    for {
        select {
        case <-ticker.C:
            writer, err := c.Conn.NextWriter(websocket.TextMessage)
            if err != nil {
                log.Println("roomViewerConn: failed to get writer:", err)
                return
            }

            _, err = writer.Write([]byte(fmt.Sprintf("%d", len(peers.Connections))))
            if err != nil {
                log.Println("roomViewerConn: failed to write viewer count:", err)
                return
            }
        }
    }
}

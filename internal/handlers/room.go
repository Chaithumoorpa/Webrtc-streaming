package handlers

import (
    "crypto/sha256"
    "fmt"
    "time"

    "github.com/gofiber/websocket/v2"
    "github.com/pion/webrtc/v3"
    w "webrtc-streaming/pkg/webrtc"
)

// 🔌 WebSocket for broadcaster
func RoomWebsocket(c *websocket.Conn) {
    uuid := c.Params("uuid")
    if uuid == "" {
        return
    }
    _, _, room := createOrGetRoom(uuid)
    w.RoomConn(c, room.Peers)
}

// WebSocket for viewer
func RoomViewerWebsocket(c *websocket.Conn) {
    uuid := c.Params("uuid")
    if uuid == "" {
        return
    }

    w.RoomsLock.Lock()
    if peer, ok := w.Rooms[uuid]; ok {
        w.RoomsLock.Unlock()
        roomViewerConn(c, peer.Peers)
        return
    }
    w.RoomsLock.Unlock()
}

// Create or retrieve room
func createOrGetRoom(uuid string) (string, string, *w.Room) {
    w.RoomsLock.Lock()
    defer w.RoomsLock.Unlock()

    h := sha256.New()
    h.Write([]byte(uuid))
    suuid := fmt.Sprintf("%x", h.Sum(nil))

    if room := w.Rooms[uuid]; room != nil {
        if _, ok := w.Streams[suuid]; !ok {
            w.Streams[suuid] = room
        }
        return uuid, suuid, room
    }

    p := &w.Peers{TrackLocals: make(map[string]*webrtc.TrackLocalStaticRTP)}
    room := &w.Room{Peers: p}
    w.Rooms[uuid] = room
    w.Streams[suuid] = room

    return uuid, suuid, room
}

// Viewer connection ticker (optional)
func roomViewerConn(c *websocket.Conn, p *w.Peers) {
    ticker := time.NewTicker(1 * time.Second)
    defer ticker.Stop()
    defer c.Close()

    for {
        select {
        case <-ticker.C:
            w, err := c.Conn.NextWriter(websocket.TextMessage)
            if err != nil {
                return
            }
            w.Write([]byte(fmt.Sprintf("%d", len(p.Connections))))
        }
    }
}

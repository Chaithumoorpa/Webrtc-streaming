package handlers

import (
    "fmt"
    "os"
    "time"

    "github.com/gofiber/fiber/v2"
    "github.com/gofiber/websocket/v2"
    w "webrtc-streaming/pkg/webrtc"
)

// Stream metadata endpoint (optional)
func Stream(c *fiber.Ctx) error {
    suuid := c.Params("suuid")
    if suuid == "" {
        return c.Status(400).JSON(fiber.Map{"error": "Missing stream ID"})
    }

    ws := "ws"
    if os.Getenv("ENVIRONMENT") == "PRODUCTION" {
        ws = "wss"
    }

    w.RoomsLock.Lock()
    room, ok := w.Streams[suuid]
    w.RoomsLock.Unlock()

    if !ok || room == nil {
        return c.Status(404).JSON(fiber.Map{
            "streamId": suuid,
            "status":   "not_found",
        })
    }

    return c.JSON(fiber.Map{
        "streamId":        suuid,
        "status":          "active",
        "timestamp":       time.Now().Unix(),
        "hostname":        c.Hostname(),
        "type":            "stream",
        "streamWebSocket": fmt.Sprintf("%s://%s/stream/%s/websocket", ws, c.Hostname(), suuid),
        "viewerWebSocket": fmt.Sprintf("%s://%s/stream/%s/viewer/websocket", ws, c.Hostname(), suuid),
    })
}

// WebSocket for broadcaster
func StreamWebSocket(c *websocket.Conn) {
    suuid := c.Params("suuid")
    if suuid == "" {
        return
    }

    w.RoomsLock.Lock()
    if stream, ok := w.Streams[suuid]; ok {
        w.RoomsLock.Unlock()
        w.StreamConn(c, stream.Peers)
        return
    }
    w.RoomsLock.Unlock()
}

// WebSocket for viewer
func StreamViewerWebSocket(c *websocket.Conn) {
    suuid := c.Params("suuid")
    if suuid == "" {
        return
    }

    w.RoomsLock.Lock()
    if stream, ok := w.Streams[suuid]; ok {
        w.RoomsLock.Unlock()
        viewerConn(c, stream.Peers)
        return
    }
    w.RoomsLock.Unlock()
}

// Viewer connection ticker (optional)
func viewerConn(c *websocket.Conn, p *w.Peers) {
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

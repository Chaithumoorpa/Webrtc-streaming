package handlers

import (
    "fmt"
    "log"
    "os"
    "time"

    "github.com/gofiber/fiber/v2"
    "github.com/gofiber/websocket/v2"
    w "webrtc-streaming/pkg/webrtc"
)

// Stream metadata endpoint
func Stream(c *fiber.Ctx) error {
    suuid := c.Params("suuid")
    if suuid == "" {
        log.Println("Stream: missing stream ID")
        return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Missing stream ID"})
    }

    protocol := "ws"
    if os.Getenv("ENVIRONMENT") == "PRODUCTION" {
        protocol = "wss"
    }

    w.RoomsLock.Lock()
    stream, ok := w.Streams[suuid]
    w.RoomsLock.Unlock()

    if !ok || stream == nil {
        log.Printf("Stream: stream not found for ID %s\n", suuid)
        return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
            "streamId": suuid,
            "status":   "not_found",
        })
    }

    log.Printf("Stream: metadata served for stream ID %s\n", suuid)
    return c.JSON(fiber.Map{
        "streamId":        suuid,
        "status":          "active",
        "timestamp":       time.Now().Unix(),
        "hostname":        c.Hostname(),
        "type":            "stream",
        "streamWebSocket": fmt.Sprintf("%s://%s/stream/%s/websocket", protocol, c.Hostname(), suuid),
        "viewerWebSocket": fmt.Sprintf("%s://%s/stream/%s/viewer/websocket", protocol, c.Hostname(), suuid),
    })
}

// WebSocket for broadcaster
func StreamWebSocket(c *websocket.Conn) {
    suuid := c.Params("suuid")
    if suuid == "" {
        log.Println("StreamWebSocket: missing stream ID")
        return
    }

    w.RoomsLock.Lock()
    stream, ok := w.Streams[suuid]
    w.RoomsLock.Unlock()

    if !ok || stream == nil {
        log.Printf("StreamWebSocket: stream not found for ID %s\n", suuid)
        return
    }

    log.Printf("Broadcaster connected to stream: %s\n", suuid)
    w.StreamConn(c, stream.Peers, stream)
}

// WebSocket for viewer
func StreamViewerWebSocket(c *websocket.Conn) {
    suuid := c.Params("suuid")
    if suuid == "" {
        log.Println("StreamViewerWebSocket: missing stream ID")
        return
    }

    w.RoomsLock.Lock()
    stream, ok := w.Streams[suuid]
    w.RoomsLock.Unlock()

    if !ok || stream == nil {
        log.Printf("StreamViewerWebSocket: stream not found for ID %s\n", suuid)
        return
    }

    log.Printf("Viewer connected to stream: %s\n", suuid)
    viewerConn(c, stream.Peers)
}

// Viewer connection ticker
func viewerConn(c *websocket.Conn, peers *w.Peers) {
    ticker := time.NewTicker(1 * time.Second)
    defer ticker.Stop()
    defer c.Close()

    for {
        select {
        case <-ticker.C:
            writer, err := c.Conn.NextWriter(websocket.TextMessage)
            if err != nil {
                log.Println("viewerConn: failed to get writer:", err)
                return
            }

            _, err = writer.Write([]byte(fmt.Sprintf("%d", len(peers.Connections))))
            if err != nil {
                log.Println("viewerConn: failed to write viewer count:", err)
                return
            }
        }
    }
}

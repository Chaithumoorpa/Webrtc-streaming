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

// GET /stream/:suuid
// Returns stream metadata + websocket endpoints
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

	w.RoomsLock.RLock()
	stream, ok := w.Streams[suuid]
	w.RoomsLock.RUnlock()

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
		// Broadcaster (victim) WS endpoint for this stream
		"streamWebSocket": fmt.Sprintf("%s://%s/stream/%s/websocket", protocol, c.Hostname(), suuid),
		// Viewer (helper) WS endpoint for this stream
		"viewerWebSocket": fmt.Sprintf("%s://%s/stream/%s/viewer/websocket", protocol, c.Hostname(), suuid),
	})
}

// WS /stream/:suuid/websocket
// Broadcaster (victim) WebSocket: creates PC, receives remote tracks, relays SDP/ICE to viewers
func StreamWebSocket(c *websocket.Conn) {
	suuid := c.Params("suuid")
	if suuid == "" {
		log.Println("StreamWebSocket: missing stream ID")
		return
	}

	w.RoomsLock.RLock()
	stream, ok := w.Streams[suuid]
	w.RoomsLock.RUnlock()

	if !ok || stream == nil {
		log.Printf("StreamWebSocket: stream not found for ID %s\n", suuid)
		return
	}

	log.Printf("Broadcaster connected to stream: %s\n", suuid)
	// IMPORTANT: broadcaster uses RoomConn (role=broadcaster inside)
	w.RoomConn(c, stream.Peers, stream)
}

// WS /stream/:suuid/viewer/websocket
// Viewer (helper) WebSocket: receives the offer from Peers, sends answer/ICE back
func StreamViewerWebSocket(c *websocket.Conn) {
	suuid := c.Params("suuid")
	if suuid == "" {
		log.Println("StreamViewerWebSocket: missing stream ID")
		return
	}

	w.RoomsLock.RLock()
	stream, ok := w.Streams[suuid]
	w.RoomsLock.RUnlock()

	if !ok || stream == nil {
		log.Printf("StreamViewerWebSocket: stream not found for ID %s\n", suuid)
		return
	}

	log.Printf("Viewer connected to stream: %s\n", suuid)
	// IMPORTANT: viewer uses StreamConn (role=viewer inside)
	w.StreamConn(c, stream.Peers, stream)
}

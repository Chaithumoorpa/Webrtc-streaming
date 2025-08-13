package handlers

import (
	"crypto/sha256"
	"fmt"
	"log"

	"github.com/gofiber/websocket/v2"
	"github.com/pion/webrtc/v3"
	w "webrtc-streaming/pkg/webrtc"
)

// Broadcaster WebSocket: the victim connects here
func RoomWebsocket(c *websocket.Conn) {
	uuid := c.Params("uuid")
	if uuid == "" {
		log.Println("RoomWebsocket: missing uuid")
		return
	}

	_, _, room := createOrGetRoom(uuid)
	log.Printf("Broadcaster connected to room: %s\n", uuid)

	// Hand over to signaling-aware broadcaster handler
	w.RoomConn(c, room.Peers, room)
}

// Viewer WebSocket: the helper connects here
func RoomViewerWebsocket(c *websocket.Conn) {
	uuid := c.Params("uuid")
	if uuid == "" {
		log.Println("RoomViewerWebsocket: missing uuid")
		return
	}

	// Ensure room exists
	w.RoomsLock.RLock()
	room, ok := w.Rooms[uuid]
	w.RoomsLock.RUnlock()

	if !ok || room == nil {
		log.Printf("RoomViewerWebsocket: room not found for uuid %s\n", uuid)
		return
	}

	log.Printf("Viewer connected to room: %s\n", uuid)

	// Hand over to signaling-aware viewer handler
	w.StreamConn(c, room.Peers, room)
}

// Create or retrieve room safely; also make sure global registries are initialized
func createOrGetRoom(uuid string) (string, string, *w.Room) {
	w.RoomsLock.Lock()
	defer w.RoomsLock.Unlock()

	// Ensure global registries exist
	if w.Rooms == nil {
		w.Rooms = make(map[string]*w.Room)
	}
	if w.Streams == nil {
		w.Streams = make(map[string]*w.Room)
	}

	suuid := generateStreamUUID(uuid)

	// Return existing room if available
	if room := w.Rooms[uuid]; room != nil {
		if _, exists := w.Streams[suuid]; !exists {
			w.Streams[suuid] = room
		}
		return uuid, suuid, room
	}

	// Create new room
	peers := &w.Peers{
		TrackLocals: make(map[string]*webrtc.TrackLocalStaticRTP),
	}
	room := &w.Room{
		Peers: peers,
	}
	// Important: let peers know their room
	peers.Room = room

	w.Rooms[uuid] = room
	w.Streams[suuid] = room

	log.Printf("New room created: %s (stream ID: %s)\n", uuid, suuid)
	return uuid, suuid, room
}

// Generate a deterministic stream UUID from the room uuid
func generateStreamUUID(uuid string) string {
	h := sha256.New()
	_, _ = h.Write([]byte(uuid))
	return fmt.Sprintf("%x", h.Sum(nil))
}

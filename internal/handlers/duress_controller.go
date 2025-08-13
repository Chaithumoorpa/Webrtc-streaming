package handlers

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/pion/webrtc/v3"
	w "webrtc-streaming/pkg/webrtc"
)

// ----------------- Types -----------------

type HelpRequest struct {
	Name   string `json:"name"`
	Zone   string `json:"zone"`
	Mobile string `json:"mobile"`
	Status string `json:"status"` // "open" | "taken" | "closed"
}

// ----------------- Global State -----------------

var (
	helpLock             sync.RWMutex
	helpRequests         []HelpRequest           // append-only list of requests
	helpAcknowledgements = make(map[string]string) // victimName -> helperName
	nameToRoom           = make(map[string]string) // victimName -> roomID
)

// ----------------- Helpers -----------------

func wsScheme() string {
	if os.Getenv("ENVIRONMENT") == "PRODUCTION" {
		return "wss"
	}
	return "ws"
}

// ----------------- HTTP Handlers -----------------

// POST /duress/help
// Start a help + streaming session for a victim.
func StartHelpSession(c *fiber.Ctx) error {
	var req HelpRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request"})
	}
	req.Status = "open"

	// Create a room
	roomID := uuid.NewString()
	streamID := roomID

	peers := &w.Peers{TrackLocals: make(map[string]*webrtc.TrackLocalStaticRTP)}
	room := &w.Room{Peers: peers}

	// Register room
	w.RoomsLock.Lock()
	if w.Rooms == nil {
		w.Rooms = make(map[string]*w.Room)
	}
	if w.Streams == nil {
		w.Streams = make(map[string]*w.Room)
	}
	w.Rooms[roomID] = room
	w.Streams[streamID] = room
	w.RoomsLock.Unlock()

	// Track request + mapping
	helpLock.Lock()
	helpRequests = append(helpRequests, req)
	nameToRoom[req.Name] = roomID
	helpLock.Unlock()

	ws := wsScheme()
	host := c.Hostname()

	return c.JSON(fiber.Map{
		"status":    "success",
		"roomId":    roomID,
		"streamId":  streamID,
		"timestamp": time.Now().Unix(),
		"user": fiber.Map{
			"name":   req.Name,
			"zone":   req.Zone,
			"mobile": req.Mobile,
		},
		"broadcasterWs":      fmt.Sprintf("%s://%s/duress/%s/websocket", ws, host, roomID),
		"viewerWebsocketUrl": fmt.Sprintf("%s://%s/duress/%s/viewer/websocket", ws, host, roomID),
	})
}

// GET /duress/listen
// Helpers poll for the latest OPEN request (not taken/closed).
func DuressListen(c *fiber.Ctx) error {
	helpLock.RLock()
	defer helpLock.RUnlock()

	ws := wsScheme()
	host := c.Hostname()

	for i := len(helpRequests) - 1; i >= 0; i-- {
		req := helpRequests[i]
		if req.Status == "open" { // do not surface "taken" here
			rid := nameToRoom[req.Name] // may be empty if room not created
			resp := fiber.Map{
				"name":   req.Name,
				"zone":   req.Zone,
				"mobile": req.Mobile,
				"status": req.Status,
			}
			if rid != "" {
				resp["roomId"]             = rid
				resp["viewerWebsocketUrl"] = fmt.Sprintf("%s://%s/duress/%s/viewer/websocket", ws, host, rid)
				resp["broadcasterWs"]      = fmt.Sprintf("%s://%s/duress/%s/websocket", ws, host, rid)
			}
			return c.JSON(resp)
		}
	}
	return c.JSON(nil)
}

// POST /duress/give_help  (form: name=<victim>&helper=<helper>)
func GiveHelp(c *fiber.Ctx) error {
	requester := c.FormValue("name")
	helper := c.FormValue("helper")
	if requester == "" || helper == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Missing parameters"})
	}

	helpLock.Lock()
	defer helpLock.Unlock()

	helpAcknowledgements[requester] = helper
	for i, req := range helpRequests {
		if req.Name == requester {
			helpRequests[i].Status = "taken" // keep session open; client logic simpler
			break
		}
	}
	return c.JSON(fiber.Map{"status": "success"})
}

// POST /duress/listen_for_helper (form: name=<victim>)
func ListenForHelper(c *fiber.Ctx) error {
	requester := c.FormValue("name")
	if requester == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Missing requester name"})
	}

	helpLock.RLock()
	helper, ok := helpAcknowledgements[requester]
	status := "open"
	if ok {
		// compute status (open/taken stays "open" here; only "closed" after help_completed)
		for _, r := range helpRequests {
			if r.Name == requester {
				if r.Status == "closed" {
					status = "closed"
				}
				break
			}
		}
	}
	rid := nameToRoom[requester]
	helpLock.RUnlock()

	if !ok {
		return c.JSON(nil)
	}

	ws := wsScheme()
	host := c.Hostname()

	return c.JSON(fiber.Map{
		"helper":              helper,
		"status":              status,
		"roomId":              rid,
		"viewerWebsocketUrl":  fmt.Sprintf("%s://%s/duress/%s/viewer/websocket", ws, host, rid),
		"broadcasterWs":       fmt.Sprintf("%s://%s/duress/%s/websocket", ws, host, rid),
	})
}

// POST /duress/help_completed  (form: name=<victim>&helper=<helper>)
func HelpCompleted(c *fiber.Ctx) error {
	requester := c.FormValue("name")
	helper := c.FormValue("helper")
	if requester == "" || helper == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Missing parameters"})
	}

	helpLock.Lock()
	defer helpLock.Unlock()

	delete(helpAcknowledgements, requester)
	for i, req := range helpRequests {
	if req.Name == requester {
			helpRequests[i].Status = "closed"
			break
		}
	}
	return c.JSON(fiber.Map{"status": "success"})
}

// ---------- Optional REST fallbacks for SDP/ICE fanout (WS-first flow already handles this) ----------

// POST /duress/broadcast/sdp
func BroadcastSDP(c *fiber.Ctx) error {
	var req struct {
		RoomID string `json:"roomId"`
		SDP    string `json:"sdp"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request"})
	}

	w.RoomsLock.RLock()
	room, ok := w.Rooms[req.RoomID]
	w.RoomsLock.RUnlock()
	if !ok || room == nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Room not found"})
	}

	room.Peers.Broadcast("offer", req.SDP)
	return c.JSON(fiber.Map{"status": "success"})
}

// POST /duress/broadcast/candidate
func BroadcastCandidate(c *fiber.Ctx) error {
	var req struct {
		RoomID    string `json:"roomId"`
		Candidate string `json:"candidate"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid candidate"})
	}

	w.RoomsLock.RLock()
	room, ok := w.Rooms[req.RoomID]
	w.RoomsLock.RUnlock()
	if !ok || room == nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Room not found"})
	}

	room.Peers.Broadcast("candidate", req.Candidate)
	return c.JSON(fiber.Map{"status": "success"})
}

// POST /duress/viewer/sdp
func ViewerSDP(c *fiber.Ctx) error {
	var req struct {
		RoomID string `json:"roomId"`
		SDP    string `json:"sdp"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request"})
	}

	w.RoomsLock.RLock()
	room, ok := w.Rooms[req.RoomID]
	w.RoomsLock.RUnlock()
	if !ok || room == nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Room not found"})
	}

	room.Peers.Broadcast("answer", req.SDP)
	return c.JSON(fiber.Map{"status": "success"})
}

// POST /duress/viewer/candidate
func ViewerCandidate(c *fiber.Ctx) error {
	var req struct {
		RoomID    string `json:"roomId"`
		Candidate string `json:"candidate"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid candidate"})
	}

	w.RoomsLock.RLock()
	room, ok := w.Rooms[req.RoomID]
	w.RoomsLock.RUnlock()
	if !ok || room == nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Room not found"})
	}

	room.Peers.Broadcast("candidate", req.Candidate)
	return c.JSON(fiber.Map{"status": "success"})
}

// GET /duress/session_info?name=<victim>
func SessionInfo(c *fiber.Ctx) error {
	name := c.Query("name")
	if name == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Missing name"})
	}

	helpLock.RLock()
	roomID, ok := nameToRoom[name]
	helpLock.RUnlock()
	if !ok {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Not found"})
	}

	ws := wsScheme()
	host := c.Hostname()

	return c.JSON(fiber.Map{
		"roomId":             roomID,
		"viewerWebsocketUrl": fmt.Sprintf("%s://%s/duress/%s/viewer/websocket", ws, host, roomID),
		"broadcasterWs":      fmt.Sprintf("%s://%s/duress/%s/websocket", ws, host, roomID),
	})
}

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

// Help session model
type HelpRequest struct {
    Name   string `json:"name"`
    Zone   string `json:"zone"`
    Mobile string `json:"mobile"`
    Status string `json:"status"` // "open" or "closed"
}

// Global tracking
var (
    helpLock             sync.RWMutex
    helpRequests         []HelpRequest
    helpAcknowledgements = make(map[string]string)
)

// 1. Start Help + Stream Session
func StartHelpSession(c *fiber.Ctx) error {
    var req HelpRequest
    if err := c.BodyParser(&req); err != nil {
        return c.Status(400).JSON(fiber.Map{"error": "Invalid request"})
    }

    req.Status = "open"

    helpLock.Lock()
    helpRequests = append(helpRequests, req)
    helpLock.Unlock()

    roomID := uuid.New().String()
    suuid := fmt.Sprintf("%x", roomID)
    w.ActiveRoomID = roomID

    peers := &w.Peers{TrackLocals: make(map[string]*webrtc.TrackLocalStaticRTP)}
    room := &w.Room{Peers: peers}

    w.RoomsLock.Lock()
    w.Rooms[roomID] = room
    w.Streams[suuid] = room
    w.RoomsLock.Unlock()

    ws := "ws"
    if os.Getenv("ENVIRONMENT") == "PRODUCTION" {
        ws = "wss"
    }

    return c.JSON(fiber.Map{
        "status":    "success",
        "roomId":    roomID,
        "streamId":  suuid,
        "timestamp": time.Now().Unix(),
        "user": fiber.Map{
            "name":   req.Name,
            "zone":   req.Zone,
            "mobile": req.Mobile,
        },
        "broadcasterWs":      fmt.Sprintf("%s://%s/duress/%s/websocket", ws, c.Hostname(), roomID),
        "viewerWebsocketUrl": fmt.Sprintf("%s://%s/duress/%s/viewer/websocket", ws, c.Hostname(), roomID),
    })
}

// 2. Poll for active help requests
func DuressListen(c *fiber.Ctx) error {
    helpLock.RLock()
    defer helpLock.RUnlock()

    for i := len(helpRequests) - 1; i >= 0; i-- {
        if helpRequests[i].Status == "open" {
            return c.JSON(helpRequests[i])
        }
    }
    return c.JSON(nil)
}

// 3. Accept Help Request
func GiveHelp(c *fiber.Ctx) error {
    requester := c.FormValue("name")
    helper := c.FormValue("helper")

    if requester == "" || helper == "" {
        return c.Status(400).JSON(fiber.Map{"error": "Missing parameters"})
    }

    helpLock.Lock()
    helpAcknowledgements[requester] = helper

    // Mark matching help request as "closed"
    for i, req := range helpRequests {
        if req.Name == requester {
            helpRequests[i].Status = "closed"
            break
        }
    }

    helpLock.Unlock()
    return c.JSON(fiber.Map{"status": "success"})
}


// 4. Check helper assignment + session status
func ListenForHelper(c *fiber.Ctx) error {
    requester := c.FormValue("name")

    helpLock.RLock()
    defer helpLock.RUnlock()

    helper, ok := helpAcknowledgements[requester]
    if !ok {
        return c.JSON(nil)
    }

    for _, req := range helpRequests {
        if req.Name == requester {
            return c.JSON(fiber.Map{
                "helper": helper,
                "status": req.Status,
            })
        }
    }

    return c.JSON(fiber.Map{
        "helper": helper,
        "status": "open", // fallback (shouldn’t be used)
    })
}

// 5. Mark Help as Completed (and cleanup)
func HelpCompleted(c *fiber.Ctx) error {
    requester := c.FormValue("name")
    helper := c.FormValue("helper")

    if requester == "" || helper == "" {
        return c.Status(400).JSON(fiber.Map{"error": "Missing parameters"})
    }

    helpLock.Lock()
    delete(helpAcknowledgements, requester)

    for i, req := range helpRequests {
        if req.Name == requester {
            helpRequests[i].Status = "closed"
            break
        }
    }

    helpLock.Unlock()
    return c.JSON(fiber.Map{"status": "success"})
}

// Broadcaster SDP Offer
func BroadcastSDP(c *fiber.Ctx) error {
    var req struct {
        RoomID string `json:"roomId"`
        SDP    string `json:"sdp"`
    }

    if err := c.BodyParser(&req); err != nil {
        return c.Status(400).JSON(fiber.Map{"error": "Invalid request"})
    }

    w.RoomsLock.RLock()
    room, ok := w.Rooms[req.RoomID]
    w.RoomsLock.RUnlock()

    if !ok {
        return c.Status(404).JSON(fiber.Map{"error": "Room not found"})
    }

    room.Peers.Broadcast("offer", req.SDP)
    return c.JSON(fiber.Map{"status": "success"})
}

// Broadcaster ICE Candidate
func BroadcastCandidate(c *fiber.Ctx) error {
    var req struct {
        RoomID   string `json:"roomId"`
        Candidate string `json:"candidate"`
    }

    if err := c.BodyParser(&req); err != nil {
        return c.Status(400).JSON(fiber.Map{"error": "Invalid candidate"})
    }

    w.RoomsLock.RLock()
    room, ok := w.Rooms[req.RoomID]
    w.RoomsLock.RUnlock()

    if !ok {
        return c.Status(404).JSON(fiber.Map{"error": "Room not found"})
    }

    room.Peers.Broadcast("candidate", req.Candidate)
    return c.JSON(fiber.Map{"status": "success"})
}

// Viewer SDP Answer
func ViewerSDP(c *fiber.Ctx) error {
    var req struct {
        RoomID string `json:"roomId"`
        SDP    string `json:"sdp"`
    }

    if err := c.BodyParser(&req); err != nil {
        return c.Status(400).JSON(fiber.Map{"error": "Invalid request"})
    }

    w.RoomsLock.RLock()
    room, ok := w.Rooms[req.RoomID]
    w.RoomsLock.RUnlock()

    if !ok {
        return c.Status(404).JSON(fiber.Map{"error": "Room not found"})
    }

    room.Peers.Broadcast("answer", req.SDP)
    return c.JSON(fiber.Map{"status": "success"})
}

// Viewer ICE Candidate
func ViewerCandidate(c *fiber.Ctx) error {
    var req struct {
        RoomID   string `json:"roomId"`
        Candidate string `json:"candidate"`
    }

    if err := c.BodyParser(&req); err != nil {
        return c.Status(400).JSON(fiber.Map{"error": "Invalid candidate"})
    }

    w.RoomsLock.RLock()
    room, ok := w.Rooms[req.RoomID]
    w.RoomsLock.RUnlock()

    if !ok {
        return c.Status(404).JSON(fiber.Map{"error": "Room not found"})
    }

    room.Peers.Broadcast("candidate", req.Candidate)
    return c.JSON(fiber.Map{"status": "success"})
}

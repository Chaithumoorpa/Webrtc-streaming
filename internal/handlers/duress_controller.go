package handlers

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
)

type HelpRequest struct {
	Name   string `json:"name"`
	Zone   string `json:"zone"`
	Mobile string `json:"mobile"`
	Status string `json:"status"` // open | taken | closed
}

var (
	helpLock             sync.RWMutex
	helpRequests         []HelpRequest
	helpAcknowledgements = make(map[string]string) // requester -> helper
	nameToRoom           = make(map[string]string) // requester -> roomId
)

func StartHelpSession(c *fiber.Ctx) error {
	var req HelpRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid request"})
	}
	if req.Name == "" {
		return c.Status(400).JSON(fiber.Map{"error": "Missing name"})
	}
	if req.Zone == "" {
		req.Zone = "Unknown"
	}
	req.Status = "open"

	helpLock.Lock()
	defer helpLock.Unlock()

	rid := nameToRoom[req.Name]
	if rid == "" {
		rid = fmt.Sprintf("r-%d", time.Now().UnixNano())
		nameToRoom[req.Name] = rid
		helpRequests = append(helpRequests, req)
	} else {
		// refresh existing record
		found := false
		for i := range helpRequests {
			if helpRequests[i].Name == req.Name {
				helpRequests[i].Status = "open"
				helpRequests[i].Zone = req.Zone
				helpRequests[i].Mobile = req.Mobile
				found = true
				break
			}
		}
		if !found {
			helpRequests = append(helpRequests, req)
		}
	}

	scheme := "ws"
	if os.Getenv("ENVIRONMENT") == "PRODUCTION" {
		scheme = "wss"
	}

	return c.JSON(fiber.Map{
		"status":    "success",
		"roomId":    rid,
		"timestamp": time.Now().Unix(),
		"user": fiber.Map{
			"name":   req.Name,
			"zone":   req.Zone,
			"mobile": req.Mobile,
		},
		"broadcasterWs":      fmt.Sprintf("%s://%s/duress/%s/websocket", scheme, c.Hostname(), rid),
		"viewerWebsocketUrl": fmt.Sprintf("%s://%s/duress/%s/viewer/websocket", scheme, c.Hostname(), rid),
	})
}

func DuressListen(c *fiber.Ctx) error {
	helpLock.RLock()
	defer helpLock.RUnlock()

	scheme := "ws"
	if os.Getenv("ENVIRONMENT") == "PRODUCTION" {
		scheme = "wss"
	}
	for i := len(helpRequests) - 1; i >= 0; i-- {
		r := helpRequests[i]
		if r.Status == "open" {
			rid := nameToRoom[r.Name]
			resp := fiber.Map{
				"name":   r.Name,
				"zone":   r.Zone,
				"mobile": r.Mobile,
				"status": r.Status,
			}
			if rid != "" {
				resp["roomId"]             = rid
				resp["viewerWebsocketUrl"] = fmt.Sprintf("%s://%s/duress/%s/viewer/websocket", scheme, c.Hostname(), rid)
				resp["broadcasterWs"]      = fmt.Sprintf("%s://%s/duress/%s/websocket", scheme, c.Hostname(), rid)
			}
			return c.JSON(resp)
		}
	}
	return c.JSON(nil)
}

func GiveHelp(c *fiber.Ctx) error {
	requester := c.FormValue("name")
	helper := c.FormValue("helper")
	if requester == "" || helper == "" {
		return c.Status(400).JSON(fiber.Map{"error": "Missing parameters"})
	}

	helpLock.Lock()
	defer helpLock.Unlock()

	helpAcknowledgements[requester] = helper
	for i := range helpRequests {
		if helpRequests[i].Name == requester {
			helpRequests[i].Status = "taken"
			break
		}
	}
	return c.JSON(fiber.Map{"status": "success"})
}

func ListenForHelper(c *fiber.Ctx) error {
	requester := c.FormValue("name")
	if requester == "" {
		return c.Status(400).JSON(fiber.Map{"error": "Missing requester name"})
	}

	helpLock.RLock()
	defer helpLock.RUnlock()

	helper, ok := helpAcknowledgements[requester]
	if !ok {
		return c.JSON(nil)
	}

	scheme := "ws"
	if os.Getenv("ENVIRONMENT") == "PRODUCTION" {
		scheme = "wss"
	}
	rid := nameToRoom[requester]

	status := "open"
	for _, r := range helpRequests {
		if r.Name == requester && r.Status == "closed" {
			status = "closed"
			break
		}
	}

	return c.JSON(fiber.Map{
		"helper":             helper,
		"status":             status,
		"roomId":             rid,
		"viewerWebsocketUrl": fmt.Sprintf("%s://%s/duress/%s/viewer/websocket", scheme, c.Hostname(), rid),
		"broadcasterWs":      fmt.Sprintf("%s://%s/duress/%s/websocket", scheme, c.Hostname(), rid),
	})
}

func HelpCompleted(c *fiber.Ctx) error {
	requester := c.FormValue("name")
	helper := c.FormValue("helper")
	if requester == "" || helper == "" {
		return c.Status(400).JSON(fiber.Map{"error": "Missing parameters"})
	}

	helpLock.Lock()
	defer helpLock.Unlock()

	delete(helpAcknowledgements, requester)
	for i := range helpRequests {
		if helpRequests[i].Name == requester {
			helpRequests[i].Status = "closed"
			break
		}
	}
	return c.JSON(fiber.Map{"status": "success"})
}

func SessionInfo(c *fiber.Ctx) error {
	name := c.Query("name")
	if name == "" {
		return c.Status(400).JSON(fiber.Map{"error": "Missing name"})
	}

	helpLock.RLock()
	rid, ok := nameToRoom[name]
	helpLock.RUnlock()
	if !ok || rid == "" {
		return c.Status(404).JSON(fiber.Map{"error": "Not found"})
	}

	scheme := "ws"
	if os.Getenv("ENVIRONMENT") == "PRODUCTION" {
		scheme = "wss"
	}

	return c.JSON(fiber.Map{
		"roomId":             rid,
		"viewerWebsocketUrl": fmt.Sprintf("%s://%s/duress/%s/viewer/websocket", scheme, c.Hostname(), rid),
		"broadcasterWs":      fmt.Sprintf("%s://%s/duress/%s/websocket", scheme, c.Hostname(), rid),
	})
}

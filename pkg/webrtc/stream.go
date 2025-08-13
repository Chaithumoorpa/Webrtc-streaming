package webrtc

import (
	"encoding/json"
	"log"
	"os"
	"sync"

	"github.com/gofiber/websocket/v2"
	"github.com/pion/webrtc/v3"
)

// Handles a viewer (helper) WebSocket
func StreamConn(c *websocket.Conn, p *Peers, room *Room) {
	config := webrtc.Configuration{}
	if os.Getenv("ENVIRONMENT") == "PRODUCTION" {
		config = turnConfig
	}

	pc, err := webrtc.NewPeerConnection(config)
	if err != nil {
		log.Println("Viewer PeerConnection creation failed:", err)
		return
	}
	defer pc.Close()

	// We will SEND media to viewer (from TrackLocals), so add transceivers recvonly from viewer POV
	for _, typ := range []webrtc.RTPCodecType{webrtc.RTPCodecTypeVideo, webrtc.RTPCodecTypeAudio} {
		if _, err := pc.AddTransceiverFromKind(typ, webrtc.RTPTransceiverInit{
			Direction: webrtc.RTPTransceiverDirectionRecvonly,
		}); err != nil {
			log.Println("AddTransceiver error:", err)
			return
		}
	}

	newPeer := PeerConnectionState{
		PeerConnection: pc,
		Websocket: &ThreadSafeWriter{
			Conn:  c,
			Mutex: sync.Mutex{},
		},
		Role: "viewer",
	}

	// Register viewer
	p.ListLock.Lock()
	p.Connections = append(p.Connections, newPeer)
	p.ListLock.Unlock()
	log.Printf("New viewer connected. Total peers: %d", len(p.Connections))

	// Send ICE to viewer as JSON
	pc.OnICECandidate(func(i *webrtc.ICECandidate) {
		if i == nil {
			return
		}
		candJSON, err := json.Marshal(i.ToJSON())
		if err != nil {
			log.Println("ICE candidate marshal error:", err)
			return
		}
		if err := newPeer.Websocket.WriteJSON(&websocketMessage{
			Event: "candidate",
			Data:  string(candJSON),
		}); err != nil {
			log.Println("Send candidate error:", err)
		}
	})

	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		log.Printf("Viewer PC state: %s", state.String())
		switch state {
		case webrtc.PeerConnectionStateFailed:
			_ = pc.Close()
		case webrtc.PeerConnectionStateClosed:
			p.SignalPeerConnections(room)
		}
	})

	// Sync tracks and send OFFER to viewer
	p.SignalPeerConnections(room)

	// Handle incoming WS messages
	for {
		_, raw, err := c.ReadMessage()
		if err != nil {
			log.Println("Viewer WS read error:", err)
			return
		}

		var msg websocketMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			log.Println("Viewer WS unmarshal error:", err)
			return
		}

		switch msg.Event {
		case "candidate":
			var candInit webrtc.ICECandidateInit
			if err := json.Unmarshal([]byte(msg.Data), &candInit); err != nil {
				candInit = webrtc.ICECandidateInit{Candidate: msg.Data}
			}
			if err := pc.AddICECandidate(candInit); err != nil {
				log.Println("AddICECandidate error:", err)
				return
			}

		case "answer":
			// Android viewer sends plain SDP
			answer := webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: msg.Data}
			if err := pc.SetRemoteDescription(answer); err != nil {
				log.Println("SetRemoteDescription(answer) error:", err)
				return
			}

		case "duress-stop":
			log.Println("Viewer requested duress termination")
			_ = pc.Close()
			return

		default:
			log.Println("Unknown event from viewer:", msg.Event)
		}
	}
}

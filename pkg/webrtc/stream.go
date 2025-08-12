package webrtc

import (
    "encoding/json"
    "log"
    "os"
    "sync"

    "github.com/gofiber/websocket/v2"
    "github.com/pion/webrtc/v3"
)

// Handles a viewer connection to a WebRTC stream
func StreamConn(c *websocket.Conn, p *Peers, room *Room) {
    config := webrtc.Configuration{}
    if os.Getenv("ENVIRONMENT") == "PRODUCTION" {
        config = turnConfig
    }

    peerConnection, err := webrtc.NewPeerConnection(config)
    if err != nil {
        log.Println("PeerConnection creation failed:", err)
        return
    }
    defer peerConnection.Close()

    // Add transceivers for video and audio
    for _, typ := range []webrtc.RTPCodecType{webrtc.RTPCodecTypeVideo, webrtc.RTPCodecTypeAudio} {
        if _, err := peerConnection.AddTransceiverFromKind(typ, webrtc.RTPTransceiverInit{
            Direction: webrtc.RTPTransceiverDirectionRecvonly,
        }); err != nil {
            log.Println("AddTransceiver error:", err)
            return
        }
    }

    newPeer := PeerConnectionState{
        PeerConnection: peerConnection,
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
    log.Printf("New viewer connected. Total viewers: %d", len(p.Connections))

    // Handle ICE candidates
    peerConnection.OnICECandidate(func(i *webrtc.ICECandidate) {
        if i == nil {
            return
        }

        candidateJSON, err := json.Marshal(i.ToJSON())
        if err != nil {
            log.Println("ICE candidate marshal error:", err)
            return
        }

        if err := newPeer.Websocket.WriteJSON(&websocketMessage{
            Event: "candidate",
            Data:  string(candidateJSON),
        }); err != nil {
            log.Println("Send candidate error:", err)
        }
    })

    // Monitor connection state
    peerConnection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
        log.Printf("Viewer connection state changed: %s", state.String())

        switch state {
        case webrtc.PeerConnectionStateFailed:
            if err := peerConnection.Close(); err != nil {
                log.Println("Failed connection close error:", err)
            }
        case webrtc.PeerConnectionStateClosed:
            p.SignalPeerConnections(room)
        }
    })

    // Sync tracks with viewer
    p.SignalPeerConnections(room)

    // Handle incoming WebSocket messages
    for {
        _, raw, err := c.ReadMessage()
        if err != nil {
            log.Println("WebSocket read error:", err)
            return
        }

        var message websocketMessage
        if err := json.Unmarshal(raw, &message); err != nil {
            log.Println("WebSocket message unmarshal error:", err)
            return
        }

        switch message.Event {
        case "candidate":
            var candidate webrtc.ICECandidateInit
            if err := json.Unmarshal([]byte(message.Data), &candidate); err != nil {
                log.Println("Candidate unmarshal error:", err)
                return
            }
            if err := peerConnection.AddICECandidate(candidate); err != nil {
                log.Println("AddICECandidate error:", err)
                return
            }

        case "answer":
            var answer webrtc.SessionDescription
            if err := json.Unmarshal([]byte(message.Data), &answer); err != nil {
                log.Println("Answer unmarshal error:", err)
                return
            }
            if err := peerConnection.SetRemoteDescription(answer); err != nil {
                log.Println("SetRemoteDescription error:", err)
                return
            }

        case "duress-stop":
            log.Println("Viewer requested duress termination")
            peerConnection.Close()
            return
        }
    }
}


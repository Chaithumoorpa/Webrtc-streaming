package webrtc

import (
    "encoding/json"
    "log"
    "os"
    "sync"

    "github.com/gofiber/websocket/v2"
    "github.com/pion/webrtc/v3"
)

// 🎥 Room represents a WebRTC session
type Room struct {
    Peers     *Peers
    LastOffer string
}

// 🔌 Handles a new WebSocket connection for a room
func RoomConn(c *websocket.Conn, p *Peers, room *Room) {
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
        Role: "broadcaster",
    }

    // Register peer
    p.ListLock.Lock()
    p.Connections = append(p.Connections, newPeer)
    p.ListLock.Unlock()
    log.Println("New peer connected. Total peers:", len(p.Connections))

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

    // Handle connection state changes
    peerConnection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
        log.Printf("PeerConnection state changed: %s", state.String())

        switch state {
        case webrtc.PeerConnectionStateFailed:
            if err := peerConnection.Close(); err != nil {
                log.Println("PeerConnection close error:", err)
            }
        case webrtc.PeerConnectionStateClosed:
            p.SignalPeerConnections(room)
        }
    })

    // Handle incoming media tracks
    peerConnection.OnTrack(func(t *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
        log.Printf("Received remote track: %s (%s)", t.ID(), t.Kind().String())

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

    // Sync peers after setup
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
        }
    }
}

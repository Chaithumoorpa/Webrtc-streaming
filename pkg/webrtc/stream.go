package webrtc

import (
    "encoding/json"
    "log"
    "os"
    "sync"

    "github.com/gofiber/websocket/v2"
    "github.com/pion/webrtc/v3"
)

func StreamConn(c *websocket.Conn, p *Peers) {
    var config webrtc.Configuration
    if os.Getenv("ENVIRONMENT") == "PRODUCTION" {
        config = turnConfig
    }

    peerConnection, err := webrtc.NewPeerConnection(config)
    if err != nil {
        log.Print("Failed to create PeerConnection:", err)
        return
    }
    defer peerConnection.Close()

    // Add transceivers for receiving video and audio
    for _, typ := range []webrtc.RTPCodecType{webrtc.RTPCodecTypeVideo, webrtc.RTPCodecTypeAudio} {
        if _, err := peerConnection.AddTransceiverFromKind(typ, webrtc.RTPTransceiverInit{
            Direction: webrtc.RTPTransceiverDirectionRecvonly,
        }); err != nil {
            log.Print("Failed to add transceiver:", err)
            return
        }
    }

    newPeer := PeerConnectionState{
        PeerConnection: peerConnection,
        Websocket: &ThreadSafeWriter{
            Conn:  c,
            Mutex: sync.Mutex{},
        },
    }

    p.ListLock.Lock()
    p.Connections = append(p.Connections, newPeer)
    p.ListLock.Unlock()

    log.Printf("New viewer connected. Total viewers: %d", len(p.Connections))

    // Handle ICE candidates
    peerConnection.OnICECandidate(func(i *webrtc.ICECandidate) {
        if i == nil {
            return
        }

        candidateString, err := json.Marshal(i.ToJSON())
        if err != nil {
            log.Println("Failed to marshal ICE candidate:", err)
            return
        }

        if writeErr := newPeer.Websocket.WriteJSON(&websocketMessage{
            Event: "candidate",
            Data:  string(candidateString),
        }); writeErr != nil {
            log.Println("Failed to send ICE candidate:", writeErr)
        }
    })

    // Monitor connection state
    peerConnection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
        log.Printf("Viewer connection state: %s", state.String())
        switch state {
        case webrtc.PeerConnectionStateFailed:
            if err := peerConnection.Close(); err != nil {
                log.Print("Error closing failed connection:", err)
            }
        case webrtc.PeerConnectionStateClosed:
            p.SignalPeerConnections()
        }
    })

    // Trigger offer to sync tracks
    p.SignalPeerConnections()

    // Handle incoming signaling messages
    message := &websocketMessage{}
    for {
        _, raw, err := c.ReadMessage()
        if err != nil {
            log.Println("WebSocket read error:", err)
            return
        } else if err := json.Unmarshal(raw, &message); err != nil {
            log.Println("Failed to unmarshal WebSocket message:", err)
            return
        }

        switch message.Event {
        case "candidate":
            candidate := webrtc.ICECandidateInit{}
            if err := json.Unmarshal([]byte(message.Data), &candidate); err != nil {
                log.Println("Failed to parse ICE candidate:", err)
                return
            }
            if err := peerConnection.AddICECandidate(candidate); err != nil {
                log.Println("Failed to add ICE candidate:", err)
                return
            }

        case "answer":
            answer := webrtc.SessionDescription{}
            if err := json.Unmarshal([]byte(message.Data), &answer); err != nil {
                log.Println("Failed to parse SDP answer:", err)
                return
            }
            if err := peerConnection.SetRemoteDescription(answer); err != nil {
                log.Println("Failed to set remote description:", err)
                return
            }

        case "duress-stop":
            log.Println("Viewer requested duress termination")
            peerConnection.Close()
            return

        }
    }
}

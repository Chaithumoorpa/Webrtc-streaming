package webrtc

import (
    "encoding/json"
    "log"
    "os"
    "sync"

    "github.com/gofiber/websocket/v2"
    "github.com/pion/webrtc/v3"
)

type Room struct {
    Peers *Peers
}

func RoomConn(c *websocket.Conn, p *Peers) {
    var config webrtc.Configuration
    if os.Getenv("ENVIRONMENT") == "PRODUCTION" {
        config = turnConfig
    }

    peerConnection, err := webrtc.NewPeerConnection(config)
    if err != nil {
        log.Print(err)
        return
    }
    defer peerConnection.Close()

    for _, typ := range []webrtc.RTPCodecType{webrtc.RTPCodecTypeVideo, webrtc.RTPCodecTypeAudio} {
        if _, err := peerConnection.AddTransceiverFromKind(typ, webrtc.RTPTransceiverInit{
            Direction: webrtc.RTPTransceiverDirectionRecvonly,
        }); err != nil {
            log.Print(err)
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

    log.Println("New peer connected. Total peers:", len(p.Connections))

    peerConnection.OnICECandidate(func(i *webrtc.ICECandidate) {
        if i == nil {
            return
        }

        candidateString, err := json.Marshal(i.ToJSON())
        if err != nil {
            log.Println(err)
            return
        }

        if writeErr := newPeer.Websocket.WriteJSON(&websocketMessage{
            Event: "candidate",
            Data:  string(candidateString),
        }); writeErr != nil {
            log.Println(writeErr)
        }
    })

    peerConnection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
        log.Printf("PeerConnection state changed: %s", state.String())
        switch state {
        case webrtc.PeerConnectionStateFailed:
            if err := peerConnection.Close(); err != nil {
                log.Print(err)
            }
        case webrtc.PeerConnectionStateClosed:
            p.SignalPeerConnections()
        }
    })

    peerConnection.OnTrack(func(t *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
        log.Printf("Received remote track: %s (%s)", t.ID(), t.Kind().String())

        trackLocal := p.AddTrack(t)
        if trackLocal == nil {
            log.Println("Failed to create local track")
            return
        }
        defer p.RemoveTrack(trackLocal)

        buf := make([]byte, 1500)
        for {
            i, _, err := t.Read(buf)
            if err != nil {
                log.Println("Error reading from remote track:", err)
                return
            }
            // log.Printf("Relaying %d bytes from track %s", i, t.ID())

            if _, err = trackLocal.Write(buf[:i]); err != nil {
                log.Println("Error writing to local track:", err)
                return
            }
        }
    })


    p.SignalPeerConnections()

    message := &websocketMessage{}
    for {
        _, raw, err := c.ReadMessage()
        if err != nil {
            log.Println(err)
            return
        } else if err := json.Unmarshal(raw, &message); err != nil {
            log.Println(err)
            return
        }

        switch message.Event {
        case "candidate":
            candidate := webrtc.ICECandidateInit{}
            if err := json.Unmarshal([]byte(message.Data), &candidate); err != nil {
                log.Println(err)
                return
            }
            if err := peerConnection.AddICECandidate(candidate); err != nil {
                log.Println(err)
                return
            }

        case "answer":
            answer := webrtc.SessionDescription{}
            if err := json.Unmarshal([]byte(message.Data), &answer); err != nil {
                log.Println(err)
                return
            }
            if err := peerConnection.SetRemoteDescription(answer); err != nil {
                log.Println(err)
                return
            }
        }
    }
}

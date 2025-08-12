package webrtc

import (
    "encoding/json"
    "log"
    "sync"
    "time"

    "github.com/gofiber/websocket/v2"
    "github.com/pion/rtcp"
    "github.com/pion/webrtc/v3"
)

// Global room registry
var (
    RoomsLock    sync.RWMutex
    Rooms        map[string]*Room
    Streams      map[string]*Room
    ActiveRoomID string
)

// TURN/STUN configuration
var turnConfig = webrtc.Configuration{
    ICETransportPolicy: webrtc.ICETransportPolicyRelay,
    ICEServers: []webrtc.ICEServer{
        {URLs: []string{"stun:turn.localhost:3478"}},
        {
            URLs:           []string{"turn:turn.localhost:3478"},
            Username:       "akhil",
            Credential:     "sharma",
            CredentialType: webrtc.ICECredentialTypePassword,
        },
    },
}

// Peer management
type Peers struct {
    Room        *Room
    ListLock    sync.RWMutex
    Connections []PeerConnectionState
    TrackLocals map[string]*webrtc.TrackLocalStaticRTP
}

type PeerConnectionState struct {
    PeerConnection *webrtc.PeerConnection
    Websocket      *ThreadSafeWriter
    Role           string
}

type ThreadSafeWriter struct {
    Conn  *websocket.Conn
    Mutex sync.Mutex
}

func (t *ThreadSafeWriter) WriteJSON(v interface{}) error {
    t.Mutex.Lock()
    defer t.Mutex.Unlock()
    return t.Conn.WriteJSON(v)
}

// Add remote track and trigger sync
func (p *Peers) AddTrack(t *webrtc.TrackRemote, room *Room) *webrtc.TrackLocalStaticRTP {
    p.ListLock.Lock()
    defer func() {
        p.ListLock.Unlock()
        p.SignalPeerConnections(room)
    }()

    trackLocal, err := webrtc.NewTrackLocalStaticRTP(t.Codec().RTPCodecCapability, t.ID(), t.StreamID())
    if err != nil {
        log.Println("AddTrack error:", err)
        return nil
    }

    p.TrackLocals[t.ID()] = trackLocal
    return trackLocal
}

// Remove track and trigger sync
func (p *Peers) RemoveTrack(t *webrtc.TrackLocalStaticRTP, room *Room) {
    p.ListLock.Lock()
    defer func() {
        p.ListLock.Unlock()
        p.SignalPeerConnections(room)
    }()

    delete(p.TrackLocals, t.ID())
}

// Sync peer connections with current tracks
func (p *Peers) SignalPeerConnections(room *Room) {
    p.ListLock.Lock()
    defer func() {
        p.ListLock.Unlock()
        p.DispatchKeyFrame()
    }()

    attemptSync := func() bool {
        for i := range p.Connections {
            pc := p.Connections[i].PeerConnection

            if pc.ConnectionState() == webrtc.PeerConnectionStateClosed {
                p.Connections = append(p.Connections[:i], p.Connections[i+1:]...)
                log.Println("Removed closed connection")
                return true
            }

            existingSenders := map[string]bool{}
            for _, sender := range pc.GetSenders() {
                if sender.Track() != nil {
                    existingSenders[sender.Track().ID()] = true
                    if _, ok := p.TrackLocals[sender.Track().ID()]; !ok {
                        if err := pc.RemoveTrack(sender); err != nil {
                            log.Println("RemoveTrack error:", err)
                            return true
                        }
                    }
                }
            }

            for _, receiver := range pc.GetReceivers() {
                if receiver.Track() != nil {
                    existingSenders[receiver.Track().ID()] = true
                }
            }

            for trackID := range p.TrackLocals {
                if !existingSenders[trackID] {
                    if _, err := pc.AddTrack(p.TrackLocals[trackID]); err != nil {
                        log.Println("AddTrack error:", err)
                        return true
                    }
                }
            }

            offer, err := pc.CreateOffer(nil)
            if err != nil {
                log.Println("CreateOffer error:", err)
                return true
            }

            if err = pc.SetLocalDescription(offer); err != nil {
                log.Println("SetLocalDescription error:", err)
                return true
            }

            offerString, err := json.Marshal(offer)
            if err != nil {
                log.Println("Marshal offer error:", err)
                return true
            }

            room.LastOffer = string(offerString)
            p.BroadcastTo("viewer", "offer", room.LastOffer)

            if err = p.Connections[i].Websocket.WriteJSON(&websocketMessage{
                Event: "offer",
                Data:  room.LastOffer,
            }); err != nil {
                log.Println("Send offer error:", err)
                return true
            }
        }
        return false
    }

    for attempt := 0; attempt < 25; attempt++ {
        if !attemptSync() {
            break
        }
    }

    go func() {
        time.Sleep(3 * time.Second)
        p.SignalPeerConnections(room)
    }()
}

// Request keyframes from all receivers
func (p *Peers) DispatchKeyFrame() {
    p.ListLock.Lock()
    defer p.ListLock.Unlock()

    for _, conn := range p.Connections {
        for _, receiver := range conn.PeerConnection.GetReceivers() {
            if receiver.Track() != nil {
                _ = conn.PeerConnection.WriteRTCP([]rtcp.Packet{
                    &rtcp.PictureLossIndication{
                        MediaSSRC: uint32(receiver.Track().SSRC()),
                    },
                })
            }
        }
    }
}

//  Broadcast to all peers
func (p *Peers) Broadcast(event, data string) {
    p.ListLock.RLock()
    defer p.ListLock.RUnlock()

    for _, conn := range p.Connections {
        _ = conn.Websocket.WriteJSON(&websocketMessage{
            Event: event,
            Data:  data,
        })
    }
}

// Broadcast to peers by role
func (p *Peers) BroadcastTo(role, event, data string) {
    p.ListLock.RLock()
    defer p.ListLock.RUnlock()

    for _, conn := range p.Connections {
        if conn.Role == role {
            _ = conn.Websocket.WriteJSON(&websocketMessage{
                Event: event,
                Data:  data,
            })
        }
    }
}

// Remove closed connections
func (p *Peers) CleanupClosedConnections() {
    p.ListLock.Lock()
    defer p.ListLock.Unlock()

    active := []PeerConnectionState{}
    for _, conn := range p.Connections {
        if conn.PeerConnection.ConnectionState() != webrtc.PeerConnectionStateClosed {
            active = append(active, conn)
        }
    }
    p.Connections = active
}

// Get connection stats
func (p *Peers) GetConnectionStats() map[string]interface{} {
    p.ListLock.RLock()
    defer p.ListLock.RUnlock()

    return map[string]interface{}{
        "totalConnections": len(p.Connections),
        "activeTracks":     len(p.TrackLocals),
    }
}

// WebSocket message format
type websocketMessage struct {
    Event string `json:"event"`
    Data  string `json:"data"`
}

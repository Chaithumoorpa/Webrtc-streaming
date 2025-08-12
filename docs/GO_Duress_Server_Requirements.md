# Go Duress Server — Requirements Specification

This document summarizes the current server behavior found in the Fiber + Pion codebase. Place it in `/docs` to guide future development.

---

## 1. Functional Scope
- **Start Help Session**: Creates a room (`uuid`), derives a stream ID, registers an open help request, and returns WS URLs for broadcaster/viewer
- **Request Tracking**: Help requests and helper acknowledgments kept in memory with thread-safe access
- **Signaling Fan-out**: WebRTC offers/answers/ICE are broadcast to all peers via WebSocket
- **Media Relay**: Broadcaster tracks added to viewers; periodic PLIs request keyframes for resiliency

## 2. In-memory Data Model
- `HelpRequest { name, zone, mobile, status }`
- Global registries (mutex-guarded): `helpRequests`, `helpAcknowledgements`, `Rooms`, `Streams`, `ActiveRoomID`
- Each `Room` owns `Peers` with connections and track locals

## 3. HTTP API (Fiber)
All routes prefixed with `/duress` and CORS enabled for `*`
1. `POST /duress/help` – start help session and return room/stream IDs, WS URLs
2. `GET /duress/listen` – poll for latest open request
3. `POST /duress/give_help` – assign helper; closes request
4. `POST /duress/listen_for_helper` – victim polls for helper assignment/status
5. `POST /duress/help_completed` – mark help session done and cleanup mappings
6. Optional stream meta endpoints (legacy tools) under `/room` and `/stream` paths

## 4. WebSocket Signaling & Media
- **Broadcaster WS**: `/duress/:roomId/websocket` registers peer and broadcasts `duress-alert`
- **Viewer WS**: `/duress/:roomId/viewer/websocket` registers viewer and mirrors tracks
- **Message Format**: `{ "event": "offer|answer|candidate|duress-alert|duress-stop", "data": "<string>" }`
- Server auto-generates offers to viewers when tracks change and sends them over WS

## 5. Room/Peer Lifecycle & Concurrency
- Rooms created on help start; legacy `/room/:uuid` helpers supported
- `Peers.SignalPeerConnections` prunes closed connections, adds missing tracks, renegotiates, and emits offers with retry/backoff
- `Peers.DispatchKeyFrame` sends PLIs for each receiver track
- Global registries protected by `RoomsLock` / peer locks; WS writes via `ThreadSafeWriter`

## 6. TURN/STUN & RTC Configuration
- Production uses ICE relay only with STUN/TURN at `turn.localhost:3478`, credentials hardcoded in config
- Standalone TURN server binary provided in `tools/turn` (see `turn.go`)

## 7. Server Bootstrap & Middleware
- Defaults to `:8080` when `$PORT` empty; TLS optional via `--cert`/`--key` flags
- Logging and permissive CORS enabled
- Room registries initialized on startup; keyframe dispatcher goroutine launched

## 8. Validation & Error Handling
- `POST /duress/help` returns `400` on invalid body; other POST routes validate required fields and return `400` when missing
- `404` for missing rooms/streams on signaling/stream routes

## 9. Non-functional Requirements
- **Throughput**: Multiple viewers per room with renegotiated offers for each connection
- **Latency**: PLI dispatch every 3s plus during reconf keeps streams recoverable
- **Security**: ENV toggles WS/WSS; TURN config for production
- **Observability**: Logs connection state changes and viewer counts (ticker writers)

## 10. Contract Tests (Happy Path)
1. `POST /duress/help` → 200 + room/stream IDs, WS URLs
2. `GET /duress/listen` → latest open request or `null`
3. `POST /duress/give_help` → 200 `"success"`
4. Victim `POST /duress/listen_for_helper` → `{helper,status}`
5. Broadcaster and viewer WS flow: offers → answers/candidates → playback
6. `POST /duress/help_completed` closes session; polling shows none

---

### Known Issue
Deriving `streamId` uses `fmt.Sprintf("%x", roomID)` instead of SHA-256; requirement specifies SHA-256 hex digest

:::task-stub{title="Use SHA-256 to derive streamId"}
1. In `internal/handlers/duress_controller.go`, import `crypto/sha256`.
2. Replace the current `suuid` assignment with:
   ```go
   h := sha256.Sum256([]byte(roomID))
   suuid := hex.EncodeToString(h[:])
   ```
3. Ensure `encoding/hex` is imported for hex encoding.
4. Rebuild and verify returned `streamId` matches new hash.
:::

---

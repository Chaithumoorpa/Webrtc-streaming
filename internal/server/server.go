package server

import (
    "flag"
    "os"
    "time"

    "github.com/gofiber/fiber/v2"
    "github.com/gofiber/fiber/v2/middleware/cors"
    "github.com/gofiber/fiber/v2/middleware/logger"
    "github.com/gofiber/websocket/v2"

    "webrtc-streaming/internal/handlers"
    w "webrtc-streaming/pkg/webrtc"
)

var (
    addr = flag.String("addr", ":"+os.Getenv("PORT"), "")
    cert = flag.String("cert", "", "")
    key  = flag.String("key", "", "")
)

func Run() error {
    flag.Parse()

    if *addr == ":" {
        *addr = ":8080"
    }

    app := fiber.New()
    app.Use(logger.New())

    app.Use(cors.New(cors.Config{
        AllowOrigins: "*",
        AllowHeaders: "Origin, Content-Type, Accept",
        AllowMethods: "GET,POST,OPTIONS",
        AllowCredentials: true,
        ExposeHeaders: "Content-Length",
    }))

    // Duress Help Flow
    app.Post("/duress/help", handlers.StartHelpSession)             // Starts help + stream
    app.Get("/duress/listen", handlers.DuressListen)                // Polls for help requests
    app.Post("/duress/give_help", handlers.GiveHelp)                // Acknowledge help
    app.Post("/duress/listen_for_helper", handlers.ListenForHelper) // Get assigned helper
    app.Post("/duress/help_completed", handlers.HelpCompleted)      // Mark help as done
    app.Get("/duress/session_info", handlers.SessionInfo)


    // WebRTC Signaling APIs (for Kotlin clients)
    app.Post("/duress/broadcast/sdp", handlers.BroadcastSDP)
    app.Post("/duress/broadcast/candidate", handlers.BroadcastCandidate)
    app.Post("/duress/viewer/sdp", handlers.ViewerSDP)
    app.Post("/duress/viewer/candidate", handlers.ViewerCandidate)

    // WebSocket Media Channels
    app.Get("/duress/:roomId/websocket", websocket.New(handlers.DuressWebSocket))
    app.Get("/duress/:roomId/viewer/websocket", websocket.New(handlers.DuressViewerWebSocket))

    // Room & Stream Setup (Optional legacy endpoints for internal tools)
    app.Get("/room/:uuid/websocket", websocket.New(handlers.RoomWebsocket))
    app.Get("/room/:uuid/viewer/websocket", websocket.New(handlers.RoomViewerWebsocket))
    app.Get("/stream/:suuid/websocket", websocket.New(handlers.StreamWebSocket))
    app.Get("/stream/:suuid/viewer/websocket", websocket.New(handlers.StreamViewerWebSocket))

    // Initialize registry
    w.Rooms = make(map[string]*w.Room)
    w.Streams = make(map[string]*w.Room)

    go dispatchKeyFrames()

    if *cert != "" {
        return app.ListenTLS(*addr, *cert, *key)
    }
    return app.Listen(*addr)
}

func dispatchKeyFrames() {
    for range time.NewTicker(3 * time.Second).C {
        for _, room := range w.Rooms {
            room.Peers.DispatchKeyFrame()
        }
    }
}

package server

import (
	"log"
	"os"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/websocket/v2"

	"webrtc-streaming/internal/handlers"
)

func Run() {
	app := fiber.New()

	app.Use(logger.New())
	app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
		AllowHeaders: "*",
		AllowMethods: "GET,POST,OPTIONS",
	}))

	// health
	app.Get("/", func(c *fiber.Ctx) error { return c.SendString("OK") })

	// HTTP API
	api := app.Group("/duress")
	api.Post("/help", handlers.StartHelpSession)
	api.Get("/listen", handlers.DuressListen)
	api.Post("/give_help", handlers.GiveHelp)
	api.Post("/listen_for_helper", handlers.ListenForHelper)
	api.Post("/help_completed", handlers.HelpCompleted)
	api.Get("/session_info", handlers.SessionInfo)

	// WS upgrade guard
	app.Use("/duress/:roomId/*", func(c *fiber.Ctx) error {
		if websocket.IsWebSocketUpgrade(c) {
			return c.Next()
		}
		return fiber.ErrUpgradeRequired
	})

	// WS endpoints
	app.Get("/duress/:roomId/websocket", websocket.New(handlers.DuressWebSocket))
	app.Get("/duress/:roomId/viewer/websocket", websocket.New(handlers.DuressViewerWebSocket))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Listening on :%s\n", port)
	if err := app.Listen(":" + port); err != nil {
		log.Fatal(err)
	}
}

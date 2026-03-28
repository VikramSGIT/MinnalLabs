package main

import (
	"log"

	"github.com/gin-gonic/gin"
	"github.com/iot-backend/internal/config"
	"github.com/iot-backend/internal/db"
	"github.com/iot-backend/internal/enrollment"
	"github.com/iot-backend/internal/google"
	"github.com/iot-backend/internal/mqtt"
	"github.com/iot-backend/internal/oauth"
	"github.com/iot-backend/internal/state"
)

func main() {
	cfg := config.LoadConfig()

	db.InitDB(cfg)
	db.RunMigrations()

	state.InitState(cfg, db.DB)
	state.SyncProductCaps()
	state.StartSync()

	mqtt.InitMQTT(cfg)

	oauth.InitOAuth()

	r := gin.Default()

	oauth.SetupOAuthRoutes(r)
	google.SetupGoogleRoutes(r)
	enrollment.SetupEnrollmentRoutes(r, cfg)

	serverAddr := cfg.Server.Host + ":" + cfg.Server.Port
	log.Printf("Starting HTTP server on %s", serverAddr)
	if err := r.Run(serverAddr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

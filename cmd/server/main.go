package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/iot-backend/internal/admin"
	"github.com/iot-backend/internal/config"
	"github.com/iot-backend/internal/db"
	"github.com/iot-backend/internal/enrollment"
	"github.com/iot-backend/internal/google"
	"github.com/iot-backend/internal/middleware"
	"github.com/iot-backend/internal/mqtt"
	"github.com/iot-backend/internal/oauth"
	"github.com/iot-backend/internal/ota"
	"github.com/iot-backend/internal/state"
)

func main() {
	cfg := config.LoadConfig()

	db.InitDB(cfg)
	db.RunMigrations()

	state.InitState(cfg, db.DB)
	state.SyncProductCaps()
	state.SyncDevices()
	state.StartSync()

	mqtt.InitMQTT(cfg)
	ota.StartWorker(cfg)

	oauth.InitOAuth(cfg)

	r := gin.Default()
	r.MaxMultipartMemory = 10 << 20
	r.Use(middleware.SecurityHeaders())
	r.Use(middleware.LimitRequestBody(1 << 20))
	r.Use(cors.New(cors.Config{
		AllowOrigins:     cfg.FrontendAllowedOrigins(),
		AllowMethods:     []string{"GET", "POST", "DELETE", "PUT", "PATCH", "OPTIONS"},
		AllowHeaders:     []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	oauth.SetupOAuthRoutes(r, cfg)
	google.SetupGoogleRoutes(r)
	enrollment.SetupEnrollmentRoutes(r, cfg)
	admin.SetupAdminRoutes(r, cfg)

	serverAddr := cfg.Server.Host + ":" + cfg.Server.Port
	srv := &http.Server{
		Addr:    serverAddr,
		Handler: r,
	}

	go func() {
		log.Printf("Starting HTTP server on %s", serverAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("HTTP server forced to shutdown: %v", err)
	}

	mqtt.Client.Disconnect(250)

	sqlDB, err := db.DB.DB()
	if err == nil {
		sqlDB.Close()
	}

	log.Println("Server exited cleanly")
}

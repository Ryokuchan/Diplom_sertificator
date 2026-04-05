package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"diasoft-diploma-api/internal/api"
	"diasoft-diploma-api/internal/api/handlers"
	"diasoft-diploma-api/internal/config"
	"diasoft-diploma-api/internal/database"
	"diasoft-diploma-api/internal/kafka"
	"diasoft-diploma-api/internal/logger"
	"diasoft-diploma-api/internal/redis"
	"diasoft-diploma-api/internal/worker"
)

func main() {
	log := logger.New()
	ctx := context.Background()

	cfg := config.Load()

	if cfg.JWTSecret == "" {
		log.Fatal("JWT_SECRET is required")
	}

	db, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal("Failed to connect to database", "error", err)
	}
	defer db.Close()

	if cfg.AdminEmail != "" && cfg.AdminPassword != "" {
		if err := database.EnsureAdmin(ctx, db, cfg.AdminEmail, cfg.AdminPassword, log); err != nil {
			log.Error("EnsureAdmin failed", "error", err)
		}
	}

	rdb := redis.Connect(cfg.RedisURL)
	defer rdb.Close()

	kafkaProducer := kafka.NewProducer(cfg.KafkaBrokers, log)
	defer kafkaProducer.Close()

	w := worker.NewWorker(db, kafkaProducer, log)
	w.OnJobDone = func(userID int64, jobID string, status string) {
		handlers.GlobalHub.SendToUser(userID, handlers.NotifyMessage{
			Type:    "job_done",
			Payload: map[string]string{"job_id": jobID, "status": status},
		})
	}
	w.Start(ctx)

	kafkaConsumer := kafka.NewConsumer(cfg.KafkaBrokers, cfg.KafkaGroup, log)
	kafkaConsumer.SetEnqueuer(w)
	go kafkaConsumer.Start(ctx, db)

	server := api.NewServer(cfg, db, rdb, kafkaProducer, w, log)

	go func() {
		log.Info("Starting server", "address", cfg.ServerAddress)
		if err := server.Run(cfg.ServerAddress); err != nil {
			log.Fatal("Server error", "error", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("Shutting down...")
	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutCtx); err != nil {
		log.Error("Shutdown error", "error", err)
	}
}

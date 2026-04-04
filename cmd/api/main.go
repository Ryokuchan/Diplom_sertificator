package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"diasoft-diploma-api/internal/api"
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

	db, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal("Failed to connect to database", "error", err)
	}
	defer db.Close()

	if err := database.EnsureAdmin(ctx, db, cfg.AdminEmail, cfg.AdminPassword, log); err != nil {
		log.Error("EnsureAdmin failed", "error", err)
	}

	rdb := redis.Connect(cfg.RedisURL)
	defer rdb.Close()

	kafkaProducer := kafka.NewProducer(cfg.KafkaBrokers, log)
	defer kafkaProducer.Close()

	// Start worker for file processing
	worker := worker.NewWorker(db, kafkaProducer, log)
	worker.Start(ctx)

	kafkaConsumer := kafka.NewConsumer(cfg.KafkaBrokers, cfg.KafkaGroup, log)
	kafkaConsumer.SetEnqueuer(worker)
	go kafkaConsumer.Start(ctx, db)

	server := api.NewServer(cfg, db, rdb, kafkaProducer, worker, log)

	go func() {
		log.Info("Starting server", "address", cfg.ServerAddress)
		if err := server.Run(cfg.ServerAddress); err != nil {
			log.Fatal("Failed to start server", "error", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("Shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Error("Server forced to shutdown", "error", err)
	}

	log.Info("Server exited")
}

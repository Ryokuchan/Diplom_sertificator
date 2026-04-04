package kafka

import (
	"context"
	"encoding/json"

	"github.com/segmentio/kafka-go"

	"diasoft-diploma-api/internal/database"
	"diasoft-diploma-api/internal/logger"
	"diasoft-diploma-api/internal/models"
)

// JobEnqueuer is an interface to avoid a direct import of the worker package.
type JobEnqueuer interface {
	EnqueueJob(id string, filePath string, userID int64)
}

type Consumer struct {
	reader  *kafka.Reader
	log     *logger.Logger
	enqueue JobEnqueuer
}

func NewConsumer(brokers []string, group string, log *logger.Logger) *Consumer {
	return &Consumer{
		reader: kafka.NewReader(kafka.ReaderConfig{
			Brokers:        brokers,
			GroupID:        group,
			Topic:          "diploma-events",
			MinBytes:       10e3,
			MaxBytes:       10e6,
			CommitInterval: 1000,
		}),
		log: log,
	}
}

func (c *Consumer) SetEnqueuer(e JobEnqueuer) {
	c.enqueue = e
}

func (c *Consumer) Start(ctx context.Context, db *database.DB) {
	for {
		msg, err := c.reader.ReadMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				c.log.Info("Consumer shutting down")
				return
			}
			c.log.Error("Failed to read message", "error", err)
			continue
		}

		var event models.DiplomaEvent
		if err := json.Unmarshal(msg.Value, &event); err != nil {
			c.log.Error("Failed to unmarshal event", "error", err)
			continue
		}

		c.handleEvent(ctx, db, &event)
	}
}

func (c *Consumer) handleEvent(ctx context.Context, db *database.DB, event *models.DiplomaEvent) {
	switch event.Type {
	case "diploma.created":
		c.log.Info("Diploma created", "diploma_id", event.DiplomaID)
	case "diploma.verified":
		c.log.Info("Diploma verified", "diploma_id", event.DiplomaID)
	case "diploma.revoked":
		// Аннулирование диплома — обновляем статус в БД
		_, err := db.Exec(ctx,
			`UPDATE diplomas SET status = 'revoked', updated_at = NOW() WHERE id = $1`,
			event.DiplomaID,
		)
		if err != nil {
			c.log.Error("Failed to revoke diploma", "diploma_id", event.DiplomaID, "error", err)
		} else {
			c.log.Info("Diploma revoked", "diploma_id", event.DiplomaID)
		}
	case "file.uploaded":
		if c.enqueue == nil {
			c.log.Error("No enqueuer set, dropping file.uploaded event")
			return
		}
		jobID, ok1 := event.Data["job_id"].(string)
		path, ok2 := event.Data["path"].(string)
		if !ok1 || !ok2 || jobID == "" || path == "" {
			c.log.Error("Invalid file.uploaded event payload", "data", event.Data)
			return
		}
		c.enqueue.EnqueueJob(jobID, path, event.UserID)
	}
}

func (c *Consumer) Close() error {
	return c.reader.Close()
}

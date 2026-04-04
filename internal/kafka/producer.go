package kafka

import (
	"context"
	"encoding/json"
	"github.com/segmentio/kafka-go"
	"diasoft-diploma-api/internal/logger"
)

type Producer struct {
	writer *kafka.Writer
	log    *logger.Logger
}

func NewProducer(brokers []string, log *logger.Logger) *Producer {
	return &Producer{
		writer: &kafka.Writer{
			Addr:         kafka.TCP(brokers...),
			Balancer:     &kafka.LeastBytes{},
			RequiredAcks: kafka.RequireOne,
			Async:        true,
			Compression:  kafka.Snappy,
		},
		log: log,
	}
}

func (p *Producer) PublishEvent(ctx context.Context, topic string, key string, event interface{}) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}

	msg := kafka.Message{
		Topic: topic,
		Key:   []byte(key),
		Value: data,
	}

	return p.writer.WriteMessages(ctx, msg)
}

func (p *Producer) Close() error {
	return p.writer.Close()
}

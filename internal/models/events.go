package models

// DiplomaEvent represents a Kafka event related to diploma operations.
type DiplomaEvent struct {
	Type      string                 `json:"type"`
	DiplomaID int64                  `json:"diploma_id"`
	UserID    int64                  `json:"user_id"`
	Data      map[string]interface{} `json:"data"`
}
